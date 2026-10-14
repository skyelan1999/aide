# 可配置执行策略

聊天输入区的策略菜单单独提供“审批”栏：手动审批逐项确认命令，辅助审批由独立审核器审核具体命令，不确定时仍转人工。当前选择显示在策略按钮摘要中，复用会话草稿与运行中任务的原有审批状态及接口；不改变权限规则或模型参数配置。桌面以四栏展示，窄屏依次改为两栏、单栏滚动。

设置 → 执行策略，提供数值和提示词编辑、JSON 导入导出、恢复默认。保存使用已鉴权的 GET/PUT `/api/execution-policy`，落盘于数据目录 `config/execution-policy.json`。不需要重启，下一次新任务加载。

## 字段（version=1）

| 字段 | 默认 | 范围 / 含义 |
| --- | --- | --- |
| chatBudgetSec | 7200 | 30–7200 秒实际执行预算；新任务默认 2 小时 |
| workflowBudgetSec | 900 | 30–7200 秒实际执行预算 |
| toolMaxRounds | 60 | 1–200；未保存策略时继承旧设置 |
| completionReviews | 3 | 0–10；0 关闭收尾复核 |
| researchCorrections | 2 | 0–10；覆盖纠正、承诺后续读取、提前索取测量的纠正次数 |
| agentInstruction | 内置自主执行规则 | 可编辑，最多 12000 字符，空字符串移除该段策略指令 |
| researchInstruction | 内置资料到工程流程 | 可编辑，最多 12000 字符 |
| completionInstruction | 内置目标与证据核对 | 可编辑，最多 12000 字符 |

PUT 替换整个策略文档，字段必须齐全，拒绝未知字段、null、错误类型、非法范围、多份 JSON 和超出 128 KiB 的输入。保存失败不修改旧配置。导入与恢复默认只改草稿，保存后生效；导出当前草稿。读取坏文件时设置页显示错误和默认值，新任务拒绝启动直至修复；未保存文件时正常使用默认值。

## 生效与兼容

新任务/重试将执行策略快照写入任务数据；首轮上下文、工具轮次、执行预算和收尾核对使用同一份快照。聊天新任务默认最多连续运行 2 小时；显式暂停及预算到期会保存最近检查点，用户可继续。续跑创建新的执行段：只按当时的全局与工作区有效策略刷新对应模式的时间预算，模型策略、轮次和提示词继续沿用原任务快照。运行中的任务不受设置变更影响；显式续跑会读取最新预算，因此旧任务里保存的 360 秒不会继续限制下一执行段。子任务继承父任务快照。配置更新不重写历史记录。

与 `routing-policy.json` 分开：后者控制模型 Profile 路由，本配置控制执行循环。与审批权限分开：自定义提示词不会授权工具，不改变沙箱、文件提案、网络域名或危险命令限制。真实来源覆盖检查、工具事实与权限执行仍由运行时约束；新增工具、协议或权限能力仍需要实现代码。

## 当前交付边界

本次不自动调用付费模型；编译/语法检查不代表完整模型执行或结论一致性验收。部署状态记录在 `docs/tasks/execution-policy-20261007.json`。

## 辅助审批按钮状态（源码候选，2026-10-09）

任务的 `approvalState` 区分 `reviewing`、`approved`、`manual` 和 `aborted`。开启辅助审批后，命令／文件提案首次进入待审状态即标记审核中；页面仅展示状态与命令／差异，不展示人工确认、应用或忽略按钮。独立审查放行后自动继续，转人工、审核失败、超时、应用失败时显示人工操作。服务重启不沿用审核中的状态，避免隐藏按钮后无限等待；旧记录缺少新字段时保留人工入口。

