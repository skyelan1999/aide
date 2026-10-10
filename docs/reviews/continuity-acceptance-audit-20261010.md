# 连续工作与知识能力：验收审计 · 2026-10-10

总目标仍进行中，12项工作流均不能按完整范围标记完成。当前证据优先于任务账本中较早的阶段摘要；历史失败及修复记录保留。

## 当前基线

- 接手基线：`78bbf406ce2403e9c6093f74fe5182f10c3d08c0`。用户随后要求先推送功能批次，源码/测试/归档提案和文档已通过提交8070c0c推送origin/main；推送结果见[功能批次交接](function-batch-20261010.md)，正式发布与其余验收继续。
- 星图最终修复 full：36项 PASS，Go race/vet通过；收据 `20261010T011324648367Z`，源码指纹 `7f2a30689e7d906b930d0733f3ef1a323290772e17eb9e2bfdf0226842e09da9`。随后提交范围格式检查修复了测试脚本行尾空格，并为jsPDF官方原包许可证注释设置单文件空格检查豁免；推送前全量重跑36项PASS、Go race/vet退出0，收据20261010T012418170337Z，指纹783ed132f9fa5e1a9dbc7d6ba944076fbec28c9dc992c77d4d261aa7dc26123b。历史收据保留于下文。
- 服务：隔离18211保留；按用户明确要求运行项目start.command，实际9999已重建为当前已通过full的源码。保留镜像aide:before-live-acceptance-20261010用于回退。UI证据采用系统Safari。
- 原始日志和截图位于忽略目录；逐项结果保存在[总任务账本](../tasks/aide-continuity-knowledge-20261009.json)的 `current_checkpoint_20261010`。

类型绑定旧AST诊断修复已通过定向race、重建候选（start.command）API及系统Safari日志复验；最新 full 已覆盖星图来源覆盖状态 JS/CSS 及新增检查；该界面同时通过重建和系统 Safari 验收。回归不替代未完成的逐项产品验收。

## 逐项覆盖与缺口

| 原始工作流 | 当前证据定位 | 尚未满足的范围 |
| --- | --- | --- |
| 工作现场恢复 | chat_refresh_browser, workspace_scene_fix, file_scroll_recovery, file_workspace_isolation_browser, sky_workspace_default_fix, ssh_workspace_recovery_browser | 本地与隔离SSH草稿/目录恢复已有Safari证据；精确星图镜头与相位、标签页隔离和首次503恢复已有Safari证据（sky_recovery）；SSH光标滚动、关闭标签后的草稿分支/容量管理仍缺验收。 |
| 保存状态与冲突 | autosave_conflict_browser, offline_save_browser, ssh_browser_save_conflict, ssh_autosave_assets_browser, history_autosave_lifecycle_fix | 本地合并、服务中断重试与SSH保存已验证；修复后的SSH历史恢复及立即自动保存已由Safari和远端读回复验（ssh_history_postfix_system_browser）；物理网络中断仍未测。 |
| Markdown 历史闭环 | history_assets_browser, ssh_autosave_assets_browser, history_autosave_lifecycle_fix | 本地/SSH SVG外部变更与整版恢复有读回证据；本地CSV、draw.io源文件和ZIP附件外部更新/整版恢复/去重已通过Safari与SHA256读回；本地draw.io实际插入/再次编辑/SVG嵌入XML/资源独立变更/历史恢复渲染及SHA256读回通过（drawio_embed_system_browser）；SSH图表编辑、其他类型和备份管理未全部覆盖。 |
| 真实环境验收 | lock_browser, lock_entry_fix, ssh_workspace_recovery_browser, windows_acceptance | Safari锁态同步与点击解锁、隔离SSH已有验证；跨会话真实模型审批、键盘锁入口、长时间使用未完整验证；Windows not_run。 |
| 任务成果舱 | outcome_system_browser_download | 真实模型本地文件任务的系统应用回执、S编号原文和JSON下载已通过Safari（outcome_system_receipts）；运行中跨页快照一致性和发布成果卡未验收。 |
| 审批与授权管理 | approval_management_browser | Safari规则到期设置/撤销与持久化通过；真实任务建立记忆规则、跨会话匹配和人工/辅助审批全局切换尚缺实际模型证据。 |
| 连接能力仪表 | capabilities_browser | 本地目录及解析器导入已查；当前时间插件宿主实际执行及SSH根目录通过/中断失败/恢复通过已有证据；Go模型派发、其他插件、写权限与真实提供商连接仍未闭环。 |
| 渐进索引 | local_cursor_persistence, shared_catalogue_checkpoint, progressive_over1000_browser | 本地进程重启续扫及Safari超过1000节点增量加载已验证；来源按钮覆盖状态可见性及自动扫描完成切换已通过Safari（coverage_visible_system_browser）；大库性能、远程大目录及真实FTP/FTPS/HTTP/SMB/MCP来源语义仍需验收。 |
| 混合检索 | document_continuation_checkpoint, knowledge-hybrid-retrieval-20261009 | Safari原文和关键词RAG续查通过；向量真实提供商及远程全文检索未完整验证。 |
| 代码关系可信度 | code_path_browser, knowledge-language-service-20261009 | Safari完整两包Go样例4条类型绑定、2条动态未解析及L12跳转通过；旧AST诊断修复定向race通过，候选Safari/API复验及当前full通过；第三方依赖工程、SSH和其他语言仍缺证据。 |
| 星图路径探索 | code_path_browser, knowledge-path-exploration-20261009 | Safari单跳与本地两跳代码候选路径、方向标识、L4源码跳转及外部修改失效已查；完整业务链、报告到来源段落、SSH和主题/响应式尚未全部验收。 |
| 知识时光 | knowledge-timeline-20261009, ssh-continuity-integration-20261009 | API及旧组件增改比较有证据；系统Safari本地新增/修改/移除与前后原文、32条保留和淘汰显示已通过；SSH浏览器、主题/窄屏、存储故障及性能仍缺证据；不把索引观察冒称语义结论变化。 |

