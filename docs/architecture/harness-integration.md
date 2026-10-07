# Harness 机制集成（源码与隔离候选已验收）

任务：`harness-integration-20261007`。用户要求集成 Codex、Claude Code、DSH 可借鉴机制；“加密”为笔误，不增加数据加密。

当前集成目标已完成源码与隔离候选验收。Safari 设置保存/重载/导入导出/重启保留、工作树创建/选择/归档、人工审批、辅助审批及 768/390 像素布局均已实际操作。用户确认后开启辅助审批，本机模拟审核器只批准确切验收命令，任务完成、命令 exit0、文件与审核记录均已落盘；随后输入区切回手动审批。原始 `start.command` 启动候选18189 exit0，正式服务未替换。后文早期阶段/阻塞状态为历史记录，以此段及最新验收 JSON 为准。

本次只增加 `.execution-policy-field` 的路径/提交文本换行样式。quick 18 项通过，当前指纹 `7c0ba932987f7d255c03c85147ed4f7b5037e6cbb1be446fe2775885c5e00820`，候选二进制 SHA-256 为 `c6b49514cdd84c624d537cc1e51e67b807e60cab355e24608f493b6860c7e7a2`。此前完整分批 Go race 收据属于 CSS 修复前指纹，不冒充当前全量回归；本次未重跑 Go 回归。截图位于 `/private/tmp/aide-harness-ui-evidence/narrow-worktrees-768-fixed.png` 与 `narrow-worktrees-390-fixed.png`。

## 集成范围与阶段

1. 持久化检查点、追加事件日志、结果未知恢复（源码完成，隔离回归通过）。
2. 默认/全局/工作区配置与任务快照（后端接口和设置编辑器源码完成；页面保存、重载、导入导出及重启保留已验收）。
3. 显式启用的 Hooks、Skills、自定义子代理（第一版源码完成）；插件同步服务装配（源码完成，隔离回归通过）。
4. 复用现有独立审批，统一工具权限与审批结果；任务工作树管理（第一版源码完成，隔离回归通过）。
5. 后端隔离验收、主要页面操作、辅助审批开关和窄窗口实看已完成；发布交付单独记录。

阶段完成不等于整个目标完成。不能以同名接口、存在插件或编译成功声称行为与其他产品等价。

## 执行检查点

`toolLoop` 在模型请求、重试、截断参数续写请求，以及工具执行前调用 `checkpointExecution`。
它先保存所属会话快照，再追加并同步 `execution-events/<task-id>.jsonl`。两步任何一步失败，该动作不进入执行。
工具结果在继续下一动作前保存；保存失败终止本轮。

事件包含版本、任务 ID、递增序号、阶段、类型、工具及调用 ID、参数或结果的 SHA-256 和时间。日志目录0700、文件0600。事件不复制参数/结果原文；原始会话数据仍按已有存储方式保留。

会话快照是恢复依据，事件日志用于审计，不是自动重放队列。续跑创建的新任务沿用序号并通过 `resumedFrom` 关联原任务。

重启时 running 和 awaiting_clarification 均转 interrupted。恢复为未配对工具调用补充消息：

- `TOOL_NOT_DISPATCHED`：新格式检查点中没有执行入口标记，可按当前任务和权限重新决策。
- `TOOL_OUTCOME_UNKNOWN`：已进入执行入口但无持久化结果，不能声称成功或未执行。先核查状态，无法核实的副作用重试需人工确认。
- 旧格式没有执行标记，缺失结果一律保守归为未知。

运行时不会自动重放这些工具。恢复提示不新增授权；原有审批与权限仍适用。

### 边界

不是 exactly-once：崩溃可发生在产生外部副作用后、结果保存前。没有远端幂等键时必须核查状态。
这次覆盖中央任务工具循环，不覆盖所有独立模型请求、文件应用接口或任意外部程序。
进行中的流式帧未逐帧持久化。不承诺断电/磁盘损坏容错。
日志读取、会话删除清理和坏记录提示已实现并有隔离回归覆盖；没有断电或磁盘损坏实验。

