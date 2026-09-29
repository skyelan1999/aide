# 文件预览工具栏与性格演化修复

日期：2026-09-29。基线：`ccec6367e498d815991c589b30e4e523b04c5728`。

## 根因与修复

- 后加载的 `macos.css` 中 `.file-view-head > div { flex: 1 }` 同时拉伸路径与操作组，按钮组内部靠左。改为仅路径伸展，操作组 `flex: 0 0 auto` 并靠右；窄屏保留换行及路径截断。
- 日志记录演化 `finish_reason=length` 且正文为空。原调用继承聊天推理设置，只有 2048 输出 token，推理可耗尽正文预算。演化专用配置关闭 thinking，给出 8192 token 上限、120 秒整体超时，不改聊天设置，不无限重试。模型不遵守参数时仍可能失败，原性格保留并返回真实错误。
- 自动演化成功后此前仅保存计数/历史，漏存新性格正文。现在同时持久化 settings.json。

## 验证

- Docker `go test -mod=vendor -race ./internal/server -run 'Test(Evol|EndToEndEvolution|Trigger|AuditOnAttempt|InsufficientSample|Rollback|Reset|CompressMode|ManualEvolve)' -count=1`：PASS，19.556 秒。
- 模拟模型回归覆盖非推理参数、聊天设置不变、空正文 length 错误保留原性格、自动演化正文落盘。
- `git diff --check`、JS 语法、`bash scripts/version.sh check`：PASS。
- 实际浏览器打开隔离 HTTP 预览，使用当前 index.html 原始 header 和完整 CSS，无用户数据。1280px 桌面按钮右边缘 1252（28px 内边距）；390px 窄屏右边缘 374（16px 内边距），scrollWidth=390，无溢出。深/浅色窄屏已观察。
- 预览仅验证布局，不冒充文件保存端到端；生产 HTTPS 与真实付费模型验证 NOT_RUN。
- 全量 `go test -mod=vendor -race -count=1 ./...`：FAIL，server 411.895s，退出 1。失败为 `TestAssistantMessageAgenticFallsBackToAnalyze`、`TestProjectAideMemoryFollowsWorkspace`、`TestLegacyGlobalAideMemoryMigratesOnceToProjectCache`、`TestStartTaskCompactsOverBudgetHistoryBeforeProviderCall`。运行对象包含此前未提交改动；不是独立提交的全量通过凭证。日志未报告 DATA RACE。tts PASS（4.836s）。原串联 vet 因测试失败未运行，另行执行。
- i18n 回归 PASS。全局文档检查发现既有 2026-09-28 报告的 5 处坏链接；Windows 路由自测因 WinError 1314 无符号链接权限失败（其余 2 项通过）。不修改系统权限来绕过。
- release-check 实际返回 BLOCKED：缺 full 收据、Git 非干净状态及未完成验证/清理阶段，因此不升版打标签。

源码 SHA-256（当前 Windows 工作文件）：macos.css `277A7707CE9F4700E751F0DBDBA5826AFF9139448550B60024B678B06772DED4`；personality_evolution.go `2C39864DF589E261B990FD1908574B27D17D8772C4597D12C47429E892D5FEC0`；personality_evolution_test.go `F32507965B43B6B8C4E650D34BBD9EFF2C5319E39025D435AF72FFD85A8FC4B0`。Git 换行规范化后的身份以提交为准。

## 交付与回滚

仅提交本任务文件。此前工作区、缓存、文件面板、RCA 等改动保留本地，不纳入本次 push。
使用现有版本脚本追加当前版本“未发布源码补丁”记录，不绕过 clean-main/full 门禁升版或打 tag。
当前服务不重启、不替换；源码 push 不代表线上已更新。回滚本次源码提交即可撤回修复，没有迁移或删除用户数据。

源码提交：`9f05864`（含版本脚本 note 生成的记录，当前版本仍为 0.1.12.0 RC1；未新建 tag）。隔离预览进程已停止。完整验收失败不阻止用户授权的独立源码推送，但禁止声称已发布合格版本。

最终结果：独立 `go vet -mod=vendor ./...` PASS（Docker 退出 0）。源码 `9f05864de43fd2d96782497f32ae53e42a90d253` 已 push 到 origin/main，并经 ls-remote 核实。原始测试日志保留于 `.agent-state/personality-targeted-race.log` 和 `.agent-state/personality-full-race.log`；临时测试容器、预览脚本已清理。生产仍为原服务，需要后续构建/部署才会看到新 UI。
