# Aide 对照 Codex 的能力差距评估

评估日期：2026-10-06  
范围：Aide 当前仓库与本地工作树；Codex 能力以 OpenAI 官方公开资料为参照。本文是能力评估和补齐建议，不代表功能已实现或发布。

## 结论

Aide 已有成熟的单工作区 Agent 基础：会话与队列、文件读写、命令执行、三级权限设置、计划/提案/审查流程、子代理、插件工具、图像附件和流式轨迹。当前最明显差距不是“再加几个提示词”，而是执行环境、权限审批与模型协议适配：

1. 没有浏览器控制和桌面控制；容器里的 Python/Node 不能控制宿主 Safari、屏幕或键鼠。
2. MCP 目前只支持 stdio 引用和只读工具；缺少可控写操作、OAuth/HTTP 连接和面向用户的确认流程。
3. `run_shell` 的安全主要依赖容器边界、工具策略和命令规则；插件代码与命令同属可信容器用户，并不是每插件 OS 级隔离。
4. 模型接入以 OpenAI Chat Completions 兼容接口为核心。模型能否稳定调用工具、看图、长程推理和遵守约束，取决于具体模型与兼容网关；Aide 不能用插件弥补模型本身不具备的能力。

## 实施进度（2026-10-06）

用户已授权按建议路线推进。当前工作树已开始实现模型能力登记：模型可标记工具调用、视觉、结构化输出、推理参数和音频输入/输出的支持状态，记录核验来源和日期；显式标记不支持时，服务端会阻止工具调用/结构化输出请求或移除推理扩展参数。旧配置未提供这些字段时保持兼容行为，状态仍是“未知”，不能视为已验证。

该进度只代表代码变更已写入当前未提交工作树。现已增加两项实现：模型能力声明会通过设置 API 往返保存；`run_shell` 对写入、网络和外部状态命令使用现有澄清确认卡，只有用户明确确认后才执行，拒绝/取消不会执行。只读命令白名单还会拒绝 shell 替换、命令串联、通配符和换行语法，避免复杂 shell 表达式被前缀误放行。全仓 race 测试在此边界加固前通过；加固后相关模型/审批 race 回归用例、go vet 与快速静态验证通过。实际 Safari UI 验收尚未进行：Safari 当前指向已运行的 RC15 实例，未重启服务以加载这份工作树。

这不是整条路线全部完成。未实现的能力仍包括：每插件 OS 级隔离/独立网络策略、宿主浏览器和桌面桥接、MCP HTTP/OAuth 与写工具授权、任务级 Git worktree/diff 审阅、项目环境 setup/缓存、插件签名目录，以及更完整的跨重启长任务恢复。现有 Python/ZIP 插件和设置页属于未提交工作树代码，不是发布能力。后续需要按独立阶段实现并分别验收，不能把“测试通过”理解成这些未开发能力已经交付。

Codex 的有效做法是把模型、工具、技能/插件、沙箱、审批和实际执行环境一起设计。Aide 可借鉴其分层思路，不应把 Codex 的模型专属表现当成任意 OpenAI 兼容模型都能达到的保证。

## 现有能力底座

| 能力 | Aide 当前状态 | 证据/边界 |
| --- | --- | --- |
| 工作区理解与编辑 | 已有列文件、读文件、搜索、Office/PDF 提取、文件修改提案 | `docs/PRD.md` WF/TL；远程 SSH 工作区部分能力受限 |
| 命令与验证 | 容器内 `run_shell`，支持 read-only/workspace-write/danger-full-access 三档、超时、取消与输出限制 | `internal/server/workflow.go`、`command.go`；容器并非 OS 级每任务沙箱 |
| 工作流 | 计划→提案→审查，支持阶段选择和自动编排 | `docs/PRD.md` §2.3、`workflow.go` |
| 子代理 | 可派生子会话并继承工作区快照 | `plugins/subagent`、`workflow.go`；并发/隔离深度弱于独立 worktree/独立云环境 |
| 插件和 Python | JS 插件工具、设置页与 ZIP/Python 包能力正在工作树开发 | 本地未提交 diff 与 `docs/plugin-protocol.md`；不等于已进入发布版本 |
| 图像输入 | 图片作为多模态附件交给被判定支持视觉的模型 | `internal/server/attachments.go`；模型能力目前由用户模型声明和 ID 片段推断 |
| 语音 | 浏览器 ASR 转写、小秘研判、独立 TTS 和本地/在线回退 | 非原生音频模型输入/输出闭环；识别能力随浏览器而变 |
| 网络搜索 | `web_search` 返回网页标题、URL 与摘要 | `workflow.go` `webSearch`；不是带可见页面会话的浏览器，也不保证拿到完整正文 |
| MCP | stdio 服务发现；模型只能调用标注为只读的工具 | `mcp_source.go`、`docs/PRD.md` WF-11；无 streamable HTTP/交互 OAuth/写操作审批 |

## 差距清单与优先级