## 配置层级

优先级：内置默认 → 全局执行策略 → 当前工作区覆盖 → 已创建任务快照。
工作区以现有工作区身份（mode/path/host）的SHA-256隔离，覆盖文件位于数据目录 `config/workspace-policies/<hash>.json`。
不会自动读取或执行仓库提交的配置，避免不可信仓库获得 Hook/权限授权。

保留原 `GET/PUT /api/execution-policy` 为全局配置；GET 增加 effectivePolicy/workspaceId。

新增（沿用既有鉴权和origin约束）：

- `GET /api/execution-policy/workspace`：覆盖项、合并结果、工作区标识、优先级。
- `PUT /api/execution-policy/workspace`：替换该工作区的覆盖项；允许只提供部分字段，未给字段继承全局。未知字段、null、类型/范围错误、非单个对象、超128KiB拒绝。
- `DELETE /api/execution-policy/workspace`：清除覆盖，恢复全局继承。

新任务/重试及上下文预览使用合并结果，续跑保留旧策略快照。子任务继承既有父任务策略快照。
工作区层现在仅覆盖 ExecutionPolicy 字段，不改变沙箱、模型Provider、插件权限或独立审批策略。设置界面已增加工作区 JSON 覆盖编辑器、导出和合并结果查看。功能与浏览器验收待进行。

## 当前证据

已在已有开发容器执行 gofmt 和 `go build -buildvcs=false -o /tmp/aide-harness-integration ./cmd/aide`，退出码0；`git diff --check`通过。
新增隔离回归已运行；未调用付费模型，未替换运行服务或发布镜像。
正式交付必须补齐目标其余范围并分别记录行为验收和发布状态。

## 扩展配置与运行连接（第二阶段源码）

内置默认 → 数据目录全局 `config/harness.json` → 工作区 `config/workspace-harness/<hash>.json` → 任务快照。
`GET/PUT/DELETE /api/harness-config` 与 `/api/harness-config/workspace` 使用现有接口鉴权。PUT 替换该层 JSON 覆盖，数组整组替换，未知字段/null/类型错误/越界拒绝；最大128KiB。
设置内提供全局和工作区 JSON 编辑器、保存、重新加载、清除覆盖、导出、合并结果查看。工作区编辑器提交身份，切换工作区后拒绝旧草稿保存。

字段示例（不表示已部署此配置）：

```json
{
  "version": 1,
  "skills": [{"name":"source-check","description":"核对资料来源", "instruction":"只报告实际返回的页码与内容。"}],
  "agents": [{"name":"reader","description":"资料阅读", "instruction":"记录来源和缺口", "tools":["read_file","read_skill"]}],
  "hooks": [{"name":"inspect","event":"before_tool","tool":"write_file","command":"pwd","enabled":false,"timeoutSec":10}],
  "denyTools": [],
  "maxAgentDepth": 3
}
```

Skills 由操作者显式配置，说明可内联或读取工作区相对文本路径（二选一），64KiB上限。目录摘要在上下文计数前注入，`read_skill` 按需读取。说明不新增工具权限；不会自动扫描不可信仓库执行内容。
子代理可以指定角色、同一Provider内模型ID、现有采样Profile和工具范围；继承父任务策略/扩展快照。工具列表未设置/null表示继承，显式空数组表示禁用全部工具；指定列表只能缩小父任务范围。深度默认3、可配0–6，0禁用子代理。没有新增多Provider路由或无限并发承诺。
Hooks 支持 `before_tool`/`after_tool` 和精确工具名或 `*`，默认无Hook。命令走同一run_shell审批与沙箱，超时1–120秒从审批结束后计算。失败停止当前循环；Hook本身不递归触发Hooks。Hook执行前/后保存独立intent/result摘要。中断后结果未知的Hook设置恢复拦截，续跑不能再次执行匹配Hook；须先检查外部状态，再明确重试任务。