## 验收边界与后续顺序

1. 本轮已完成系统Safari本地知识历史新增、修改、移除及前后原文比较，记录与截图见总账本 `timeline_system_browser`；SSH及其他边界继续验收。
2. SSH历史恢复自动保存开关已补验；继续精确星图镜头恢复、键盘锁入口及响应式/主题。
3. 有可用授权模型配置后验证真实审批、向量检索和成果证据。用户在2026-10-10改为授权直接在实际运行环境验收；不再复制模型配置到18211。真实模型审批初验与发现问题见下文，向量及完整编排仍待验收。
4. 测量大库滚动/输入与持续打开，补来源适配和完整工程代码路径验证。用户确认没有Windows环境，真实Windows验收保持 `not_run`。
5. 范围内文件归属/diff审阅及文档完成后，按仓库门禁提交、全量检查、发布和部署。回归通过、源码推送、Release附件与生产激活分别记录。

记忆神经可视化按用户要求已归档为备用提案，普通项目记忆和已有数据保留；见[归档说明](../proposals/memory-neural-20261010/README.md)。

完整门禁句柄82161已终止，exit0；输出 `.agent-state/coverage-full-output-20261010.txt` 与 full 收据均确认36项 PASS。

## 插件宿主实际执行补充

在隔离18211容器的 Node24.21.0 中，以仓库实际 `plugin_host.js` call 协议调用随候选挂载的 `current-time/get_current_datetime`：Asia/Shanghai 返回真实时间及ISO周；无效时区返回 `ok:false`。临时结果文件已移除，无模型和凭据调用。收据 `.agent-state/continuity-runtime/plugin-current-time-execution-20261010.json`。此项验证插件宿主与该工具，不覆盖模型编排、Go派发入口或所有插件；能力仪表的入口读取检查仍维持原范围。

## 多跳路径系统 Safari 补充

隔离18211实际新增 Go 三层样例：Entry→Middle→Leaf 查询返回两步，逐条显示静态名称候选及L3:37/L4:38；第二关系源链接打开只读文件并选中L4。默认反向未找到，勾选逆向后返回两条“逆向探索”证据。外部编辑移除Middle→Leaf后，自动同步连接259→258并清空旧路径，提示重新查询。截图与收据 `.agent-state/continuity-runtime/path-multihop-receipt-20261010.json`；样例归档到索引外，临时标签关闭，原草稿附件保留。此证据补齐本地候选多跳/方向/行跳转/失效，不覆盖SSH、完整业务链或性能。关闭类型分析时跨包main→Build未找到；保留该范围限制。

## SSH 能力检查系统 Safari 补充（2026-10-10）

