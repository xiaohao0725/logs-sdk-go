# 日志管理平台 Go SDK

[English Documentation](https://github.com/xiaohao0725/logs-sdk-go/blob/main/README_EN.md) | [API Reference](https://pkg.go.dev/github.com/xiaohao0725/logs-sdk-go)

`logs-sdk-go` 是日志管理平台的 Go 语言 SDK，提供 Gin 和标准 `net/http` 中间件，一行代码即可自动采集 HTTP 请求的完整日志（请求/响应头体、客户端信息、设备信息、错误堆栈等），异步批量上报，对业务性能零影响。

## 功能特性

- ✅ **一行代码接入**：`router.Use(client.GinMiddleware())`
- ✅ **完整采集**：60+ 字段——请求头/体、响应头/体、客户端 IP/端口/类型、设备信息、TLS 版本、API 版本
- ✅ **自动识别**：客户端类型（Web / 小程序 / App / 服务端 / 其他）、请求来源（Referer / 微服务调用链）
- ✅ **Panic 捕获**：即使业务代码 panic，也能记录完整堆栈（限制 8KB）
- ✅ **UUID v7**：32 位十六进制无连字符，天然按时间排序
- ✅ **敏感脱敏**：Authorization / Cookie 自动脱敏，不记录明文
- ✅ **异步非阻塞**：环形缓冲区 + 后台定时刷新，不阻塞业务请求
- ✅ **离线缓存**：网络故障时自动缓存到本地文件，恢复后自动重传
- ✅ **优雅关闭**：`Close()` 确保缓冲日志全部上报
- ✅ **手动日志**：`Info()` / `Warn()` / `Error()` / `Debug()` 支持手动记录业务事件
- ✅ **分布式追踪**：自动传递 W3C Trace Context（trace_id / span_id）

## 安装

```bash
go get github.com/xiaohao0725/logs-sdk-go@v0.3.0
```

要求 Go 1.22+。

## 快速开始

### Gin 框架

```go
package main

import (
    logsdk "github.com/xiaohao0725/logs-sdk-go"
    "github.com/gin-gonic/gin"
)

func main() {
    // ① 创建客户端
    client, err := logsdk.New(logsdk.Config{
        Endpoint:    "https://api.logs.codexs.cn/api/v1/ingest/logs",
        APIKey:      "clog_pk_xxx",
        APISecret:   "clog_sk_xxx",
        ProjectSlug: "my-project",
        Environment: "production",
    })
    if err != nil {
        panic(err)
    }
    defer client.Close()

    // ② 重传离线缓存的日志
    client.FlushOffline()

    // ③ 注册 Gin 中间件——一行代码接入
    router := gin.Default()
    router.Use(client.GinMiddleware())

    router.GET("/api/hello", func(c *gin.Context) {
        c.JSON(200, gin.H{"message": "hello"})
    })

    router.Run(":8080")
}
```

### 标准 net/http

```go
client, _ := logsdk.New(logsdk.Config{...})
defer client.Close()

mux := http.NewServeMux()
mux.HandleFunc("/api/hello", helloHandler)

// 使用标准中间件
http.ListenAndServe(":8080", client.StandardMiddleware()(mux))
```

### 手动日志

```go
// 记录业务事件
client.Info("用户登录成功", map[string]interface{}{
    "user_id": "123",
})
client.Error("订单创建失败", err, map[string]interface{}{
    "order_id": "456",
})
client.Warn("库存不足", map[string]interface{}{
    "sku": "ABC-123",
})
```

## 配置参数

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Endpoint` | `string` | **必填** | 日志上报地址 |
| `APIKey` | `string` | **必填** | SDK 认证密钥（公钥） |
| `APISecret` | `string` | **必填** | SDK 认证密钥（私钥） |
| `ProjectSlug` | `string` | **必填** | 项目短标识 |
| `Environment` | `string` | `"production"` | 运行环境：production / staging / development |
| `ServiceName` | `string` | `""` | 微服务名称 |
| `BufferSize` | `int` | `1000` | 本地环形缓冲区容量，满 80% 自动 flush |
| `FlushInterval` | `int` | `5` | 定时刷新间隔（秒） |
| `MaxRetries` | `int` | `3` | 最大重试次数，指数退避 |
| `MaxBodySize` | `int` | `4096` | 请求/响应体最大采集大小（字节） |
| `MaxStackSize` | `int` | `8192` | 错误堆栈最大采集大小（字节） |

## 采集字段一览

### 请求信息
`method`, `scheme`, `full_url`, `host_header`, `path`, `query_string`, `origin`, `request_headers` (JSON, 脱敏), `request_body` (≤4KB), `request_body_size`, `content_type`

### 响应信息
`status_code`, `response_headers` (JSON), `response_body` (≤4KB), `response_body_size`

### 客户端信息
`client_ip`, `client_ip_chain` (完整代理链), `client_type` (web/miniprogram/app/server/other), `client_port`

### 设备信息
`user_agent` (原始), `device_type` (Desktop/Mobile/Tablet/Bot), `browser`, `browser_version`, `os_name`, `os_version`

### TLS / 协议
`tls_version` (1.2/1.3), `tls_cipher`, `proto` (HTTP/1.1, HTTP/2), `api_version`, `referer`

### 追踪与关联
`trace_id` (W3C Trace Context), `span_id`, `parent_span_id`, `user_id`, `session_id`, `request_id` (短 ID)

### 错误与堆栈
`is_error`, `error_type` (panic/http_error/business_error/timeout), `error_message`, `error_stack`, `panic_location`

### 来源信息
`project_slug`, `environment`, `service_name`, `host`, `process_id`

## 架构设计

```
HTTP 请求进入
  │
  ├─ ① GinMiddleware / StandardMiddleware
  │     ├─ 生成 UUID v7（32 位无连字符）
  │     ├─ 读取并缓存请求体
  │     └─ 记录开始时间
  │
  ├─ ② 业务 Handler（带 panic recover 保护）
  │     └─ panic → 捕获完整堆栈 → 标记 is_error=true → 重新抛出
  │
  ├─ ③ 构建 LogEntry（60+ 字段）
  │     ├─ 客户端类型检测（web / 小程序 / App / server）
  │     ├─ 请求来源识别（Referer / 小程序来源 / 微服务调用链）
  │     └─ 敏感字段脱敏（Authorization / Cookie）
  │
  ├─ ④ 写入环形缓冲区（非阻塞）
  │
  └─ ⑤ 后台定时刷新（每 5s 或缓冲 80% 满）
        └─ 批量 POST 到 Ingestion API → 指数退避重试 → 失败则离线缓存
```

## 离线缓存

网络故障时，SDK 自动将日志保存到系统临时目录：

```
/tmp/logs-sdk-offline/
├── offline-20260621T120000.json
├── offline-20260621T120005.json
└── ...
```

- 最大缓存 50MB，超过自动清理旧文件
- 超过 24 小时的缓存自动删除
- 调用 `FlushOffline()` 或 `Close()` 时自动重传

## 版本历史

| 版本 | 日期 | 变更 |
|------|------|------|
| v0.3.0 | 2026-06-21 | 新增 TLS/协议/API版本/Referer/耗时分解/request_id 等 8 字段 |
| v0.2.0 | 2026-06-21 | 新增离线缓存（断网本地存储，恢复自动重传） |
| v0.1.0 | 2026-06-21 | 初始版本：Gin/标准中间件、异步缓冲、重试、手动日志 |

## License

UNLICENSED — 内部使用