| 优先级 | 差距 | 类型 | 影响 | 建议补齐 |
| --- | --- | --- | --- | --- |
| P0 | 操作型审批缺统一闭环 | Aide 产品/安全 | 当前文件提案有审批；shell 在所选沙箱模式内直接执行。浏览器点击、外部提交、下载、桌面键鼠没有细粒度逐次确认 | 统一 action proposal：展示目标、动作和参数摘要；用户批准后短时、单次执行；拒绝/取消/超时均由服务端强制执行并可审计 |
| P0 | 沙箱不是每任务/每插件边界 | 运行环境/架构 | Docker 提供容器级限制，但插件以同一 aide 用户运行；工作目录可写挂载，`/local` 宿主路径也被挂载；危险命令黑名单不等于 OS sandbox | 先把只读/写/联网权限做成服务端能力声明；再评估 per-run worktree/container。默认关闭插件网络，必要时按域名放行 |
| P0 | 浏览器与电脑控制缺失 | Aide 产品 + 宿主环境 | 当前容器无法访问 macOS Accessibility、Safari 页面或用户键鼠 | 增加 macOS 本机桥接服务；浏览器和桌面分开授权。站点白名单、应用白名单、操作前确认、取消、截图/DOM 脱敏必须在服务端校验。先做受控浏览器会话，再评估原生桌面操作 |
| P1 | MCP 只有只读 stdio | Aide 产品 | 无法安全执行外部系统写操作；不能直接复用宿主 Codex MCP；OAuth 与长连 HTTP 服务缺失 | 实现 streamable HTTP/SSE 与 OAuth；工具声明仅用于 UI 提示，写操作仍走 Aide 逐次审批；每连接可独立停用/撤权 |
| P1 | 插件格式没有捆绑 Skills、MCP、hooks 的统一清单 | Aide 产品 | ZIP 包能捆绑 JS/Python 工具，但工作流指引、连接器和生命周期 hook 仍各自为政；设置 JSON 不是权限授予 | 扩展 manifest：skills、MCP servers、工具权限、配置 schema、生命周期 hooks；安装时显示权限和依赖；默认不自动授予网络/桌面权限 |
| P1 | Git 工作区隔离与审阅集成不足 | Aide 产品/环境 | 有 Git 命令但没有一键任务 worktree、变更集隔离、内嵌 diff 审阅/行级评论与 PR 创建闭环 | 每个开发任务可选 branch/worktree；呈现改动清单和 diff；把测试结果与提交 SHA 绑定；GitHub 等写操作经用户确认 |
| P1 | 环境安装与可复现验证不够自动 | Aide 产品/运行环境 | Docker 镜像有固定依赖；没有按项目自动识别 setup 脚本、依赖缓存和任务级网络策略 | 项目环境配置文件 + 显式 setup 命令；默认网络关闭；域名 allowlist 后安装依赖；缓存按锁文件指纹隔离 |
| P2 | 第三方接入与插件发现生态弱 | Aide 产品 | 手动上传插件/登记 MCP 为主；没有统一插件目录、兼容性声明、签名与升级渠道 | 做本地/私有插件目录、来源签名、版本锁定与回滚；对“已安装/已授权/可调用”分状态显示 |
| P2 | 长任务恢复和跨设备交接弱 | Aide 产品/部署 | 服务重启后运行中的任务标记 interrupted；会话虽持久化，但缺少云端持续运行与执行环境迁移 | 先增强 checkpoint/resume 和可恢复工具状态；再做可选远程 Runner，而非默认把项目发送到云端 |

## 模型限制与弥补方式

| 模型侧限制 | 当前体现 | 可以怎样弥补 | 不能靠什么解决 |
| --- | --- | --- | --- |
| 工具调用遵循度和参数正确率不同 | Aide 下发统一 OpenAI 风格 function tools；不同模型对并行工具、多步调用、长 schema 的稳定性差异明显 | 做按模型的工具调用评测集；控制 schema 长度；参数校验失败后给结构化错误并允许有限重试；为弱工具模型提供显式阶段/人工确认模式 | 再写一段系统提示词不能保证模型会正确调用工具 |
| 视觉能力不一致 | 图片输入在 `visionGateLocked` 按显式配置或模型 ID 片段判断 | 模型目录保存服务端能力元数据和最近验证时间；上传一张小图做能力探测；错误时明确切换建议 | 浏览器/桌面工具不等同于模型会看图；非视觉模型无法从截图推理 |
| 推理参数并非跨供应商标准 | `provider.go` 请求 Chat Completions，并把 `thinking`/`reasoning_effort` 等字段按统一逻辑写入请求 | 建 provider adapter：按供应商映射/剔除参数；配置能力声明，显示模型实际支持项 | 不能对所有兼容端点盲目发送同一扩展字段 |
| 上下文窗口与长程一致性不同 | Aide 可估算上下文并压缩历史，但摘要仍由当前模型生成 | 设置真实模型窗口元数据；重要文件/决策做可引用的任务状态摘要；长任务分段验收；每次继续时回读关键源文件 | 单靠增加工具轮数不能弥补上下文遗忘或模型不稳定 |
| 代码生成和自查质量不同 | Aide 可以运行命令/测试，但模型决定改什么与如何解释结果 | 将编译、测试、lint、diff 审查作为独立可核验阶段；错误原文回传；让模型按证据总结；关键修改加入确定性校验 | 模型自称“已验证”不等于命令实际通过 |
| 音频输入/输出格式和质量不同 | 当前语音路径主要依赖浏览器 ASR 与 Aide 的语音研判/TTS 管线 | 提供浏览器兼容矩阵和本机/服务端 ASR 选择；单独标注转写、意图研判、语音回复分别走的模型/引擎 | 更强聊天模型不能修复 Safari/Web Speech API 不支持或麦克风权限被拒 |

