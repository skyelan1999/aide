# Aide 连续工作与知识能力目标

用户于 2026-10-09 授权全部执行。总目标处于进行中；以下不是已经完成的功能列表。


当前逐项状态以[2026-10-10验收审计](../reviews/continuity-acceptance-audit-20261010.md)为准；下文保留各阶段历史边界。

## 工作现场恢复

会话草稿与附件、文件草稿、目录、光标、滚动、星图镜头，按工作区隔离；刷新/重启恢复，不自动发送。

候选提供同源共享恢复记录统计与只读JSON导出，按工作区/类型列出序列化大小，浏览器使用/配额另列。导出不含其他打开标签页独有的即时快照；尚无导入、配额清理、保留期或跨设备恢复。原生Safari已验证统计、直接下载及专业配色浅/深色、390×844窄屏布局；尚不能视为完整容量管理。

## 保存状态与冲突

区分本地记录和服务器保存；断网保留、409暂停、对比合并、重试、不静默覆盖。

## Markdown 历史闭环

正文+嵌入资源整版恢复；外部编辑检测、无实际变更不追加、存储与备份管理。

单文档历史统计和完整归档ZIP导出已提供，含全部版本与去重对象，导出校验摘要；原生Safari下载与独立解包已验证。完整服务数据备份、导入、保留期和垃圾回收管理仍待完成，具体边界见[Markdown历史](../plugins/markdown-history.md)。

## 真实环境验收

本地及可用SSH：创建/修改/保存/重开/恢复、跨会话审批、锁屏、Windows输入与长时间打开。

## 任务成果舱

目标、文件变化、执行、验证、证据、缺口、发布状态统一成果卡。

## 审批与授权管理

解释自动通过和人工原因，作用范围、有效期、撤销和全局同步。

## 连接能力仪表

SSH连接/目录/权限、解析器、插件工具可用性、模型连接及实际检查。

## 渐进索引

项目/目录范围、索引覆盖和预算状态、重点优先、不虚报全库。

## 混合检索

关键词+可配置向量检索、原文精确检索、来源定位和内容指纹。

## 代码关系可信度

确定引用/候选调用/未解析显式区分，可选语言服务。

## 星图路径探索

节点间证据路径、按钮到服务和报告到来源，跳转行号或段落。

## 知识时光

按时间比较真实节点增改移除、历史依据与结论演变。

## 首批实现与边界

新增 `continuity.js` 使用同源 IndexedDB 保存工作区内的会话文字、附件与目录状态，以及文件未保存正文、读取hash、光标和滚动位置。设置“工作现场与本地草稿恢复”可关闭读写恢复；关闭不删除既有记录。单条恢复记录限 2 MiB，超限和存储失败明确提示。记录只在当前浏览器，不是远端备份；当前尚无配额清理面板、保留期或跨设备恢复。

会话重开按当前服务器配置的 workspaceId 隔离，文件恢复提示不会直接覆盖服务器。文件保存冲突暂停自动保存，读取服务器最新正文，提供当前正文、本地正文及合并编辑区；只有采用合并内容后更新保存hash。取消不放宽保存校验。保存成功仅清理正文相同的恢复记录，事务内检查，避免误删其他标签页的新草稿；多标签页同一文件的不同草稿当前仍为最后一次写入，需要后续保留独立分支。

源码阶段尚未自动重开编辑窗口，或保证最后180ms输入在浏览器强杀时完成写入。其他提案的进展见后续各批记录和目标任务账本。Go 编译、JS语法及组件浏览器检查已运行；浏览器检查使用真实 IndexedDB 和模拟服务器冲突，不能替代实际文件API、完整工作台、SSH、锁屏、Windows或跨标签页的验收。

## 第二批：历史读取与外部变更

打开历史列表时重新观察 Markdown 正文及直接引用资源，只有指纹变化才追加快照；当前文件不可读时仍提供已有历史并报告检查失败。正文与资源读取验证对象 SHA-256，工作区读取固定本地路径或 SSH 会话代际。历史窗口跟随锁定关闭并阻止锁定期间重开。

实际临时文件和 API 的三个 Go 回归（含 race）通过，浏览器组件锁定／解锁检查通过。本批当时尚未实现整版恢复，后续第三批已补充；后台持续检测、历史管理和真实 SSH 恢复验收仍待完成。

## 第三批：Markdown 整版恢复