审查响应接受纯 JSON 或完整的 JSON 代码块，拒绝从任意说明文字中猜测批准。新文件内容审查限额由 24000 提高到 64000 字节；超限仍转人工。提示词明确授权覆盖完成任务所必需的常规步骤，模型不得用 ask_user 重复 run_shell／write_file 自带的审批。沙箱、危险操作拦截和人工审批回退继续生效；辅助审批不表示任何命令均自动批准。

当前仅完成源码、语法与编译检查，未运行真实模型审批比例或浏览器按钮切换验收，未更新生产。见[任务账本](../tasks/assisted-approval-ui-20261009.json)。


## 全局审批偏好与长期授权（源码候选，2026-10-09）

审批栏标注“审批 · 全局”。手动／辅助模式由服务端 `config/approval-policy.json` 持久化，属于同一个 Aide 实例；新建、重试、续跑采用全局值，旧客户端任务请求中的 `autoReview` 不再决定模式。修改后，所有运行、等待、排队及暂停任务立即更新，取消旧审核代次；切换辅助模式后重新审核等待中的命令和文件。已经开始执行的操作不会被切换撤回。历史已结束任务保留当时记录。

浏览器通过 `approval-policy-changed` SSE 同步，不依赖会话 localStorage；连接恢复及页面回到前台重新读取服务端状态。审批切换不改变模型路由、人格、沙箱或插件权限。不同 Aide 服务实例不共享此偏好。旧版本无此配置时默认手动。

人工命令卡片提供“仅本次允许”和“永远记住并允许”；文件提案提供“仅本次应用”和“永远记住并应用”。长期规则绑定工作空间身份及任务根，命令按完整字符串的 SHA-256 匹配，文件按全部路径、原版本哈希和完整新内容匹配。不生成通配符或命令前缀授权；工作空间、命令、内容或原版本变化须重新确认。长期授权在手动模式也有效，未命中的操作仍需人工；策略栏可清空全部审批记忆。最多 1000 条，满时明确拒绝新增，不静默替换旧授权。

记忆文件仅保存规则指纹和作用域，不存命令全文、文件内容或模型密钥。删除、权限修改及内联解释器／脚本类命令不提供长期授权（可仅批准本次）；保护区和沙箱检查始终生效。文件沿用原有版本检查和工作空间匹配，不因记忆跳过冲突处理。保存规则失败不放行本次操作；规则保存成功而执行失败时规则仍保留，执行结果必须单独判断。

接口：`GET /api/approval-policy` 返回模式、修订号及记忆数量；`PUT` 接受 `enabled`；`DELETE /api/approval-policy/rules` 撤销全部规则。旧任务级 approval-mode 接口仍可调用，但现在改变全局模式。answer/apply 接受可选 `remember`，只记录服务器当前待审操作，命令同时校验审批轮次。

本次构建与静态检查通过；跨标签页、重启持久化、真实模型和人工按钮交互尚未运行验收，生产实例未替换。见[全局审批任务](../tasks/approval-global-memory-20261009.json)。


### 审批卡片的有效期（2026-10-09）

澄清与命令审批通过 `awaitUserAnswer → /sessions/{id}/runs/{run}/answer` 等待应答。每个新问题携带独立 `questionId` 和轮次；新版网页提交该编号，服务端拒绝指向其他问题的答案。旧客户端仍保留轮次校验兼容路径。按钮捕获创建时的会话编号，跨会话切换不会改变应答目标；同一问题正在提交或已提交时不重复发送。

仅 `awaiting_clarification` 状态渲染问题按钮。服务重启将等待任务标为中断并清除过期问题；结束等待后保存清除状态、发送 `clarification-resolved` 流事件并广播会话更新。网页收到过期/已应答等 409 响应后移除旧卡片并重新获取会话，不重新启用旧按钮。

本次源码编译及 JavaScript 语法检查通过；尚未启动候选服务进行浏览器联调，也未更新生产实例。


## 单条授权管理与审批依据（源码检查点，2026-10-09）

