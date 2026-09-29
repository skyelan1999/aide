# Aide 全局代码分析报告（2026-09-28）

- **分支**：`feature/permission-panel`　**评审基线**：`190657f`（TR-09 SSE 顺滑性）
- **范围**：Go 后端全量（非测试约 22,978 行 / 58 文件；测试约 11,085 行 / 56 文件）+ 原生 JS 前端（`app.js` 7,324 行、`style.css` 1,028 行、`macos.css`、`locales/en.js`、`index.html`）
- **形态**：Go 单服务后端 + `go:embed internal/server/web` 进镜像 + Docker Compose
- **结论一句话**：未发现 P0 阻断缺陷；**10 条 P1 全部修复并验证**；25 条 P2 中 17 条已修、8 条记录待决策；67 条 P3 中 32 条已修、其余为建议项。后端并发纪律与安全基线扎实，前端经渲染/UI 专项走查后达到 Codex 同档水准。

---

## 1. 执行摘要

本轮按“静态全量审查 → 专家团交叉复核 → Playwright 全功能走查 → 渲染专项验证 → 深度 UI 走查 → 分级修复 → Docker 内验证 → 重建回归”的闭环执行。

- **静态审查**：8 位只读专家（后端核心并发 / 后端 HTTP·文件 / 后端集成凭证 / 后端语音·办公·插件 / 前端核心交互 / 前端渲染器 / 前端 CSS / 安全横切）+ 1 位测试失败复核。
- **运行验证**：Playwright 全功能走查（9 大项 / 22 检查点）、渲染专项（10 类查看器 + 42 语言代码块）、深度 UI（8 维度 × 三主题 × 三窗口尺寸 × 流式/空闲）。
- **关键防误报**：
  - `files.go:202-226`“跟随符号链接”经容器实测确认读取全程经 `os.Root`，逃逸软链返回 `path escapes from parent`、内容不泄漏 → **判定非缺陷，未改**。
  - 3 个非已知测试失败（迁移回滚 / vault 无密码 / 小秘门控）经 git 证据确认为**有意行为变更导致的测试漂移**，已同步测试断言跑绿，未误改正确代码。
  - mermaid/SQLite 两处 DOM 注入，因主 CSP 为 `script-src 'self'`（无 `unsafe-inline/unsafe-eval`）使注入脚本不执行，定为 P2 而非 P1，但仍做了深度防御修复。
- **修复过程中发现并修复 1 个自身引入的回归**：STL teardown 重构漏改 `geometry/stlGeometry` 两处引用导致 3D 整屏失败（render-validation-001），以及远程写冲突收紧后把“远端不存在的新文件”误判 409（d7ec2fb），均已修正并复核。

---

## 2. 缺陷统计（去重后唯一缺陷 102 条）

| 严重级 | 数量 | 已修 | 记录/建议 | 判定非缺陷 |
|--------|------|------|-----------|------------|
| **P0 阻断** | 0 | — | — | — |
| **P1 重要** | 10 | **10** | 0 | 0 |
| **P2 一般** | 25 | 17 | 8 | （另 1 条非缺陷计入 P2 位） |
| **P3 建议** | 67 | 32 | 35 | — |
| **合计** | **102** | **59** | **42** | **1** |

> 原始记录 106 条，含 4 条跨专家重复（mermaid、SQLite XSS、CSP 内联样式各被多位专家独立报告），去重后 102 条。完整逐行清单见 [`master-findings-table.md`](./master-findings-table.md)，机器可读见 [`master-findings-full.json`](./master-findings-full.json)。

按 8 大分类覆盖：逻辑错误 / 代码异常 / 并发与数据竞争 / 边界条件 / 错误处理 / 安全 / 资源泄漏（goroutine·文件句柄·锁·WebGL 上下文）/ UX 与交互逻辑。

---

## 3. P1 重要缺陷（10 条，全部已修）