可显式预览并恢复正文与已归档的直接引用资源；使用来源身份和每个文件当前摘要校验，恢复前备份，资源先写正文最后写，每项写后读回确认，保存操作记录。未保存草稿先处理，恢复时暂时只读，错误显示操作编号，不自动重复远端写入。多文件操作不保证跨进程原子性；异常中断记录管理与撤销界面、完整工作台与 SSH 真实恢复仍待验收。

真实临时文件/API 的恢复与冲突回归通过，浏览器组件对比范围／显式恢复并更新编辑器通过；剩余目标仍按原范围推进。

## 第四批：星图与会话光标恢复

星图按知识工作区身份保存浏览器本地视角、来源筛选、节点查询、知识/代码视图、关系视角与深度、选中节点、星域层级、动效开关、持续漂移相位与面板滚动。刷新或解锁重新加载后核对来源和节点身份；失效星域退回仍存在的上级。恢复不触发原文/RAG检索或模型请求；载入期间有新操作时不覆盖该操作。锁定前保存状态，锁定期间不写恢复记录。会话草稿另保存光标范围和滚动位置。

组件浏览器使用真实 IndexedDB：拖动、选中节点、来源筛选、关闭动效后刷新，星点位置和详情保持一致；代码视图、调用候选和向外2跳选项刷新后恢复。截图位于 `.agent-state/starmap-session-preview/recovery-before-final.png` 与 `recovery-after-final.png`。该预览仅含模拟图接口，未验证真实代码分析、工作区切换、锁屏联动或完整工作台会话光标。后续使用 `start.command` 隔离实例验收。

## 第五批：已保存文件位置与真实 API 验收

文件位置记录单独存储，不重复保存已提交正文。相同文件指纹且载入期间没有新输入/选区操作时恢复光标和滚动；指纹变化跳过旧位置。组件浏览器实际刷新保留选区3068–3119与滚动1351.5px；模拟外部指纹变化后保持默认位置，未应用旧记录。完整编辑器、只读查看器与Windows组合仍待验收。

通过 `start.command` 启动隔离真实实例 https://localhost:18211/，工作区和运行数据分别为 `.agent-state/continuity-runtime/workspace` 与 `data`。实际鉴权API通过文件保存、历史观察与去重、外部图片变更、正文和图片一起恢复；过期图片摘要返回409且正文未写入。结果为 `.agent-state/continuity-runtime/api-receipt.json`。该批 API 收据对应当时二进制，不覆盖后续实现。18211 已在后续批次重建，当前包含文件位置恢复、成果舱、引用文件操作、目录附件与来源 AI 可见性；完整界面验收仍待完成。新实例证书提示阻止浏览器访问，已请求用户亲自处理，未绕过。SSH未执行。18211暂保留供验收；生产9999未替换。

## 第六批：任务成果舱与原始执行证据

任务卡新增“成果舱”入口，将目标、工作区身份、模型计划、文件提案/应用记录、工具返回、验证报告、研究来源、未知调用及发布缺口集中显示。文件应用标记只说明历史记录，未读取当前文件复核；模型计划和验证报告保留为声明，不能推导独立验收通过。完成状态同样不代表发布成功。

接口 `GET /api/sessions/{id}/runs/{run}/outcome` 分页返回文件及执行摘要（默认100，上限500）；`GET /api/sessions/{id}/runs/{run}/outcome/evidence/E0001?digest=…` 返回对应工具完整参数/结果，编号按已有工具列表1起计数。摘要与原文指纹不一致返回409，必须重新打开；会话与任务归属检查、全局鉴权沿用现有服务。仅未完成的调用意图计入未知结果。执行日志入口保留，尚未在成果舱内展示日志时间线。

JSON导出为成果摘要：标注分页是否全部载入、文本是否截断及未包含完整原始证据，不称完整证据包。当前导出不冻结运行中任务的跨页快照；再次请求可能看到新增记录。浏览器内的数据仅按文本显示。关闭、会话切换和锁定会终止旧窗口更新。结构化发布回执尚未接入，因此发布状态明确为未记录；它不分析聊天或命令文本猜测发布成功。

