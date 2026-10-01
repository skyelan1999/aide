# `internal/server` 阅读索引

这里是 aide 的 Go 服务端 package。HTTP 路由、`App` 状态以及会话和任务能力在同一 package 内协作；新增能力时优先使用职责明确的文件名，不要将单个实现文件移进子目录制造新的 package 边界。

## 常见阅读路径

| 目标 | 建议入口 |
| --- | --- |
| 启动、依赖装配和 API 路由 | `server.go` → `cmd/aide/main.go` |
| 任务运行、工具循环和审批提案 | `workflow.go` → `tool_calls.go`、`command.go` |
| 模型请求与流式返回 | `provider.go` → `profiles.go`、`stream_broker.go` |
| 会话上下文和预算 | `context.go`、`context_*_test.go` |
| 文件访问、传输和来源引用 | `files.go`、`file_transfer.go`、`file_upload_*.go`、`sources.go`、`mcp_source.go` |
| 本地/SSH 工作区 | `workspace_config.go`、`ssh_session.go`、`ssh_keyutil.go`、`paths.go` |
| 插件协议与后台宿主 | `plugins.go`、`plugin_daemon.go`、`plugin_learning.go`、`plugin_host.js` |
| 设置、人格和配置迁移 | `persona.go`、`personality_evolution.go`、`config_backup.go`、`migration.go` |
| 语音助理与历史 | `voice_agent.go`、`voice_personality.go`、`voice_samples.go`、`assistant_agent.go`、`assistant_history.go` |
| 系统诊断和外部接口 | `system_log.go`、`debug.go`、`integrity.go` |
| 独立 TTS 合成实现 | `tts/` package |
| 浏览器端实现 | `web/`（由 `server.go` embed 并通过 HTTP 提供） |

测试文件沿用对应实现的能力前缀，例如 `provider_stream_test.go`、`file_upload_test.go`。查找行为时优先读实现和同名/同前缀测试，再查 [架构与接口索引](../../docs/architecture.md)。

## 拆分为子 package 的准入条件

提取子目录前先确认该能力可以通过少量导出类型和函数协作，不需要读取或修改 `App` 的私有字段；宿主协议或存储细节不应泄露为跨 package 的便利接口。满足后再移动实现与测试、调整依赖，并在 `docs/architecture/code-layout.md` 更新目录图。这样每个目录都表达真实的依赖边界，而不只是给大文件换一个位置。
