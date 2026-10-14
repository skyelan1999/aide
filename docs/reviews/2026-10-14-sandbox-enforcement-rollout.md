# 沙箱执行接入与本机8097更新 · 2026-10-14

用户反馈“沙箱配置好像基本上没用”。确认了三个具体问题：命令面板没有检查沙箱模式；工作区可写仅有文本/路径粗筛；无模型配置时保存沙箱偏好被模型必填校验挡住。只读文件提案应用及部分AI内置写工具也缺少统一拒绝。未获得用户具体失败模式，不能宣称复现了所有场景。

## 提交与结果

- `d99cfb5`：新增独立Linux子进程Landlock规则和实际文件操作测试。
- `39c6602`：接入聊天Shell、Hooks和命令面板；只读在审批前拒绝写命令，禁止应用写提案和调用已知内置写工具/未知只读边界插件。非法枚举400；无模型时可保存沙箱偏好，兼容设置控件附带的空activeModel。命令结束回收其进程组，保留取消和超时。
- 两批均推送至`fix/sandbox-enforcement`。没有合并main或创建Release。原生非Linux及SSH未接入远端内核规则时受限命令明确失败；完全访问保留原有执行能力，仍不绕过审批、工具权限或产品私有区保护。

每条命令启动时固定当时模式；新配置用于下一条命令，已经启动的进程不追溯更换策略。服务本身、用户主动的编辑器/文件管理器和应用状态存储不是该子进程沙箱的目标。临时HOME/TMPDIR和Go缓存按命令独立创建/清理，不再继承可写全局HOME/缓存；这会失去跨命令缓存复用。

## 验证证据

两仓quick通过。隔离Linux Docker内的扩展Go race：execsandbox规范仓1.744s / 本机1.374s，server规范仓53.251s / 本机52.145s。覆盖Sandbox、只读识别、审批、Harness、命令面板、Shell取消、远端取消、暂停、会话模型和并行分析。

最终补充配置持久化、设置/模型/密码兼容和子进程清理后重跑：execsandbox规范仓2.040s / 本机2.036s，server规范仓52.599s / 本机86.118s，均通过。

```sh
go test -race -mod=vendor ./internal/execsandbox ./internal/server -run '^(TestSandbox|TestExecShell|TestRemoteShell|TestHarnessIndependentReviewer|TestBasicReadOnly|TestAssistedBasicCommand|TestModel|TestSettings|Test.*Password)' -count=1 -timeout=3m
```

测试实际执行并读回：只读拒绝项目写入；工作区模式允许项目写入而拒绝目录外Python/Node写、符号链接写穿、删除/移动和未授权目录读取；完全访问可写隔离临时目录中的外部目标；私有临时区可用。另验命令面板三种模式、AI写工具/提案拒绝、配置落盘及下一条命令生效。补充Node/Python项目内正例，避免把解释器启动失败误算为越界防护。远端取消桩显式使用完全访问，确保仍实际走SSH取消路径。只读审核用例改验“审核调用数为0”，而非无意义地批准后才被沙箱阻止。

配置测试最初暴露无模型400问题，修复后又发现夹具误读旧根目录settings.json；改用真实`SettingsPath`验证。初始失败不作为PASS，最终检查已通过。新受限实现没有使用真实模型或修改用户的沙箱/审批偏好。本轮无前端改版，实际浏览器设置控件仍受既有本机证书提示限制，未绕过。

## 运行更新

- 更新前无运行/待澄清任务。只停止、备份并重建Compose `aide`；原数据、home卷和bind挂载保持；其他容器未重启。
- 运行镜像 `sha256:81af4a00fffead2874761264ced97258d0158583fb19ac3654855d49ba6ab975`，healthy。
- 原镜像保留为`aide:sandbox-previous-20261014`，核验ID `sha256:044204baf7f45ee99df604c4ec9ddd85d3a97ccf071207c3b9b2d99ac75e1130`。回滚可重标为`aide:local`并只重建AIDE；不自动覆盖数据卷。
- 停机后只读备份`.agent-state/sandbox-enforcement/data-before-update.tar.gz`，14305字节、39条，可读非空；SHA256 `3870629614fba18401c4ed25d4e99b769113b4f56392dad82225fb363f30f8c4`。含服务数据，不提交内容。
- 候选镜像以`cap-drop ALL`和`no-new-privileges`的正式权限条件离线测试，实际二进制helper拒绝只读写入、目录外写入及Python符号链接写穿，允许项目内正常写入。
- 正式8097：healthz、鉴权sessions/config/approval-policy均200。新config报告ABI8、linux-landlock、workspace-write、filesystemEnforced=true。原workspace-write及辅助审批关闭状态保持。实际POST command执行`true`返回0；Python试写随机命名的`/tmp/aide-sandbox-outside-probe-*`得到PermissionError，随后确认目标不存在。没有向真实工作区写验收文件或触发模型。
- 首页和app.js服务字节与本机源码一致。镜像来自保留其他未提交工作的本机兼容仓，不宣称为规范仓干净提交的可重现发行。

## 必须保留的边界

这是本地Shell的文件内容/目录项访问限制，并带信号/抽象Unix套接字域限制，不能称为全系统沙箱。没有网络隔离；Landlock不能完整限制chmod/chown、时间戳/扩展属性等元数据。可信插件宿主和其他服务原生能力不由此内核规则隔离，workspace-write下仍受各自已有权限/路径实现约束。原生非Linux和SSH受限执行未实现，不静默声称已隔离。依据见[Linux内核Landlock文档](https://docs.kernel.org/userspace-api/landlock.html)。

详见[执行策略](../architecture/execution-policy.md)与[任务账本](../tasks/sandbox-enforcement-20261014.json)。运行日志和脱敏回执保留在两仓`.agent-state/sandbox-enforcement/`。