隔离18211配置内部网络中的真实OpenSSH测试来源。系统Safari点击检查：根目录读取通过196ms；停止测试服务器后重新检查失败213ms，旧通过结果被替换；同一服务器恢复后检查通过255ms，无需刷新。三次界面均明确配置可写不代表写权限已验证。收据 `.agent-state/continuity-runtime/capabilities-ssh-receipt-20261010.json`，截图 `capabilities-ssh-pass-safari-20261010.png`、`capabilities-ssh-failed-safari-20261010.png`、`capabilities-ssh-recovered-safari-20261010.png` 均在同目录。临时来源和凭据已移除并核对，原来源身份保留，内部测试服务器及网络已清理。此项不覆盖WAN、物理丢包、所有子目录权限或写操作。

## 实际运行环境真实模型审批补充（2026-10-10）

用户授权直接在实际环境运行，未复制模型配置、会话或SSH凭据。系统Safari在9999（0.1.17.0-RC2）新建验收会话#39：一次`pwd`实际执行exit0，无人工按钮；新建`.cache/aide/approval-live-20261010.txt`经独立模型审核批准、`applied:true`，未点击人工审批。原容器读回28字节，SHA-256 `b8c95b6f886c9614ca7f7a1125b5111fa77364525e8bf0bd3bfc90864637dd9d`，精确匹配样例。测试会话和文件保留作证据，未调整生产设置。收据 `.agent-state/continuity-runtime/production-approval-receipt-20261010.json`，截图 `production-approval-applied-safari-20261010.png`。

发现两个未闭环问题：模型在提案批准之前反复读回并保留“未落盘”的过时结论；界面在后端已完成之后仍显示“辅助审批中”，刷新才修正。当前源码`app.js`轮询已增加审核中及批准待应用状态，相关VM覆盖批准、手动交接、临时读取失败，均通过；修复后系统Safari复验仍待完成。已构建并以start.command启动隔离候选，但模拟审核记录启动后会被正常降级为手动确认，不能作为审核中自动同步通过证据。当前9999未升级；旧36项full收据不能用于这次新增源码修复的发布。跨会话、记忆规则、向量模型和真实成果舱仍未完成验收。

### 待审批读回和系统应用回执修复

- 新文件提案未应用时，`read_file` 返回明确的待审批状态，不伪装成真实文件读取，也不返回提案内容冒充磁盘字节；提示本轮结束后审批，避免重复读取和绕过审批。
- 独立审核结束后追加 `file_application_receipt` 步骤，保留模型原回复和执行时间顺序。仅在绑定工作区仍相同且为本地环境时实际读回、比较完整内容并记录字节数及 SHA-256；发生外部变更或读回错误则回执失败。待人工审批、工作区切换和远程环境不声明本地验证成功。
- `TestAssistedApprovalNewFiles|TestReviewedFileResult` 定向 Go race PASS（已缓存官方 Go 容器、`--pull never --network none`）。涵盖待审批、应用成功、内容变化、工作区切换及回执去重。
- 系统 Safari /18211：本机无凭据模拟 provider 延迟独立审核16秒，页面显示“辅助审批中，无需操作”，之后不刷新、不点击审批自动完成；系统回执实际读回18字节，SHA-256 `2ae92ac843de4b23ba5d3a9d9040f4c2004d1219136771f4e7ce002a02d35f5f`。
- 模拟会话44：`ed4ed33ee0bb0dbe4108500ed217c091`，run `50c913255426cf7d7f6d5b38c70b3d49`。两个模拟会话43/44已可恢复归档；模拟器随候选停止，候选原模型配置与原先不存在的审批配置已恢复，通过 `start.command` 重启。原会话42草稿仍保留。
- 收据 `.agent-state/continuity-runtime/review-receipt-browser-20261010.json`、等待/完成 Safari 截图保存。本次仅证明候选前后端衔接，不替代生产真实 provider 修复后验收。生产9999仍未替换，向量真实 provider 验收尚缺。
- 新 full 已启动（handle74612），未结束前不记录 PASS；此前 full 不覆盖本次新增后端回执及工具语义变化。

复验结果：新 full 已结束，36项全部PASS，Go race/vet退出0；收据时间 `20261010T000112159183Z`，指纹 `9c6ef98e704c88bc665fb38134a84ebef40ab11f279a36a04b267b62deb7dd2f`，日志 `.agent-state/verify-20261010T000112159183Z.log`。仍不替代真实模型修复后验收与其他逐项缺口。