| ID | 位置 | 缺陷与证据 | 修复 | Commit |
|----|------|-----------|------|--------|
| backend-integrations-001 | `ssh_session.go:394-405` | `ensureSourceSession` 把每来源私钥写 `/tmp/aide-src-<id>.key`、口令写 `.askpass`，全代码库无删除路径（主工作区有清理、来源路径漏了）；换源/删源后长期驻留 | 写后 `defer os.Remove`；新增 `killSourceSession`，在来源删除或 host/port/user/auth 变更时关闭旧 ControlMaster 并清理 | `2ab603c` `21b9084` |
| backend-integrations-002 | `mcp_source.go:105` | MCP stdio 子进程 `cmd.Env` 未设置，继承 `os.Environ()`，可读 `AI_API_KEY` 等模型密钥（SSH/sftp 都已裁剪 env，唯独 MCP 漏） | `mcpMinimalEnv()` 白名单（PATH/HOME/TMPDIR/LANG/LC_*/NODE_EXTRA_CA_CERTS） | `a533a82` |
| backend-persona-001 | `office.go:71-91` | `runOfficeScript` 用 `cmd.Output()` 跑 python3/soffice，无 context/超时；损坏 docx 转换死锁时 goroutine 永久挂起 | `exec.CommandContext` 60s + `Setpgid` + 进程组 SIGKILL + 有界输出（1MiB/64KiB） | `60f8046` |
| backend-http-001 | `server.go:2000-2017` | `getSession` 未校验小秘会话密码门，直接返回完整 Messages，绕过 `isAssistantUnlocked`（与 voiceFilter/assistant-message 不一致） | 未解锁只回元数据、`Messages=[]`，不合并小秘历史 | `94e2951` |
| backend-http-002 | `server.go:1989` | `deleteAllArchived` 仍按旧平铺 `dataPath/session-<id>.json` 删除，而 `save()` 已落 `sessions/active/`；磁盘残留、重启归档复活、计数虚报 | 改 `SessionPath(dataPath,id,"active")`，实测删除后磁盘消失、重启不复活 | `94e2951` |
| frontend-viewers-001 | `app.js:5450,5993` | Web Speech 兜底朗读 `onerror` 不区分 `canceled` 就继续 `next()`，锁屏 `ttsCancel()` 与“朗读→停止”都停不下来 | 识别 canceled/interrupted/aborted，置 stopped 终止朗读链，onend/next 加守卫 | `3e43415` |
| frontend-viewers-002 | `app.js:7279,7300,7307` | SQLite 查看器表名/列名/单元格 `String(val)` 零转义拼 innerHTML，外部 db 可存储型 XSS | 全部过 `escapeHtml()`（含错误信息） | `3e43415` `516527c` |
| frontend-viewers-003 | `app.js:3680-3708` | STL 查看器关闭/切文件时 rAF 循环、WebGLRenderer、ResizeObserver 全泄漏，反复打开耗尽 WebGL 上下文 | 统一 `teardownViewer`：cancel rAF、dispose renderer/geometry/material/controls、disconnect RO；切文件前调用 | `3e43415` |
| frontend-viewers-004 | `app.js:5112,5117-5118` | mermaid `securityLevel:'loose'` + 原始 SVG 经 innerHTML 直插，绕过 Markdown 消毒器，AI 产出可注入标签/交互 | 改 `securityLevel:'strict'`，正常 flowchart 不变、标签 HTML 被转义 | `3e43415` |
| render-validation-001 | `app.js:3767,3776` | **修复引入回归**：STL teardown 重构把几何体改名 `stlGeometry`，但这两行仍引用未定义裸 `geometry`，任何 .stl 都报 ReferenceError 整屏失败 | 两处 `geometry`→`stlGeometry`，复核 3D 正常 | `de6d83d` |

---

## 4. P2 一般缺陷（25 条：17 已修 / 8 记录）

### 4.1 已修复（17 条）