策略 → 审批 → 管理审批授权，显示当前实例已记住的类型、工作区、根目录、载荷摘要、创建时间及有效期。可修改单条有效期或撤销；操作使用服务端 revision，陈旧修改返回409并要求重读。永久规则与旧配置兼容；无效或已到期的期限不自动放行。撤销影响后续检查，不取消在途操作。

首次人工命令／文件提案保留“仅本次”，记住时可选永久、1小时、1天或30天。answer/apply 的可选 expiresAt 为未来一年内的 RFC3339 时间；省略时永久。授权继续精确匹配原有工作区、根目录与完整载荷，不扩展为目录或命令前缀授权。

- GET `/api/approval-policy/rules`：返回 revision 与安全规则元数据，不返回原始载荷。
- PUT `/api/approval-policy/rules/{rule}`：提交 revision 和 expiresAt，留空为永久。
- DELETE `/api/approval-policy/rules/{rule}`：提交 revision，撤销指定规则。

审批记录区分人工确认、模型独立审核和授权规则匹配，带当时范围、载荷指纹；规则匹配另带规则编号及期限。记住的命令／文件提案在放行前保存记录，保存失败不放行。成果舱展示审批依据，最多保留最近50条；旧记录缺失来源时不补造。批准记录不证明执行成功，文件写入仍受路径和版本检查。长期审计归档及只读免审批依据尚未接入。

相关 Docker Go race 和模拟接口组件浏览器检查已通过；完整工作台、真实模型、跨窗口同步及锁屏联调仍未完成。详见[本次检查点](../reviews/approval-checkpoint-20261009.md)。生产9999未替换，发行基线仍为0.1.17.0 RC2。

## 基础命令与审核恢复（2026-10-14）

只读判断先解析有限的字面命令语法，接受带引号的路径/搜索模式，以及每一段都已证明只读的管道、`&&` 组合。例：`cat "file with spaces.txt"`、`rg 'TODO|FIXME' .`、`find . -name '*.go'`、`sed -n '1,120p' README.md`、`cat README.md | head -n 5`、`pwd && ls`。`cd 'some folder' && ls` 只改变该命令进程的工作目录，不持久改变会话根。现有路径/保护区检查仍在执行阶段生效。

这是保守的字面语法子集，不是通用Shell解析器：不证明只读的语法仍走原审批链。未引用的通配符、变量/命令替换、重定向、后台、任意内联脚本不走只读路径；任何一段为写操作都需要审批。`find -exec/-delete/-fprint`、`rg --pre`、`file -C/--compile` 和 Git 输出文件/外部diff/textconv等选项继续排除。`sed`仅支持数字行范围的`-n ...p`读取，不开放`-i`或任意sed程序。手动模式和read-only沙箱复用同一只读判断，不因辅助审批开关而把写命令当成读取。

具体命令的独立审核仍使用任务选定模型、提供商/凭据和推理强度，不改全局审批模式。首轮输出预算512 tokens；仅空正文或无效审批JSON在同一个60秒期限内补一次2048 tokens请求。明确manual决定（即使缺少理由/风险字段）不重试成批准；网络/HTTP失败、工具调用、取消、两次无效响应均转人工，不自动放行。回退记录区分授权链缺失、配置/凭据不可用、长度超限、请求失败、空决定和格式错误。重试用量计入原任务；成功HTTP返回空正文时也保留实际/估算用量，避免少计。

这不是写操作白名单：`mkdir`、重定向、运行代码等仍由独立模型结合真实用户任务逐项审核，审批通过后仍走沙箱和路径约束。文件提案审核保持原流程；本轮恢复逻辑只接入Shell命令审核。没有调用真实模型测量放行率，测试使用隔离模拟HTTP和临时工作区。

Literal quoted reads and compositions of proven read-only commands avoid unnecessary approval requests. Unknown shell syntax and write/helper options remain on the ordinary approval path. The command reviewer retries only empty/malformed decisions once under the original deadline, never retries an explicit manual verdict into approval, and preserves all authorization, generation, persistence and sandbox checks. Empty successful responses now retain token usage. This does not enable assisted mode or broaden writing permissions automatically.

