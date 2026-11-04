# 模块与插件异常定位

此功能用于回答“哪个模块、哪个插件版本、哪次任务、在哪一阶段失败”。它复用系统日志和已有诊断包，并增加有界的结构化事件记录；不改变授权、重试、任务结果、沙箱或插件返回值。

## 检测入口

| 范围 | 记录阶段 | 关联信息 |
| --- | --- | --- |
| 页面脚本/异步/资源与服务可达性 | 复用独立的 [崩溃监测](crash-diagnostics.md)，含初始化阶段 | 浏览器本机事件编号、源文件/行列、仅标签内的脱敏堆栈；不自动上传，不把断连误判成已证明的服务崩溃 |
| 已鉴权 HTTP API | `http`，包括文件、资料源、设置、更新、知识、消息、命令和任务模块；`fail` 的 4xx/5xx 及未经过 fail 的 5xx | 服务端生成的 requestId、HTTP 状态、模块；错误响应头给 incidentId |
| 请求 panic | `panic`；原有 HTTP 恢复和流中止语义保留 | 与原 panic 日志相同 incidentId、panic 类型、源代码位置 |
| 插件聚合 | `load-host` / `surface-decode` / `load-plugin` | 进程失败、surface 解码失败、单插件加载异常；可识别时包含 pluginId |
| 插件调用 | `call`，覆盖一次性 worker 和常驻 daemon 的错误、超时、取消、结果读取/解析及工作区失效 | pluginId、实际注册版本、tool、runId、workspaceId、requestId |
| 插件显式失败结果 | `result-status`：顶层 `ok:false`、`status:error/failed/crashed` 或非空字符串 error | RPC 可能成功但插件明确报告失败；只记录，不改返回结果，不猜测普通正文或深层引用数据 |
| 常驻插件 | `daemon-restart` / `daemon-heartbeat` | 非预期退出与心跳超时；保留原退避策略，不无条件重放调用 |
| 原生工具与命令 | `execute`，包括工具拒绝和 shell 非零退出/执行错误 | 工具名、任务/工作区、耗时；不保存命令、参数或 stdout/stderr |
| 模型请求 | `provider / stream` 与会话请求快照 | 每次 Provider 请求快照记录开始/结束、耗时、成功/失败/空响应状态及结构化错误分类；incident 关联同一任务/requestId，重试仍由现有执行逻辑决定 |
| 上下文压缩 | `context / summary` | 摘要失败，包括 JSON、网络、超时、取消；不保存对话、提示词或摘要正文 |
| 任务终态 | `workflow / task-failed` 等已有错误入口 | runId、可获得的 sessionId；保留原有 recentErrors |
| 原生执行边界 panic | `*-panic` | 尽力记录后按原语义重新抛出，不把未完成操作伪装成恢复成功 |

事件带 UTC 时间、独立 incidentId、请求/任务/工作区关联、模块与阶段、插件版本与工具名、错误分类、可重试提示、耗时、错误链**类型**、最多 8 个源代码位置，以及版本、构建提交与进程 instanceId。Node 插件异常还保留白名单错误类型、系统错误码（如 EIO/ENOENT）和包内相对文件/行/列，单次 worker、daemon RPC 和加载阶段都可回溯；不导出原始 JavaScript stack 或错误消息。同类来源/阶段/类型生成稳定 fingerprint，便于聚类；它不代表已经证明根因相同。

常见分类：`TIMEOUT`、`CANCELED`、`ACCESS_DENIED`、`STATE_CONFLICT`、`INVALID_REQUEST`、`INVALID_JSON`、`CONNECTION_FAILED`、`CONNECTION_REFUSED`、`DNS_FAILED`、`TLS_CERTIFICATE`、`NOT_FOUND`、`REPORTED_FAILURE`、`PANIC`、`OPERATION_FAILED`。取消是 info，权限/冲突/参数等通常为 warn；“retryable”只是诊断提示，不会自动再次执行有副作用操作。

## 查看与回溯