3项成果舱 Go race 回归通过，覆盖历史/声明边界、稳定编号与分页、完整证据/摘要冲突、归属/鉴权、非法参数、快照不共享可变计划，以及已完成意图不计入未知结果。最新候选二进制由 `start.command` 重启18211隔离实例，使用明确合成会话记录（未调用模型、无生产数据），真实鉴权API验证上述主要读接口；结果位于 `.agent-state/outcome-preview/api-receipt.json`。该二进制已包含文件位置恢复与成果舱。

组件浏览器实际打开、展开并读取原始参数与返回；390px窄屏内容宽354px，无横向溢出。截图 `.agent-state/outcome-preview/original-evidence.png` 与 `narrow.png`。组件API为模拟记录，不能替代完整工作台。点击JSON导出后，浏览器下载事件15秒超时，下载验收未通过，仍待修复或实际浏览器复核；深色主题、运行中分页变化、完整工作台锁态与SSH尚未验收。18211证书提示等待用户亲自处理，未绕过。

### 2026-10-09 引用来源 AI 作用域补充

引用来源增加“对工作区可见”眼睛开关，按工作区身份持久化，控制后续 AI 工具、附件和星图/RAG上下文，保留手动浏览。实现与实际验收边界见[来源AI可见性任务](../tasks/source-ai-visibility-20261009.json)。目录附件及右键入口见[附件任务](../tasks/file-task-attachment-20261009.json)。整体目标仍未完成，未进入发布。

## 审批授权管理首批

策略中的“管理审批授权”列出当前实例记住的规则：类型、工作区、根目录、精确载荷摘要、创建与到期时间。现有规则默认永久，与已有“永远记住”保持兼容。可将单条规则设为未来一年内的到期时间或永久，或显式撤销；服务使用 revision 校验，陈旧修改返回409。只匹配同一工作区、根目录及完整命令或文件提案，执行前检查到期，无法解析的有效期不放行。已到期的规则在用户再次显式记住后重新建立，不扩大权限。

撤销与有效期变更持久化并广播全局审批事件；不取消已经执行的操作。原有全部清空入口保留。管理UI已完成后述模拟接口组件检查，完整工作台尚未验收，也未部署到18211或生产9999；首次审批卡已在后续补充期限选择；审批依据解释和运行中变更的完整联调仍在本目标范围内。

本批实际验证：Docker `aide:local` 执行 `go test -race -buildvcs=false ./internal/server -run "TestApprovalRuleManagement|TestRunShellRequiresExactConfirmationForWrites" -count=1 -timeout=90s` 通过（3.671秒），包含规则清单、revision冲突、到期拒绝、显式重新记住、单条撤销和持久化，以及原有命令确认回归；JS语法与文档链接检查通过。此结果不证明浏览器管理界面或真实模型审批已验收。

### 首次审批期限与组件检查

命令确认及文件提案卡保留“仅本次”，记住操作可选择永久、1小时、1天或30天，提交时计算到期时间；服务在明确当前审批应答内验证并持久化。现有接口省略 expiresAt 仍按永久处理。已存在相同规则再次明确审批时可修改期限，不扩大匹配范围。独立审核记录增加时间及“审批通过不证明执行成功”说明；后续批次已将规则匹配依据写入运行证据；完整工作台和真实模型联调仍待验收。

实际 Docker Go race：`TestApprovalRuleManagement|TestApprovalRememberExpiryFromAnswer|TestRunShellRequiresExactConfirmationForWrites` 通过（4.301秒），包括无效期限不消耗应答、明确应答保存期限和重复应答409。组件预览抽取实际 app.js 管理窗口与期限选择函数，接口为模拟：浏览器选择1小时后按钮切换、到期时间修改后重读、单条撤销后显示无授权、关闭后焦点回到入口已观察。截图 `.agent-state/approval-management-preview/theme.png`（补齐实际深色主题令牌）。撤销点击观察曾超时，后续页面及模拟DELETE回执确认操作已完成，未重复执行。完整工作台、跨窗口广播、真实锁屏、首次文件提案期限和真实模型审批尚未验收；18211和生产9999仍未部署本批。

### 审批决策来源与成果舱证据

新审批记录区分人工确认、模型独立审核及记住的授权匹配，带工作区、根目录、载荷指纹；授权匹配记录额外带规则编号和当时期限。首次人工确认记录的是批准或未批准，文件提案批准仍需经过路径、工作区与版本检查。记住的命令／文件提案在放行前持久化决策记录，保存失败不放行；记录证明当时决策，不证明实际执行成功，不取消在途操作。

