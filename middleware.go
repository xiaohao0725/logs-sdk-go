package logsdk

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// GinMiddleware 返回 Gin 框架的 HTTP 中间件处理函数。
// 自动采集：请求头/体、响应头/体、客户端信息、panic 捕获与堆栈、耗时统计。
// 一行代码集成：router.Use(client.GinMiddleware())
func (c *Client) GinMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// ① 生成 UUID v7（32 位无连字符）
		entryUUID := newLogUUID()
		startTime := time.Now()

		// ② 读取请求体（缓存以支持后续 handler 读取，Gin 默认不缓存 Body）
		bodyBytes, _ := io.ReadAll(ctx.Request.Body)
		ctx.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		// ③ ★ panic 捕获 —— 即使业务代码 panic，也能记录完整堆栈
		defer func() {
			if r := recover(); r != nil {
				// 记录 8KB 的堆栈信息
				stackBuf := make([]byte, c.maxStackSize)
				n := runtime.Stack(stackBuf, false)
				entry := c.buildEntry(ctx, entryUUID, startTime, bodyBytes)
				entry.IsError = true
				entry.ErrorType = "panic"
				entry.ErrorMessage = fmt.Sprintf("%v", r)
				entry.ErrorStack = string(stackBuf[:n])
				entry.PanicLocation = extractPanicLocation(string(stackBuf[:n]))
				c.Send(entry) // 异步发送，不阻塞
				panic(r)      // 重新抛出，保持业务代码的 panic 行为
			}
		}()

		// ④ 执行业务 Handler
		ctx.Next()

		// ⑤ 正常响应：构建完整日志条目
		entry := c.buildEntry(ctx, entryUUID, startTime, bodyBytes)
		if entry.StatusCode >= 400 {
			entry.IsError = true
			entry.ErrorType = "http_error"
			entry.ErrorMessage = entry.ResponseBody
		}
		c.Send(entry) // 异步发送
	}
}

// StandardMiddleware 返回标准 net/http 中间件（适用于非 Gin 框架）。
func (c *Client) StandardMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			entryUUID := newLogUUID()
			startTime := time.Now()

			bodyBytes, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			// 包装 ResponseWriter 以捕获状态码和响应体
			wrapped := &responseWriter{ResponseWriter: w, statusCode: 200}

		defer func() {
				if rec := recover(); rec != nil {
					stackBuf := make([]byte, c.maxStackSize)
					n := runtime.Stack(stackBuf, false)
					entry := c.buildStandardEntry(r, wrapped, entryUUID, startTime, bodyBytes)
					entry.IsError = true
					entry.ErrorType = "panic"
					entry.ErrorMessage = fmt.Sprintf("%v", rec)
					entry.ErrorStack = string(stackBuf[:n])
					entry.PanicLocation = extractPanicLocation(string(stackBuf[:n]))
					c.Send(entry)
					panic(rec)
				}
			}()

			next.ServeHTTP(wrapped, r)

			entry := c.buildStandardEntry(r, wrapped, entryUUID, startTime, bodyBytes)
			if entry.StatusCode >= 400 {
				entry.IsError = true
				entry.ErrorType = "http_error"
				entry.ErrorMessage = entry.ResponseBody
			}
			c.Send(entry)
		})
	}
}

