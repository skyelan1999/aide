# 连续工作目标：当前证据与剩余项审计

任务：[continuity-evidence-audit-20261016](../../tasks/continuity-evidence-audit-20261016.json)。本轮是实际进展：在当前9999新增推理写回证据，并把总账本12项逐条对齐最新收据。总目标保持完整且未完成；没有因这份审计收窄成功标准。

## 当前优先项

旧“全局默认推理强度”入口已被用户后续明确要求移除，当前应验收的是会话策略面板中的独立推理强度，不能为满足旧任务把重复入口重新加回。两个本轮自建会话各保存high/medium/low/off/auto，合计10次；交叉API读回与规范磁盘JSON的选择和revision逐次一致，旧revision返回409，非法值返回400且记录不变。没有调用模型，因此不证明上游实际推理量；没有操作完整认证Safari或额外重启服务。

两条自建会话均已删除，原有全部会话文件、鉴权与来源哈希不变；全局配置除合法会话编号37→39外保持。编号未回退。原始探针与逐次观察在私有 `.agent-state/continuity-current-audit/`，脱敏收据见[当前证明](continuity-current-proof.json)。

## 十二项逐条核对

“已补证据”保留其原验收日期与范围，不宣称本轮重新运行所有历史探针。所有12项仍为in_progress。未验证边界并非全部都新增为必须实现的功能；原验收要求保持总账本中的原文，服务变体等扩展边界单列。