成果舱读取任务审批记录的独立快照，展示来源、原因、具体操作、范围及摘要。最多保留最近50条；旧记录未补造来源，明确显示历史来源缺失。完整审计长期归档、只读免审批路径依据和跨窗口规则撤销的实际联调仍需完成。审批策略不是操作系统沙箱。

实际相关 Go race 回归通过（6.273秒），覆盖匹配指纹、记忆来源、成果舱快照不共享可变审批记录、未知会话和保存失败不放行、人工确认保存期限及重复应答拒绝。浏览器组件使用模拟接口，实际打开成果舱并展开授权匹配记录，看到了规则编号、期限、范围及摘要；截图 `.agent-state/approval-receipts-preview/evidence.png`。完整工作台和真实模型尚未验收；本批未部署；源码推送记录见[审批检查点](../reviews/approval-checkpoint-20261009.md)。

另外运行现有 `TestAssistedApprovalNewFiles|TestWorkflowApprovalConflictAndPersistence|TestHarnessManualApprovalSaveFailureDoesNotRelease|TestSSHApply` 的 Go race 回归通过（13.011秒）。SSH 提案测试使用测试夹具，不能替代真实远端目录／写入联调；辅助模型为本地模拟服务，未调用付费提供商。

## 连接与插件能力仪表首批

设置新增连接与能力栏，将配置、工具登记与具体检查分开，按工作区/配置/凭据绑定结果；五分钟失效、重启清空，并发检查与配置中途变化不误采纳。支持本地/SSH根目录、来源列表、MCP发现、插件入口、文档依赖及模型列表。源码行为、实际回归和未验证项见[能力仪表](capability-dashboard.md)。本批尚未在完整工作台、真实SSH和提供商上验收，不能称连接诊断目标完成。


## 轨迹内的成果舱（2026-10-09）

- 轨迹一级入口统一为「历史 / 调用记录 / 成果舱」，移除任务卡上的独立成果弹窗入口。
- 成果舱默认选最新任务，可切换同会话其他任务；原始证据、分页、审批依据、缺口和摘要 JSON 导出沿用原接口。小秘会话仅提供对话历史，成果页显示无任务成果提示。
- 历史格式和压缩按钮只在历史页显示；成果摘要使用成果页自身导出按钮。切换页签、关闭轨迹及锁定会使旧请求失效，避免延迟返回覆盖新视图。
- 浏览器组件夹具验收覆盖三页签、历史任务选择、原始证据展开、快速切换和 390px 窄屏；使用真实前端源码与模拟 API，不代表生产会话完整联调。未部署、未发布。


## 索引配置首批

当前工作区可保存本地文件/目录/深度预算与重点相对路径，更新使星图缓存失效。优先处理已发现的重点目录，不把预算内结果当全库。配置API与实际扫描回归通过，组件UI保存及重读已操作；本地目录范围与续扫队列已接入，远端SSH续扫与大库性能仍待验收。详情见[知识星图](knowledge-map.md)。整体目标继续进行中。

## 混合检索缓存检查点

工作区可配置兼容 embedding 接口、模型和融合权重，默认关闭；密钥保存在 SecretVault。原文检索不请求模型。文档向量按工作区/提供商/模型/配置代次和片段内容身份加密缓存，查询向量实时生成；旧维度缓存失效后降级并重建。后端本机HTTP夹具已验证重复查询、冷启动、内容变化与模型变化；真实提供商、完整鉴权工作台与SSH联调仍待完成，生产尚未部署。

## Go 快照类型解析检查点

可选 Go 类型服务已接入当前工作区索引配置与代码图；只提升成功检查的已索引包具体绑定，接口/函数变量保持未确认。当前本机隔离鉴权工作台已实际保存重读配置、查看类型标签、点击源文件并选中对应行。完整项目依赖、SSH、Windows和其他语言的可选类型服务仍待验收或扩展。详情见[知识星图](knowledge-map.md)。总目标保持进行中。

## 系统浏览器成果导出补验（2026-10-09）

