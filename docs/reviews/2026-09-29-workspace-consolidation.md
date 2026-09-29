# 剩余改动整理与源码推送

日期：2026-09-29；客户端：Codex desktop；任务：consolidate-workspace。
用户授权：“整理一下推上去”。基线 `9295a22867dd25ef0806df56fb895f8582f7f01e`。
分支：`codex/consolidate-workspace-20260929`。这是保留进度的源码交付，不是发布或生产部署。

## 分组

1. 后端：SSH/SFTP 父目录创建、远程工具执行及取消；项目缓存同步和来源注册表归属；上传/搜索 API；自动路由默认值；部分 RCA 后端；相关回归测试。
2. 前端与交接：搜索框内按钮、默认隐藏文件、拖拽上传、目录位置记忆、命令历史、SSH 测试布局、心跳显示；PRD/架构与任务账本。

workflow.go/context.go 同时涉及缓存、SSH 与 RCA，保留完整关联修改，不人为拆成无法编译的独立提交。
已有任务中的“未提交/未推送”是原执行阶段的历史记录；各任务新增 consolidation 字段指向本次交付。
版本沿用 **0.1.12.0 RC1**，未升版、未打 tag、未构建发布镜像。

## 明确未完成项

- RCA 仅有阶段校验、提示与报告工具后端；验证之后的 UI 入口、专用自动化测试及任意格式报告交付未完成。draw.io 路径存在性也尚未严格验证。不能标成完整功能。
- 项目记忆隔离及旧记忆迁移存在失败回归，修复后才可合并发布。
- 前端搜索、拖拽、命令历史等完整页面交互验收仍 NOT_RUN；本次只整理源码与文档，没有新增 UI 实现，不重新宣称浏览器已验收。
- 心跳仅证明应用仍发事件，不证明 SSH 远端正常；取消测试证明本地 SSH 客户端退出，不保证远端子进程已清理。24 次预算作用于所有工具，不仅 SSH。
- 搜索上限为 200 目录/500 结果；上传仅文件拖拽，未实现文件夹递归上传；真实远端网络稳定性未验收。

## 验证证据

本次重新运行：`node --check`（app.js、locales/en.js）、`node scripts/test_i18n.cjs`、`node scripts/test_stream_url.cjs`、`git diff --check`、`bash scripts/version.sh check`，均 PASS。
本次不改业务源码，沿用紧邻上一轮对同一业务内容的检查结果，不把沿用结果写成重跑：

- `go vet -mod=vendor ./...` PASS；性格演化定向 race PASS。
- 全量 race FAIL，server 411.895s；失败：`TestAssistantMessageAgenticFallsBackToAnalyze`、`TestProjectAideMemoryFollowsWorkspace`、`TestLegacyGlobalAideMemoryMigratesOnceToProjectCache`、`TestStartTaskCompactsOverBudgetHistoryBeforeProviderCall`。日志 `.agent-state/personality-full-race.log`，结论亦见 [上轮记录](2026-09-29-personality-toolbar.md)。
- 全局文档有 5 处历史坏链接；Windows 路由测试因符号链接权限失败。没有 full PASS 收据，发布门禁不通过。

## 运行与回滚

未连接真实远端主机、未调用付费模型、未修改凭据、未重启服务、未清理用户缓存或未跟踪资料。
全部原有源码/测试/任务记录均保留在分支；主分支不合入这些未完成内容。
如不采用该整理分支，保持 main 即可；若后续合并后撤回，使用对应 merge/源码提交的 revert，不用 reset/clean 清空用户工作。