| 原目标 | 最新已补的分范围证据 | 仍缺的证明或明确边界 |
| --- | --- | --- |
| 工作现场恢复 | 草稿导航、文件位置和星图镜头恢复有先前页面/组件证据；当前full覆盖恢复代际与会话/工作区隔离回归。 [chat-draft-navigation-20261015](../../tasks/chat-draft-navigation-20261015.json)、[continuity-roadmap](../../architecture/continuity-roadmap.md) | 当前完整工作台的刷新/重开、SSH文件光标与目录现场、多标签恢复分支和容量边界仍需直接验收。 |
| 保存状态与冲突 | 已补真实ENOSPC/权限故障、部分成功返回、准确正文哈希、409和重试；UI部分保存状态也已有修复与组件证据。 [storage-fault-live](../2026-10-15/storage-fault-live.md)、[history-partial-save](../2026-10-15/history-partial-save.md) | 当前认证工作台中的SSH历史恢复/自动保存联动、保存状态与冲突对比交互仍需复核。物理网络中断属于尚未测到的边界，不用ENOSPC替代。 |
| Markdown 历史闭环 | 已有正文/直接资源恢复、外部变化去重、ZIP导出与导入预览/合并、管理预览证据；另已修复损坏历史对象被复用并补实际二进制验收。 [markdown-history-storage-ui-20261015](../../tasks/markdown-history-storage-ui-20261015.json)、[history-object-integrity](../2026-10-15/history-object-integrity.md)、[history-partial-save](../2026-10-15/history-partial-save.md) | 完整工作台的嵌入内容展示、恢复后状态与管理操作尚未形成全流程证据；不可逆清理/保留策略实际执行仍按原审批边界处理。 |
| 真实环境验收 | 本地及隔离真实SSH协议链、跨会话审批与旧Safari锁态已有分范围证据；当前9999健康、配置/会话保全也已核对。 [ssh-approval-lifecycle](../2026-10-15/ssh-approval-lifecycle.md)、[workbench-polish](../2026-10-16/workbench-polish.md) | 当前完整工作台的创建/修改/保存/重开/恢复、锁屏与长期打开联测仍缺；用户SSH主机未代验。Windows继续由用户验收。 |
| 任务成果舱 | 真实Qwen隐藏活动及runningRuns归零、旧任务retry输入边界已验证；当前快照发布记录导入、去重、伪造来源/旧快照/错误会话拒绝也已验证。 [outcome-hidden-active-live](../2026-10-15/outcome-hidden-active-live.md)、[outcome-live-reasoning-audit](../2026-10-15/outcome-live-reasoning-audit.md) | 完整已认证工作台跨页、隐藏活动的可见呈现及发布记录卡/导入控件仍待验。操作者记录不是远端部署成功；不能把API隐藏活动继续列成完全未验。 |
| 审批与授权管理 | 真实隔离SSH授权的期限、撤销、精确身份和模式切换已有协议/API证据；会话停止与暂停隔离又补了真实模型复核。 [ssh-approval-lifecycle](../2026-10-15/ssh-approval-lifecycle.md)、[session-run-interaction-20261015](../../tasks/session-run-interaction-20261015.json) | 当前Safari并发撤销/过期控件、跨会话逆向模式即时同步和用户SSH主机行为仍待直接验收。 |
| 连接能力仪表 | 独立SFTP读写/失败恢复、四个解析器导入已有证据；真实模型读取、图像理解和并行调度也有独立任务记录。 [source-capability-live](../2026-10-15/source-capability-live.md)、[provider-output-interruption-20261015](../../tasks/provider-output-interruption-20261015.json)、[parallel-10x-levels-20261015](../../tasks/parallel-10x-levels-20261015.json) | 完整认证仪表的状态/错误展示、用户SSH目录权限、插件实际工具结果与仪表的一致性仍缺完整工作流证据。解析器能导入不等于任意文档处理通过。 |
| 渐进索引 | 本地5000文件和FTP/FTPS选定目录续扫/重启已有证据；HTTP单资源、MCP能力说明、SMB单资源也已补真实服务链。 [source-index-live](../2026-10-15/source-index-live.md)、[ftps-index-latency](../2026-10-15/ftps-index-latency.md)、[http-mcp-index-live](../2026-10-15/http-mcp-index-live.md)、[smb-index-live](../2026-10-15/smb-index-live.md) | 当前完整星图对真实目录的首屏、逐批可见、优先范围/预算/覆盖提示仍需联测。单资源HTTP/MCP/SMB证据不能证明其远端目录扫描；大远程库与服务变体属于单列边界。 |
| 混合检索 | FTP/FTPS、HTTP/MCP说明和SMB的字面检索、原文指纹/身份及重启后定位已有实际协议证据。 [source-index-live](../2026-10-15/source-index-live.md)、[http-mcp-index-live](../2026-10-15/http-mcp-index-live.md)、[smb-index-live](../2026-10-15/smb-index-live.md) | 当前认证浏览器中查询到原文/位置的完整链仍需验证；用户主机/协议变体另列。真实向量提供商按用户要求暂缓，不擅自调用。 |
| 代码关系可信度 | JS/Python词法遮蔽与Go接口误连已修复，真实SFTP保持候选/确定/未解析边界及调用位置；不是只停留在旧的两包样例。 [code-relation-live](../2026-10-15/code-relation-live.md)、[knowledge-language-service-20261009](../../tasks/knowledge-language-service-20261009.json) | 当前完整浏览器的可信度标识和诊断联动仍需验收；完整依赖项目及其他语言未证明。已有外部调用保持未解析，不能将其描述为已完成跨依赖类型绑定。 |
| 星图路径探索 | 真实SFTP路径API、确定调用行列、旧revision409与原文哈希已验证；引用文件快速预览另有组件与接口证据。 [code-relation-live](../2026-10-15/code-relation-live.md)、[starmap-file-preview-20261015](../../tasks/starmap-file-preview-20261015.json)、[knowledge-path-exploration-20261009](../../tasks/knowledge-path-exploration-20261009.json) | 按钮到服务的完整多段链、报告到来源的可证明路径及浏览器行号/段落跳转，仍未形成端到端证据；主题/窄屏/SSH交互不能由路径API代验。 |
| 知识时光 | 本地/隔离SSH节点增改移除、前后原文、容量和留存已有证据；真实存储故障与新PID回读又补了后端边界。 [knowledge-timeline-20261009](../../tasks/knowledge-timeline-20261009.json)、[storage-fault-live](../2026-10-15/storage-fault-live.md) | 当前SSH浏览器时间比较、历史依据的完整定位和结论演变仍需直接证据；不从文件增改自动推断语义结论演变。 |

