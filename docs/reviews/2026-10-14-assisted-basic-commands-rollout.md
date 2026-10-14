# 基础命令审批改进与8097更新 · 2026-10-14

用户反馈“一些很基本的命令辅助审批根本就不管用”。本轮未得到具体命令样例，也没有在本机数据中找到既有审批回执，不能确定用户当次失败的唯一根因或真实模型的放行率。源码确认：只读识别拒绝所有引号及组合语法，导致普通读取进入审批；命令审核仅512 tokens且失败原因笼统。另有运行事实：8097全局审批接口返回 `enabled=false, revision=0, rememberedRules=0`。已异步询问界面是否显示开启，未据此擅自切换模式。

## 两批修复

- `a2d7cbf`：带引号路径/模式、数字行范围`sed -n`、每段均判定为只读的管道与`&&`组合走只读路径。未引用通配符、Shell求值、重定向、后台、任意代码、写入/辅助执行选项不能走该路径。`find -exec/-delete/-fprint`、`rg --pre`、`file -C/--compile`和Git输出文件/外部diff选项继续拒绝，包括拆引号和相关选项变体。原有执行阶段的沙箱、保护区及路径检查保留。
- `635a082`：仅对命令审核的空决定或无效JSON补一次请求，预算512→2048，共享原60秒期限和所选模型/推理配置；明确manual立即结束，即使其他字段不完整，也不重试成批准。HTTP/网络失败或工具调用转人工。原因区分授权、配置、长度和响应故障。空正文的成功HTTP响应保留用量，补请求不会少计。

两批已推送 `fix/assisted-basic-commands`。`mkdir`、重定向和脚本等仍走具体命令审核，没有增加写命令自动白名单；文件提案审核保持原流程。全局审批开关及记住规则不变。没有Release或main合并。

## 验证

两仓quick通过。隔离Linux Docker、模拟HTTP模型、临时工作目录，广泛定向race通过（规范仓44.851s / 本机兼容仓42.247s）：

```sh
go test -race -mod=vendor ./internal/server -run '^(TestBasicReadOnly|TestReadOnlyAllowed|TestApproval|TestAssisted|TestHarness|TestGlobalApproval|TestRemembered|TestRunShell|TestShell|TestEmpty|TestProvider|TestPause|TestSessionModelSelection|TestConversationParallel)' -count=1 -timeout=4m
```

最终补充边界及端到端mkdir恢复定向race通过（规范仓9.745s / 本机8.718s）：

```sh
go test -race -mod=vendor ./internal/server -run '^(TestBasicReadOnly|TestReadOnlyAllowed|TestApprovalReviewer|TestAssistedBasicCommand|TestRunShellRequires|TestHarnessReadOnly|TestHarnessIndependentReviewer|TestShellBlocked)' -count=1 -timeout=3m
```

实际在临时目录执行带空格路径的cat、sed读取、cat/head管道和pwd/ls组合，无待审问题或审核模型请求。模拟mkdir先空响应、再有效批准，命令成功且只有一个审批回执；覆盖两次失败、明确人工、未授权、取消、工具调用、HTTP错误和两次请求用量汇总。既有人工写入确认、read-only沙箱、授权链、长期规则、暂停/恢复、并行和模型选择测试仍通过。没有真实模型调用或真实放行率测量。无新增UI，浏览器交互未作为本轮后端验收；实际工作台证书提示依旧需要用户手动处理。

## 本机更新与回滚

更新前运行/待澄清任务数为0。仅停止并重建Compose的`aide`，原数据/home命名卷及工作目录挂载保持；Docmost、数据库、Redis和其他AIDE测试容器未重启。

- 新镜像 `sha256:044204baf7f45ee99df604c4ec9ddd85d3a97ccf071207c3b9b2d99ac75e1130`，healthy。
- 原镜像保留为`aide:assisted-basic-previous-20261014`，核验ID `sha256:2cf54f033f8a92292faf057b05ac53c060f6af6ba3f6844203527637217dae35`。回滚可重新标记为`aide:local`后只重建AIDE，不自动覆盖数据卷。
- 停机后只读挂载数据卷备份到忽略目录`.agent-state/assisted-basic-commands/data-before-update.tar.gz`，14305字节、39条，tar可读且非空；SHA256 `c84c55bc2757b568c051b8d961e78c2466b4caca6d8a9a7bb10ba7a1a50bed1d`。含服务数据，不提交内容。
- `/healthz`、鉴权后的`/api/sessions`、`/api/config`、`/api/approval-policy`均200。首页与app.js实际服务字节匹配本机源码；审批enabled/revision/规则数量与更新前一致，仍关闭。

运行镜像来自保留其他开发改动的本机兼容仓，不宣称为规范仓干净提交的可重现发行。两仓日志及脱敏smoke在`.agent-state/assisted-basic-commands/`；详见[任务账本](../tasks/assisted-basic-commands-20261014.json)。
