# aide 架构

## 设计

浏览器界面通过带 Bearer token 的同源 REST API 访问单个 Go 服务。服务以 `go:embed` 提供静态资源，不需要前端开发服务器。Docker 是工具执行环境，本地 bind mount 提供工作项目及只读上下文。

```mermaid
flowchart LR
    UI[浏览器工作台] --> HTTP[Go HTTP API / 鉴权]
    HTTP --> Sessions[会话与任务状态]
    HTTP --> Files[文件工具 / os.Root]
    HTTP --> Shell[命令运行器 / 超时与取消]
    Sessions --> Plan[规划]
    Plan --> Propose[生成文件提案]
    Propose --> Review[审查]
    Review --> Approval[用户应用]
    Approval --> Files
    Plan --> Model[兼容 Chat Completions 的模型]
    Propose --> Model
    Review --> Model
    Files --> Work[/workspace 可读写挂载]
    Files --> Ref[/context 只读挂载]
    Sessions --> Data[/data 持久卷]
    Shell --> Runtime[Docker 内 Go / Python / Node / Git]
```

## 模块边界

| 文件 | 责任 | 后续扩展 |
| --- | --- | --- |
| `server.go` | 路由、认证、配置、会话持久化、关闭处理 | 用户与项目隔离、数据库存储 |
| `provider.go` | 单次 Chat Completions 请求、超时、错误处理 | Provider 接口、SSE、工具调用协议 |
| `workflow.go` | 任务状态、上下文、规划/提案/审查、应用 | 可配置 DAG、工具注册、审批策略 |
| `files.go` | 根目录约束、文本大小限制、版本哈希、原子替换 | 精确 diff、文件搜索、补丁应用 |
| `command.go` | Linux shell 运行、输出流、并发上限、取消进程组 | PTY/WebSocket、命令日志与重放 |
| `web/` | 会话、任务、文件、模型设置、命令面板 | Monaco 编辑器、可视工作流画布 |

不为初版引入 Cordis 兼容层；DSH 是参考对象，aide 的运行时与代码独立。当前顺序工作流在 Go goroutine 中执行，无外部编排服务依赖。

## 数据与状态

会话单独存成 `/data/session-<id>.json`，先写临时文件、fsync，再 rename。状态包含用户输入、助手回答、任务、各步骤产物、文件提案、建议命令及应用标记。`settings.json` 存储模型配置，`access-token` 保存本地访问令牌；权限均为 0600。

任务流转：

```text
running → completed                    对话或无文件方案
running → awaiting_approval → completed 有文件方案，由用户应用
running → failed / cancelled            模型错误、协议错误、停止、超时
running → interrupted                   服务重启后的恢复标记
```

每个会话最多一个运行任务，全服务最多四个模型任务；命令最多四个并发。模型请求最长两分钟，整个任务最长六分钟，命令最长一分钟。

工作流 JSON 格式：

```json
{"summary":"实现说明","files":[{"path":"relative/path.go","content":"完整文件内容"}],"commands":["go test ./..."]}
```

模型提供的 `baseHash`、`before`、`applied` 不可信，服务使用真实附件快照覆盖它们。已有文件必须包含在本次任务附件中；新文件必须尚不存在。用户应用前，服务重新验证内容哈希。应用是逐文件原子写入，进度逐文件持久化。

## API

除静态资源与 `/healthz` 外，所有 API 需要 `Authorization: Bearer <token>`。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/config` | 不含密钥的配置与运行信息 |
| PUT | `/api/settings` | 保存 API 地址、模型和密钥 |
| GET | `/api/files?root=workspace&path=.` | 列目录；root 也可为 context |
| GET | `/api/file?root=workspace&path=README.md` | 读文本与版本哈希 |
| PUT | `/api/file` | `{path,content,hash}`；hash 为空仅允许新文件 |
| GET / POST | `/api/sessions` | 列出/创建会话 |
| GET | `/api/sessions/{id}` | 完整会话及任务状态 |
| POST | `/api/sessions/{id}/runs` | `{prompt,mode,attachments}` |
| POST | `/api/sessions/{id}/runs/{run}/cancel` | 取消任务 |
| POST | `/api/sessions/{id}/runs/{run}/apply` | 应用已审查的文件方案 |
| POST | `/api/command` | `{command,cwd}`，返回 NDJSON 输出与退出状态 |

附件格式为 `[{"root":"workspace","path":"README.md"}]`。只有工作区可写；辅助资料仅可读。运行模型调用需要显式用户任务；保存配置不会自动发出调用。
