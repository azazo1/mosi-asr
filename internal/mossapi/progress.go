package mossapi

import (
	"io"
	"time"
)

// progressInterval 是上传进度的最小回调间隔, 避免高频刷屏.
const progressInterval = 2 * time.Second

// progressReader 包装请求体, 周期性汇报已发送字节数.
type progressReader struct {
	r          io.Reader
	total      int64
	sent       int64
	lastReport time.Time
	callback   func(sent, total int64)
}

// newProgressReader 构造带进度回调的读取器.
func newProgressReader(r io.Reader, total int64, callback func(sent, total int64)) io.ReadCloser {
	return &progressReader{
		r:          r,
		total:      total,
		lastReport: time.Now(),
		callback:   callback,
	}
}

// Read 实现 io.Reader, 顺带触发进度回调.
func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.sent += int64(n)
		if p.callback != nil && time.Since(p.lastReport) >= progressInterval {
			p.lastReport = time.Now()
			p.callback(p.sent, p.total)
		}
	}
	return n, err
}

// Close 关闭底层读取器.
func (p *progressReader) Close() error {
	if c, ok := p.r.(io.Closer); ok {
		return c.Close()
	}
	return nil
}