## 当前基线与门禁

当前主线基线为 `f2c7e226100a609b14fd402db72a22b3169afc3a`。核对隐藏活动、代码关系、部分保存、对象完整性、SSH授权、并行上下文及工作台交付均为当前HEAD祖先；这证明相关提交已集成，不替代各自场景验收。当前运行镜像4cc2308dbb4b健康，当前源码与其构建摘要一致。

本轮只有普通文档变化。脚本重新计算受控输入指纹，与既有干净main full86精确一致，可复用该收据；没有声称新跑full。原full绑定源码cc114734，server race552.597秒及vet通过，指纹为 `fc3d2b98af58fe9d417bba53765429ad994cc8fff6f3232ea0b1064945f76a6a`。因此不重复重建镜像或导出相同离线包。独立main门禁与远端核对另补实际结果。

## 后续可独立推进

优先把星图“按钮到服务、报告到来源”的完整证据链与结论演变要求拆到现有源码和验收场景，补真正缺失的实现或端到端证据；SSH现场恢复与授权界面联动也继续保留。完整认证Safari沿用用户暂缓决定，不重复请求或代点证书告警。Windows由用户验收，真实向量测试暂缓。未把这些缺口写成完成，也未把总目标标记完成。

独立main门禁已通过，按精确相同输入复用full86；非强制推送 ea333509a591e5046fcff6b295bb881f09f169c3 后与远端main逐字核对一致。最后的普通文档收据随后推送，不变更产品或镜像，也不标记总目标完成。

## 后续当前状态补记

上文表格与基线是最初审计时的状态。其后main已精确推送至 `b31be2ffc7beece21035db1aab92f96b0d8fdab5`，本机9999更新为8d636fd镜像，ARM64离线包完成摘要核对及本机导入；最终产品输入full88通过（race568.873秒、全包vet、非root19项资源）。见[运行交付](workbench-runtime-delivery.md)。另一设备导入未执行。

- 知识时光的“结论/依据演变仍需实现”已由后续完整功能与分范围证据推进：AI/文档历史正文、人工主题、片段外引用定义及独立依据变化、非Markdown原话选择和1000来源筛选可达性均有回归；实际临时App的索引、磁盘、HTTP与页面已验。见[覆盖审计](knowledge-conclusion-coverage.md)。完整认证浏览器、用户SSH和长期大库边界仍未证明，不能把文件变化等同语义变化。
- 连接能力仪表新增[真实插件生命周期验收](capability-plugin-lifecycle.md)：实际内置插件、Node宿主、Go派发与鉴权能力路由验证正常/参数错误、停用/恢复、入口损坏/修复，专项race及全包vet通过。本批不改产品实现；最终新增回归full88/race556.471秒及全包vet已通过。它补API级工具结果/仪表状态一致性，完整认证仪表UI仍待。

完整Safari继续按用户决定暂缓，IAB直接访问9999也受证书拦截；未绕过警告。其余十项本批没有重新全流程验收，仍以各自直接证据及缺口为准；原12项保持in_progress，Windows与真实向量边界不变。

## 本轮主分支交付

新增插件生命周期回归的最终full88和干净main独立门禁已通过，推送退出0，远端精确核对 `960b0d0e04eba673fc429b052711649973e167cd`。后续仅补文档收据，产品输入不变；本批未改产品实现、不重启9999或重复打包。原12项仍进行，完整浏览器及用户SSH等边界不变。

后续[文件恢复异步修复](file-recovery-async.md)又直接复现并修复旧位置覆盖新光标、锁态成功回执确认/清理草稿两项缺陷。负例、专项、实际组件和最终full88/race566.699秒/vet通过；独立main门禁与精确推送c00855b、本机9999运行8a351653、13会话/配置/鉴权/挂载保全及离线包本机导入完成。该进展补现场恢复与保存状态的局部缺口，不代表完整认证工作台或SSH工作流通过；原12项保持进行。
