# Changelog

## v0.6.0 (2026-08-17)

### 新增
- 日志条目增加 `is_callback` 标记，自动识别平台回调通知触发的请求。

### 修复
- 修复 `UploadInfraLogs` 未自动生成 UUID、补齐 SDK hash 的问题。
- 基础设施日志上报时自动补全 `project_slug`、`host`、`timestamp`，避免被服务端丢弃。

### 发布
- 版本升级至 0.6.0，并通过 `pkg.go.dev` 与 GitHub Release 发布。
