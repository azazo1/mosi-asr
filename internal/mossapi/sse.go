package mossapi

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// sseReader 逐条读取服务端事件流.
//
// 服务端使用标准 SSE 格式: 每条事件由若干 "data:" 行组成, 以空行结束.
// 这里只关心 data 字段, 因为事件类型已经写在 data 的 JSON 里.
type sseReader struct {
	r *bufio.Reader
}

func newSSEReader(r io.Reader) *sseReader {
	return &sseReader{r: bufio.NewReaderSize(r, 64<<10)}
}

// next 读取下一条事件, 返回 io.EOF 表示流正常结束.
// 心跳与注释行会被跳过, 返回 nil, nil 表示本次没有可处理的事件.
func (s *sseReader) next() (*StreamEvent, error) {
	var data []string

	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				if strings.TrimSpace(line) != "" {
					if field, value, ok := splitSSEField(line); ok && field == "data" {
						data = append(data, value)
					}
				}
				if len(data) > 0 {
					return s.decode(data)
				}
				return nil, io.EOF
			}
			return nil, err
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if len(data) == 0 {
				continue
			}
			return s.decode(data)
		}
		field, value, ok := splitSSEField(line)
		if !ok || field != "data" {
			continue
		}
		data = append(data, value)
	}
}

// decode 把 data 行合并成事件对象.
func (s *sseReader) decode(data []string) (*StreamEvent, error) {
	payload := strings.TrimSpace(strings.Join(data, "\n"))
	if payload == "" {
		return nil, nil
	}
	// 兼容 OpenAI 风格的结束标记.
	if payload == "[DONE]" {
		return nil, io.EOF
	}

	ev := &StreamEvent{}
	if err := json.Unmarshal([]byte(payload), ev); err != nil {
		return nil, errors.New("解析流式事件失败: " + err.Error() + ", 原始内容: " + truncate(payload, 300))
	}
	ev.Raw = payload
	return ev, nil
}

// splitSSEField 拆分 "field: value" 行, 去掉冒号后的单个空格.
func splitSSEField(line string) (field, value string, ok bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		// 只有字段名没有值的行, 按空值处理.
		return strings.TrimSpace(line), "", line != ""
	}
	field = strings.TrimSpace(line[:idx])
	value = strings.TrimPrefix(line[idx+1:], " ")
	return field, value, true
}