在 `start.command` 启动的 18211 隔离候选中，系统 Safari 从会话 #42 的轨迹切换到成果舱并点击“导出成果 JSON”。文件实际写入浏览器下载目录，4311 字节；解析后包含 2 份文件提案和 2 条执行记录，明确标注摘要导出、全部页已载入、文本有截断及未包含原始证据。SHA-256 为 `3add73be880aa438127142e9342e20a9f5cafc70bba16209b03473fa8a5ed79a`。收据为 `.agent-state/continuity-runtime/outcome-safari-download-20261009.json`，截图为同目录 `outcome-safari-20261009.png`。此前组件下载等待超时保留为历史失败，本次真实浏览器下载通过。会话为持久化合成验收数据，不代表模型执行；运行中任务跨页一致性与真实任务全链仍需单独验收。

## 当前源码 SSH/SFTP 复验（2026-10-10）

撤下记忆神经可视化并保留普通记忆兼容层后，运行双服务器隔离验收，`-race -count=2` 通过（28.223 秒）。覆盖远端记忆回读、工作区/自动系统文档/独立 SFTP 的 Markdown 整版恢复与冲突拒绝、来源文件操作、原文检索与时间线、渐进索引及独立进程续扫。测试前后源码 SHA-256 一致。收据见 [SSH 联调任务](../tasks/ssh-continuity-integration-20261009.json)。

这是容器内真实 SSH/SFTP 服务及 API 验收，未替代系统浏览器 SSH 工作现场恢复、物理断网、Windows 或真实模型/向量提供商验收。生产实例未更新；总目标保持进行中。

当前源码指纹 `1c117eae047bc3f424c8689c60fe33700959b91fb66b4656462225a30d6ede2f` 的完整门禁通过：server Go race 398.779 秒、TTS 2.861 秒、go vet 通过。日志 `.agent-state/verify-20261009T161646006596Z.log`。系统 Safari 的本地能力仪表检查见[能力仪表验收](capability-dashboard.md)。完整门禁与局部浏览器检查仍不代表总目标全部验收或已发布。

### 2026-10-10 Safari 锁屏同步验收

在隔离实例 18211 临时配置测试密码，实际验证后端锁态同步、工作台密码解锁、解锁后刷新不保持锁屏，以及原工作台解锁后原星图自动恢复。锁态代际依次为 1、3；这证明该次后端驱动路径，不能替代所有入口或长时间空闲验收。

左下角运行卡片的锁定入口点击未改变锁态，原因尚未确认，该项仍未通过。测试结束恢复原设置并移除本次创建的锁态文件；生产 9999 未改。实际记录见 `.agent-state/continuity-runtime/lock-browser-receipt-20261010.json`。

### 2026-10-10 锁定入口修复

锁定蒙版原先仅在悬停时启用命中；已配置密码时改为持续可点击，悬停仍只控制视觉呈现。增加 Enter/空格操作及未配置密码时的禁用和焦点状态。`node scripts/lock-entry.test.cjs` 覆盖真实绑定源码的鼠标、键盘、重复绑定与禁用状态；系统 Safari 在 18211 连续两次点击显示密码锁屏，一次密码解锁成功。键盘实际 Tab 导航仍未验收。临时配置已恢复，生产实例未改。此前无响应项由这次证据补齐；不代表全部锁屏场景完成。

### 2026-10-10 星图刷新恢复

系统 Safari 在隔离实例 18211 改变未执行搜索文字、工作区来源筛选和动效状态后刷新，三项状态恢复，页面提示未自动执行检索或 AI。拖动和滚轮操作已执行，但暗背景截图不足以证明精确镜头坐标一致，镜头精确一致性与跨工作区隔离仍待验收。测试后恢复原搜索文字、全部来源及动效，镜头归位。

### 2026-10-10 Safari code-path acceptance

Current full gate: 30 checks PASS; fingerprint `f1dd7d2f07601035d85d5804e9b0253de5db57ac9203448a786868e94808a2d4`; log `.agent-state/verify-20261009T173108678885Z.log`. Native Safari on isolated 18211 verified the one-step static candidate path entryPoint to collectEvidence at L8:9. Opening the relation source selected `return collectEvidence()` in the read-only viewer at L8. Evidence screenshots: `.agent-state/continuity-runtime/code-path-safari-20261010.png` and `code-path-source-safari-20261010.png`. This proves local candidate-path navigation, not runtime execution or type binding. Search and knowledge view restored; fixture retained. Production 9999 unchanged; SSH/theme/performance and release acceptance remain.

### 2026-10-10 File draft scroll recovery fix