## 沙箱执行接入与内核文件规则（2026-10-14）

此前聊天Shell有字符串/路径检查，命令面板未接入，`workspace-write`并非操作系统隔离。本轮把命令面板、模型`run_shell`和Hooks接入同一模式检查；只读模式的写命令在询问审批前拒绝，文件提案的最终应用也拒绝。AI直接生成图纸、工程文档、Office批注/文件和归档的内置写工具增加只读入口检查，无法声明只读边界的第三方插件在只读模式不调用。用户主动使用文件管理器/编辑器的操作不属于AI沙箱，服务自己的会话/审批记录、记忆和配置持久化不被冻结。

Linux本地命令使用独立子进程在`exec`Shell前应用Landlock（最低ABI6）；服务本身不收紧权限。新子进程锁定OS线程，设置`no_new_privs`并加载规则后直接exec，限制继承到Python、Node和后续子进程。只读允许读取工作区和运行环境；工作区可写额外允许当前任务目录中的文件内容/目录项写入、创建、删除、移动，符号链接不会授予目标目录权限。可读运行路径为`/usr`、`/bin`、`/sbin`、`/lib`、`/lib64`、`/etc`、`/dev`、`/proc/self`、`/context`及工作区；`/data`、其他工作区和宿主目录不因读取`/`而全盘放行。

每条受限命令有单独临时目录、HOME、TMPDIR和Go缓存，读写临时区不等于授予项目写权限；命令结束清理。原全局HOME/缓存不传给受限Shell，避免该目录成为越界通道，因此这些命令的缓存不跨调用复用。标准输入输出管道和`/dev/null`用于正常I/O；没有把已有应用数据FD传给子进程。禁止创建设备，限制向域外进程发信号和连接域外抽象Unix套接字。命令结束或取消时回收其进程组，超时逻辑保留。

能力不足时不静默降为仅靠文本匹配：Linux ABI不足、内核调用被禁用、原生非Linux进程或SSH未接入远端helper时，受限命令明确失败且不执行。SSH和原生非Linux的完全访问仍沿用原执行路径。配置非法枚举返回400，省略值保留旧值。尚未配置模型时也允许独立保存沙箱偏好，兼容设置控件携带的空activeModel字段，不要求先填写模型API。`/api/config.sandbox`报告模式、backend、ABI、available及执行范围；它是本机可用能力与当前模式说明，不是每个插件的运行回执。全局沙箱值在每次命令调度时读取并固定为该命令策略；切换不追溯解除或追加已启动进程的限制。

必须区分三层：沙箱决定允许的访问范围；审批决定具体操作是否得到授权；工具权限决定工具是否可用。完全访问不会自动批准写命令或启用被禁用工具，小秘私有区等既有产品保护保留。

**范围限制：** 本次是本地Shell文件内容/目录项隔离，不是完整VM或容器。没有隔离网络；Landlock不完整限制chmod/chown、时间戳和扩展属性等文件元数据，不能据此宣称工作区外所有状态均不可改变。可信Node插件宿主及其他服务端原生能力不由这层进程规则隔离，workspace-write下仍遵循其原有权限和路径实现；它们不能被宣传成同等内核沙箱。详见[Linux内核Landlock文档](https://docs.kernel.org/userspace-api/landlock.html)。

Local model/Hook shell calls and the command panel now share policy enforcement. A fresh Linux child applies Landlock before exec; restricted modes fail closed when enforcement is unavailable. Read-only blocks write approval/application and built-in AI workspace mutators. Full access remains independent of approval and tool authorization. Private scratch directories are disposable. This is local shell file-content/directory-entry confinement, with explicit network, metadata, trusted-plugin, native-platform and SSH limits; already-running commands retain their launch policy.