| ID | 位置 | 缺陷 | Commit |
|----|------|------|--------|
| backend-http-003 | `assistant_agent.go:88-92` | `va.memory` 解引用后才判 `va!=nil`，潜在 nil panic（调用方当前都提前判空） | `1a96648` |
| backend-http-004 | `files.go:1181-1197` | SSH 写文件用 readWorkspaceText 探测冲突，二进制旧文件被误判“不存在”，静默绕过哈希检测 | `add32c2` `d7ec2fb` |
| backend-persona-002 | `voice_samples.go:56-387` | `?profile=`/路径 id 直接拼 filepath.Join，无 safePath，`profile=../../etc` 可越目录读写删 `.enc` | `4f1d1f1` |
| backend-persona-003 | `persona.go:335-347` | `personaReset` 目标人格无密文时跳过密码校验，并 `a.personaKey=in.Password` 覆盖内存密钥 | `0756a12` |
| backend-persona-005 | `plugins.go:312-318` | `callPluginTool` 用 `CombinedOutput()` 无界缓存，坏插件 70s 窗口可狂写 GB 级输出致 OOM | `7ce08ce` |
| backend-persona-006 | `assistant_history.go:180` | `assistantMessageHandler` 未持锁读 `a.sessions` map，与 dispatch 写存在并发 map 读写 | `327a6b6` |
| frontend-core-001 | `app.js:1772` | 设置面板每次打开重复挂载 resize 监听与 aideUI 订阅，开关 N 次主题/resize 触发 N 次 | `3e43415` |
| frontend-core-003 | `app.js:3497` | `sanitizeHtml` 只匹配开头 `^\s*`，`java\tscript:` 等 scheme 内嵌控制字符可绕过 | `3e43415` |
| frontend-css-001 | `style.css:21-58` | 未定义令牌 `--faint`（只有 `--faintest`）被当颜色，折叠箭头/来源 chip 图标退化 | `7ae7652` |
| frontend-css-002 | `style.css` 多处；`macos.css:333` | 一批未定义令牌（--border/--bg-sub/--bg-hover/--accent-bg/--text-dim/--panel2 等）无 fallback，背景透明/边框 currentColor | `7ae7652` |
| frontend-css-003 | `style.css:289` | `.call-table .call-tool{color:#a5b4fc}` 特异性压过 `var(--accent)`，浅色对比约 1.9:1 不可读 | `bcf0123` |
| frontend-css-005 | `style.css:306` | `.win-preset.active` 白字压 --accent，green dark 浅绿底上约 1.5:1 | `c685608` |
| frontend-viewers-005 | `app.js:3797-3799` | PDF teardown 只挂 dialog close，file-view 页签/切文件不执行 `pdfDoc.destroy()/io.disconnect()` | `3e43415` |
| frontend-viewers-006 | `app.js:4343-4352,3591` | DXF/图片查看器每次打开新增永久 window mousemove/mouseup 监听 | `3e43415` |
| frontend-viewers-008 | `app.js:4986-4999` | 全局搜索无请求时序控制，快速输入旧响应覆盖新结果（加 searchSeq） | `3e43415` |
| render-validation-002 | `app.js:4676-4679` | Markdown fenced code 仅 JS/TS 高亮，40 种语言纯文本；改为对已注册语言走 `hljs.highlight` | `de6d83d` |
| runtime-functional-001 | apply 调用处 | apply 冲突 409 时按钮原样残留、无 toast；改为显式 catch toast + 按钮恢复 | `516527c` |

### 4.2 记录未修 / 需用户决策（8 条）