Native Safari exposed a focus-order defect: restoring a draft preserved selection but moved the viewport back to the caret. Recovery now focuses with preventScroll before restoring selection, applies input updates, then restores scroll and dispatches the scroll event for the line gutter. Focused source regression passes. On isolated 18211, before/after refresh and explicit draft restore both show line 60 at the top and the same selected draft text. The server fixture remains 8000 bytes without the unsaved marker. Screenshots: `.agent-state/continuity-runtime/file-recovery-fixed-before-safari-20261010.png` and `file-recovery-fixed-after-safari-20261010.png`. This is local TXT acceptance; SSH/Windows and exact pixel measurement remain. Candidate restarted through start.command; production unchanged. Full gate requires a fresh run after this fix.

### Workspace isolation checkpoint (2026-10-10)

The isolated authenticated API switched to a second local workspace and read its distinct baseline. The original workspace configuration was restored and its server file remained unchanged. `node scripts/continuity-isolation.test.cjs` passed for same-ID chat, scene, file and file-view records, including stale and matching deletion. This uses an in-memory IndexedDB fixture, not Safari storage acceptance; actual browser workspace switching remains pending.

The recovery-scroll full run completed 31 commands with exit 0, but the source fingerprint changed during the run, so the aggregate gate is FAIL. A fixed-source full rerun is required before release.

### Live workspace transition defect (2026-10-10)

Native Safari at the isolated 18211 instance saved workspace B and the authenticated API confirmed the B path. Workspace A's unsent session #42 excerpt and file attachment remained visible. Settings read back the B path, establishing that this was an actual transition rather than a failed save. The original empty path was restored through Safari. `saveWorkspaceConfig` refreshes configuration, sources and files but does not reset or restore the workspace chat scene; its in-memory draft map is keyed only by session ID. This acceptance is FAIL. A storage-key fixture cannot substitute for the live transition. The fix must preserve A before switching, suspend recovery writes during transition, reject stale asynchronous responses, clear A scene state, and recover only B records; Safari A/B/A verification remains required.

### Live workspace transition repair (2026-10-10)

The workbench now persists the old workspace before saving its replacement, scopes the in-memory draft cache by workspace and session, suspends local writes while switching, invalidates pending session selection, rejects file-list replies for an old scope/location/query, clears the old scene, and restores the destination's scene. The workspace label updates immediately.

Native Safari on isolated 18211 verified distinct B unsent text without A's attachment, restoration of A's original excerpt and attachment, repeat B recovery, and B reload recovery. Final A was restored. Earlier contaminated B records were explicitly replaced with a disposable B draft for this test; no automatic migration of legacy contaminated drafts is claimed. The source VM transition test and lock-entry regression passed. The previous 32-check full PASS predates this repair and must be superseded. SSH transitions, standalone file/camera isolation, and unavailable Windows remain outside this browser evidence.

### Same-path file workspace isolation in Safari (2026-10-10)

The disposable standalone TXT route was opened in workspace A with an existing local draft. After switching only the isolated backend to B, reloading the same route displayed B's distinct baseline without A's recovery prompt. Restoring A and reloading brought back its prompt; explicit restoration selected `AFT-RECOVERY-20261010`. A's server file remained 8000 bytes without the draft marker; B's baseline was unchanged. The temporary tab was closed and original workspace configuration restored. This proves local same-path reload isolation for an existing A draft; it does not prove live standalone workspace transitions, a B draft, SSH or Windows.

### 2026-10-10: star map workspace defaults

When changing to a workspace without a saved sky scene, the map resets search, retrieval mode, filters, camera, motion phase and selection to defaults. Old AI questions, answers and retrieval batches are cleared. A saved destination scene is restored instead; asynchronous recovery does not overwrite input made while loading.

Safari on isolated port 18211 verified a live local A → fresh C → A transition without reload: C had an empty query and node retrieval; A recovered its previous query and RAG mode. The candidate was launched with `start.command`. This does not prove SSH scene isolation, Windows behavior or real model retrieval. The previous full run passed Docker Go race/vet but failed the session-click VM test because its fixture omitted the new draft-key helper. The fixture now loads the actual helper and the focused check passes; the full gate must be rerun.

### 2026-10-10: remote autosave and asset restoration

System Safari on isolated 18211 saved a Markdown edit through a real disposable SFTP service; direct remote readback matched. An external SVG-only edit produced a new history version with identical Markdown. Safari restored the prior resource, and remote SHA256 readback confirmed both the restored SVG and unchanged body.

