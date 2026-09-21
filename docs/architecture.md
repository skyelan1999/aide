# aide 架构

> 本文描述 aide 的**当前实现**（main 分支，PRD v1.3）。增量设计文档见 [`doc/architecture/`](../doc/architecture/)：主题系统（`2026-09-21-theme-switching.md`）、设置中心（`2026-09-21-settings-panel.md`）、模型参数 Profile 与策略路由（`2026-09-21-model-profiles.md`）。

## 设计

浏览器界面通过带 Bearer token 的同源 REST API 访问单个 Go 服务。服务以 `go:embed` 提供静态资源（含 `web/` 下递归子目录），不需要前端开发服务器。Docker 是工具执行环境，本地 bind mount 提供工作项目及只读上下文。

前端分三层：**令牌主题层**（`themes/{light,dark}/tokens.css`，79 个冻结语义令牌）→ **设置存储层**（`settings-init.js`，`<head>` 首位同步脚本，唯一事实源）→ **应用层**（原生 JS/CSS）。界面设置以 JSON 文档持久化于 `localStorage['aide.ui']`；设置面板由 `settings-schema.json` 数据驱动渲染。模型调用参数由 Profile（`profiles.json`）与策略（`routing-policy.json`）在服务端解析。

```mermaid
flowchart LR
    UI[浏览器工作台] --> HTTP[Go HTTP API / 鉴权]
    HTTP --> Sessions[会话与任务状态]
    HTTP --> Files[文件工具 / os.Root]
    HTTP --> Shell[命令运行器 / 超时与取消]
    HTTP --> Profiles[模型参数 Profile / 策略路由]
    Sessions --> Plan[规划]
    Plan --> Propose[生成文件提案]
    Propose --> Review[审查]
    Review --> Approval[用户应用]
    Approval --> Files
    Plan --> Model[兼容 Chat Completions 的模型]
    Propose --> Model
    Review --> Model
    Profiles --> Model
    Profiles --> Policy[profiles.json / routing-policy.json]
    Files --> Work[/workspace 可读写挂载]
    Files --> Ref[/context 只读挂载]
    Sessions --> Data[/data 持久卷]
    Shell --> Runtime[Docker 内 Go / Python / Node / Git]
```

## 模块边界

| 文件 | 责任 | 后续扩展 |
| --- | --- | --- |
| `server.go` | 路由、认证、配置、会话持久化、Profile 加载、关闭处理 | 用户与项目隔离、数据库存储 |
| `profiles.go` | 系统/用户 Profile、参数校验、`profiles.json` 持久化、auto 路由策略解析 | 策略表达式引擎、Profile 导入导出 |
| `provider.go` | 单次 Chat Completions 请求、Profile 参数透传、reasoner 参数剔除、超时与错误处理 | SSE 流式、工具调用协议 |
| `workflow.go` | 任务状态、上下文、规划/提案/审查、应用、策略解析与任务记录 | 可配置 DAG、工具注册、审批策略 |
| `files.go` | 根目录约束、文本大小限制、版本哈希、原子替换 | 精确 diff、文件搜索、补丁应用 |
| `command.go` | Linux shell 运行、输出流、并发上限、取消进程组 | PTY/WebSocket、命令日志与重放 |
| `server_test.go` / `profiles_test.go` | 14 个测试函数：鉴权、文件、设置、工作流、Provider、命令、恢复、Profile、路由 | — |
| `web/index.html` / `app.js` / `style.css` | 会话、任务、文件、模型设置、命令面板、设置面板（Glass UI）、策略按钮 | Monaco 编辑器、可视工作流画布 |
| `web/settings-init.js` | 界面设置 JSON 存储（`aide.ui`）+ 主题引擎（首帧前同步应用） | 更多设置字段 |
| `web/settings-schema.json` | 设置面板结构定义（数据驱动渲染） | 新增分组/控件类型 |
| `web/themes/{dark,light}/tokens.css` | 79 个语义令牌的暗/明两套取值 | 第 3/4 套方案（架构支持 ≤8） |

不为初版引入 Cordis 兼容层；DSH 是参考对象，aide 的运行时与代码独立。当前顺序工作流在 Go goroutine 中执行，无外部编排服务依赖。

## 数据与状态

会话单独存成 `/data/session-<id>.json`，先写临时文件、fsync，再 rename。状态包含用户输入、助手回答、任务、各步骤产物、文件提案、建议命令、应用标记，以及本次任务的 `strategy`/`profile`。`settings.json` 存储模型配置，`access-token` 保存本地访问令牌；权限均为 0600。

