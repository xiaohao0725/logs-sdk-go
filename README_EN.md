# Log Management Platform Go SDK

[中文文档](./README.md) | [API Reference](https://pkg.go.dev/github.com/xiaohao0725/logs-sdk-go)

`logs-sdk-go` is the Go SDK for the Log Management Platform. Provides Gin and standard `net/http` middleware with one-line integration for automatic HTTP request log collection.

## Features

- ✅ **One-line integration**: `router.Use(client.GinMiddleware())`
- ✅ **Full capture**: 60+ fields including request/response headers & body, client device info, TLS version, API version
- ✅ **Auto-detection**: Client type (Web/MiniProgram/App/Server/Other), request origin (Referer/microservice call chain)
- ✅ **Panic recovery**: Captures full stack traces even when business code panics
- ✅ **UUID v7**: 32-char hex without hyphens, naturally time-sorted
- ✅ **Data sanitization**: Auto-masks Authorization/Cookie headers
- ✅ **Async non-blocking**: Ring buffer + background timer flush, zero business impact
- ✅ **Offline cache**: Auto-caches to local files on network failure, auto-retransmits on recovery
- ✅ **Graceful shutdown**: `Close()` ensures all buffered logs are sent
- ✅ **Manual logging**: `Info()` / `Warn()` / `Error()` / `Debug()` for business events
- ✅ **Distributed tracing**: W3C Trace Context propagation (trace_id / span_id)

## Installation

```bash
go get github.com/xiaohao0725/logs-sdk-go@v0.3.0
```

Requires Go 1.22+.

## Quick Start

### Gin Framework

```go
package main

import (
    logsdk "github.com/xiaohao0725/logs-sdk-go"
    "github.com/gin-gonic/gin"
)

func main() {
    client, _ := logsdk.New(logsdk.Config{
        Endpoint:    "https://api.logs.codexs.cn/api/v1/ingest/logs",
        APIKey:      "clog_pk_xxx",
        APISecret:   "clog_sk_xxx",
        ProjectSlug: "my-project",
        Environment: "production",
    })
    defer client.Close()
    client.FlushOffline()

    router := gin.Default()
    router.Use(client.GinMiddleware())
    router.Run(":8080")
}
```

### Standard net/http

```go
client, _ := logsdk.New(logsdk.Config{...})
defer client.Close()

mux := http.NewServeMux()
http.ListenAndServe(":8080", client.StandardMiddleware()(mux))
```

## Configuration

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `Endpoint` | `string` | **Required** | Log ingestion API URL |
| `APIKey` | `string` | **Required** | SDK authentication key (public) |
| `APISecret` | `string` | **Required** | SDK authentication key (private) |
| `ProjectSlug` | `string` | **Required** | Project short identifier |
| `Environment` | `string` | `"production"` | production / staging / development |
| `ServiceName` | `string` | `""` | Microservice name |
| `BufferSize` | `int` | `1000` | Ring buffer capacity, flushes at 80% |
| `FlushInterval` | `int` | `5` | Flush interval in seconds |
| `MaxRetries` | `int` | `3` | Max retries with exponential backoff |
| `MaxBodySize` | `int` | `4096` | Max request/response body size (bytes) |
| `MaxStackSize` | `int` | `8192` | Max error stack trace size (bytes) |

## Collected Fields

### Request
`method`, `scheme`, `full_url`, `host_header`, `path`, `query_string`, `origin`, `request_headers` (JSON, sanitized), `request_body` (≤4KB), `request_body_size`, `content_type`

### Response
`status_code`, `response_headers` (JSON), `response_body` (≤4KB), `response_body_size`

### Client
`client_ip`, `client_ip_chain` (full proxy chain), `client_type` (web/miniprogram/app/server/other), `client_port`

### Device
`user_agent` (raw), `device_type` (Desktop/Mobile/Tablet/Bot), `browser`, `browser_version`, `os_name`, `os_version`

### TLS / Protocol
`tls_version` (1.2/1.3), `tls_cipher`, `proto` (HTTP/1.1, HTTP/2), `api_version`, `referer`

### Trace & Correlation
`trace_id` (W3C), `span_id`, `parent_span_id`, `user_id`, `session_id`, `request_id` (short ID)

### Error & Stack
`is_error`, `error_type` (panic/http_error/business_error/timeout), `error_message`, `error_stack`, `panic_location`

## Architecture

```
HTTP Request
  → GinMiddleware: generate UUID v7, cache body
  → Business Handler (with panic recovery)
  → Build LogEntry (60+ fields, auto-detect client type, sanitize headers)
  → Ring Buffer (non-blocking push)
  → Background Timer (every 5s or 80% full)
     → Batch POST to Ingestion API → Retry → Offline Cache on failure
```

## Offline Cache

On network failure, logs are saved to system temp directory. Auto-retransmit on `FlushOffline()` or `Close()`.

## Changelog

| Version | Date | Changes |
|---------|------|---------|
| v0.3.0 | 2026-06-21 | Added TLS/protocol/API version/Referer/latency breakdown/request_id fields |
| v0.2.0 | 2026-06-21 | Added offline cache with auto-retransmit |
| v0.1.0 | 2026-06-21 | Initial release: Gin/std middleware, async buffer, retry, manual logging |

## License

UNLICENSED — Internal use