1. 在现有“设置 → 系统 → 系统日志”找到 `AIDE-INCIDENT`，取得 incident/request/run/module/plugin/code/fingerprint。日志时间与浏览器错误时间对应。
2. owner 访问令牌可读取 `GET /api/diagnostics/incidents`；支持 `incidentId`、`requestId`、`runId`、`module`、`pluginId`、`fingerprint`、`code`、`severity` 精确过滤，`limit` 为 1–256，结果按新到旧返回。鉴权与同源约束沿用普通 API；未鉴权请求不会填满事件库。
3. HTTP 错误返回 `X-AIDE-Request-ID`，可记录的已鉴权错误另返回 `X-AIDE-Incident-ID`。任务对象的 `diagnosticTraceId` 将启动请求与后台工具/模型事件关联；旧历史任务可能没有该字段。
4. 既有 `POST /api/debug/actions/diagnostic-bundle` 增加 `incidents`、`diagnosticHealth`、只含 ID/版本/启用状态的插件清单和 daemon 状态/崩溃次数，不改变独立调试令牌、开关、审计与管理面限制。导出前仍按原流程核对授权。
5. `GET /api/debug/sessions/{id}/runs/{run}/dataflow` 下载单次 run 的有界数据流 dump：Provider 请求状态、步骤状态、工具派发/结果事件、incident 与异常检测结果按时间汇总。工具调用 ID 在 dump 内替换为 `call-N`，参数/结果/正文只保留字节数，不导出内容。
6. 先看最早发生的底层插件/连接事件，再沿同一 requestId/runId 对照后续工具与任务失败；源代码位置与 commit 指向发生时的代码。不要把最后一次工作流失败直接认作第一根因。

## 保留与隐私

内存只保留最近 256 条；数据目录 `diagnostics/incidents.jsonl` 最多约 4 MiB 后轮转，另保留 `.1`、`.2` 两段。POSIX 文件为 0600、目录 0700；读取与写入都拒绝符号链接诊断目录及非普通日志文件，已有普通目录在写入时收紧权限。启动后首次查询/记录加载最近历史，进程重启不直接丢失已有文件；计数/原始文件不作为用户工作区内容。

Incident 不保存 raw error、用户正文、文件内容、命令、参数、URL、凭据或任意插件返回内容。数据流 dump 虽读取已有会话请求快照与工具执行日志，响应也只输出状态、时间、数量、字节数、结构化分类及脱敏事件元数据；不输出快照正文、提示词、工具参数/结果或原始 call ID。错误链只存类型，位置只含代码文件 basename/行号；不能据此还原原始输出。已有系统日志和 recentErrors 的脱敏规则继续有效。

诊断落盘失败不改变原操作结果；保留有界内存并记录 `AIDE-DIAGNOSTIC-PERSIST`。API 的 health 显示 `persistenceErrors`、`malformedRecords` 和 `droppedFromMemory`，不能把不可写目录或损坏历史静默当作“没有异常”。这些计数表示当前进程观察到的情况，不是假装持久化的全历史累计。

## 故障注入与限制

`internal/server/incidents_test.go` 覆盖真实插件抛错、显式失败结果、请求鉴权/关联、并发记录、重启读取、文件轮转与权限、持久化失败、压缩失败、取消分类、panic 语义与内容不泄露。日常关键 Go 回归包含这些用例，Release 完整 race/vet 再验证跨模块调用链。

此处是失败边界观测，不是完整分布式追踪或所有业务状态的语义判断。未知插件返回的任意文本、无失败标记的错误结果、外部硬件故障、宿主 OS 的强制杀进程/OOM、断电前未落盘数据，不能保证自动判断或完整保存。HTTP 已提交流遇 panic 仍按原逻辑中止；原生边界 panic 记录后仍可终止进程。Bridge 报告服务可达不等于系统授权正确，权限状态本身仍以对应宿主能力检查为准。

与发布测试矩阵的关系见 [Release 验收](../testing/release-validation.md)。故障注入中预期拒绝/取消可产生 warn/info，不能简单把“事件数不为零”当作整个 Release 失败；必需场景是否满足由其结果断言和完整收据决定。
