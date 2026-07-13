// Package logsdk 日志采集 SDK，提供一行代码接入的 HTTP 中间件。
// 自动采集所有 HTTP 请求的完整信息（请求/响应头体、客户端信息、设备信息等），
// 异步批量上报到日志管理平台，对业务性能零影响。
package logsdk

import "time"

// Config SDK 客户端配置。
type Config struct {
	// Endpoint 日志上报地址，如 "https://api.logs.codexs.cn/api/v1/ingest/logs"
	Endpoint string `json:"endpoint"`

	// APIKey SDK 认证密钥（公钥），项目注册时自动生成
	APIKey string `json:"api_key"`

	// APISecret SDK 认证密钥（私钥），用于请求签名
	APISecret string `json:"api_secret"`

	// ProjectSlug 项目短标识，项目注册时分配
	ProjectSlug string `json:"project_slug"`

	// Environment 当前运行环境：production/staging/development
	// 默认 "production"
	Environment string `json:"environment"`

	// ServiceName 微服务名称，用于区分不同服务的日志
	ServiceName string `json:"service_name"`

	// BufferSize 本地环形缓冲区容量，默认 1000
	// 缓冲区满 80% 时自动触发 flush
	BufferSize int `json:"buffer_size"`

	// FlushInterval 定时刷新间隔（秒），默认 5 秒
	FlushInterval int `json:"flush_interval"`

	// MaxRetries 最大重试次数，默认 3 次
	MaxRetries int `json:"max_retries"`

	// MaxBodySize 请求/响应体最大采集大小（字节），默认 4096（4KB）
	// 超长自动截断并在日志中标记
	MaxBodySize int `json:"max_body_size"`

	// MaxStackSize 错误堆栈最大采集大小（字节），默认 8192（8KB）
	MaxStackSize int `json:"max_stack_size"`
}

// DefaultConfig 返回默认配置值。
func DefaultConfig() Config {
	return Config{
		Environment:   "production",
		BufferSize:    1000,
		FlushInterval: 5,
		MaxRetries:    3,
		MaxBodySize:   4096,
		MaxStackSize:  8192,
	}
}

// LogEntry 日志条目，由 SDK 中间件自动构建（所有可采集的信息）。
// 注：ip 地理信息（uid, client_country 等）和 analysis 字段由服务端补充，SDK 不设置。
type LogEntry struct {
	// ── 基础标识 ──
	UUID       string    `json:"uuid"`        // UUID v7，32 位十六进制无连字符，SDK 自动生成
	Timestamp  time.Time `json:"timestamp"`    // 请求开始时间 (UTC)
	DurationMs int64     `json:"duration_ms"`  // 请求耗时（毫秒）

	// ── 请求信息（完整采集）──
	Method          string `json:"method"`            // HTTP 方法
	Scheme          string `json:"scheme"`            // http / https
	FullURL         string `json:"full_url"`          // ★ 完整请求 URL（scheme://host/path?query）
	HostHeader      string `json:"host_header"`       // Host 请求头
	Path            string `json:"path"`              // 请求路径
	QueryString     string `json:"query_string"`      // 查询参数
	Origin          string `json:"origin"`            // ★ 请求来源页面 URL（Referer 头）
	RequestHeaders  string `json:"request_headers"`   // ★ 完整请求头（JSON，敏感字段脱敏）
	RequestBody     string `json:"request_body"`      // ★ 完整请求体（限制 4KB）
	RequestBodySize int64  `json:"request_body_size"` // 请求体原始大小（字节）
	ContentType     string `json:"content_type"`      // 请求 Content-Type

	// ── 响应信息（完整采集）──
	StatusCode       int    `json:"status_code"`         // HTTP 状态码
	ResponseHeaders  string `json:"response_headers"`    // ★ 完整响应头（JSON）
	ResponseBody     string `json:"response_body"`       // ★ 完整响应体（限制 4KB）
	ResponseBodySize int64  `json:"response_body_size"`  // 响应体原始大小（字节）

	// ── 客户端信息 ──
	ClientIP      string `json:"client_ip"`       // 真实客户端 IP
	ClientIPChain string `json:"client_ip_chain"` // ★ 完整代理链 X-Forwarded-For
	ClientType    string `json:"client_type"`     // ★ 客户端类型：web/miniprogram/app/server/other
	ClientPort    int    `json:"client_port"`     // 客户端端口
	UserAgent     string `json:"user_agent"`      // 原始 User-Agent 字符串

	// ── ★ 错误与堆栈 ──
	IsError       bool   `json:"is_error"`       // 是否异常（status>=500 或有 recover 到的 panic）
	ErrorMessage  string `json:"error_message"`   // 错误消息
	ErrorType     string `json:"error_type"`      // panic/sdk_error/business_error/http_error/timeout
	ErrorStack    string `json:"error_stack"`     // ★ 完整错误堆栈（限制 8KB）
	PanicLocation string `json:"panic_location"`  // ★ panic 位置（函数名+文件+行号）

	// ── 关联与追踪 ──
	TraceID      string `json:"trace_id"`       // W3C Trace Context
	SpanID       string `json:"span_id"`        // 当前 Span ID
	ParentSpanID string `json:"parent_span_id"` // 父 Span ID
	UserID       string `json:"user_id"`        // 当前用户 ID
	SessionID    string `json:"session_id"`     // 会话 ID

	// ── 来源信息 ──
	ProjectSlug string `json:"project_slug"` // 项目标识
	Environment string `json:"environment"`  // production/staging/development
	ServiceName string `json:"service_name"` // 微服务名称
	Host        string `json:"host"`         // 来源主机名
	ProcessID   string `json:"process_id"`   // 进程 PID

	// ── TLS/协议 ──
	TLSVersion       string `json:"tls_version"`       // TLS版本: 1.2/1.3
	TLSCipher        string `json:"tls_cipher"`        // TLS密码套件
	Proto            string `json:"proto"`             // HTTP协议版本
	APIVersion       string `json:"api_version"`       // API版本
	Referer          string `json:"referer"`           // HTTP Referer
	UpstreamStatus   int    `json:"upstream_status"`   // 上游状态码
	LatencyBreakdown string `json:"latency_breakdown"` // 耗时分解JSON
	RequestID        string `json:"request_id"`        // 短请求ID
	// ── 自定义扩展 ──
	Tags map[string]interface{} `json:"tags"` // 自定义标签
}

// InfraLogEntry represents an infrastructure/middleware log entry.
type InfraLogEntry struct {
	UUID           string `json:"uuid"`
	Timestamp      string `json:"timestamp"`
	ProjectSlug    string `json:"project_slug"`
	SourceType     string `json:"source_type"`
	SourceName     string `json:"source_name"`
	Host           string `json:"host"`
	Level          string `json:"level"`
	Message        string `json:"message"`
	Metadata       string `json:"metadata"`
	TraceID        string `json:"trace_id"`
	RelatedAPIUUID string `json:"related_api_uuid"`
	IsError        bool   `json:"is_error"`
	ErrorDetail    string `json:"error_detail"`
}