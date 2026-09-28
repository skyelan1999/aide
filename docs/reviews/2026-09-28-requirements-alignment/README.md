# 需求对齐报告（2026-09-28）

- 基线：`feature/permission-panel` @ `f9c4e02`（工作树干净）
- 范围：`docs/PRD.md`（v3.2，M1–M13 / 111 功能点）× `docs/tasks/*.json`（43）× 真实代码 `internal/server`（Go）与 `internal/server/web`（原生 JS/CSS，go:embed）
- 方法：**仅静态阅读**（Read/Grep/Glob）+ git 只读核对。未执行 docker build/up、未重启容器、未做任何运行时改动（运行时验证由并行 agent 负责）。
- 纪律：每条状态都指到具体文件:符号或文档；拿不准的一律标「待确认」，不凭空勾选。

## 核心结论（先读）

- **111 个功能点：已交付 108、部分 3、未开始 0；另有 5 处「文档与代码不一致」（描述偏差，非功能缺失）。**
- **MM-05（插件感知训练）最终判定：已交付。** PRD 标「待开始」是过时的——`plugin_learning.go` 已实现并接入系统提示、有单测。证据见下。
- **TR-09（SSE 顺滑）最终判定：部分交付/待验证，不是「未开始」。** 后端每事件 flush、`X-Accel-Buffering: no`、15s 心跳均已落地；「逐字顺滑」属体验属性，需长回答 E2E 实测。
- **§6.2 的 draw.io 前端联动并非整体「待补全」**：init 握手 / 空白画布手画 / save 回写 / 超时失败提示均已实现；唯一明确缺口是 `.drawio.svg`/`.drawio.png` 关联。
- **E2E 验收、容器重建实测仍「待验证」**——这是运行时事项，本次静态阅读无法证实，且有并行 agent 在跑，保持原判断。

---

## ① 已交付（PRD 功能点 → 实现位置 → 验证状态）

> 全量 108 点见 [traceability.md](./traceability.md)；此处列关键符号，验证状态分 `[测试]`/`[代码]`。

- **会话 SM**：`summarizeTopic` 并发标题（workflow.go:430,310 `[测试]`）；`wrapSteer` 插话包装（:940）；排队/插话 queue_steer_test.go；取消保留流 stream_broker.go；配置备份 config_backup.go `[测试]`。
- **工作区 WF**：files.go `PUT/GET /api/files`、`/api/file/raw`；office.go Office 解析；attachments.go 8/80KB `[测试]`；SSH/SFTP workspace_config.go + ssh_session.go。
- **工具 TL**：builtinTools workflow.go:117-138（实际 22 个）；search_text required=["query"](:130)；web_search DDG+降级(:1999)；ask_user(:138,2878)。
- **模型 MD**：多模型/Profile profiles.go；MD-08 推理强度 provider.go:85-91 真实注入 thinking/reasoning_effort。
- **权限 AU**：三级沙箱 SandboxMode；危险拦截 shellBlocked；60s/并发4/128KB command.go:134,127,35；ToolMaxRounds 默认60 server.go:146；锁屏 LockTimeoutSec+SHA-256 kdf.go。
- **记忆性格 MM**：memory_access.go；persona.go AES-256-GCM；**MM-05 plugin_learning.go（见 §④）**。
- **多 Agent AG**：autoModePrompt(:1496)、profileInventoryPrompt(:1528) 多次 spawn+核对。
- **四阶段 FL**：requirement/design/implementation/verify phase prompts（workflow.go:1491/1724/1733/1743）。
- **轨迹 TR**：runEvents(:3364) SSE；/api/context-preview 共用构建器(server.go:1001, workflow.go:258) `[测试]`；Token 费用/轨迹 debug.go。
- **部署 DP**：start.command→compose；Dockerfile go:embed；/healthz；auth_verify.go 令牌。
- **插件 PL**：plugins.go/plugin_daemon.go/plugin_host.js；surface 面板。
- **外观 UI**：themes/、i18n.js+locales/、settings-schema.json、retry 路由(OQ-01 关闭)。
- **语音 VO**：voice_agent.go、/api/voice-filter(server.go:1428) send/ignore/standby(server.go:1501)、voice_samples.go 历史、tts/。

---

## ② 部分交付与缺口

