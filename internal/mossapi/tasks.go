package mossapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// GetTask 查询异步任务状态与结果.
func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error) {
	if taskID == "" {
		return nil, fmt.Errorf("task_id 不能为空")
	}
	resp, err := c.do(ctx, "查询任务", func(ctx context.Context) (*http.Request, error) {
		req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/v1/audio/tasks/"+taskID), nil)
		if rerr != nil {
			return nil, fmt.Errorf("%w: %v", errRequestBody, rerr)
		}
		return req, nil
	}, false)
	if err != nil {
		return nil, err
	}

	task := &Task{}
	body, err := readJSON(resp, task)
	if err != nil {
		return nil, err
	}
	task.Raw = json.RawMessage(body)
	return task, nil
}

// WaitTask 轮询任务直到成功, 失败或超时.
//
// interval 为轮询间隔, timeout 为最长等待时间, onPoll 每次拿到状态时回调,
// 便于上层输出进度. 服务端返回的 retry_after 会覆盖 interval.
func (c *Client) WaitTask(ctx context.Context, taskID string, interval, timeout time.Duration,
	onPoll func(*Task)) (*Task, error) {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}

	for {
		task, err := c.GetTask(ctx, taskID)
		if err != nil {
			return nil, err
		}
		if onPoll != nil {
			onPoll(task)
		}
		switch task.Status {
		case StatusSuccess:
			return task, nil
		case StatusFailed:
			return task, fmt.Errorf("任务 %s 执行失败: %s", taskID, taskErrorText(task))
		}

		wait := interval
		if task.RetryAfter > 0 {
			wait = time.Duration(task.RetryAfter) * time.Second
		}
		if wait < 500*time.Millisecond {
			wait = 500 * time.Millisecond
		}
		if !deadline.IsZero() {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return task, fmt.Errorf("等待任务 %s 超过 %s 仍未完成", taskID, timeout)
			}
			if wait > remaining {
				wait = remaining
			}
		}

		select {
		case <-ctx.Done():
			return task, ctx.Err()
		case <-time.After(wait):
		}
	}
}

func taskErrorText(task *Task) string {
	if task == nil {
		return "未知原因"
	}
	if task.Error != nil {
		return task.Error.Error()
	}
	return "服务端未返回失败原因"
}