当前构建证据：第二阶段在已有开发容器执行gofmt与 `go build -buildvcs=false -o /tmp/aide-harness-integration ./cmd/aide` 退出0；两个JS文件 `node --check` 与 `git diff --check` 退出0。相关隔离回归已通过；子代理使用本地模拟模型验证模型ID和工具范围。浏览器布局的实际验收仍未完成。

第三阶段继续补齐插件装配、任务工作树、授权追溯与日志管理源码（见下）。各机制的运行验收及正式交付尚未完成，目标仍在进行中。

## 授权、插件与日志管理（第三阶段源码）

子代理记录 parentTaskId，独立审批沿父会话/任务链查找真实用户消息和插话；子代理生成的user-role任务文本只作为delegatedTask待审数据。缺失父任务、已删除父会话或循环归属转人工。历史子任务无parentTaskId时不猜授权。审批仍仅针对确切shell命令，浏览器/电脑控制仍保留原有逐动作确认。
所有插件调用均在入口检查当前启用状态和全局工具禁用状态，覆盖普通与daemon插件。同步插件新增provides/requires契约和ctx.consume真值装配；详见 `docs/plugin-protocol.md`。工具权限仍独立于服务依赖，不新增模型自行安装/启用权限。

`GET /api/sessions/{id}/runs/{run}/journal?after=0&limit=100` 为已鉴权的追加事件读取接口，要求任务属于指定会话，默认100/最多1000条，使用next序号翻页，读取普通文件上限64MiB。遇到坏记录或未完整写入记录给warning，未将其冒充结果。删除单会话或清空归档会话会删除所属日志；活跃/中断任务不按年龄自动清除。操作系统删除失败会记录服务日志，不承诺磁盘损坏恢复。

## 任务工作树（第三阶段源码）

设置提供名称/起始版本输入、创建、选择和归档入口。`GET/POST /api/worktrees` 与 `POST /api/worktrees/{id}/archive` 使用现有鉴权；仅支持本地Git仓库根，读取工作区身份防止旧草稿错项目，read-only模式拒绝修改。工作树放在该仓库已忽略的 `.cache/aide/worktrees/<随机ID>`，路径组件禁止符号链接。最多64个登记，保存在数据目录config/worktrees.json。
创建先解析固定commit，先持久化creating记录，再用参数化Git调用创建detached HEAD。禁用Git hooks/fsmonitor；有checkout过滤器时拒绝自动checkout，需要操作者在终端审阅处理。不会复制原工作区未提交修改、不自动提交、合并或推送。失败标outcome_unknown并保留文件/登记，不自动重试或清理未知副作用。
选择按钮将路径填入既有工作区设置，保存后才生效；新任务记录worktreeId并使用既有WorkspaceID/AgentRoot快照。子任务继承，续跑保留旧任务；原任务不会因切换目录被重定向。归档只改登记状态、保留全部文件，不创建快照提交或删除checkout；当前活动工作树需先切回原目录再归档。

构建结果：第三阶段Go构建退出0；插件宿主、页面JS语法检查和diff检查通过。源码已实现不等于工作树、依赖装配、授权追溯或恢复的运行验收通过。正式服务仍未替换，未发布。


## 隔离验收更新（2026-10-07）

新增 `internal/server/harness_integration_test.go` 验证配置API/严格字段/重启快照、执行前保存失败停止、日志0600/摘要/归属/翻页/删除、未知工具与Hook恢复拦截、Skills读取、父授权链、子代理实际模型ID与父工具交集、插件服务真值传递/停用/循环/重名、本地Git隔离创建和保留文件归档。

人工审批请求先保存才显示，答复先持久化才唤醒；保存失败不放行，新增答复保存失败回归。插件拒绝覆盖内置工具名，重启留下的creating工作树登记归为outcome_unknown。