工程目录（工作区根）另有两份运行时可保存的 JSON：

- `profiles.json`：策略与用户 Profile（`{"version":1,"strategy":"manual"|"auto","activeProfile":"<id>","profiles":[…]}`）。3 个系统 Profile（`default`/`precise`/`creative`）**硬编码在 Go**，文件无法篡改；写入沿用原子 JSON。
- `routing-policy.json`（或 `routing-policy.md` 内 ```json 块）：auto 路由规则 `{rules:[{when:{mode|promptContains},use}],default}`，首个命中生效，无命中/文件缺失回落 `default`。

任务流转：

```text
running → completed                    对话或无文件方案
running → awaiting_approval → completed 有文件方案，由用户应用
running → failed / cancelled            模型错误、协议错误、停止、超时
running → interrupted                   服务重启后的恢复标记
```

每个会话最多一个运行任务，全服务最多四个模型任务；命令最多四个并发。模型请求最长两分钟，整个任务最长六分钟，命令最长一分钟。

Profile 参数在 `startTask` 时解析为 `ProfileParams`（temperature 0–2、top_p 0–1、max_tokens 1–8192、两个 penalty −2–2、response_format text/json_object、stop ≤16 项，LIM-22），随 `complete()` 合并进请求体；模型名含 `reasoner` 时自动剔除 temperature/top_p/penalty。`startTask` 缺省 strategy=manual、profile=default，行为与改造前一致。

工作流 JSON 格式：

```json
{"summary":"实现说明","files":[{"path":"relative/path.go","content":"完整文件内容"}],"commands":["go test ./..."]}
```

模型提供的 `baseHash`、`before`、`applied` 不可信，服务使用真实附件快照覆盖它们。已有文件必须包含在本次任务附件中；新文件必须尚不存在。用户应用前，服务重新验证内容哈希。应用是逐文件原子写入，进度逐文件持久化。

## 前端与设置

- **主题**：`settings-init.js` 在 `<head>` 首位同步执行（CSP 禁内联），首帧前写 `html[data-theme]`/`data-theme-pref`；`localStorage['aide.ui']` 为唯一 JSON 设置文档（旧键 `aide.theme` 自动迁移），`window.aideUI` 为 JSON 门面、`window.aideTheme` 为兼容门面；明/暗/跟随系统三态收纳在设置面板分段控件中。
- **设置面板**：品牌 logo 打开 Glass 面板（backdrop blur + 弹性缓动），内容由 `settings-schema.json` 驱动；新增控件类型只需注册渲染器（`controlRenderers`）。
- **策略按钮**：聊天栏左侧弹层选择 `auto` 或手动 Profile，选择即 PUT `/api/profiles`；发送任务时携带 strategy/profile，任务记录展示实际生效配置。

## API

除静态资源与 `/healthz` 外，所有 API 需要 `Authorization: Bearer <token>`。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/config` | 不含密钥的配置与运行信息 |
| PUT | `/api/settings` | 保存 API 地址、模型和密钥 |
| GET | `/api/profiles` | 合并列表（3 系统 + 用户 Profile）+ strategy + activeProfile |
| PUT | `/api/profiles` | `{strategy,activeProfile,profiles}`；系统 id 不可改；参数越界/未知 id 400 |
| GET | `/api/files?root=workspace&path=.` | 列目录；root 也可为 context |
| GET | `/api/file?root=workspace&path=README.md` | 读文本与版本哈希 |
| PUT | `/api/file` | `{path,content,hash}`；hash 为空仅允许新文件 |
| GET / POST | `/api/sessions` | 列出/创建会话 |
| GET | `/api/sessions/{id}` | 完整会话及任务状态 |
| POST | `/api/sessions/{id}/runs` | `{prompt,mode,attachments,strategy,profile}`；后两者可选，缺省 manual/default |
| POST | `/api/sessions/{id}/runs/{run}/cancel` | 取消任务 |
| POST | `/api/sessions/{id}/runs/{run}/apply` | 应用已审查的文件方案 |
| POST | `/api/command` | `{command,cwd}`，返回 NDJSON 输出与退出状态 |

附件格式为 `[{"root":"workspace","path":"README.md"}]`。只有工作区可写；辅助资料仅可读。运行模型调用需要显式用户任务；保存配置不会自动发出调用。
