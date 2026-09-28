# 关键代码定位清单（静态阅读证据）

> 基线：分支 `feature/permission-panel` @ `f9c4e02`（工作树干净）。
> 方法：仅静态阅读 + 只读 Grep/Glob/Read，未运行 docker / 未重启容器 / 未编译。
> 「验证状态」分两档：**[测试]** = 有对应 `*_test.go` 或提交信息声称已跑；**[代码]** = 代码符号存在但本次未做运行时验证（运行时验证由并行 agent 负责）。

## A. 本报告重点结论的直接证据

### MM-05 插件感知训练（PRD 原标「待开始」→ 实为已交付）
- `internal/server/plugin_learning.go:3` 头注释即写明 `MM-05 插件感知训练（plugin-aware learning）`。
- `plugin_learning.go:72` `pluginCapabilityHint()`：从插件 surface 生成「能力域→工具」系统提示段。
- `plugin_learning.go:285` `pluginExperienceHint()`：生成「插件使用经验」段；`:193 recordPluginExperience` 落盘 `memory/core/plugin-experience.md`。
- 接入点 `internal/server/context.go:278,281`：构建系统提示时注入上述两个 hint。
- 调用点 `internal/server/workflow.go:1144`：每次插件工具调用后 `recordPluginExperience(...)`。
- 测试 `internal/server/plugin_learning_test.go`（覆盖 hint/记录/裁剪）。**[测试]**
- 结论：代码真实存在、已接入、有单测；PRD §6 M6 行「待开始」过时。

### TR-09 SSE 顺滑排查（PRD 原标「待排查优化」→ 后端已就绪，长回答顺滑性待实测）
- 路由 `internal/server/server.go:998` `GET /api/sessions/{id}/runs/{run}/events → a.runEvents`。
- 实现 `internal/server/workflow.go:3364 runEvents`：
  - `:3388-3391` 设置 `Content-Type: text/event-stream`、`Cache-Control: no-store`、`X-Accel-Buffering: no`（消解反向代理缓冲）。
  - `:3394-3398 writeEvent`：每条事件 `json` 写出后立即 `flusher.Flush()`。
  - `:3414-3422` 15s 心跳 `: ping` 防中间代理静默断连。
- 命令流同款即时 flush：`internal/server/command.go:27-29,41-43`（每条 output 事件后 `f.Flush()`）。
- 结论：后端 flush 及时性 + 代理缓冲头已落地；「逐字顺滑不攒块」属运行时体验，需长回答 E2E 实测（见 OQ-08），本次静态不能判定。→ 部分交付/待验证，而非「未动工」。

### draw.io 前端联动 TL-11（PRD §6.2 标「待补全」→ 大部分已补全，缺 .svg/.png 关联）
- 实现 `internal/server/web/app.js:3547 setupDrawioFrame()`：
  - `:3550` iframe 指向**本地 vendor** `/vendor/drawio/?embed=1&proto=json&spin=1`（注意：并非 PRD 正文 §3 TL-11 所写的 `embed.diagrams.net`，见不一致项）。
  - `:3559-3564` 15s 加载超时 → 显式失败提示（不白屏）。✅ 失败提示
  - `:3570-3573` 等 `msg.event==='init'` 后再 `postMessage({action:'load', xml})`；`xml` 为空时加载空白 `<mxfile>`。✅ init 握手修正 + 空白画布/手画
  - `:3574-3579` 监听 `msg.event==='save'`，回传 `newXml` 并回 ack。✅ 保存回写
- 调用点仅两处：`app.js:1342`（编辑器模态 preview）、`app.js:4847`（`/file-view` 文件视图）。
- 缺口：
  - 扩展名判断 `app.js:1289` 与 `:4786` 均为 `/\.drawio$/i`，**未匹配 `.drawio.svg` / `.drawio.png`** → PRD §6.2 要求的「.svg·.png 文件关联」未实现。
  - 会话消息流内未见第三个 `setupDrawioFrame` 调用点（PRD §3 TL-11 期望「模态/文件视图/会话消息」三处）。
- 离线 vendor 声明：`app.js:1834` 关于页 `draw.io / diagrams.net v31.5.2 ... 内嵌静态 webapp，离线图表编辑`。

## B. 工具清单核对（M3）
- `internal/server/workflow.go:116 builtinTools` 起，`:117-138` 实际注册 **22** 个工具：
  `list_sources, list_files, read_file, mcp_call, docx_structure, docx_list_comments, docx_add_comment, docx_resolve_comment, write_file, run_shell, spawn_subagent, read_memory, write_memory, search_text, semantic_search, create_diagram, web_search, create_requirement, create_design, record_implementation, record_verification, ask_user`。
