# 2026-09-30 功能整合记录

任务：`consolidate-push-20260930`。用户授权整理本轮文档与目录、合入 `main` 并推送 `origin/main`。本次不包含升版/tag、GitHub Release、镜像发布或生产部署。

## 文件归属

本轮功能实现与记录按现有目录归档：

- Go 服务端逻辑及回归：`internal/server/`；前端、主题和图标：`internal/server/web/`。
- 需求与操作说明：`docs/PRD.md`、中英文 `docs/user-guide.md`；任务交接账本：`docs/tasks/`。
- 本次整合证据：`docs/reviews/`。

检查后没有发现应移动到 `docs/` 的根目录文档；未知/忽略文件和运行数据未清理。

## 合入范围

整合当前工作区中对应既有明确需求的变更，包括 DSH 上下文估算、长会话分页、归档搜索恢复、PDF 文本读取与 TF-IDF 检索、嵌套 SFTP 上传、新建小秘会话需手动发送、小秘模型/轨迹与导出、问题分析工作流、主题与 favicon、系统日志、文件面板样式和长按拖动导航、回车发送快捷键等；各项具体范围见对应 `docs/tasks/*.json`。

所有功能变更与测试资源留在受控源码目录；新增任务 JSON 放在 `docs/tasks/`。历史已有工作区修改均逐项核对后才纳入，未使用宽泛清理或覆盖操作。

## 验证与限制

- Docker `aide:local` 镜像中运行 `go test -mod=vendor ./internal/server -run 'TestRemoteWorkspaceDispatch|TestMCPReferenceDiscoveryAndReadOnlyCall' -count=1`：通过，0.806s。
- 最初用纯 `golang:1.26-bookworm` 镜像运行时，镜像缺少 `python-docx`，SFTP 测试在 Office 后置断言失败；换到项目应用镜像后相同定向回归通过。
- `node --check internal/server/web/app.js`、`node --check internal/server/web/locales/en.js`、`git diff --check`：通过。
- `python3 scripts/check_docs.py`：通过，检查 108 份 Markdown、228 个本地链接、JPEG 标记和 FR-01..100。
- Docker `aide:local` 镜像内 `go build -mod=vendor -buildvcs=false -o /tmp/aide-merge-check ./cmd/aide`：通过。首次启用 VCS build stamp 时挂载仓库的 Git 状态读取失败；关闭 buildvcs 后完成构建检查。
- 未运行完整 Go 测试套件或官方发布门禁。Safari/UI 交互未验收：Mac 桌面锁定，自动解锁失败。真实外部 SFTP 服务器、完整 MCP 服务和所有新 UI 路径仍不应视为本轮端到端验收。

## 版本与部署

本次只合入并推送源码，不变更 `version.md`，不创建 tag，不构建/发布镜像，不重启或替换正在运行的 8097 服务。后续完整验证和发布应独立执行并记录对应代码指纹。

代码集成提交 `2054b04c46dddadf3808c367caa31624a6d722e8` 已 fast-forward 合入 `main` 并推送至 `origin/main`；推送后本地 `main` 与远端 SHA 一致。