| 编号 | 现状 | 缺什么 / 影响 |
| --- | --- | --- |
| **MD-06** | manual 策略完备；auto 按任务匹配 Profile 偏粗 | auto 路由依赖 AG-05「不足暂停建议」兜底，未做到真正按任务语义自动选档；PRD 行内自标「部分实现」属实。影响：自动模式下 Profile 选择仍需 Lead 介入。 |
| **TL-11 draw.io 前端联动** | 工具+init 握手+空白手画+save 回写+超时提示**已做**（app.js:3547-3585） | ① `.drawio.svg`/`.drawio.png` 关联未实现（扩展名判断 `/\.drawio$/i`，app.js:1289,4786）；② 会话消息流内未见第三个渲染调用点（现仅模态编辑器:1342 + 文件视图:4847）。影响：导出为 svg/png 的 drawio 不能在预览器里打开；消息里贴 drawio 附件无内联图。 |
| **TR-09 SSE 顺滑** | 后端 flush 基础设施已就绪 | runEvents 每事件 `flusher.Flush()`(:3397)、`X-Accel-Buffering: no`(:3391)、15s 心跳(:3414)；命令流同款(command.go:27,41)。缺：长回答「不攒块、不中途断流」的真实浏览器实测。影响：体验是否顺滑需 E2E 判定（OQ-08）。 |

---

## ③ 未开始与建议优先级

经代码核实，**111 点中没有任何一个处于「未动工」状态**。PRD §6.1 原列的两个「待开始/待排查」：
- MM-05 → 已交付（§④）。
- TR-09 → 后端已就绪，仅余运行时顺滑实测。

真正遗留的是「待验证/待补」而非「未开始」，建议优先级：

| 事项 | 优先级 | 理由 |
| --- | --- | --- |
| E2E 浏览器 UI 全链路验收（§6.2/OQ-08） | **高** | 语音小秘/自动模式/四阶段/配置备份/锁屏大批新功能缺真实浏览器实测；阻塞 RC 出口判定。并行 agent 正在跑，需回收其结论。 |
| 容器重建后全流程实测（§6.2/OQ-08） | **高** | 同上，验证数据卷分离/镜像可复现/令牌登录在干净环境成立。 |
| drawio `.svg`/`.png` 关联 + 会话消息内渲染 | **中** | TL-11 唯一功能缺口；改动小（扩展名正则 + 一个调用点），用户画导出图场景会用到。 |
| TR-09 长回答顺滑实测（如确有卡顿再优化） | **中** | 后端已尽力，先实测再决定是否动前端批量渲染节奏，避免过早优化。 |
| OQ-03 semantic_search 改名/加注 | **低** | 仅工具描述措辞（"Semantic vector search" → 实为 TF-IDF），不影响功能；建议下版顺手改描述。 |
| OQ-04 SSH 下 AI run_shell 自动执行打通 | **低（已知边界）** | workflow.go:2421 仍拦截；手动 /api/command 已支持 execRemote，打通 AI 侧收益有限。 |
| OQ-05 streamable HTTP MCP | **低** | stdio MCP 已满足当前，HTTP/宿主机桥接复杂度高，非目标。 |
| OQ-07 交互 PTY | **低（非目标）** | 无 pty 依赖，按 PRD 列入非目标。 |

---

## ④ PRD 与代码不一致点（含命名/语义误导）

1. **MM-05 状态错误（最重要）**：PRD §6 M6 行与 §6.1 把 MM-05 列为唯一「待开始」。实际 `plugin_learning.go` 头注释即标 `MM-05 插件感知训练`，已实现能力域感知 `pluginCapabilityHint()`(:72)、使用经验 `pluginExperienceHint()`(:285)、调用记录 `recordPluginExperience()`(:193)，并在 `context.go:278,281` 注入系统提示、`workflow.go:1144` 落经验，附 `plugin_learning_test.go`。→ **已交付，PRD 状态过时**（本次已改 PRD 该行）。

2. **TL-10 `semantic_search` 名实不符（=OQ-03 仍开放）**：工具描述 `workflow.go:131` 写 `"Semantic vector search by meaning"`，但实现 `:2181 semanticSearch` 注释明写「离线 TF-IDF 余弦相似度（非向量 embedding）」，分词 `tokenizeTFIDF:2129`、停用词 `:2120`。命名仍误导，PRD §3 TL-10 行虽诚实标注了「TF-IDF」，但下发给模型的工具描述没改。→ OQ-03 维持开放。