The autosave switch remained disabled after history restoration until reload. This is an open UI lifecycle defect, so the whole flow is only partially accepted. Original autosave settings and source registrations were restored, and the temporary SFTP container/network and test credential were removed. This local fixture does not establish WAN SSH reliability or Windows behavior.

### 2026-10-10: restore callback writable-state repair

History restore now returns the editor to its prior writable state before invoking the successful update callback. This lets autosave recompute the control state correctly. Source VM checks cover success, request failure and stale workspace guards. Safari on isolated 18211 confirmed successful restoration followed by an immediately usable autosave switch, including enabling it without a reload; the original off preference was restored. The post-fix browser check used a local history fixture; remote body and asset restore were verified earlier through SFTP, with the defect then observed. Latest full gate remains in progress.

### 2026-10-10: incremental thousand-node browser acceptance

Safari on isolated 18211 automatically grew a one-directory, 1105-file fixture from 806 total nodes to 1111 without a reload. The source log exposed per-batch quotas and a 1732-entry catalogue limit; the actual API confirmed 1105 files and one directory ready with no limiting reasons. This demonstrates rendering beyond 1000 and incremental discovery under a configured budget, not unlimited indexing or measured large-library performance. Original configuration was restored and generated fixtures were removed.

### 2026-10-10: SSH workspace scene recovery

Native Safari on isolated 18211 opened a real disposable SSH workspace without inheriting the local draft or attachment. An unsent SSH-specific chat draft and remote child directory survived refresh. A standalone remote Markdown unsaved draft also survived refresh and explicit recovery; remote readback confirmed the baseline was not overwritten. Restoring the original local configuration and refreshing returned the original local excerpt and attachment. Configuration switching used the authenticated API followed by Safari reload; this does not establish live UI switching or WAN interruption handling. Temporary SSH credentials were cleared and the container/network removed.

The latest full gate completed successfully with 35 checks, including Docker Go race/vet, against content fingerprint `a1b1cb65ed56b5e981cbe1309636134c2f483ff10c983980ce664f8babd6d7de`. Completion of that gate does not close the remaining real-provider, Windows, performance or release acceptance.

### 成果舱系统回执（2026-10-10）

成果API将服务端持久化步骤file_application_receipt投影到独立systemReceipts，编号S+步骤序号，单条预览2400字符；摘要systemReceipts不计入模型提交的verificationReports。原始证据端点同时支持S编号，以任务所有权和name白名单限制读取，digest变化返回409。现存任务无需重跑模型即可展示保存的回执。完整原文和执行状态保留，不从completed推断本地验证成功：回执也可能明确说明待人工审批、远程未读回或工作区变化。

界面独立显示系统回执及历史范围提示，支持展开原始记录；JSON导出包含回执预览、哈希与截断标志，textTruncated涵盖回执。导出仍是摘要，不包含所有原始工具证据，也不重新验证当前文件。

Go race覆盖失败/待人工状态、长回执截断、独立快照、摘要分类、编号白名单、任务身份和digest冲突。原生Safari在实际9999会话40验证S0002原文与下载JSON4708字节，SHA-256 b7686477fa9bc92399f469213fd312456270448ec47216ab412d145aa24f8003。多分页运行中快照和发布记录仍待验收。

### 星图标签页隔离与首次失败恢复（2026-10-10）

星图现场记录按工作区与当前标签页保存，使用同步sessionStorage快照和IndexedDB共享回退。已有页面刷新恢复自身视角；新标签首次读取最近共享现场，不提供关闭标签后的多分支选择。首次索引请求失败后，自动同步先重试完整加载与现场恢复，再进入增量更新；恢复时不自动执行AI或正文检索。数值source VM通过；最终原生Safari验证两页独立镜头和相位，真实首次HTTP 503后自动重试仍恢复本页现场。详情非零滚动、嵌套路径、关闭标签后多分支和容量淘汰尚缺浏览器证据，见当日审计。

### 同文件标签页草稿与位置隔离（2026-10-10）