新增Harness和审批回归通过（4.147s）；最新race检查通过（11.988s），候选构建退出0。扩展工具/工作流回归通过（10.827s）。完整Go回归未全通过：旧模型测试仅更新计划却期望收尾检查，现改为实际读文件后验收；daemon测试启动未启用插件，现显式启用以保持停用约束；Office测试缺开发容器Python依赖。全量结果在任务记录单列，不把局部通过当作全量通过。

开发容器仅补齐Node；候选 `/tmp/aide-harness-integration` 未替换生产、未提交推送、未发布镜像。Mac仍锁定，配置保存/重载、审批卡片和工作树选择/归档浏览器验收NOT_RUN。目标保持进行中，不声称与其他产品完全等价。


## 配置使用与生效方式

设置中的「执行策略」调整预算、工具轮次、自主执行/资料调查/收尾提示词，支持JSON导入导出。相应「当前工作区执行策略覆盖」只填写项目需要覆盖的字段，例如：

```json
{"chatBudgetSec": 900, "toolMaxRounds": 80, "completionReviews": 2}
```

「扩展机制配置」分别编辑全局和项目层的Skills、Hooks与子代理。填写JSON后点击保存，重新加载确认持久化；合并结果可查看。项目层数组整体替换全局同名数组，显式空数组清除该项目配置，清除覆盖恢复继承。插件服务在插件面板安装/启用后按依赖装配。上述配置变更无需编译、重新导入或重启产品。已有运行/续跑任务保留创建时快照；新任务/重试取当前合并配置。

只读资料角色示例：

```json
{"agents":[{"name":"reader","description":"只读核实资料","instruction":"只报告实际工具返回的来源、页码和缺口。","tools":["read_file","read_skill"]}],"maxAgentDepth":2}
```

模型通过 `spawn_subagent` 的agent参数选择reader角色。未填写model使用父任务模型；指定model仅选择同一Provider中的模型，非跨Provider路由。工具列表只能缩小父任务权限。运行Hooks需显式enabled，命令仍接受run_shell同一审批和沙箱约束。

聊天审批提供任务级独立审核开关；关闭可随时转人工。审核仅对该次确切命令生效，不是永久授权。回归以本地模拟审批模型验证：批准后实际生成文件；在read-only沙箱中，同一批准无法写文件。真实模型判断质量仍未验收，不把模拟服务输出当成真实模型表现。

「任务工作树」填写名称和已有版本，创建后选择并保存工作区才切换。创建不复制原工作区未提交修改；归档保留文件，活动工作树需先切回父目录。creating中断登记转结果未知，先检查目录与Git状态，不自动checkout重试。

## 全量回归结果更新

2026-10-07，开发容器使用临时PYTHONPATH补齐python-docx/openpyxl/python-pptx，Office脚本路径显式指向/src/scripts/office。`go test ./... -json -count=1 -timeout=300s` 退出0：server 211.676s，tts 2.475s，553个测试/子测试PASS。外部实时协议、真实TTS差异、PDF外部工具、密钥生成环境等6个测试SKIP；另cmd/aide无测试，不表示实际外部服务已验收。Go vet退出0。

新增独立审批实际执行/只读沙箱回归PASS 0.946s。完整race回归正在另行记录，不以普通回归替代。浏览器尝试打开隔离开发地址https://localhost:18188失败，ERR_CERT_AUTHORITY_INVALID；未绕过证书校验。Mac锁定亦未解除。页面验收仍NOT_RUN。


## 验收对应表

