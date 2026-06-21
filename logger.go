package logsdk

import (
	"fmt"
	"runtime"
	"time"
)

// Info 手动记录一条信息级别日志到日志平台。
// 用于记录 SDK 中间件无法自动捕获的业务事件。
func (c *Client) Info(message string, tags map[string]interface{}) {
	c.manualLog("info", message, "", "", tags)
}

// Warn 手动记录一条警告级别日志到日志平台。
func (c *Client) Warn(message string, tags map[string]interface{}) {
	c.manualLog("warn", message, "", "", tags)
}

// Error 手动记录一条错误日志到日志平台。
// 自动捕获当前调用栈信息。
func (c *Client) Error(message string, err error, tags map[string]interface{}) {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	c.manualLog("error", message, errMsg, captureStack(2), tags)
}

// Debug 手动记录一条调试级别日志到日志平台。
// 在非开发环境下，默认不发送（由服务端过滤）。
func (c *Client) Debug(message string, tags map[string]interface{}) {
	if c.config.Environment == "production" {
		return // 生产环境不发送调试日志
	}
	c.manualLog("debug", message, "", "", tags)
}

// manualLog 构建手动日志条目并发送。
func (c *Client) manualLog(level, message, errorMsg, stack string, tags map[string]interface{}) {
	entry := &LogEntry{
		UUID:         newLogUUID(),
		Timestamp:    time.Now().UTC(),
		DurationMs:   0,
		Method:       "MANUAL",
		Scheme:       "",
		FullURL:      "",
		Path:         fmt.Sprintf("/_log/%s", level),
		Origin:       "manual",
		RequestHeaders:  "{}",
		RequestBody:     message,
		RequestBodySize: int64(len(message)),
		StatusCode:       0,
		ResponseHeaders:  "{}",
		ResponseBody:     "",
		UserAgent:        "",
		ClientType:       "server",
		IsError:          level == "error",
		ErrorMessage:     errorMsg,
		ErrorType:        "manual_" + level,
		ErrorStack:       stack,
		TraceID:          "",
		SpanID:           "",
		ProjectSlug:      c.config.ProjectSlug,
		Environment:      c.config.Environment,
		ServiceName:      c.config.ServiceName,
		Host:             c.hostname,
		ProcessID:        c.pid,
		Tags:             tags,
	}
	c.Send(entry)
}

// captureStack 捕获当前调用栈信息（限制 8192 字节）。
// skip 为跳过的调用帧数（0=当前函数，1=调用者...）。
func captureStack(skip int) string {
	buf := make([]byte, 8192)
	n := runtime.Stack(buf, false)
	if n == 0 {
		return ""
	}
	return string(buf[:n])
}