// buildEntry 从 Gin Context 构建完整的 LogEntry（Gin 版本）。
func (c *Client) buildEntry(ctx *gin.Context, entryUUID string, startTime time.Time, reqBody []byte) *LogEntry {
	// 截断请求体
	reqBodyStr := truncateString(string(reqBody), c.maxBodySize)
	// 捕获响应体（Gin ResponseWriter 需从 ctx.Writer.Size() 间接获取）
	respBodyStr := captureGinResponseBody(ctx, c.maxBodySize)
	respBodySize := int64(ctx.Writer.Size())

	return &LogEntry{
		UUID:        entryUUID,
		Timestamp:   startTime.UTC(),
		DurationMs:  time.Since(startTime).Milliseconds(),
		Method:      ctx.Request.Method,
		Scheme:      schemeOf(ctx.Request),
		HostHeader:  ctx.Request.Host,
		Path:        ctx.Request.URL.Path,
		QueryString: ctx.Request.URL.RawQuery,
		FullURL:     buildFullURL(ctx.Request),
		Origin:      detectOrigin(ctx.Request),
		RequestHeaders:  sanitizeHeadersJSON(ctx.Request.Header),
		RequestBody:     reqBodyStr,
		RequestBodySize: int64(len(reqBody)),
		ContentType:     ctx.Request.Header.Get("Content-Type"),
		StatusCode:       ctx.Writer.Status(),
		ResponseHeaders:  headersToJSON(ctx.Writer.Header()),
		ResponseBody:     respBodyStr,
		ResponseBodySize: respBodySize,
		ClientIP:         realClientIP(ctx.Request),
		ClientIPChain:    ctx.Request.Header.Get("X-Forwarded-For"),
		ClientPort:       clientPort(ctx.Request),
		ClientType:       detectClientType(ctx.Request),
		UserAgent:        ctx.Request.UserAgent(),
		TraceID:          orDefault(ctx.Request.Header.Get("X-Trace-ID"), entryUUID),
		SpanID:           entryUUID,
		ParentSpanID:     ctx.Request.Header.Get("X-Parent-Span-ID"),
		UserID:           extractUserID(ctx.Request),
		SessionID:        extractSessionID(ctx.Request),
		ProjectSlug:      c.config.ProjectSlug,
		Environment:      c.config.Environment,
		ServiceName:      c.config.ServiceName,
		Host:             getHostname(),
		ProcessID:        strconv.Itoa(osGetpid()),
		Tags:             map[string]interface{}{},
		TLSVersion:       getTLSVersion(ctx.Request),
		TLSCipher:        getTLSCipher(ctx.Request),
		Proto:            ctx.Request.Proto,
		APIVersion:       extractAPIVersion(ctx.Request.URL.Path),
		Referer:          ctx.Request.Header.Get("Referer"),
		LatencyBreakdown: "{}",
		RequestID:        entryUUID[:8],
	}
}

// buildStandardEntry 从标准 net/http 构建 LogEntry。
func (c *Client) buildStandardEntry(r *http.Request, w *responseWriter, entryUUID string, startTime time.Time, reqBody []byte) *LogEntry {
	return &LogEntry{
		UUID:        entryUUID,
		Timestamp:   startTime.UTC(),
		DurationMs:  time.Since(startTime).Milliseconds(),
		Method:      r.Method,
		Scheme:      schemeOf(r),
		HostHeader:  r.Host,
		Path:        r.URL.Path,
		QueryString: r.URL.RawQuery,
		FullURL:     buildFullURL(r),
		Origin:      detectOrigin(r),
		RequestHeaders:  sanitizeHeadersJSON(r.Header),
		RequestBody:     truncateString(string(reqBody), c.maxBodySize),
		RequestBodySize: int64(len(reqBody)),
		ContentType:     r.Header.Get("Content-Type"),
		StatusCode:       w.statusCode,
		ResponseHeaders:  headersToJSON(w.Header()),
		ResponseBody:     truncateString(w.body.String(), c.maxBodySize),
		ResponseBodySize: int64(w.body.Len()),
		ClientIP:         realClientIP(r),
		ClientIPChain:    r.Header.Get("X-Forwarded-For"),
		ClientPort:       clientPort(r),
		ClientType:       detectClientType(r),
		UserAgent:        r.UserAgent(),
		TraceID:          orDefault(r.Header.Get("X-Trace-ID"), entryUUID),
		SpanID:           entryUUID,
		ParentSpanID:     r.Header.Get("X-Parent-Span-ID"),
		UserID:           extractUserID(r),
		SessionID:        extractSessionID(r),
		ProjectSlug:      c.config.ProjectSlug,
		Environment:      c.config.Environment,
		ServiceName:      c.config.ServiceName,
		Host:             getHostname(),
		ProcessID:        strconv.Itoa(osGetpid()),
		Tags:             map[string]interface{}{},
		TLSVersion:       getTLSVersion(r),
		TLSCipher:        getTLSCipher(r),
		Proto:            r.Proto,
		APIVersion:       extractAPIVersion(r.URL.Path),
		Referer:          r.Header.Get("Referer"),
		LatencyBreakdown: "{}",
		RequestID:        entryUUID[:8],
	}
}

