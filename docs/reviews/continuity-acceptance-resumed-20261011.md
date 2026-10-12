# Aide 恢复完整验收 · 2026-10-11

## 继续检查点 · 2026-10-12

当前源码候选及缺口总览见[验收审计](continuity-acceptance-audit-20261010.md#当前复核检查点--2026-10-12)。本日更新了用户指南、架构、Markdown、插件、星图、密钥安全与工业长对话文档，并补齐英文翻译目录；完整门禁的文档/i18n检查通过。

### 续验更新 · 2026-10-12

用户已处理本机隔离验收证书提示。按用户指示运行 `./start.command`，Docker 实际重建并健康；系统 Safari 在本机 `localhost:9999` 桌面视口通过 Markdown 单行插入栏和 MD 示例表格插入/实时预览检查，未保存验收内容。随后 `python3 scripts/agent-route.py verify full` 对指纹 `cacbf4495fb88d3a68494290f4e122d99341e0adc460b54bf78398cde93b0e5b` 通过 38 项，含全 Go race/vet；完整收据 `.agent-state/verify-20261012T043636627018Z.log`。这些证据不代表全部功能已验收。阶段性功能、文档与英文界面已提交并推送 `origin/main`：`4c33da7`（67 个文件）。

当前未完成：窄屏工具栏、光标精确位置及其它媒体插入仍需系统 Safari 操作验证；Safari 随后不可操作，未形成窄屏证据。真实向量仍按用户要求暂缓，Windows 无测试环境。系统 Safari 曾显示会话 #43 本段执行达到 6 分钟并暂停，超长任务连续运行仍需独立跟进。整体发布门禁、Release 和未验收工作流继续开放。

## 分工与基线

用户改为由 Codex 继续完整验收，真实向量测试仍暂缓。Windows 无设备，保留 NOT_RUN。验收基线提交 `399e0bebebe58c4528075d2f3e2dd3510410a44e`（远端 main 已核对）；产品源码未变化，仅修复历史 UI 回归的 VM 测试夹具并补充验收文档。生产 9999 未替换，未 Release。

## 自动回归

`python3 scripts/agent-route.py verify full` 在当前 HEAD `399e0bebebe58c4528075d2f3e2dd3510410a44e` 终态 exit0，38 项 PASS，包含隔离 Docker Go race/vet。指纹 `7fbdd76d7a9bce49e29595e01dabb2218ef191b536622876fac4c4436134e03a`，日志 `.agent-state/verify-20261010T165608392339Z.log`。同一指纹下普通沙箱第一次因 Docker API 不可见而在 `aide.sh test` 失败（`.agent-state/verify-20261010T165437597688Z.log`）；只读确认引擎可用后在隔离权限下重跑通过，没有触碰生产容器。

前一轮 `.agent-state/verify-20261010T155248666849Z.log` 保留 FAIL：历史备份测试抽取的 VM 未提供新管理模块所需 `window`。夹具增加 window 和管理模块契约断言，定向 PASS 后重跑 full。不能把这次 full 当作全部真实交互通过；后续源码变化须重新生成指纹。

## 双服务器 SSH/SFTP

`AIDE_SSH_TEST_EVIDENCE_DIR=.agent-state/ssh-acceptance-2240eea bash scripts/test-ssh-workspace.sh` exit0，隔离两个服务器重复两次通过。日志 `.agent-state/ssh-acceptance-2240eea/aide-ssh-test-20261011000106-70151.log`，输入摘要前后一致；临时容器及网络由脚本清理。

覆盖目录不存在/权限错误、独立 SFTP 引用、陈旧连接恢复、文档绑定和缓存路径、Markdown 正文/资源恢复、远程重命名/归档/解压/删除及属性、搜索/时间线/渐进索引、命令退出与远程进程组取消。它不证明生产服务器凭据、物理断网或用户权限配置正确。旧 token memory 往返测试不代表已停用的记忆神经 UI 验收。

## 系统 Safari

候选 18211 使用 `.agent-state/continuity-runtime/start.command` 启动 exit0，二进制 SHA256 `965f364172410a30d164bc6cb01b449937337dfa48a731c1e5e3fa764eee1a56`。旧二进制已保留。复制的启动目录缺少三份 bridge-control 脚本，启动出现告警，因此桥接能力未验收；系统 Safari 的原生操作不依赖这些候选桥接脚本。

已完成实际按钮操作及独立读回：

- `notes.md` 默认保留 0/0 预览保留全部 7 版本；取消无修改。
- 独立临时目录 `acceptance-20261011/page.md`，正文 A + SVG A 初始归档；Safari 自动保存 B 后读回服务器文件。
- 外部修改 SVG 为 C，打开历史捕获资源变化。选择版本 1、预览并恢复正文和资源，服务器正文 A、SVG A 均读回确认。
- 恢复产生版本 4；相同内容再次手动保存不追加版本。
- 下载历史 ZIP 得到 4 版本、4 去重对象；逐对象 SHA256 与文件名一致。包 SHA256 `1b2c62a98c533b93349ea3f27de077782af10639580fabaec156c47acebba4a2`。
- 同一备份导入预览 0，确认后重新打开仍 4 版本；当前正文和 SVG 未改变。自动保存恢复为测试前关闭状态。

截图保存在 `.agent-state/continuity-runtime/`：`history-management-2240eea-safari.png`、`history-restored-2240eea-safari.png`、`history-import-idempotent-2240eea-safari.png`。临时数据与下载留存用于复核，未混入生产数据。

## 星图观察补充

从工作台真实入口打开星图后，自动索引包含新建 `acceptance-20261011/page.md`，节点原文与服务器一致。节点/名称搜索返回两个同名文件，路径可区分。星际日志的连续观察比较显示插件清单 `surface.json` 的 generatedAt 从重启前变化至本次启动时间；随后扫描轮次从176继续至198，观察保留32/淘汰35保持不变。此次没有复现无内容变化的历史增长，不据此覆盖全部时间线/容量验收。截图 `starmap-current-node-2240eea-safari.png` 留存于候选证据目录。

## 后续 Safari 星图实测 · 2026-10-11

在系统 Safari 的 18211 候选执行文档原文检索 `Original A.`，实际继续分页到 129/129 份已索引文档，在 `acceptance-20261011/page.md` 第 3 行命中 1 条；模式显示“逐字原文”，没有调用模型。再以同词执行本地关键词 RAG，分页至 129/129，命中同一文件/行，显示“本地关键词 RAG”。该候选当前报告129份已索引文档，星图来源状态明确表示全库总数未统计、部分来源分批，故仅对这一索引快照成立。

打开代码层级视图后，Safari 显示 1 个恒星系统、218 个真实节点、6 份源码、53 个声明、41 条调用候选、133 个未解析调用、0 个类型绑定，并显示“静态调用候选”和完整性限制说明。证明UI将候选与未解析项如实区分；不证明运行时或整个仓库代码关系正确。截图 `code-hierarchy-current-399e0be-safari.png`。

同一系统 Safari 当前代码视图内用两个实际检索出的隔离文件编号执行路径查询，并启用目录归属关系。当前有限索引返回“未找到路径 · 索引不完整。不代表没有真实联系。”没有把不完整索引误报为无关系。此处证明了路径查询的边界提示；正向多跳路径和关系源跳转仍沿用已有 `path_multihop_system_browser` 收据，不据此扩大到完整仓库。

## 成果舱 Safari 实测 · 2026-10-11

新开系统 Safari 标签访问隔离候选 `https://localhost:18211/`，打开已有隔离会话 `#42 成果舱 API 验收（测试记录）` 的轨迹，切到“输入与输出”，再打开单轮详情和 `E0001 read_file` 原始回执。UI显示1轮、输入1条、回复0条；`first.md` 是“提案未应用”，`second.txt` 是“应用已有记录，当前文件未复核”；同时明示2条工具调用、1个 pending/dispatched 的 `run_shell` 未取得持久化结果、建议命令尚未运行。虽测试会话状态标记 `completed`，视图仍没有将其提升为已验证成功，也明确建议/验证报告不等于独立验收证据。原始参数 `{\"path\":\"first.md\"}`、截断返回和内容指纹可展开。截图 `.agent-state/continuity-runtime/outcome-input-output-20261011-safari.jpg`。

这是持久化测试夹具的 UI/证据格式验收，不代表本轮模型或 shell 真正执行，也不闭合跨页运行中快照、当前文件应用状态重读或发布成果卡。

## 后续 Safari 状态恢复复核 · 2026-10-11

系统 Safari 在隔离候选 18211 中刷新工作台后，仍显示原活动验收会话、未发送的测试草稿、`notes.md` 附件和“其他恢复草稿 · 5”；发送按钮未触发。随后从工作区知识星图入口重新打开，搜索词 `page.md`、代码层级视图和两条同名结果仍可见，页面明确提示“已恢复上次星图视角与筛选；未自动执行检索或 AI。”这只确认当前 Safari 标签的工作台草稿及有限星图状态恢复，不覆盖其他工作区/标签隔离、镜头精确坐标/相位或重启恢复。

星图右上角“星际日志”可展开并显示索引来源计数、累计扫描文件/目录、每批预算、扫描轮次、待扫描目录、容量上限、文本截取范围和“全库总数未统计”。正文索引限制、代码解析支持语言/字节预算，以及静态调用候选并非运行时跟踪的边界均在面板中可读。此处是隔离实例中的 UI 呈现证据，不代表远端来源或超预算/故障分支完整。

文件查看器设置页显示行号、语法高亮、Markdown 默认预览、标题目录、插入工具栏、CSV/TSV 表格预览、Markdown 历史版本追踪和草稿恢复均已启用；编辑模式为左右对比。没有修改这些设置。Markdown 左右编辑/预览页中，标题目录以折叠标签吸附在编辑器与预览区的中线；点击后在中线旁弹出目录项，标题可见且不占据编辑内容顶部。

在候选实例工作区的 `/workspace/acceptance-20261011/` 通过 UI 新建 `csv-render-acceptance.csv`，输入表头 `name,count,active` 和两条样例数据，点击保存后页面显示“服务器已保存”，切换预览显示 3 行 × 3 列表格，列标题与 `alpha/3/true`、`beta/8/false` 值均与输入一致。这是 CSV 基本 Excel 式预览的真实浏览器证据，不覆盖大型文件、编码/公式/编辑往返等场景。清理时 Aide 的删除确认明确标示“永久删除、不可撤销”；未确认该操作，因此临时验收 CSV 保留在隔离验收目录，不触及其它文件。

Safari 控制连接在首次选择既有星图标签时中断；重新绑定后从工作台入口完成了验证。验收文档提交 `e297a466f267e620d18466de7206961a952a5f45` 的 `git push origin main` 返回 exit 0，输出显示 `827bc23..e297a46 main -> main`；随后 `git ls-remote` 因 GitHub DNS 解析失败未能独立核对远端 HEAD。工作区本地干净，不能把独立远端核对标成通过。

## 剩余验收

永久裁剪/对象清理、跨工作区导入、其他嵌入类型、恢复容量/强杀/断网/冲突、跨会话审批/规则管理、实际插件和模型、星图路径/时间线/镜头/增量更新、输入输出分页、锁态同步、主题与窄屏仍需逐项证据。按[完整验收清单](luna-acceptance-handoff-20261010.md)和[目标源码核对表](continuity-acceptance-audit-20261010.md)继续，不能用本页的部分 PASS 关闭总目标。

真实向量暂缓；Windows NOT_RUN。发布须补可执行回滚、当前输入 full 和阶段证据，再运行 release-check；本页不是发布收据。

2026-10-11 再运行 `python3 scripts/agent-route.py release-check aide-continuity-knowledge-20261009`，当前门禁仍 BLOCKED：缺少 rollback，design、implementation、verification、documentation、cleanup 阶段未完成。不得将成功 push 等同于 release。

## Go race 全包复核 · 2026-10-11

修复 `webAuthnManager` 的后台 pruner 生命周期：manager 提供幂等 `Close()`，通过停止/完成通道结束定时循环；`App.Close()` 负责关闭 manager；WebAuthn 测试 app 注册清理，并增加关闭后退出及重复关闭测试。原因依据此前 600 秒失败日志中的 goroutine 堆栈：大量 `pruneLoop` 停在 ticker，并使 SFTP 缺失目录用例迟迟无法结束。

Docker 定向 race 检查 `TestSFTPFailureWithZeroExit` 与 `TestWebAuthnManagerCloseStopsPruner` 通过，18.086 秒。随后完整 `go test -race -v -count=1 ./internal/server -timeout=15m` 通过，`ok aide/internal/server 702.018s`；此前挂住的 `TestSFTPFailureWithZeroExit/source/missing` 及其余 SFTP 子用例均 PASS。`TestKnowledgeLocalProcessHelper` 与 `TestSourceFileActionsLiveSFTP` 因需要单独启动的外部夹具按测试设计 SKIP；它们不计为真实生产 SSH 验收。

正式 `verify full` 的外层默认给每条命令 600 秒，短于本机此全包 race 实测时长。正式路由首次在无 Docker socket 的普通权限下失败；提权重跑后，Go 默认 10 分钟测试超时在 `TestSSHSessionInvalidatedMasterAfterKillTimeout` 处触发，虽然单独 15 分钟诊断运行已完成。现将 `scripts/aide.sh test` 的 `go test` 超时显式设为 15 分钟，并将 `scripts/agent-route.py` 对该完整命令的外层上限设为 1200 秒，其他检查仍为 600 秒；正式全量复核再次运行，只有它完成且源码指纹一致后才登记 PASS。该单独手工 Docker 结果不替代 full 收据。真实向量仍按用户要求暂缓，Windows 无测试环境仍为 NOT_RUN；Safari 锁屏或未操作到的边界也不会因单测 PASS 自动关闭。

### 正式 Full 收据

`python3 scripts/agent-route.py verify full` 最终 exit 0，38 项全部通过，源码指纹 `6f93ef31c2185705f565a9e6aa596b87a042fe759cb910c66e5d2076a3569cf5`，记录时间 `20261010T183733620736Z`，日志 `.agent-state/verify-20261010T183733620736Z.log`。Docker `internal/server` race 全包 679.043 秒，`go vet ./...` exit 0。此前两次失败日志保留：普通权限无法访问 Docker socket，以及原 Go 默认 10 分钟超时；此收据是在提权本地 Docker 环境、显式 15 分钟 Go 超时和指纹未变化的正式重跑结果。

该收据证明源码回归，不覆盖尚未执行的真实向量检索、Windows 实机、产品级跨工作区/标签隔离与全部人工审批路径；是否 release 仍以 release-check 和其余阶段证据为准。

### 提交与推送

修复提交 `c21189b`（`fix: stop WebAuthn cleanup loop on shutdown`），包含生命周期修复、测试、正式 Go 超时设置和验收收据。`git push origin main` 返回 exit 0：`4704cf1..c21189b main -> main`。随后独立 `git ls-remote origin refs/heads/main` 因本机 DNS 无法解析 `github.com` 失败；因此记录 push 客户端的成功回执，但不声称已独立核实远端 HEAD。release-check 在本地干净提交上仍因缺 rollback 及五个阶段未完成而 BLOCKED；未发布或部署。

## 全局审批反向切换回归 · 2026-10-11

为补齐全局审批“辅助 → 手动”后“手动 → 辅助”的自动回归，新增 `internal/server/approval_policy_test.go`。三个测试覆盖：手动切换会取消两个活动会话的审核上下文、提高代次、持久化会话快照并广播跨标签策略事件；辅助模式切回会同步两个暂停会话、持久化状态并广播；已记住的精确审批规则不跨 SSH 主机或工作区命中。

在现有 `aide:local` Docker 镜像中运行：`/usr/local/go/bin/gofmt -w internal/server/approval_policy_test.go`，随后 `go test -race -count=1 ./internal/server -run '^Test(GlobalApprovalModeManualStopsReviewAcrossSessions|GlobalApprovalModeAssistedResumesAcrossSessions|RememberedApprovalDoesNotCrossSSHRoot)$' -timeout=90s`，结果 `ok aide/internal/server 6.593s`；`git diff --check` 通过。此为服务端持久化/通知回归，不替代真实系统浏览器的两标签交互验收。

本轮再次检查系统 Safari 时，电脑处于锁定状态，桌面控制返回“Mac is locked”；没有继续改动实际实例的全局审批设置，也没有发送模型任务。审批 UI 的真实反向切换和规则面板操作保留为未完成验收。真实向量检索仍按用户要求暂缓，Windows 实机仍无环境。

## 当前输入完整自动门禁 · 2026-10-11

在审批回归测试及本交接更新后，运行 `python3 scripts/agent-route.py verify full`。终态 exit 0，38 项 PASS；当前输入指纹 `4afd7fbdc94871cb216388bf2481e5a26af69bb29b4c23c641fe4a56d652df34`，基线 HEAD `f6c3ea53bef2c41e71faa0a41ca4a1cad21742d5`，日志 `.agent-state/verify-20261010T191351778252Z.log`。Docker race：`aide/internal/server` 756.829s、`aide/internal/server/tts` 3.710s；`scripts/aide.sh test` exit 0（含 `go vet ./...`）。完整结果在 `.agent-state/full.json`。

此收据覆盖当前代码和审批定向回归，但不关闭真实浏览器跨标签审批交互、真实模型/插件、生产 SSH、物理断网、长时与锁屏恢复等产品验收。Safari 在本次尝试时 Mac 锁定，未操作工作台或修改现有会话。真实向量验收遵照用户指示暂缓；Windows 实机没有可用环境。发布仍需 release-check 所要求的前置阶段、备份/回滚步骤和候选发布验证，不因 full 通过而自动完成。


## 全局辅助审批暂停任务状态修复 · 2026-10-11

Safari 对当前 9999 实例的只读检查发现：全局审批显示“辅助审批”，会话列表包含普通聊天 #43；代码路径 `changeGlobalApprovalMode` 曾把所有 running/paused/queued task 都设为 reviewing，而 `reviewPendingCommand` 仅会审核真正等待答复的 shell，文件审核也仅处理真实 awaiting_approval 文件，因此普通暂停任务会出现幽灵审核状态。未对 #43 发送消息或执行操作。

修复位于 `internal/server/approval_policy.go`：只有含未响应 shell clarification/live AnswerCh 或含真实 awaiting_approval 文件的任务才进入审核；全局切模式会清除非待审批任务上的陈旧状态，同时保留跨会话模式、generation 和取消逻辑。`approval_policy_test.go` 增加暂停任务遗留 reviewing 状态清除断言。

验证：定向审批 race PASS，`python3 scripts/agent-route.py verify quick` PASS；随后当前源码完整 `verify full` exit 0，38 项 PASS（包括 Docker Go race 与 vet），指纹 `1aa494bfeec3d9b3d8a8c79a76843e7db8692b30dd1568c95b448607e2ae2249`，日志 `.agent-state/verify-20261010T235350052960Z.log`。候选通过仓库 `start.command` 重建后，Safari 实际在 #43 普通暂停聊天期间切到手动审批，再切回辅助审批；两次页面均显示“审批模式已全局生效”，#43 保持“已暂停”且无“审核中”标记。另在新标签打开同一 Aide 后，策略菜单读回全局辅助审批已选中。全程没有发送会话消息或触发工具，原辅助审批模式已恢复。新标签首次打开短暂出现“已锁屏”遮罩，切回原页后再次打开同一标签恢复正常；这一锁屏生命周期表现另列为尚待分析的问题。此次只验证审批状态呈现和模式同步，不证明真实模型审核决策质量。

候选启动收据：`AIDE_OPEN_BROWSER=0 ./start.command` exit 0，重建 `aide:local` 并启动 `https://127.0.0.1:9999`，脚本健康检查通过；镜像 manifest `sha256:e04232b79f84e13177a95570ed074e5e62fcaf94931c0bef0247bd449c074537`。既有 `aide-data`、`aide-home` 命名卷和工作区挂载未移除。

## 长会话预算与当前运行实例复核 · 2026-10-12

按用户指示执行仓库 `AIDE_OPEN_BROWSER=1 ./start.command`，成功构建当前源码镜像并重启本机 9999 实例；未删除或重置数据卷。Safari 系统浏览器随后自动进入已认证的 `https://localhost:9999/` 工作台。实例健康检查通过。

通过本机认证只读接口 `GET /api/execution-policy` 读取实际生效策略（令牌仅在容器进程内读取并使用，未打印或保存）：`chatBudgetSec=7200`（2 小时）、`workflowBudgetSec=900`（15 分钟）、`toolMaxRounds=60`，无配置警告。源码默认值一致。故当前没有 6 分钟的聊天总时限；聊天执行的单个活动段最长 2 小时，工作流单段 15 分钟，另有 60 轮工具调用上限。应用通过持久化检查点、显式暂停/续跑延续任务；续跑会把该任务当前模式的预算刷新为最新值。

源码另有一处独立的 6 分钟超时：开始聊天前若上下文超出窗口，`workflow.go` 中自动历史压缩请求用 `context.WithTimeout(..., 6*time.Minute)` 限制单次压缩 HTTP 操作。这不是聊天任务时限；压缩超时会返回可见错误且不启动该轮任务。后续需用真实模型/长历史测量压缩时延、取消行为和恢复体验，再决定是否需要可配置或分阶段处理。

Safari 只读查看既有 #43 显示“本段实际执行已达 6m0s（不含等待回答/审批），检查点已保存，可继续”，同时任务状态为“已暂停”。这只证明该任务的当前执行段曾在 6 分钟处暂停；并不表示 9999 当前配置仍有 6 分钟上限。由于 #43 是用户现有会话，本次未点击“恢复任务”、未发送新消息或改动会话。要验证续跑能否在真实模型下完成超过 6 分钟的连续工作，仍需单独安排安全任务；本次不据配置读取宣称该端到端模型验收已通过。

全仓复跑已完成：隔离 Docker 中 `go test -race -count=1 -timeout=30m ./...` exit 0，`aide/internal/server` PASS（989.296s），`aide/internal/server/tts` PASS（3.356s）；随后 `go vet ./...` exit 0。运行起始代码基线为 `645fbbf`；之后至当前 `9fc9f61` 只追加验收文档，没有代码变更，`git status` clean。此结果关闭全包 race 超时缺口，但不关闭真实模型下超 6 分钟续跑、长历史压缩及锁屏恢复验收。真实向量检索继续按用户要求暂缓；Windows 实机验收仍因无环境为 NOT_RUN。发布门禁保持开放。

## 2026-10-12 状态更新

- Windows：用户已在自己的 Windows 环境直接测试，反馈“感觉没什么问题”，并明确说后续无需我处理。当前清单不再将 Windows 列为待办或阻塞；更早的“无环境/未运行”保留为历史快照。
- 知识路径：在系统 Safari 的 `localhost:9999` 当前代码索引中，以 `C072c217f8209 Commands.cmd_help` 和 `C93c0aec588c0 Commands.basic_help` 完成一条 1 步路径。页面显示 `commands.py` 第 1123 行 AST 静态名称候选与源位置链接，并明确注明索引仍有缺口、路径不是运行轨迹。
- 向量/embedding 验收仍按用户要求暂缓。其他总体工作流、SSH/真实审批边界、长时运行和发布回滚仍需依各自任务台账继续验收。

### 星图源位置复核补充

系统 Safari 点击关系证据的“查看关系源位置”后到达 Aide 只读文件路由，地址含 `root=context`、`commands.py`、`line=1123`、`endLine=1123` 与 `readOnly=true`。当时工作台处于“已锁屏”，正文与行号高亮未能读回；未尝试绕过应用锁屏。引用目录星图显示部分覆盖：945 个文件、55 个目录，目录容量上限 1,000 条，仍有 333 个目录任务待扫描。

## 发送前压缩超时复核 · 2026-10-12

源码中的聊天任务默认预算为 7,200 秒；旧任务可携带 360 秒策略快照，但显式续跑会刷新当前模式预算。另一个六分钟点位是发送新消息前的上下文超限压缩，不是聊天任务总时限。本次将其独立截止值改为两小时，取消信号仍继承 HTTP 请求上下文，摘要仍采用快照匹配和原子保存后提交。该次源码修改已继续接上会话进度 API 与左侧卡片轮询，展示真实 map/reduce 批次、折叠保存和终态；服务端只返回阶段和估算计数，不返回正文，并新增逐批事件回归断言。

后续核对：隔离 full 回归对源码指纹 `7a780784b82c696c9bad1e643673c73588188f79c84fcd8b8cfde3bd51a5b45b` 通过，全包 Go race 测试与 `go vet ./...` 均 exit 0。只重启了绑定当前源码只读目录的隔离候选 `aide-budget-20261012`（18217），生产 9999 未重启；Safari 已登录并读回候选工作台。候选未配置模型，因此未发送模型任务，真实模型自动压缩、长历史动态与端到端时延/恢复仍未验收。早先因默认沙箱无法访问 Docker 而未运行回归的记载仅代表当时状态，之后已通过受控隔离 Docker runner 完成。代码检查点已推送 `origin/main`，提交 `c4cda4267104f24585777dfb32a3595d583e1b7b`；未打标签、未发布或替换生产服务。
