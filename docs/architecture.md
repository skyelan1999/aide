# aide 架构与接口索引

> **启动配置以 `.env` 为准**：`start.command` → `scripts/aide.sh` → Docker Compose，统一读取 `AIDE_PORT`（默认 8097）和 `COMPOSE_FILE`。临时验收端口不是用户启动入口。目录范围和 macOS 共享根模式见 [工作目录配置](workspace-paths.md)。

核对日期：2026-09-23。本次交付目标 0.1.6.0 RC4；aide 融合 AI 与 IDE，让人更专注于专业工作；架构采用 Go、浏览器与 Docker，支持按场景扩展。历史验收保留于 verification.md，发布身份由 tag 与镜像记录确认。

## 系统结构

```mermaid
flowchart TB
    Browser[浏览器：会话 / 文件 / 设置 / 轨迹] --> API[Go HTTP API：令牌与同源校验]
    API --> Runs[任务与上下文构造器]
    API --> Workspace[工作区身份 / 本地与 SSH 驱动]
    API --> Storage[会话 / 设置 / 来源 / 统计]
    Runs --> Provider[Chat Completions 提供商]
    Runs --> Tools[内置工具 / Node 插件宿主]
    Tools --> Proposals[待批准文件与命令提案]
    Proposals --> Approval[用户确认]
    Approval --> Workspace
    Workspace --> Mounts[/workspace · /context · /local]
    Storage --> Data[/data 持久卷]
```

Go 标准库 HTTP 单体；模型步骤在 goroutine 中执行，文本 token 通过 SSE（`GET /api/sessions/{id}/runs/{run}/events`）增量推送到浏览器，最终任务状态仍由 `GET /sessions/{id}` 持久化兜底。前端使用原生 JS/CSS 和本地 vendor Markdown 库，由 `go:embed` 编入二进制。Node 插件宿主是可信代码执行器，不是权限沙箱。

## 模块职责

| 文件 | 责任 |
| --- | --- |
| `cmd/aide/main.go` | 启动与退出 |
| `internal/server/server.go` | 路由、鉴权、配置、会话存储、统计与费率、构建身份 |
| `workflow.go` | 任务状态、工具循环、提案、应用、摘要压缩 |
| `context.go` | 上下文预览、预算拦截、请求快照 |
| `provider.go` | 模型请求、参数、usage、超时；complete（非流式）+ completeStream（SSE，400 时去 stream_options 重试） |
| `profiles.go` | 内置/用户 Profile、策略文件、参数校验 |
| `workspace_config.go` / `ssh_session.go` | 工作区身份、本地映射、SSH/SFTP 生命周期 |
| `files.go` / `sources.go` | 路径/内容策略、读写冲突、辅助资料驱动 |
| `command.go` | 非交互 shell、NDJSON 输出、超时与取消 |
| `plugins.go` / `plugin_host.js` | 插件登记、加载、schema 和 handler |
| `web/settings-init.js` | 同步首帧外观、`aide.ui`、系统外观响应与跨标签同步 |
| `web/settings-schema.json` / `app.js` | 设置导航、控件与应用交互；SSE 流式渲染（rAF 批量、live 文本/光标/工具行、会话快照去重）；排队/插话（Codex 风格队列条、发送/停止同键、一键回底） |
| `web/themes/**` / `style.css` / `macos.css` | 颜色变量、既有样式与新版表现层 |

文件名省略前缀时均位于 `internal/server/`。不维护容易过时的文件行数/测试数量，查当前源码与本次测试输出。

## 两种路由不能混淆

- 产品模型路由：`profiles.json` + `routing-policy.json`，选择模型参数 Profile。
- 开发 Agent 路由：`AGENTS.md` → `docs/agent/WORKFLOW.md` + `router.json`，管理需求到发布的开发过程，不参与用户模型任务执行。

## 数据与权限

会话写入 `/data/session-<id>.json`；模型连接配置、令牌、Token 统计/费率等也在数据卷。`profiles.json` 与模型策略位于工程目录。来源登记在配置的缓存路径，秘密在数据卷；UI 外观在浏览器 `localStorage['aide.ui']`。

Compose 将工作区可写挂载 `/workspace`，参考资料只读挂载 `/context`；`/local` 默认是可写 HOME。`os.Root` 和路径检查约束文件 API，但手动命令/Node 插件仍具有容器用户可访问的目录权限。插件 helper 当前读取 `/workspace`，不能把它等同于所有远程工作区的统一文件驱动。

任务快照记录工作区身份；旧提案、编辑器保存需要通过身份和哈希检查。文件应用为逐文件原子替换，可能部分成功。压缩用结构化摘要与近期历史构造后续请求，不保证删除 Runs 或缩小磁盘。

## API 索引

除静态资源、`/healthz` 外，`/api/` 路由需要 Bearer token；以 `server.go: Handler` 为完整注册表。

| 方法 | 路由 | 用途 |
| --- | --- | --- |
| GET | `/api/config` | 脱敏配置、version/revision/buildCommit |
| PUT | `/api/settings` | 模型连接与模型列表 |
| GET | `/api/models`、`/api/balance` | 提供商代理；支持程度依赖上游 |
| GET / PUT | `/api/profiles` | Profile 与当前策略 |
| GET / PUT | `/api/workspace-config` | 工作目录、文档、缓存、远程连接 |
| GET / PUT | `/api/sources` | 辅助资料注册表 |
| GET | `/api/files`、`/api/file` | 目录/文本读取 |
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
| GET | `/api/token-stats` | 用量与费用汇总 |
| GET / PUT | `/api/token-pricing` | 费率管理 |
| GET / POST | `/api/plugins` | 插件列表/上传 |
| PUT / DELETE | `/api/plugins/{id}` | 启停/删除 |
| GET | `/api/plugin-surface` | 插件能力清单 |
| POST | `/api/command` | 命令执行，NDJSON 输出 |

这里是导航索引，不代替每个 handler 中的完整请求结构、验证与错误码。

## 任务与工具

`running → completed / awaiting_approval / failed / cancelled`；待审批提案应用后 completed；服务重启将运行任务标记 interrupted。

规划、提案、审查每个阶段可进入模型工具循环，当前循环最多 10 轮。`list_files`/`read_file` 直接读，`write_file`/`run_shell` 产生提案。工具结果回传模型；已有文件写入仍需显式附件快照。插件参数 schema 进入工具声明，但可信插件本身能直接使用 Node 能力，不能声称写操作都被安全沙箱阻止。

## 界面设置

```json
{"version":1,"theme":"system","palette":"blue"}
```

`theme` 为 light/dark/system，`palette` 为 blue/green。页面 `data-theme` 是解析后的明暗，`data-theme-pref` 是用户选择，`data-palette` 是风格。专业与经典各一排三项，通过 `aideUI.setAppearance()` 一次保存组合。旧 `aide.theme` 和中间版本 classic 偏好迁移；设置 schema 还包含消耗统计、模型参数、关于。

## 验证与扩展

变更前读取 [统一开发工作流](agent/WORKFLOW.md)。新前端须验证浏览器实际交互；正式发布须在没有 `/web` 挂载的构建镜像中验证 embed 资源。早期预览使用 RC5 后端 + 工作区静态文件；本次发布另行验证无静态目录覆盖的镜像。

待独立规划：PTY、目录分页、会话归档、真实 MCP、统一插件文件驱动。不要把登记入口或预设名称当成这些能力已经存在。SSE 已落地。