清理复核：初次恢复的是旧 `data/settings.json`，API确认模拟模型仍在，因此随后修正到实际 `data/config/settings.json` 的模型字段；再次通过 `start.command` 重启并确认 API 为 `baseURL=http://127.0.0.1:1`、model为空、审批enabled=false。模拟器进程已随容器停止退出137，三个临时Safari标签页关闭，原会话42草稿及note-037附件恢复。浏览器刷新曾恢复共享记录中的模拟会话44，已重新选择原会话42；此观察不证明跨标签页工作现场隔离已完成。

## 标签页工作现场隔离修复与复验

共享 IndexedDB 的 scene/workbench 会被其他标签页最后一次选择覆盖。现在聊天草稿和会话/目录现场先写入当前浏览上下文的 sessionStorage，刷新优先恢复本页快照；首次打开新页才使用共享 IndexedDB 兜底。空快照也固定到本页；异步兜底读取不得覆盖途中产生的新编辑。文件自动保存与服务器保存流程保持原有接口。

- VM：工作区隔离、双页不同草稿及附件结构、刷新、opener 初始副本、空现场、读取竞态、关闭恢复开关和2 MiB上限均通过。
- 原生 Safari /18211：A会话45未发送草稿与index-resume-acceptance目录，B会话46另一草稿与recovery-isolation-b目录；先刷新B再切回刷新A，两页各自保持会话、草稿和目录。无模型请求。两页已关闭，测试会话可恢复归档，原42草稿及note-037附件仍在。
- 收据 `.agent-state/continuity-runtime/tab-recovery-browser-20261010.json`，截图 `tab-recovery-A-safari-20261010.png`、`tab-recovery-B-safari-20261010.png`。
- 此次浏览器测试没有为测试草稿新增附件；附件结构仅VM覆盖。关闭全部标签页后多分支恢复、容量淘汰、Windows和长时稳定性未验收。首开新页依然从共享记录恢复最近现场，不把本次修复称为完整多分支草稿管理。
- 候选以start.command启动；生产9999未替换。本次新增源码后旧full指纹失效，新full正在运行。

实际环境只读核对：9999 的 embedding-policy 返回200，enabled=false、model为空、hasKey=false。未读取输出密钥、未复制配置或修改设置。真实向量provider验收为NOT_RUN，等待用户在Aide配置服务及模型；收据 `.agent-state/continuity-runtime/production-embedding-config-20261010.json`。

标签页修复后的full已完成：36项全部PASS，Go race/vet退出0，收据时间20261010T001752317315Z，指纹91a364ad912b6bdfa014d7ad35586a7215afeaa04fa30c845ce4c3f1211a1050。此历史收据覆盖当时源码，不替代后续改动及其余产品范围。

## 项目 start.command 实际环境复验

按用户2026-10-10要求运行项目根目录start.command，源码标签59fd0da04b69233b41c8e79f2f7a321fbaf4920b33643aee09696679214c4014，镜像sha256:1d07a5dd064623e8f8f7edec60f77e8c86fec1ae8d063192c377352a588c233e，启动退出0。旧镜像保留为aide:before-live-acceptance-20261010，数据、模型保险库与审批配置没有复制或重置。此为用户要求的实际环境启动，不代表正式发布门禁完成。

系统Safari在9999创建真实模型会话40，run eb35b2ef2a9aa828276844d337cff705：write_file提案后read_file返回待审批元信息，模型不再反复读取；独立模型批准后，页面无需刷新自动completed，并显示系统应用回执。独立磁盘读回30字节，内容精确为AIDE_POSTREVIEW_REAL_20261010加换行，SHA-256 86f1af7d059bfdf3d0a65de42d66d770ccc35aa9f128f2a437febb3e113aeb6a。未点任何人工审批按钮。仅覆盖一次本地文件，跨会话记忆规则、SSH及全局切换仍缺验收。收据production-postfix-approval-20261010.json及production-postfix-receipt-safari-20261010.png位于.agent-state/continuity-runtime。

同一真实任务成果舱：原始工具参数/返回、模型审批依据和应用记录可见；导出JSON真实下载4023字节，SHA-256 6a78683afbae9289cc03a45b84c4c9dd702560396b1be0956f1010f9d05dedb2。exportScope明确不含原始证据。发现系统应用回执未纳入成果舱独立证据/导出，继续修复；不能将本次真实任务验收等同整个成果舱完成。收据production-real-outcome-20261010.json和production-outcome-evidence-safari-20261010.png同目录。