// ──────────────── 辅助函数 ────────────────

// newLogUUID 生成 UUID v7 并去掉连字符，返回 32 字符十六进制字符串。
func newLogUUID() string {
	id, _ := uuid.NewV7()
	return strings.ReplaceAll(id.String(), "-", "")
}

// schemeOf 检测请求使用的协议（http/https）。
func schemeOf(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	return "http"
}

// buildFullURL 构建完整的请求 URL（scheme://host/path?query）。
func buildFullURL(r *http.Request) string {
	u := schemeOf(r) + "://" + r.Host + r.URL.Path
	if r.URL.RawQuery != "" {
		u += "?" + r.URL.RawQuery
	}
	return u
}

// realClientIP 获取真实客户端 IP。
// 优先级：X-Forwarded-For 最左 > X-Real-IP > RemoteAddr
func realClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

// clientPort 提取客户端端口号。
func clientPort(r *http.Request) int {
	_, portStr, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return 0
	}
	port, _ := strconv.Atoi(portStr)
	return port
}

// detectClientType 根据请求头特征识别客户端类型。
func detectClientType(r *http.Request) string {
	// 1. 优先信任业务自定义头（最准确）
	if ct := r.Header.Get("X-Client-Type"); ct != "" {
		return ct
	}
	// 2. User-Agent 特征检测
	ua := r.Header.Get("User-Agent")
	if strings.Contains(ua, "MicroMessenger") || strings.Contains(ua, "miniProgram") {
		return "miniprogram"
	}
	// 3. 服务端调用特征（有 Caller 头且无浏览器特征）
	if r.Header.Get("X-Caller-Service") != "" {
		return "server"
	}
	// 4. Web 浏览器特征（有 Referer/Origin 且 UA 为浏览器）
	if (r.Header.Get("Referer") != "" || r.Header.Get("Origin") != "") &&
		(strings.Contains(ua, "Mozilla") || strings.Contains(ua, "Chrome") ||
			strings.Contains(ua, "Safari") || strings.Contains(ua, "Firefox")) {
		return "web"
	}
	return "other"
}

// detectOrigin 根据客户端类型返回请求来源标识。
func detectOrigin(r *http.Request) string {
	switch detectClientType(r) {
	case "web":
		if ref := r.Header.Get("Referer"); ref != "" {
			return ref
		}
		return r.Header.Get("Origin")
	case "miniprogram":
		appID := r.Header.Get("X-MiniProgram-AppId")
		path := r.Header.Get("X-MiniProgram-Path")
		return fmt.Sprintf("miniprogram:%s%s", appID, path)
	case "app":
		return fmt.Sprintf("app:%s/%s/%s",
			r.Header.Get("X-App-Name"),
			r.Header.Get("X-App-Version"),
			r.Header.Get("X-App-Scene"))
	case "server":
		return fmt.Sprintf("server:%s/%s",
			r.Header.Get("X-Caller-Service"),
			r.Header.Get("X-Caller-Version"))
	default:
		return "unknown"
	}
}

// sanitizeHeadersJSON 将请求头序列化为 JSON，并对敏感字段脱敏。
func sanitizeHeadersJSON(headers http.Header) string {
	safe := make(map[string]string, len(headers))
	for k, v := range headers {
		if len(v) == 0 {
			continue
		}
		// 对敏感字段脱敏
		lower := strings.ToLower(k)
		if lower == "authorization" || lower == "cookie" || lower == "set-cookie" {
			if len(v[0]) > 20 {
				safe[k] = v[0][:15] + "..."
			} else {
				safe[k] = "***"
			}
			continue
		}
		safe[k] = strings.Join(v, ", ")
	}
	b, _ := json.Marshal(safe)
	return string(b)
}