| ID | 位置 | 未修原因与风险 | 建议方案 |
|----|------|----------------|----------|
| backend-core-001 | `workflow.go:2924-2932` | ask_user buffered(1) 应答在“接收→复位状态”窗口内，二次提交的陈旧应答可能串到下一个澄清问题。触及 ask_user 核心状态机，擅改风险高 | 接收后在锁内清空/复位 AnswerCh 与 awaiting 状态，并对重复 answerTask 做幂等拒绝；建议专项处理 |
| backend-integrations-003 | `sources.go:74-79,190` | per-source SFTP 密码/私钥仍明文 `sources-secrets.json`，与已上 AES-256-GCM vault 的主工作区威胁模型不一致；属设计级改造 | Password/Key 改走 `vault.Put(sourceID 命名空间)`，同步改备份信封/factory_reset/导入导出 |
| backend-integrations-004 | `ssh_keyutil.go:57-60` | ssh-keygen `-P <口令>` 在进程列表可见；容器内单用户暴露窗口极小，且 OpenSSH 对 SSH_ASKPASS 支持跨版本不确定 | 验证镜像 openssh ≥8.4 后切 askpass，避免回归 |
| backend-persona-004 | `plugin_daemon.go:166-216,585-625`；`plugins.go:194-290` | daemon Start/Stop 与 runPluginHost 持全局 `a.mu` 同步跑 node（最长 15s），期间所有走 a.mu 的请求冻结；移出锁需重审 registry/surface 一致性窗口 | 锁内快照、锁外拉起或异步 202 + 前端轮询 |
| frontend-css-004 | `style.css:390-393` | 工作流四阶段选中色（蓝/琥珀/紫/青绿）为**刻意语义区分**，折叠成 2–3 令牌会让阶段无法区分 | 如需 green 配色，新增 8 个 `--phase-*` 令牌并在 green.css 覆写 |
| frontend-viewers-007 | `app.js:4526-4532` | docx 批注 `anchorIndex` 恒 0（死代码，计数未赋值）；需理解 docx-preview 批注定位语义 | 修正计数赋值或移除死代码 |
| frontend-viewers-009 | `app.js:3871-3887` | PDF 快速缩放/翻页对进行中 render 无取消，产生重叠渲染与闪烁；属渲染行为改动 | 保存 renderTask 并在新渲染前 `cancel()` |
| （桶布局，见 backend-http findings 011） | `save()` | 硬编码 active 桶导致 archived/assistant 目录成死目录；归档仅内存标记 | 需统一会话存储布局决策 |

---

## 5. P3 建议项（67 条：32 已修 / 35 记录）

完整逐条见 [`master-findings-table.md`](./master-findings-table.md)。已修复的 P3 集中在：

- **后端**：edge TTS 帧 16MB 上限、sherpa `--` 开头文本前置空格（`9683572`）；WebAuthn 后台 60s 清理过期 challenge（`5d6d8f6`）；narrate 按 rune 截断不切坏 UTF-8（`e6fe568`）；comments update 串行化防丢更新（`4fd0a56`）；sqlite 临时文件写失败清理（`add32c2`）；readLocalImage 空 MIME 拒绝（`03138be`）；sftpBatch 超时判定（`2ab603c`）。
- **前端**：runPhase 完成后 delete、断网轮询不再 toast 风暴、clipboard `.catch` + i18n（`3e43415` `0f12896`）；DXF/图片鼠标监听与代码高亮 resize 清理（`3e43415`）；CSS 死/重复规则清理、控件 36px 对齐（`bcf0123`）。
- **UI 打磨（7 条全修）**：三主题主按钮对比度（light 5.59 / dark 5.42 / green 7.54）、--faint 加深至 5.05:1、选中行次要文字 4.56:1、--radius-card 统一 8px、设置关闭 blur 消焦点环、搜索框 ⌘K 提示与聚焦空态、外观 segmented 去重复标签（`6eca956` `3d9d4b6` `b0dc453`）。

**记录的 P3（择要，含安全加固）**：
- 安全：`/healthz` 免鉴权泄漏版本/完整性；密码/解锁端点无失败限速；debug 令牌 TTL 365 天；`access_token` 走 URL（file/raw、download、events）；token 存 localStorage（XSS 放大器，可经 `/api/command` 执行 shell）；建议高风险写操作加主身份二次校验；SQLite 表名 f-string；`canAccessMemory` 默认放行；remember 锁外非原子写。
- 前端：docx 逐字符拼接大文档卡顿、drawio 超时监听残留、锁屏常驻 interval、WebAuthn challenge 入 URL、Blob 立即 revoke、ZIP 无大小上限、热力图 UTC“今天”差一格、z-index 分层约定等。

---

## 6. 验证证据