## 建议路线

### 阶段 1：安全和模型适配（先做）

1. 为每个模型记录 `tool_calls`、vision、上下文窗口、结构化输出、reasoning 参数等能力及来源/验证时间；未确认时不宣称支持。
2. 拆分 OpenAI-compatible provider 适配层；至少区分标准 Chat Completions 与厂商扩展参数。
3. 把“立即执行”的高影响工具改成服务端 action proposal/approval，优先覆盖 shell 写操作、MCP 写工具、外部发送/删除动作。
4. 明确容器可见的文件根、网络默认策略和第三方插件信任级别；修订 UI 避免把 `danger-full-access` 描述为主机全权限。

### 阶段 2：浏览器与 MCP

1. 先做浏览器专用会话：从 Aide 打开隔离标签页，模型能读 DOM/截图、导航和点击；限定站点并对提交/下载逐次确认。
2. 再加入通用 MCP HTTP/OAuth；浏览器工具与其他 MCP 写工具共用审批接口，但授权名单互相隔离。
3. 让每个工具包的设置 schema 可生成 UI 控件并显示权限摘要，Secrets 继续进 Vault。

### 阶段 3：桌面操作、Git 与长任务

1. macOS 原生桥接作为独立可选组件，首版只支持截图和用户逐次确认的简单键鼠操作；不把桥接服务暴露为通用 shell。
2. 加任务级 Git worktree 与 diff/review。
3. 加 checkpoint/resume、项目 setup 配置和可选远程 Runner。

## 建议验收门槛

- 每项能力分别标识“已安装、已授权、当前可调用、最近验证”；不要用插件存在代替运行成功。
- 模型能力矩阵至少覆盖当前配置模型的工具调用、视觉附件、reasoning 参数和上下文窗口，并保存 API 探测/人工核对证据。
- 所有写操作均验证服务端权限拒绝路径、用户拒绝、取消、超时和日志脱敏；UI 按照真实运行的浏览器验收。
- 浏览器和桌面测试使用专用测试页面/应用，截图与轨迹不包含密码、Cookie、令牌或私人窗口内容。
- 发布前在发行镜像验证；当前本地未提交的插件/Python 工作不能当作已发布能力。

## 依据

### Aide 工程

- `docs/PRD.md`：WF-11、TL-01~TL-18、AU-01~AU-09、MD-08、AG、插件需求。
- `docs/architecture.md`：容器结构、MCP stdio 边界、工作流与运行状态。
- `internal/server/provider.go`：Chat Completions 请求构造、工具调用、推理参数和流式解析。
- `internal/server/attachments.go`：图片附件和视觉能力判断。
- `internal/server/workflow.go`、`command.go`：工具循环、工作流、沙箱执行。
- `docs/plugin-protocol.md`：插件运行、设置、Python 和可信插件边界。
- `compose.yaml`：`/workspace`、只读 `/context`、宿主 `/local` 挂载及容器能力限制。

### Codex 官方参考

- [Codex 插件结构](https://developers.openai.com/plugins/concepts/plugins)：插件可组合 Skills、MCP、hooks 和可选 UI。
- [Skills 与 MCP 的职责](https://developers.openai.com/plugins/concepts/skills)：Skills 提供可复用流程指引，MCP 提供实时上下文和受控动作。
- [Computer use API 指南](https://developers.openai.com/api/docs/guides/tools-computer-use)：模型需要宿主应用提供执行环境，返回截图/工具结果供下一步决策；本身不是只加 prompt 就能操作电脑。
- [Codex 浏览器与电脑策略](https://help.openai.com/en/articles/20001510-manage-browser-and-computer-use-in-your-enterprise-workspace)：浏览器与原生桌面权限分开配置，可按站点/应用约束并配置审批时长。
- [Codex 沙箱设计](https://openai.com/index/introducing-upgrades-to-codex/)：默认隔离网络，危险动作需权限批准；网络/MCP 能力扩展会增加风险。
- [Codex 插件能力扩展](https://openai.com/index/codex-for-every-role-tool-workflow/)：按角色组合应用、技能和工作流，插件只是工具层的一部分。

## 置信与限制

这是基于当前源码/文档的架构差距审查，不是对所有运行中服务、模型供应商兼容性、macOS 浏览器交互或发行包的实测。Codex 功能依赖版本、工作区策略和操作系统，引用仅用于提炼设计模式，不表示 Aide 应照搬相同功能或授权默认值。