- PRD M3 表仅列 18 个编号（TL-01~18），且：
  - `mcp_call` 与 `write_file` 都编为 **TL-04**（重号）。
  - 4 个 docx 批注工具（`:121-124`，实现见 `comments.go`）**未登记进 PRD 的 111 点**。
- `search_text` required 已是 `["query"]`（`:130`，OQ-02 已关闭）。
- `semantic_search` 描述仍为 `"Semantic vector search by meaning"`（`:131`），而实现 `:2181 semanticSearch` 注释明写「离线 TF-IDF 余弦相似度（非向量 embedding）」，分词 `tokenizeTFIDF :2129`、停用词表 `:2120`。→ OQ-03 仍成立（命名误导未修）。

## C. 权限 / 沙箱 / 远程（M5、OQ-04/07）
- 手动命令端点 `internal/server/command.go`：
  - `:136 bash --noprofile --norc`，`:142` 受限 env，`:143 Setpgid`，`:144-149` 超杀进程组。
  - `:134` 60s 超时，`:127-133` 并发上限 4（满则 429），`:35-48` stdout/stderr 128KB 截断。
  - `:90,152 X-Accel-Buffering: no`。
- AI `run_shell` 在远程模式仍被拦：`internal/server/workflow.go:2420-2421`
  `if a.workspaceMode() == "ssh" { return ... "远程工作区模式暂不支持 run_shell 自动执行，请手动在终端运行" }`。
  → OQ-04 成立（注意：手动 `/api/command` 端点 `command.go:67-103` 反而支持 SSH `execRemote`，二者行为不一致，属已知边界）。
- PTY：全仓 `*.go` 无 `pty/PTY/WantTty/crelay` 命中 → OQ-07 成立（无交互 PTY）。
- 工具轮次：`server.go:54 ToolMaxRounds`，`:146` 默认 60，`:162-163` 越界归 60（AU-08）。
- 锁屏：`server.go:72 LockTimeoutSec`；密码 SHA-256/KDF 见 `kdf.go`（AU-09）。

## D. 开放问题落点
- OQ-01 retry 路由：`POST /api/sessions/{id}/runs/{run}/retry` 已注册（PRD 已标关闭）。
- OQ-02 search_text required：`workflow.go:130` 已为 `["query"]`（已关闭）。
- OQ-03 semantic_search 命名：`workflow.go:131` 描述未改 → **仍开放**。
- OQ-04 SSH run_shell：`workflow.go:2421` 仍拦截 → **仍开放（已知边界）**。
- OQ-05 streamable HTTP MCP：`mcp_source.go` 无 streamable/http transport 命中，仅 stdio → **仍开放**。
- OQ-06 reasoning 透传：`provider.go:85-91` 按 `ReasoningEffort` 注入 `thinking`/`reasoning_effort`（已关闭）。
- OQ-07 无 PTY：见上 → **仍开放（列入非目标）**。
- OQ-08 E2E + 容器重建实测：本次仅静态阅读，未运行；**仍待验证**。

## E. global-code-review.json 5 个 open_items 对账（对照 8 条 P2 提交）
| open_item | 对应提交 | 证据 | 判定 |
| --- | --- | --- | --- |
| #1 per-source SFTP 凭据并入 vault | `e38e8ca` P2#1 (backend-integrations-003) | `source_secrets_vault.go`（头注释直指 P2#1）+ `source_secrets_vault_test.go` | 已解决 |
| #2 plugin daemon 移出全局 a.mu | `aeb1ca2` P2#2 (backend-persona-004) | `plugin_daemon.go:81/88/93/109` 自带 `m.mu/p.statusMu/p.eventsMu`；`:158/:204` 注释「慢进程在 a.mu 锁外」 | 已解决 |
| #3 ask_user 陈旧应答竞态 | `afee71` P2#3 (backend-core-001) | `workflow.go:103 answerRound` 单调 nonce；`:2902-2908` 每轮新建 channel，注释「杜绝错轮应答被应用」；`workflow_clarify_test.go` | 已解决 |
| #4 save() 会话桶布局（archived/assistant 死目录） | 无对应提交 | P2#8 `f9c4e02` 仅改 `web/style.css` 2 行（消息流/输入区列宽对齐），**未触及 on-disk 会话存储目录** | 仍开放（P2#8 是 UI 列宽，不可据此勾选） |
| #5 --phase-* 令牌 / personaSave 长度上限 / 安全 P3 加固 | 无对应提交 | 8 条 P2 不含此项 | 仍开放 |

> 另 4 条 P2（#4 ssh 口令 `07ea3d6`、#5 阶段色 `722ef1b`、#6 docx anchorIndex `3e33c90`、#7 PDF 取消 `29df1cb`、#8 列宽 `f9c4e02`）已落地代码/提交，但不对应上述 5 个 open_item，留痕于提交历史。