启动后API再次确认对话模型deepseek-flash及辅助审批enabled=true；embedding-policy仍enabled=false/model为空/hasKey=false。已有对话模型设置并不等于已配置embedding。真实向量provider尚未运行。临时实际验收标签关闭，原18211会话42草稿与附件仍在；实际会话40和测试文件保留作为可追溯证据。

## 成果舱最终应用证据补齐

源码已加入系统回执投影和S编号原始证据，snapshot复制Steps，摘要与模型报告分别计数；task-outcome.js独立区及JSON导出收录回执。TestTaskOutcome定向Go race通过4.007秒，JS语法及i18n回归通过。使用项目start.command重建实际9999退出0，未改模型/审批设置或凭据。

原生Safari打开既有真实模型任务40：S0002展开显示30字节实际读回，原始证据标明Aide/file_application_receipt；导出JSON4708字节，摘要systemReceipts=1、verificationReports=0，SHA-256 b7686477fa9bc92399f469213fd312456270448ec47216ab412d145aa24f8003。收据.agent-state/continuity-runtime/outcome-system-receipt-20261010.json，截图outcome-system-receipt-safari-20261010.png。临时标签关闭、原18211草稿保留。此处只复验已有持久化真实任务，失败/待人工/远程状态为Go覆盖而非Safari证据；运行中多分页快照及发布记录继续未验收。新full已完成：36项全部PASS，Go race/vet退出0；时间20261010T003711950608Z，指纹ac7642295583c89f9f0ac6c8e709607b497fb6cb920441bb6db1497a7b0064ee，日志.agent-state/verify-20261010T003711950608Z.log。仍不替代上述未验收范围。

## 星图恢复与瞬时失败补充（2026-10-10）

星图改用AideContinuity.readTab/writeTab，保留新页共享回退并隔离已有标签的镜头。现有starmap-workspace-scene回归增加动画目标/滚轮目标、精确镜头/相位/面板滚动、消失星域上级回退与初次失败重试，均为source VM PASS；continuity-isolation PASS。

首候选实际9999原生Safari：刷新前后yaw=0.14368155211480363、pitch=0.1670673076923077、zoom=0.5、skyTime=7337077、driftTime=7289471、搜索面板scrollTop=102完全一致，动效关闭。随后双标签试验真实复现首次updates请求503，自动增量同步绕过恢复、recoveryScopeLoaded为空和默认镜头。已修复pollLive在未恢复工作区时重试load，而不直接应用增量。

最终候选经start.command重建exit0，source_sha=9cee0caa93096bfe33e523334f543f23ca3c983abf66b6ef5ef912d6982f123d，image=sha256:7fd1027ac8676e51dfa670f81e1f65ab42621a02b1d21ce6e834c334bb9e5e56。解锁后原生Safari完成复验：B归位后A刷新，yaw=0.1510574018126888、pitch=0.2153846153846154、zoom=1.2712491503214047、skyTime/driftTime=104470完全匹配A刷新前；动效与红外均关闭。B随后刷新真实出现首次updates请求503，自动重试后显示已恢复，镜头为自身归位值0/0.1/1，skyTime=7337077、driftTime=7289471、搜索滚动102、动效/红外关闭，recoveryScopeLoaded=F59ba0de58edd。此次未建立红外开启持久化的独立数值断言，不能据此声称该项完成。

收据.agent-state/continuity-runtime/sky-recovery-20261010.json，AX记录sky-final-before/after及sky-final-b-after-safari-20261010.txt，截图sky-final-after-safari-20261010.png。两个临时实际环境星图标签已关闭，原18211会话42草稿及note-037附件保留。非零详情滚动、嵌套路径、关闭标签后的多分支恢复与容量淘汰仍待浏览器验收，Windows无设备。全量进程4373的36项命令均退出0，但因期间源码指纹变化，门禁最终exit1；最终代码全量重跑76976已完成exit0：36项全部PASS，Go race/vet退出0，源码指纹前后一致；时间20261010T011324648367Z，指纹7f2a30689e7d906b930d0733f3ef1a323290772e17eb9e2bfdf0226842e09da9，日志.agent-state/verify-20261010T011324648367Z.log。这只证明本次全量代码检查，不替代仍缺失的产品验收或正式发布。