// headersToJSON 将响应头序列化为 JSON。
func headersToJSON(headers http.Header) string {
	safe := make(map[string]string, len(headers))
	for k, v := range headers {
		if len(v) > 0 {
			safe[k] = strings.Join(v, ", ")
		}
	}
	b, _ := json.Marshal(safe)
	return string(b)
}

// extractUserID 从请求中提取用户 ID（优先 X-User-ID 头，其次 JWT/Cookie 解析）。
func extractUserID(r *http.Request) string {
	if uid := r.Header.Get("X-User-ID"); uid != "" {
		return uid
	}
	return ""
}

// extractSessionID 从请求中提取会话 ID。
func extractSessionID(r *http.Request) string {
	if sid := r.Header.Get("X-Session-ID"); sid != "" {
		return sid
	}
	return ""
}

// extractPanicLocation 从 panic 堆栈中提取 panic 发生位置。
// 堆栈首行格式: "goroutine N [running]:\n"
// panic 位置通常在 goroutine 运行帧后的第一行有意义的代码。
func extractPanicLocation(stack string) string {
	lines := strings.Split(stack, "\n")
	// 跳过 goroutine 行，找到第一个 panic 信息
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "panic(") && i+1 < len(lines) {
			nextLine := strings.TrimSpace(lines[i+1])
			if nextLine != "" {
				return nextLine
			}
		}
	}
	// 未找到 panic 行，返回第一行 Go 源代码引用
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, ".go:") {
			return line
		}
	}
	return ""
}

// captureGinResponseBody 从 Gin 的 ResponseWriter 捕获已写入的响应体。
// 注意：Gin 默认不缓存响应体，此方法只能在业务 Handler 未 hijack Writer 时工作。
func captureGinResponseBody(ctx *gin.Context, maxSize int) string {
	size := ctx.Writer.Size()
	if size <= 0 {
		return ""
	}
	// 尝试从 gin.ResponseWriter 获取
	if rw, ok := ctx.Writer.(interface{ Body() []byte }); ok {
		return truncateString(string(rw.Body()), maxSize)
	}
	return ""
}

// truncateString 截断字符串到指定最大长度，超长在末尾标记。
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...[truncated]"
}

// orDefault 若 s 为空则返回默认值。
func orDefault(s, defaultVal string) string {
	if s == "" {
		return defaultVal
	}
	return s
}

// responseWriter 包装 http.ResponseWriter 以捕获状态码和响应体。
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	body       bytes.Buffer
}

// WriteHeader 记录状态码。
func (w *responseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// Write 同时写入原始 ResponseWriter 和内部缓冲区（用于捕获响应体）。
func (w *responseWriter) Write(b []byte) (int, error) {
	w.body.Write(b) // 捕获到内部缓冲
	return w.ResponseWriter.Write(b)
}

// ──────────────── 系统信息 ────────────────

var osGetpid = os.Getpid
var osHostname = os.Hostname

func getHostname() string {
	host, _ := osHostname()
	return host
}

// ──────────────── 新增辅助函数 ────────────────

// getTLSVersion 获取 TLS 版本。
func getTLSVersion(r *http.Request) string {
	if r.TLS != nil {
		switch r.TLS.Version {
		case 0x0304: return "1.3"
		case 0x0303: return "1.2"
		case 0x0302: return "1.1"
		case 0x0301: return "1.0"
		default: return fmt.Sprintf("0x%04x", r.TLS.Version)
		}
	}
	return ""
}

// getTLSCipher 获取 TLS 密码套件名称。
func getTLSCipher(r *http.Request) string {
	if r.TLS != nil {
		return tls.CipherSuiteName(r.TLS.CipherSuite)
	}
	return ""
}

// extractAPIVersion 从路径提取 API 版本。
// 匹配 /api/v1/... → v1, /api/v2/... → v2
func extractAPIVersion(path string) string {
	for i := 0; i < len(path)-5; i++ {
		if path[i:i+5] == "/api/" && i+7 < len(path) && path[i+5] == 'v' {
			end := i + 7
			for end < len(path) && path[end] != '/' { end++ }
			return path[i+5 : end]
		}
	}
	return ""
}