3. **M3 工具编号重号 + 漏登 4 个工具**：PRD M3 表把 `mcp_call` 与 `write_file` 都编为 **TL-04**（:130-136 行）。真实 builtinTools 有 **22** 个，比 PRD 多 4 个 docx 批注工具：`docx_structure / docx_list_comments / docx_add_comment / docx_resolve_comment`（workflow.go:121-124，实现 comments.go），**未计入 111 点**。→ 编号与清单需在下版校正（本次不重排编号，仅留痕）。

4. **draw.io 加载源描述不一致**：PRD §3 TL-11 文字称用 `embed.diagrams.net` iframe；实际 `app.js:3550` 用**本地 vendor** `/vendor/drawio/?embed=1&proto=json`（离线内嵌，about 页 app.js:1834 自述「内嵌静态 webapp，离线图表编辑」）。以离线本地为准，PRD 文字应更新。

5. **§6.1 汇总口径与行内状态不符**：M4 MD-06 行内自标「部分实现」，但 §6.1 M4 行记「8 已实现」未留部分口径；M6/M9 的「1 待开始」因 MM-05 已交付、TR-09 后端就绪而失真。本次按证据修正 M6/M9/合计（见下方改动留痕）。

### 其余开放问题现状（OQ）
- OQ-01/02/06：已关闭（retry 路由、search_text required=["query"]、provider.go reasoning 透传均在代码中核实）。
- OQ-03：**仍开放**（见上 #2）。
- OQ-04：**仍开放**（workflow.go:2421 AI run_shell 在 SSH 模式仍拦；注：手动 `/api/command` 端点 command.go:67-103 已支持远程 exec，二者行为不一致）。
- OQ-05：**仍开放**（mcp_source.go 仅 stdio，无 streamable/http transport）。
- OQ-07：**仍开放/非目标**（全仓无 pty 依赖）。
- OQ-08：**仍待验证**（E2E + 容器重建，运行时事项，本次静态不可证）。

---

## 本次对账本 / PRD 的可追溯修改

> 所有改动仅在 `docs/` 下，逐条留痕：

### A. `docs/tasks/global-code-review.json` — open_items（5 项）
- `open_items[0]`（per-source SFTP 凭据并入 vault）：**关闭**。依据 `e38e8ca`(P2#1) + `source_secrets_vault.go`（头注直指 backend-integrations-003）+ `source_secrets_vault_test.go`。
- `open_items[1]`（plugin daemon 移出全局 a.mu）：**关闭**。依据 `aeb1ca2`(P2#2) + `plugin_daemon.go:81/88/93/109` 自带锁、`:158/:204` 注释「慢进程在 a.mu 锁外」。
- `open_items[2]`（ask_user 陈旧应答竞态）：**关闭**。依据 `afee71`(P2#3) + `workflow.go:103 answerRound` 单调 nonce、`:2902-2908` 每轮新建 channel + `workflow_clarify_test.go`。
- `open_items[3]`（save() 会话桶布局 archived/assistant 死目录）：**保留为开放，加注**。P2#8 `f9c4e02` 仅改 `web/style.css` 2 行（消息流/输入区列宽对齐），**未触及 on-disk 会话存储**，不能据此关闭。
- `open_items[4]`（--phase-* 令牌 / personaSave 长度上限 / 安全 P3 加固）：**保留开放**，8 条 P2 不含此项。

### B. `docs/PRD.md` — 仅改证据确凿处
- M6 MM-05 行状态：`待开始` → `已实现`（依据 plugin_learning.go 接入+单测）。
- M9 TR-09 行状态：`待排查优化` → `部分实现（后端 flush/代理缓冲已落地，长回答顺滑性待实测）`。
- §6.1 表：M6 已实现 4→5、待开始 1→0；M9 待开始列 1→0（TR-09 转部分）；合计行已实现 109→110、待开始 2→0，并加注「MD-06 实为部分实现」。
- §6.2 draw.io 联动行：`待补全` → `大部分已补全（init 握手/手画/save 回写/失败提示已落地；仅 .drawio.svg/.png 关联缺失）`。
- 不改动：TL-04 重号与 4 个 docx 工具编号（涉及重排，列为本报告不一致项，待下版统一处理）；OQ-04/05/07/08 维持原判断。

### C. 新增本报告
- `docs/reviews/2026-09-28-requirements-alignment/README.md`（本文件）
- `traceability.md`（111 点全对照）
- `evidence/code-locations.md`（关键代码定位）