| 目标机制 | 当前入口 | 行为证据 | 未完成边界 |
| --- | --- | --- | --- |
| 自主执行与独立审批 | toolLoop、approvedShell、approval-mode API | TestHarnessIndependentReviewerReleaseAndSandbox实际文件生成/只读拒绝；TestHarnessApprovalUsesRootAuthorization父任务授权追溯；真实聊天人工审批及辅助审批写出验收文件，辅助审核记录 approved、任务 completed、命令 exit0 | 真实审批模型判断质量未验收；辅助开关的本地模拟交互已通过 |
| 全局/项目分层配置 | execution-policy、harness-config API及设置编辑器 | TestHarnessConfigLayersAndStrictValidation、TestHarnessTaskSnapshotSurvivesRestart；页面保存/重载、策略JSON实际导出导入、候选重启后保留；768/390像素实看 | 新任务生效，进行中任务保留快照 |
| Hooks、Skills | runHarnessHooks、read_skill | TestHarnessSkillsToolsAndHookExecution、TestHarnessCheckpointRecoveryAndJournalOwnership | 不支持Cordis完整事件总线；Hook仍同权限 |
| 自定义子代理 | spawn_subagent agent/model/tools | TestHarnessChildUsesConfiguredModelAndNarrowTools实际模拟模型请求；继承父工具范围 | 真实Provider表现待验收 |
| 插件装配 | provides/requires、ctx.consume | TestHarnessPluginServiceAssembly真值调用；TestHarnessPluginContractsRejectInvalidAssembly循环/歧义/重名拒绝 | 同步受信模块，非跨daemon服务注入 |
| 持久化与结果未知恢复 | checkpointExecution、JSONL、restart/resume | 保存失败停止、unknown/not-dispatched判定、Hook拦截、重启快照、日志权限与归属测试 | 非exactly-once，未做断电/磁盘损坏容错实验 |
| 任务工作树 | /api/worktrees及工作区选择 | TestHarnessManagedWorktreeCreateArchive真实Git创建/隔离/保留文件归档；页面创建/选择/保存切换/切回/归档实看；768/390像素长路径换行复查 | 归档保留文件、不删除checkout |

只读命令判定新增参数约束：find删除/执行/输出文件选项、rg预处理程序、git分支/remote修改、go env写配置、file编译magic均不能按只读前缀放行；含引号等不确定shell语法保守进入常规审批。TestHarnessReadOnlyCommandOptions覆盖这些选项，read-only模式无法用独立审核放宽它们。

该模式仍复用Aide既有容器/SSH部署和命令策略，不是新增与Codex等价的内核级沙箱；workspace-write模式的关键词拦截不能证明任意脚本绝不会写到容器内其他可访问路径。可信插件也沿用现有容器用户权限。不能据本次测试宣称任意代码已经获得操作系统级路径隔离。

独立审批放行前再次核对原会话仍存在、未删除且仍实际拥有任务，审核期间归属变化转aborted、不发送确认。模拟审批增加会话移除竞态，确认没有写出文件。

完整race首轮在2GiB开发容器触发OOM，263.601s终止，不记通过；同一容器下一轮以GOMEMLIMIT=640MiB、GOGC=50、GORACE=history_size=0及-p1运行，未删除测试或修改2GiB限制。该轮编译后增加的审批归属检查须另绑定当前源码验收。

低内存整包race第二轮仍OOM（cgroup oom_kill增加至2），不记通过。下一轮按Go列出的完整顶层Test清单分25项批次，每批独立race进程、检查每个顶层测试都有PASS/FAIL/SKIP终态，再另跑tts及vet。分批覆盖与单进程整包是不同证据，不能把分批运行伪称整包PASS；清单、缺项和终态汇总保留。


## 历史交接状态（2026-10-07，后续更新见下）

最新指纹352ecc6b478a9ad6dffbb01d31c23f83667a55ac564ac34bada720f114127f84。完整顶层清单分批race已结束：473个顶层测试全部有终态，19批退出0，540测试/子测试PASS、4个环境测试SKIP，无缺项无失败。tts race4.171s、vet与quick退出0。单进程整包race仍保留两次OOM失败，不能用分批证据覆盖该历史结果。

独立候选https://localhost:18189（aide-harness-runtime-20261007）采用临时工作区和数据，验证TLS证书、API鉴权、全局/项目配置及预算保存、实际Git工作树创建/归档保留，并重启确认持久化。候选二进制SHA256：5c10de841b46fa08aa396e5e4f5f652d4e52b5af9080942e3b483e9b3701d773。生产实例aide-aide-1保持运行。

