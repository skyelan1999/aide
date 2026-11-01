# DESIGN-002 · 辅助审批风险分层与Git工作区保护设计

- 编号：DESIGN-002
- 创建时间：2026-11-01 01:24:09
- 状态：待评审
- 关联：REQ-002

## 开发流程
1. 保持既有工作树改动不覆盖；新增纯函数确定性门禁与 `.git` 路径保护，辅助审核继续只对具体待审对象做一次性判断。
2. 调整辅助审批系统策略：允许用户请求所授权的常规构建、测试、清理任务内临时产物与文件改动由模型按风险判断，不再按 `rm` / `chmod` 等词一刀切；外传、凭据、系统/权限、远程执行、工作区外、未知副作用仍转人工。现存 `shellSandboxPolicy` 继续优先且不可由模型覆盖。
3. 增强执行意图，提供基于确定性命令词法分析的操作类别/显式目标、Git 仓库上下文与容器外部影响警示；复杂语法标为部分/未知解析，原始命令始终随审查请求与 UI 保留。
4. 扩展文件提案自动审查：本地当前工作区内新建目标和已读取的现有目标均可候选；已存在文件必须携带完整未截断 before 文本、基线哈希与实际目标哈希相符；拒绝 `.git` 元数据目标、SSH/远程来源、只读、路径/绑定不可靠及审核载荷过大提案。批准仍调用现有带 guard 的版本化应用流程。
5. 测试涵盖硬拒绝不被 reviewer 覆盖、危险词不再独立造成手动、`.git` 保护、Git 检查命令、已有文件合格/不合格、目标变化与保存失败路径；运行可用的 Go/前端验证。

## 依赖条件
- `REQ-002` 定义的三层边界：shell sandbox/path guard、真实用户授权、独立辅助审查；容器/Landlock 的实际范围需要在实现和 UI 中如实描述。
- `approvalAuthorizationLocked` 仅使用真实 root 用户消息、root 任务及真实用户插话；child/delegated task prompt 不成为授权。
- `applyTaskGuarded` 为本地文件提案执行版本校验并逐项持久化；`proposalVersion`、`checkVersion` 和 Markdown history 保持原样。
- `.git` 可能是目录或 git worktree 指针文件；目标路径保护按清理后的路径分量识别。
- Go SDK 与 `gofmt` 需在实现环境可用；若不可用，报告阻断，不以静态检查代替测试。

## 架构需求
- 新增 `assistedShellGate(command, repoContext)` 结果：允许进入模型审查 / 要求人工 / 确定性禁止；绝不能改变执行器沙箱结果。保留 `shellBlocked` 与 `shellTouchesAssistantZone` 作为硬禁止；删除仅按危险词逐字拦截的辅助审批额外层。
- 对命令里精确引用 `.git` 的变更/删除/权限/重定向目标采取确定性人工门禁或拒绝自动放行；复杂动态表达式涉及仓库根且潜在删除元数据时转人工。Git 仓库存在只作为审核上下文，不能构成放行。
- Shell execution intent 扩充为确定性 token summary（命令 executable/action、候选路径、Git repo context、是否有复合/动态语法、网络/容器可能影响），无法完整解析时明确 `unknown`。UI与模型都同时展示/收到执行意图及完整原始命令。
- 文件提案资格检查针对每个未应用 Change；新文件确认目标当前不存在；已有文件要求 `BaseHash != ""`、before 无截断标记、目标路径安全且非 `.git`、解析到可写本地 root、当前文件 hash 与 `BaseHash` 一致；target/version 任何错误均 fallback manual。
- 文件审批当前环境检查必须在模型返回前后都绑定工作区身份、source binding、workspace revision、task generation、file fingerprint；最终 `applyTaskGuarded` 再次验证目标版本。若现代码没有单个函数能证明所有字段，按失败关闭，避免用简化前置校验替代应用 guard。
- 审批记录/receipt、无工具审核模型、超时与取消、手动回退和原有记忆规则保持兼容。

## 待确认项
- 实际执行路径中本机 shell 由 Landlock 限制文件访问范围，但网络、Docker daemon/宿主 socket、插件权限并未由该沙箱保证；必须核实 Docker 部署是否暴露额外挂载/daemon 接口，未能证明则只在说明中标为未知，不推测其受保护。
- 命令摘要 parser 是帮助风险判断的保守词法扫描，不是 POSIX shell AST；任何变量替换、命令替换、脚本、循环、复杂重定向不可声称已可靠解析，审查 prompt 必须以原命令为准。
- 保留 `shellBlocked` 的既有确切模式，避免把权限策略改造扩大成执行语义变化；用测试确保其拒绝仍优先。

## 变更记录
- 2026-11-01：基于 REQ-002 建立实现设计；继承 DESIGN-001 的意图摘要基础，不改变其已应用状态和既存工作树修改。