文件草稿和已保存位置现在同样优先使用标签页sessionStorage，首次新页从工作区内共享IndexedDB读取回退。已有标签刷新保持自己的正文、位置；保存/忽略仅清理本页匹配正文，不删除另一页不同正文的共享草稿。原生Safari实际9999验证本地TXT的A/B不同草稿分别刷新恢复，以及保存A后B保留并提示对比合并。仍未提供关闭所有标签后的多分支选择，也没有配额清理管理；SSH同文件、Windows及浏览器强杀证据未完成。

### 2026-10-10: closed-tab file draft branches

File recovery now retains divergent loaded-document branches in IndexedDB, scoped by workspace and file identity. A new tab offers a compact draft selector with saved time and UTF-16 text length; a live tab's own snapshot stays first. The user explicitly restores or compares a branch. Saving removes only branches with the saved body; ignoring removes the selected branch. Identical body/hash snapshots consolidate. Typing dismisses the stale recovery banner to prevent accidental replacement of newly entered text. Existing single-record drafts remain a compatibility fallback.

Each serialized draft remains limited to 2 MiB; each file's serialized branch array is capped at 32 records and 8 MiB. Exceeding either branch budget aborts the persistent transaction and shows an error; existing persistent drafts are not evicted. The current edit can remain in tab-local storage if that write succeeds. Browser storage quota may fail earlier; this is not a server backup or a whole-origin storage manager. Closing immediately before an IndexedDB transaction finishes remains subject to browser unload behavior. Chat/star-map closed-tab branch selection is not provided by this file-only change.

Native Safari on actual 9999 verified two different TXT drafts, closing both tabs, selecting Alpha in a fresh tab, saving Alpha, and comparing retained Beta against Alpha after reload. Disk readback exactly matched Alpha. Capacity and workspace isolation are source-fixture checks; SSH, Windows, quota-exhaustion UI and abrupt browser termination remain unverified.

### 成果记录分页版本（2026-10-10）

成果接口返回 `snapshot` 内容指纹；客户端加载下一页时携带该指纹。指纹涵盖完整成果投影，包括文件提案、工具返回摘要及原文指纹、审批、系统回执、验证报告、状态和会话标题。即使执行日志序号未递增，修改已有工具返回也会使旧分页失效。接口返回 HTTP 409；成果舱保留已有页以便查看，但停止追加、禁用导出，提供“重新加载成果记录”，显式丢弃旧页后从第一页重新收集。

导出字段 `exportScope.snapshot` 和 `consistentSnapshot` 标识已加载记录属于同一版本；`allPagesLoaded` 单独表示是否已加载全部页。导出仍是摘要，原始证据按编号及其独立指纹读取。该机制不冻结服务器任务、不自动重放工具，不证明文件现状或验收成功。持续更新的任务可能反复使分页失效；旧 API 客户端不传 `snapshot` 时仍可访问，但不获得分页一致性保证。计算指纹需要遍历当前成果记录；大型任务的成本尚需专项测量。


### Recovery backup import and transactional undo (2026-10-10)

Settings / file rendering now accepts schema-1 aide-local-recovery JSON backups. Preview classifies new, identical and conflicting records. Identical values do not produce writes; conflicts are skipped unless replacement is explicitly selected. Imported values and prior values are archived in one IndexedDB transaction. If any current record changed since preview, the whole import is rejected. Cancellation, lock and write failure abort rather than partially apply.

Undo requires every target to still equal the imported value. Later edits reject the whole undo and retain its archive; successful undo restores prior records or removes imported additions atomically. Operations affect shared browser records only, never live tab snapshots, server files or task submission. A first-opened new tab can consume shared fallback data.

Import and undo archives each have a 64 MiB budget; ordinary records have 2 MiB and file branch arrays 32 entries / 8 MiB limits. Undo archives are excluded from record counts and exports, but included in browser usage. Automatic cleanup and retention are not implemented. Export envelope overhead near 64 MiB may exceed the import file limit; this boundary remains to be unified. Cross-device consumption and real quota/crash evidence remain open. Preview and undo controls below a long inventory also need layout refinement.


### Recovery management follow-up (2026-10-10)

Import preview and undo operations now precede inventory groups, keeping the action controls near the toolbar even after inspecting many scopes. Export checks the complete serialized envelope against the same 64 MiB file limit used by import, in addition to the incremental record budget. The earlier envelope-overhead gap is closed; no record is removed when an oversized export is refused. The directed fixture constructs records below the aggregate limit whose complete envelope exceeds it and verifies rejection. Actual maximum-size browser transfer remains unverified.
