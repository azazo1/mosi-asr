package mossapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Options 是客户端构造参数.
type Options struct {
	BaseURL    string
	APIKey     string
	Timeout    time.Duration
	MaxRetries int
	Logger     *slog.Logger
	UserAgent  string
}

// Client 是 Moss API 客户端.
type Client struct {
	baseURL    string
	apiKey     string
	http       *http.Client
	maxRetries int
	log        *slog.Logger
	userAgent  string
}

// 默认 UserAgent, 便于服务端区分调用来源.
const defaultUserAgent = "mosi-asr/1.0 (+https://github.com/azazo1/mosi-asr)"

// New 创建客户端.
func New(opts Options) *Client {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 5 * time.Second,
		// 大文件上传不设置总时长, 只靠 ctx 与 ResponseHeaderTimeout 兜底.
		ResponseHeaderTimeout: timeout,
	}
	return &Client{
		baseURL:    strings.TrimRight(opts.BaseURL, "/"),
		apiKey:     opts.APIKey,
		http:       &http.Client{Transport: transport},
		maxRetries: max(0, opts.MaxRetries),
		log:        log,
		userAgent:  ua,
	}
}

// SetLogger 替换日志器.
func (c *Client) SetLogger(log *slog.Logger) {
	if log != nil {
		c.log = log
	}
}

// endpoint 拼接完整地址.
func (c *Client) endpoint(path string) string {
	if c.baseURL == "" {
		c.baseURL = "https://api.mosi.cn"
	}
	return c.baseURL + path
}

// requestBuilder 每次调用都会重建请求, 保证重试时可以重新打开文件体.
type requestBuilder func(ctx context.Context) (*http.Request, error)

// do 发送请求, 对网络错误与可重试状态码做指数退避重试.
//
// stream 为 true 时不重试, 因为响应体已经进入流式读取阶段.
func (c *Client) do(ctx context.Context, op string, build requestBuilder, stream bool) (*http.Response, error) {
	attempts := c.maxRetries + 1
	if stream {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			delay := backoffDelay(attempt)
			c.log.Debug("重试请求", "op", op, "attempt", attempt+1, "delay", delay.String(), "err", lastErr)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := build(ctx)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("User-Agent", c.userAgent)
		if req.Header.Get("Accept") == "" {
			req.Header.Set("Accept", "application/json")
		}

		start := time.Now()
		resp, err := c.http.Do(req)
		if err != nil {
			// 请求体构建过程中产生的错误 (例如文件读取失败) 不应重试.
			if errors.Is(err, errRequestBody) {
				return nil, err
			}
			c.log.Warn("请求失败", "op", op, "attempt", attempt+1, "elapsed", time.Since(start).String(), "err", err)
			lastErr = fmt.Errorf("请求 %s 失败: %w", op, err)
			if ctx.Err() != nil {
				return nil, lastErr
			}
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			c.log.Debug("请求成功", "op", op, "status", resp.StatusCode, "elapsed", time.Since(start).String())
			return resp, nil
		}

		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if rerr != nil {
			lastErr = fmt.Errorf("读取 %s 的响应失败: %w", op, rerr)
			continue
		}
		apiErr := parseAPIError(resp.StatusCode, body, resp.Header)
		lastErr = apiErr
		c.log.Warn("接口返回错误", "op", op, "status", resp.StatusCode, "code", apiErr.Code,
			"elapsed", time.Since(start).String())

		if !IsRetryable(apiErr) {
			return nil, apiErr
		}
		if delay, ok := retryAfter(resp.Header); ok {
			c.log.Debug("遵循服务端建议的重试间隔", "after", delay.String())
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("请求失败")
	}
	return nil, lastErr
}

// errRequestBody 标记构造请求体时发生的错误, 这类错误重试也没有意义.
var errRequestBody = errors.New("构造请求体失败")

// backoffDelay 计算第 attempt 次重试前的等待时长, 带抖动避免请求同时涌向服务端.
func backoffDelay(attempt int) time.Duration {
	base := 500 * time.Millisecond
	d := base << (attempt - 1)
	if d > 8*time.Second {
		d = 8 * time.Second
	}
	jitter := time.Duration(rand.Int63n(int64(d/2 + 1)))
	return d/2 + jitter
}

// retryAfter 解析 Retry-After 响应头, 支持秒数与 HTTP 日期两种写法.
func retryAfter(h http.Header) (time.Duration, bool) {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d, true
		}
	}
	return 0, false
}

// readJSON 解析成功响应体, 同时保留原始字节.
func readJSON(resp *http.Response, out any) ([]byte, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return body, nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return body, fmt.Errorf("解析响应 JSON 失败: %w, 原始内容: %s", err, truncate(string(body), 300))
	}
	return body, nil
}
