# aide 架构与接口索引

> 核对日期：2026-11-11；实现基线 `921a95a5`。本页是模块和接口导航，任务、上下文、权限、并行、文件与原生控制原理见署名 Skye Lan 的 [Aide 技术白皮书](architecture/aide-technical-whitepaper.md)。旧截图只说明当时界面，不是本轮运行验收。

[软件模块说明](architecture/modules.md)列出管理提案、文档绑定、页面助理、消息和 TTS 的依赖；[功能说明](features.md)与[演示](demo-guide.md)对应真实入口。

> 第一次浏览仓库可先看[代码目录导航](architecture/code-layout.md)，再按下方模块职责进入具体实现。

项目引用的下载脚本远程目录及 SFTP 中继实现、读写边界与部署步骤见 [远程目录中继](reference-relay.md)（Issue #644，源码功能，发行状态见任务账本）。

> **启动配置以 `.env` 为准**：`start.command` → `scripts/aide.sh` → Docker Compose，统一读取 `AIDE_PORT`（默认 8097）和 `COMPOSE_FILE`。临时验收端口不是用户启动入口。目录范围和 macOS 共享根模式见 [工作目录配置](workspace-paths.md)。

Aide 由 Go 服务、嵌入式浏览器 UI、工具运行时及按平台配置的原生桥接组成。浏览器和桌面壳共享后端，模型回答、工具执行、文件应用和实际验收分别留痕。最新公开程序包为 [v0.1.18.0-RC3](https://github.com/skyelan1999/aide/releases/tag/v0.1.18.0-RC3)（2026-11-02）；后续主干能力不据此视为已进入该包。发行与运行身份见 [HANDOVER](../HANDOVER.md)，历史验收见 [验证记录](verification.md)。

超长会话、摘要压缩、项目记忆及工业使用边界见[超长对话与工业复杂任务支持说明](industrial-long-conversation-support.md)。

## 系统结构

```mermaid
flowchart TB
    Browser[浏览器：会话 / 文件 / 设置 / 轨迹] --> API[Go HTTP API：令牌与同源校验]
    API --> Runs[任务与上下文构造器]
    API --> Workspace[工作区身份 / 本地与 SSH 驱动]
    API --> Storage[会话 / 设置 / 来源 / 统计]
    Runs --> Provider[Responses / Chat Completions 适配器]
    Runs --> Tools[内置工具 / Node 插件宿主]
    Tools --> Proposals[待批准文件与命令提案]
    Proposals --> Approval[人工或辅助审批及执行前重查]
    Approval --> Workspace
    Workspace --> Mounts[/workspace · /context · /local]
    Storage --> Data[/data 持久卷]
```

Go 标准库 HTTP 单体；模型步骤在 goroutine 中执行，文本 token 通过 SSE（`GET /api/sessions/{id}/runs/{run}/events`）增量推送到浏览器，最终任务状态仍由 `GET /sessions/{id}` 持久化兜底。前端使用原生 JS/CSS 和本地 vendor Markdown 库，由 `go:embed` 编入二进制。Node 插件宿主是可信代码执行器，不是权限沙箱。

## 模块职责

OpenAI 官方端点（`api.openai.com`）在手动和自动模型选择下均自动适配请求格式，不发送其他提供商的 `thinking` 扩展。Chat Completions 路径使用 `reasoning_effort` 和 `max_completion_tokens`；“自动”省略推理强度，“低/中/高”对应 `low/medium/high`。GPT-6.1 Sol 与 GPT-6 Astra 的界面选项为自动、低、中、高、超高（`xhigh`）、最高（`max`），不显示关闭；GPT-6 Sol/Luna 还支持关闭。`/api/config` 的模型条目通过 `reasoningEfforts` 提供档位，界面按所选模型显示，切换模型保留仍支持的档位，否则回到自动（模型默认强度）。历史直接请求关闭时，Sol/Astra 仍兜底为 `low`。显式标记不支持推理的模型不发送推理参数；其他提供商继续使用既有协议。官方 GPT-6 系列请求自动使用 Responses API（`reasoning.effort`、`max_output_tokens`），以支持推理与工具调用共存；流式输出、工具结果和用量统一映射回现有内部协议。请求设 `store:false`，工具轮次保留并重放加密推理输出；其他 Chat Completions 提供商不接收这些协议元数据。

自定义 OpenAI Chat Completions 提供商默认保留原有参数格式。明确支持 `reasoning_effort` 的端点可在模型设置中选择 OpenAI 或 Codex Bridge 格式；两者的 `auto` 均省略字段，`minimal/low/medium/high/xhigh/max/ultra` 原样传递。OpenAI 格式把 `off` 编码为 `none`；Codex Bridge 格式保留 `off`，让桥接服务查询模型能力并选择最低可用档（上游未报告能力时回退到 `low`）。两种显式格式均使用 `max_completion_tokens`。显式标记不支持推理的模型仍不发送该字段。

| 文件 | 责任 |
| --- | --- |
| `cmd/aide/main.go` | 启动与退出 |
| `internal/server/server.go` | 路由、鉴权、配置、会话存储、统计与费率、构建身份 |
| `workflow.go` | 任务状态、工具循环、提案、应用、摘要压缩 |
| `context.go` | 上下文预览、预算拦截、请求快照 |
| `provider.go` / `provider_responses.go` | 提供商协议适配、请求、参数、usage、超时及流式输出 |
| `profiles.go` | 内置/用户 Profile、策略文件、参数校验 |
| `workspace_config.go` / `ssh_session.go` | 工作区身份、本地映射、SSH/SFTP 生命周期 |
| `files.go` / `sources.go` / `mcp_source.go` | 路径/内容策略、读写冲突、引用来源与受限 stdio / Streamable HTTP MCP 驱动 |
| `knowledge_map.go` / `knowledge_updates.go` / `knowledge_sources.go` | 多来源有界知识索引、来源身份重查、版本差量与文件／代码缓存 |
| `knowledge_code.go` / `code_analysis/` | Go／JavaScript／Python AST与静态调用候选 |
| `knowledge_documents.go` / `document_analysis/` | 文档原文提取、本地TF-IDF RAG与原文指纹回填 |
| `web/starmap*` | 独立知识星图、自然尺度、代码探索和自动增量绘制；直接进入，无入场过场 |
| `command.go` | 本地/SSH 命令、NDJSON 输出、超时与取消；SSH 交互 PTY 输入透传原始按键字节 |
| `persona.go` / `settings_persona_persist` | 性格系统：多人格（activePersona）、AES-256-GCM 密文（personaCiphers）、可演化 personalities、解锁/保存/重置 |
| `config_backup.go` | 配置备份信封 `aide-config-backup`：导出/导入设置快照、来源与工作区密钥、小秘历史 |
| `voice_agent.go` | 语音小秘：send/ignore/standby 研判、VoiceHistoryEntry 记录、AES-256-GCM 历史加解密、双向朗读 |
| `generated_documents.go` / `document_proposals.go` | 配置驱动的自动系统文档、来源绑定提案和发布前重查 |
| `management.go` / `web/management.js` | 四项管理权限、插件预检与引用变更人工队列 |
| `assistant_watch.go` / `assistant_agent.go` | 页面统筹的任务跟进、动作与授权代审 |
| `message_center.go` / `internal/messaging/` | 联系人、群组/通话邀请、消息与文件分块恢复 |
| `tts_catalog.go` / `tts/` | 百炼目录、合成适配与提供商边界 |
| `environment_guide.go` | 系统文档自动挂载与环境引导 |
| `plugins.go` / `plugin_host.js` / `management.go` | 插件登记、加载、schema 和 handler；在线替换见[插件协议](plugin-protocol.md#在线升级与恢复) |
| `web/settings-init.js` | 同步首帧外观、`aide.ui`、系统外观响应与跨标签同步 |
| `web/settings-schema.json` / `app.js` | 设置导航、控件与应用交互；SSE 流式渲染（rAF 批量、live 文本/光标/工具行、会话快照去重）；排队/插话/暂停（运行时输入框旁固定显示暂停控制，发送键保留停止操作） |
| `web/themes/**` / `style.css` / `macos.css` | 颜色变量、既有样式与新版表现层 |

文件名省略前缀时均位于 `internal/server/`。不维护容易过时的文件行数/测试数量，查当前源码与本次测试输出。

## 两种路由不能混淆

- 产品模型路由：`profiles.json` + `routing-policy.json`，选择模型参数 Profile。
- 开发 Agent 路由：`AGENTS.md` → `docs/agent/WORKFLOW.md` + `router.json`，管理需求到发布的开发过程，不参与用户模型任务执行。

## 数据与权限

会话写入 `/data/session-<id>.json`；模型连接配置、令牌、Token 统计/费率等也在数据卷。`profiles.json` 与模型策略位于工程目录。来源登记在 `/data/config/sources.json`，秘密在数据卷；UI 外观在浏览器 `localStorage['aide.ui']`。

Compose 将工作区可写挂载 `/workspace`，参考资料只读挂载 `/context`；`/local` 映射由宿主浏览配置决定，可能包含较广的宿主目录。`os.Root` 和路径检查约束文件 API，但手动命令/Node 插件仍具有容器用户可访问的目录权限。插件 helper 当前读取 `/workspace`，不能把它等同于所有远程工作区的统一文件驱动。

任务快照记录工作区身份；旧提案、编辑器保存需要通过身份和哈希检查。文件应用为逐文件原子替换，可能部分成功。压缩用结构化摘要与近期历史构造后续请求，不保证删除 Runs 或缩小磁盘。

工作流节点的文件绑定还在实际文件打开与审批替换阶段固定目录句柄、复核文件身份，约束随提案保存；边界与验证范围见[工作流文件绑定](architecture/workflow-file-bindings.md)。

文件管理的本地/SSH大文件操作使用持久分块传输中心；分块校验、恢复、磁盘与浏览器兼容边界见[文件传输架构](architecture/resumable-file-transfers.md)。

上下文完整计量、工具交换边界、运行中整理与流中断恢复见 [上下文恢复](architecture/context-recovery.md)。

上下文预检按模型窗口与输出预留计算预算。历史接近动态阈值时会在任务结束后压缩；自动与手动压缩都通过会话级进度状态向侧栏上下文卡报告待整理条数、摘要 map/reduce 当前批次、稳定开始时间、持续耗时、折叠保存阶段及压缩前后活动上下文估算。自动压缩将每个摘要批次同步写入当前任务 SSE 与会话进度接口；手动压缩在请求期间轮询同一状态接口。前端按会话 ID 缓存该状态，用户切换会话时只显示当前会话进度，切回仍在进行的会话可恢复动画和耗时；完成、失败、取消都作为独立终态短暂展示后清除。取消信号会在每个 map/reduce 批次边界检查；提供方请求遵循请求上下文取消，任何结果只会在完整摘要生成且快照校验通过后原子写入，因此取消不会折叠原历史。摘要提取单独关闭隐藏推理，并使用有界输入、输出预算，把生成额度留给结构化摘要；摘要条目数量和长度也受提示约束。若新请求预检仍超限，则先对旧消息生成摘要、保留近期原文并重算预算，再决定是否接受。摘要调用失败、会话在压缩期间变化或压缩后仍超限都会返回错误，不会发送超预算请求。模型 SSE 流在连续 3 分钟没有有效 JSON 数据事件时会取消请求并给出可重试错误；纯传输层注释心跳不会刷新此计时。此空闲限制与单请求 2 小时总上限、聊天任务活动预算分开。项目记忆和工作流建档使用配置的项目缓存目录；SSH 工作空间选择远端缓存时，经 SFTP 拉取与写回，并在数据卷保留工作副本。默认缓存仍是本机缓存；应用会话、凭据及文档提取缓存不会随项目缓存设置一起迁移。完整配置边界见 [SSH 与存储配置](architecture/ssh-storage.md)。

工作台对思考事件仅保留有界文本，超出 131,072 个字符时保留开头和末尾片段。思考区默认折叠；折叠时只更新一行预览，不向隐藏正文节点逐 token 累积文本，展开后才装载已保留记录。最终回答沿用增量 Markdown 扫描与稳定块渲染。超长未闭合块以纯文本显示，活动区记录起始偏移并只复制新到达的尾部；稳定区同样仅复制新闭合范围，避免逐帧复制累计前缀或活动尾部。活动区偏移改变、文本回退时重建节点以保持内容正确。该路径减少长流期间的重复 DOM/Markdown 工作，但最终回答仍须进入浏览器 DOM，不能视为无上限或跨设备性能保证。


上下文卡按工作区与会话隔离缓存，只在成功响应后提交缓存键。切换期间先清空旧值，过期响应不得覆盖当前会话；同键请求合并，失败后可重试。草稿预算不会复用为会话基线，清空输入后重新获取基线。

The context card scopes its cache to the workspace and session and commits cache keys only after successful responses. Switching clears stale values; late responses cannot overwrite the current session. Pending requests are deduplicated and failures can retry. Draft budgets are invalidated before returning to the session baseline.


运行中插话使用与初始输入一致的用户气泡，按保存顺序显示在该任务输出、工具记录和错误信息之后，避免长回复将插话留在任务顶部。仍在排队的输入只出现在队列条，提升为插话后才显示气泡；重复文字按独立输入保留。会话刷新指纹显式包含所有可见任务的插话内容与排队状态，不依赖 updated 或消息数量改变。用户插话仍按原始保存顺序展示，正文阶段不推断插话的精确相对位置。

Accepted in-run interjections use the same user bubble as the original prompt and appear at the tail of their run's rendered output. Queued items remain in the queue until promoted. Repeated text remains separate input. Session snapshots include interjection content and queue flags so promotion or edits trigger rendering even without timestamp/count changes. Interjections keep their saved order; response segments do not invent exact interjection timing.

Git 文件比较接口见 [Markdown 编辑与文件历史](architecture/markdown-editing-export.md#git-文件历史比较--git-file-history-comparison)。本地仓库按所选文件读取提交／暂存／磁盘版本，不修改仓库；历史窗口可切换快照与Git比较，草稿／磁盘口径明确区分；实际8097部署验收仍独立记录。

## 多来源知识与独立产品主页

知识星图、原文检索、本地TF-IDF RAG及编号查看共用有界只读来源适配层；本地／Skill、SSH工作区、自动系统文档、独立SFTP、HTTP／HTTPS、FTP／FTPS、SMB按来源分区，MCP只索引已发现的工具说明。节点、检索结果和回填保留来源编号、名称、类型及当前身份；改配、停用和移除后拒绝旧引用。远端来源约30秒缓存、最多3个并发扫描，默认800文件／200目录预算按启用区域分配；截断、不可用及未核实状态与真实节点数量分开。HTTP／SMB单资源，不递归爬取；格式、读取、查看和插件的详细边界见[知识星图架构](architecture/knowledge-map.md)。

`site/` 是独立产品主页，仅静态 HTML/CSS/JavaScript 与本地素材，不读取会话、调用模型或接入应用 API；不经 Go `embed` 的工作台路由提供。`.github/workflows/pages.yml` 仅手动发布 `site/`，提交或推送不触发部署。主页预览、来源素材及发布操作见[主页说明](website.md)。

## API 索引

除静态资源、`/healthz` 外，`/api/` 路由需要 Bearer token；以 `server.go: Handler` 为完整注册表。

| 方法 | 路由 | 用途 |
| --- | --- | --- |
| GET | `/api/knowledge-map`、`/api/knowledge-map/updates` | 知识／代码快照、稳定编号与版本差量同步；详见[知识星图](architecture/knowledge-map.md) |
| POST | `/api/knowledge-map/assist` | 带编号和片段证据的知识／代码／RAG辅助理解 |
| POST | `/api/knowledge-map/documents/search`、`/api/knowledge-map/documents/reference` | 原文或RAG检索、回填前重查文件指纹与定位 |
| GET / PUT | `/api/knowledge-map/embedding-policy` | 工作区向量检索配置；密钥保存在保险库，默认关闭；配置版本冲突返回409 |
| GET | `/api/config` | 脱敏配置、version/revision/buildCommit |
| PUT | `/api/settings` | 模型连接与模型列表 |
| GET | `/api/models`、`/api/balance` | 提供商代理；支持程度依赖上游 |
| GET / PUT | `/api/profiles` | Profile 与当前策略 |
| GET / PUT | `/api/workspace-config` | 工作目录、文档、缓存、远程连接 |
| GET / POST | `/api/capabilities`、`/api/capabilities/check` | 能力元数据与显式范围检查，见[能力仪表](architecture/capability-dashboard.md) |
| GET | `/api/approval-policy/rules` | 审批授权清单、精确身份与有效期 |
| PUT / DELETE | `/api/approval-policy/rules/{rule}` | revision 校验下更新有效期或单条撤销 |
| GET / PUT | `/api/sources` | 引用来源注册表 |
| PUT | `/api/sources/ai-visibility` | 按工作区保存来源 AI 可见性；保留手动浏览，不替代操作系统权限 |
| POST | `/api/sources/{id}/test` | 初始化 stdio MCP 并发现工具（仅保存安全摘要） |
| GET | `/api/files`、`/api/file` | 目录/文本读取 |
| GET | `/api/file/git-history` | 本地文件 Git 探测／最近提交／HEAD、提交、暂存区和磁盘的只读比较；参见[文件历史](architecture/markdown-editing-export.md#git-文件历史比较--git-file-history-comparison) |
| PUT | `/api/file` | 保存；包含路径、正文、哈希和工作区身份 |
| GET / POST | `/api/sessions` | 会话列表/创建 |
| GET | `/api/sessions/{id}` | 会话、消息与任务 |
| POST | `/api/sessions/{id}/runs` | 启动对话或工作流 |
| POST | `/api/sessions/{id}/runs/{run}/cancel`、`/apply` | 取消/应用 |
| GET | `/api/sessions/{id}/runs/{run}/requests` | 请求快照 |
| GET | `/api/sessions/{id}/runs/{run}/events` | SSE 实时事件（step/delta/tool/status/done）；EventSource 经 `?access_token=` 鉴权 |
| POST | `/api/context-preview` | 与运行请求共用构造器的预算预览 |
| GET | `/api/search` | 会话全文搜索 |
| POST | `/api/sessions/{id}/compact` | 手动摘要压缩 |
| GET | `/api/token-stats` | 用量与费用汇总；逐调用缓存命中/未命中、币种与价格快照；`costByCurrency` 分币种汇总，兼容字段 `cost` 只计 CNY |
| GET / PUT | `/api/token-pricing` | 官方 DeepSeek/OpenAI 费率预览；可编辑自定义费率与 CNY/USD 币种 |
| GET / POST | `/api/plugins` | 插件列表/上传 |
| PUT / DELETE | `/api/plugins/{id}` | 启停/删除 |
| GET | `/api/plugin-surface` | 插件能力清单 |
| POST | `/api/command` | 命令执行，NDJSON 输出 |
| POST | `/api/config/export`、`/api/config/import` | 配置备份导出/导入（信封 `aide-config-backup`，可选含密钥/语音历史） |
| POST | `/api/sessions/{id}/runs/{run}/retry`、`/answer`、`/queue/{index}` | 重试/回答澄清问题/排队项改删升级 |
| GET / POST | `/api/file/raw`、`/api/file/download`、`/api/file/archive`、`/api/file/extract`、`/api/file/rename` | 原始文件查看、文件/目录下载与 ZIP 导出、创建 ZIP/TAR/TAR.GZ/TAR.BZ2/TAR.XZ、解压受支持归档到同级新目录、重命名；归档创建拒绝覆盖同名文件，解压限 1,000 条与 64 MiB 并拒绝危险路径、链接和特殊文件；download/raw 允许 `access_token` 供浏览器资源请求使用 |
| PATCH / DELETE | `/api/sessions/{id}`、`/api/sessions/archived/all` | 改 pinned/archived/删除/清空归档 |
| POST | `/api/persona/*`（unlock/save/reset/GET）、`/api/personas/*`（GET/active）、`/api/personality/*`（GET/PUT/reset/evolve） | 性格加解密、多人格切换、性格演化 |
| POST | `/api/voice-filter`、`/api/voice-narrate` | 小秘研判 send/ignore/standby、双向朗读 |
| GET/DELETE/POST | `/api/voice-history`（list/clear/enable/unlock/lock/change-password/disable） | 小秘工作历史与加锁 |
| POST | `/api/account/verify-password` | 账户密码校验（Argon2id，兼容旧密码摘要；可提交锁屏解锁） |

这里是导航索引，不代替每个 handler 中的完整请求结构、验证与错误码。完整注册表以 `server.go` mux 注册为准。

## 任务、工具与执行环境

任务由 `workflow.go` 驱动模型/工具往返。文件读取在允许范围内执行；文件修改形成提案，命令按沙箱和审批规则执行。已有文件可通过附件或任务内完整读取形成一致基线，不能把只读节选当作完整版本。

普通新聊天按需使用异步执行环境：X 包含主环境和最多 X−1 个私有运行态。工作者可在私有源码目录执行工具、插件和测试；主环境继续有用工作，用 `environment_collect_ready` 收集先完成者，冲突和版本检查后登记普通待审批候选。1X 不建立子运行态；SSH 等不适合私有快照的场景保留主路径。[执行环境设计](designs/execution-environments-20261101.md)说明批量入队、依赖、流式收尾、幂等收集和共享资源租约。

旧 `ParallelProgress` 分析检查点仍有无工具工作者和每用户范围三轮规划预算；这是[兼容分析路径](architecture/adaptive-parallel.md)，不是新环境调度的统一限制。子环境完成、候选已登记、文件已应用与集成已验证是四种状态。

任务保存停止/暂停意图、检查点与工具结果。用户插话、排队、审批等待和显式恢复不应互相替代；中断后不自动重放结果不明的外部动作。轨迹记录输入、模型请求、工具和审批，用实际事件表达顺序，不从墙钟跨度推算执行时间。

## 上下文、文件与应用

`context.go` 共用于预览与运行请求构造；每个模型请求有自己的窗口和输出预留，累计 Token 不是一次上下文。摘要降低活动历史，不能保证磁盘历史缩小或细节无损。大 Markdown 共享 16 KiB 内联预算；超出后使用来源、路径、长度、SHA-256 与有限节选，由 `read_file` 分段获取正文。详见[上下文恢复](architecture/context-recovery.md)及[大附件机制与验收](reviews/2026-11-09-large-markdown-attachments.md)。

`write_file`、编辑器和插件文档绑定的版本通道区分当前内容与目标身份。单文件替换不代表整个多文件/命令/远端任务的事务；部分成功应按实际回执与读回核对。[工作流文件绑定](architecture/workflow-file-bindings.md) · [文件处理插件](architecture/plugin-file-handlers.md)。

## 审批、浏览器与电脑控制

辅助审批覆盖符合条件的文件、命令、网页与电脑单次操作；审核结果计入用量并留痕。它不扩充持久应用/站点授权，不替代工作区权限和操作系统授权；执行前还要检查目标、版本、取消及环境状态。管理插件/引用等独立管理队列继续按各自规则确认。

电脑控制内置于设置，支持应用定向检查、窗口截图、视觉定位、点击、拖拽、滚动和组合键。图像定位引用绑定任务、目标和窗口，当前有效期两分钟。平台支持时可后台操作；共享实体输入仍有前台与资源调度约束，虚拟焦点不是独立桌面。[控制架构与平台边界](plugins/computer-browser-control.md)。

## 界面、原生壳与语音

Go 嵌入前端；浏览器与原生壳共享会话和任务。设置支持现代、专业、舒适与经典配色、明暗主题、背景及快捷键绑定。背景偏好为 `off | aurora | studio | modern | local`，动态效果跟随系统减少动态效果，背景遮罩强度为独立偏好。[外观说明](builtin-background.md)。

macOS 的 `modifier_gesture.swift`、`app_capture.swift` 实现可选双修饰键捕获前台窗口。PNG 和应用上下文经一次性票据绑定草稿，不自动发送。它依赖原生系统权限，浏览器页面不提供同等全局监听。

朗读、转写、双向语音通话、助理意图与消息邀请是不同流程。提供商连接、设备权限、播放与打断各有状态，设置可选不等于实际声音已经验证。[语音架构](architecture/assistant-openai-speech.md) · [消息架构](architecture/decentralized-messages.md)。

## 专题导航

| 主题 | 说明 |
| --- | --- |
| 技术全貌 | [技术白皮书](architecture/aide-technical-whitepaper.md)、[模块索引](architecture/modules.md)、[代码目录](architecture/code-layout.md) |
| 执行与恢复 | [执行策略](architecture/execution-policy.md)、[上下文恢复](architecture/context-recovery.md)、[任务成果核验](architecture/task-artifact-delivery.md) |
| 文件与数据 | [数据布局](architecture/data-layout.md)、[分块传输](architecture/resumable-file-transfers.md)、[Markdown 编辑导出](architecture/markdown-editing-export.md)、[SSH 存储](architecture/ssh-storage.md) |
| 插件与工具 | [协议](plugin-protocol.md)、[市场](architecture/plugin-market.md)、[文件查看与编辑](architecture/plugin-file-handlers.md)、[能力仪表](architecture/capability-dashboard.md) |
| 知识 | [多来源、静态代码与文档检索](architecture/knowledge-map.md) |
| 定时与统筹 | [提醒定时执行](architecture/reminder-schedules.md) |
| 运行与交付 | [启动器契约](architecture/launcher-contract.md)、[诊断](architecture/diagnostics.md)、[更新](architecture/software-updates.md)、[Release 验收](testing/release-validation.md) |

## 维护与历史

本次删除了本索引中重复的旧 UI 微调说明、旧运行实例故障播报、相互冲突的并行时间限制及“源码候选未部署”等过期当前状态。对应按日期的 [reviews](reviews/) 和 [tasks](tasks/) 保留原始结论；它们不因本次整理升级为新的测试通过记录。

本次是文档核对，不重建或另起服务；当前实例、真实模型、远端/跨平台与发布验收分别记录。详细清单见[文档维护报告](reviews/2026-11-11-aide-docs.md)。

### MapCanvas public map workbench

MapCanvas imports YAML + PGM/PNG through visible source APIs and binds only the original raster to the versioned document channel. Its specific workbench method and source-argument exception are restricted to the canonical mapcanvas ID/tool names; read-only sources may be edited/exported but not overwritten. The trusted host Save action retains scope/version/permission/atomic replacement checks. Marketplace clients require map-image-document-v1, separating the new source host from older RC3 binaries. See [MapCanvas acceptance](reviews/2026-11-11-mapcanvas.md).