### 6.1 后端门禁（golang:1.26-bookworm 容器，离线 vendor）
- `go vet ./...` → **退出码 0**；`go build ./...` → **退出码 0**。
- `go test -race ./...` → **0 个 DATA RACE、0 个 panic**（race 包退出码 1 仅因容器内缺 node 的插件/daemon 用例，见第 7 节）。
- 当时的运行日志名为 `evidence/final-race.log`、`evidence/baseline-vet-build-test.log`；这两个原始日志未随当前仓库归档，不能作为当前可点击证据。已归档的模块资料见 [`evidence/`](./evidence/)。

### 6.2 镜像重建
- 当时共记录 5 次 `docker compose build && up -d`；原始 `rebuild.log` 至 `rebuild5.log` 未随当前仓库归档。历史记录称最终容器 `aide-aide-1` **healthy**，`GET /api/sessions` → **200**；这不代表当前版本的运行结果。
- 前端资源缓存版本由 `?v=64` 升至 `?v=65`（仅资产缓存版本，非 version.md，未触发版本/tag 禁令）。

### 6.3 Playwright 全功能走查
- 9 大项 / 22 检查点：**18 PASS / 4 FAIL（走查时）**，无崩溃、无数据丢失；4 FAIL 已全部修复。
- 覆盖：登录（坏 token 401 / 正确 token）、普通对话与 SSE 流式、运行中插话与排队、工具调用、文件提案审批（apply 200/409）、md/txt/图片/pdf 查看器、SQLite 查看与持久化、设置与权限面板（沙箱三模式 + 工具开关）、会话三点菜单（置顶/归档找回/删除二次确认）。
- 清单：[`evidence/runtime/walkthrough.md`](./evidence/runtime/walkthrough.md)，23 张截图同目录。

### 6.4 渲染专项
- 查看器：PDF / DOCX / DXF / STL / PNG / drawio / Markdown / TXT / ZIP / SQLite → **最终 10/10 PASS**。
- 代码高亮：独立代码文件 7/7；Markdown fenced **42/42 语言块有高亮**（clojurescript 经 clojure 回退）；无语言包 404。
- 清单：[`evidence/render/render-checklist.md`](./evidence/render/render-checklist.md)，截图同目录。

### 6.5 深度 UI 走查
- 8 维度（视觉/组件/交互态/状态完备/布局响应式/滚动/流式回归/可访问性）× 三主题 × 三窗口尺寸（1440 / 900 / 600 宽）× 流式/空闲 → **最终全 PASS**。
- 此前反馈的“左下角白色块 / hover 圆角缺失”确认已修复：三主题左下角与侧栏底色统一、hover 高亮连续。
- 清单：[`ui-checklist.md`](./ui-checklist.md)，24+11 张截图 [`evidence/ui/`](./evidence/ui/)。

---

## 7. 已知非回归（环境问题，非代码缺陷）

golang 测试容器内**无 node**，以下用例失败属环境限制，勿当新 bug：
`TestDaemonStartStop` / `TestDaemonCallViaIPC` / `TestDaemonCrashRestart` / `TestDaemonEventReporting`、`TestNonDaemonUnchanged`、`TestPluginUploadAndValidation` / `TestPluginLifecycleAndSurface` / `TestPluginApplyErrorIsolated` / `TestPluginToolExecution`。

---

## 8. 产物索引

```
docs/reviews/2026-09-28-code-review/
├── README.md                    # 本报告
├── FINDINGS-SPEC.md             # 缺陷契约（schema / P0–P3 口径 / 防误报清单）
├── consolidate.py               # 汇总脚本
├── master-findings-full.json    # 106 条记录（含状态/重复标记）
├── master-findings-table.md     # 完整逐行清单表
├── ui-checklist.md              # 深度 UI 走查清单
├── findings/                    # 各专家原始 findings（12 个 JSON）
│   ├── backend-core / backend-http / backend-integrations / backend-persona
│   ├── frontend-core / frontend-viewers / frontend-css
│   ├── security / test-failures
│   └── runtime-functional / render-validation / ui-deep
└── evidence/                    # vet/build/race、重建、走查、渲染、UI 日志与截图
```

修复以 32 个逻辑批次提交并已全部推送至 `origin/feature/permission-panel`（`190657f..3d9d4b6`），全程未 bump version.md、未打 tag（遵守 feature 分支约束）。