当前目标blocked而非complete：连续多轮工具报告Mac锁定，当前候选页面亦ERR_CERT_AUTHORITY_INVALID；未绕过证书校验。请在Mac解锁且候选页面可正常访问后恢复，完成设置、审批、工作树和布局的真实交互验收。所有后台回归已终态，无须再次启动旧运行。未调用付费模型、未推送、未发布或替换正式实例。完整收据见docs/reviews/2026-10-07/harness-integration-verification.json。

## Latest UI acceptance via start.command

The original start.command started isolated compose project aide-harness-launcher-20261007 at https://localhost:18189/ and Safari logged in. The temporary compose mounts a precompiled candidate; launcher build text does not prove a new release image build. Production remains unchanged.

Actual UI checks: global budget480 persisted after restart; global Skills edit/save/reload; execution-policy.json downloaded (4776 bytes, version1, budget480), then imported through Safari file chooser, showing the save-required preview. Worktree ui-review was created/selected/saved, switched back and archived; fixture file retained. Default-root vs explicit /workspace hid entries; local BasePath visibility was repaired and verified in the rebuilt candidate. Existing related race test PASS2.275s. Old full receipts predate this fix; post-fix full inventory batches are running separately.

A candidate-only local mock Provider on127.0.0.1:18190 (host.docker.internal inside Docker, no API key) caused a real chat approval card. Confirming the displayed command produced approval-ui-fixture.txt with actual content ui-fixture and task completion. This proves the approval execution flow, not real-model judgment. English configuration/worktree labels were inspected; original chat text stayed unchanged.

Mac locked during narrow-window inspection; no narrow-layout result. Assisted-review toggle remains manual pending action-time user confirmation. Goal active; mock process93245 and full race40508 retained, no paid calls, push, release or production replacement. Current evidence supersedes earlier blocked handoff text. Screenshots:/private/tmp/aide-harness-ui-evidence/manual-approval.png and english-worktrees.png.

## Post-fix regression completed

Current fingerprint 689dec44ba3eccb8822096146d3210b8b9e3abe1580a718391723547880964b6: all473 top-level tests reached a terminal state,19 race batches exited0,540 tests/subtests passed,4 environment skips, no missing or failed tests. This is full inventory coverage through separate processes, not a monolithic race pass. TTS race2.929s and vet exit0; quick18 checks passed. Candidate binary005704f882db061d0b0cdcab26a59cb083dea340688d338bcbf01e4e3808e44e matches the running isolated container. All regression handles are terminal. Remaining: Mac unlock for narrow UI and action-time user permission for assisted-toggle simulation. Mock93245 remains live only for that pending check; production still running.


## 最终辅助审批页面验收（2026-10-07）

用户回复“可以”后，在隔离候选18189通过 Safari 将输入区开关设为“帮我审批”。本机模拟审核器仅对 `echo ui-fixture > approval-ui-fixture.txt` 返回 approve，其他命令返回 manual；没有调用付费模型。页面显示“已自动放行”，任务 `e4958818edec2f94081ab260794a6783` completed，工具结果 exit code: 0，文件内容 `ui-fixture\n`。持久化审核状态 approved，日志0600包含5条模型请求/工具派发/结果摘要事件。完成任务保留当时 autoReview=true 的快照，输入区已恢复“审批 · 手动”off。截图：`/private/tmp/aide-harness-ui-evidence/assisted-approved.png` 与 `assisted-restored-manual.png`。

此项证明开关、独立审核、派发、保存及返回手动的运行链路，不证明真实模型判断质量。全部明确的机制集成要求已按最新 JSON 逐项验收；无正式服务替换、付费模型验收、提交/推送或公开发布。隔离候选保留，本机模拟器 session88923 仅用于该候选。
