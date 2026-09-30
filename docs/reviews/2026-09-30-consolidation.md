# 2026-09-30 功能整合记录

> 原始分支整合记录。全量 AP 补验、文档归档、快进合并与远端推送的最终状态追加在文末；以下早期记录保留当时的测试范围。

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

## AP 补验与文档收尾（2026-09-30）

### 结果

- 正式 `python3 scripts/agent-route.py verify full`：PASS，收据 fingerprint `597973ed50d94a5d109ddca105c5e5b5e2bf4d13a27c7bed219d48222f002319`，源码 HEAD `eaba911f725424d6514d4a7da00bf228d16659e2`。覆盖 quick 清单，以及 Docker 中 `go test -race -count=1 ./...` 和 `go vet ./...`。
- AP 前端回归通过：assistant mode controls、click latency/history pagination、Shift+Enter 提示与组合键、文件类型识别、上传进度、提醒样式/CSP、语音小秘、i18n、SSE URL。
- 插件回归通过：current-time、lunar-calendar、Office、SQLite（33/33，Node 24 Docker）及 DOCX 批注测试（aide:local Docker）。
- 协议/E2E 通过：local/skill/link/FTP/FTPS/SFTP/SMB 引用及 MCP 只读工具调用；临时 sshd 上 cancel 和 timeout 均清理远端进程组。
- 隔离 Safari 实际检查：工作台页面与桌面布局可渲染；系统日志可选择 ERROR，空结果文案正确，下载操作显示成功提示。只在隔离 `/data` 和 `/workspace` 使用未配置模型的测试实例。

### 修复和文档整理

- 小秘会话此前漏禁用“轨迹”控件，现并入该模式的禁用控件同步；对应前端回归通过。
- 输入提示此前仍写 Ctrl+Enter，与 Shift+Enter 发送规则不一致，已校正中英文提示，并修复旧测试的分页 URL 与定位标记。
- 将 2026-09-26 交接文档、已完成的 i18n 方案从根目录移入 `docs/archive/`，根 `HANDOVER.md` 收敛为当前运行、验证、数据保护及合并入口。README、PRD、架构、安装和中英文用户指南改为说明 `v0.1.13.0-RC1` 发布 tag 与 tag 后源码改动的边界。安装文档核实到该 tag 没有 GitHub Release 附件，因此没有声称存在可下载的当前镜像。
- `python3 scripts/check_docs.py`：112 份 Markdown、252 个本地链接及 FR 索引通过。文档仅从活跃根目录归档，没有删除承载独有历史、验收或法规内容的材料。

### 未完成的手工 UI 覆盖

Safari 自动化期间 macOS 锁屏，之后无法继续系统浏览器操作；没有尝试绕过锁屏或证书策略。以下行为在该次 UI 验收中为 NOT RUN（代码与自动化/后端回归不因此判失败）：提醒创建/搜索/完成，窄屏侧栏和拖动导航，活跃长会话性能与搜索恢复，Shift+Enter/IME 实际输入，XLSX 预览及真实文件上传进度，置顶/头像/上下文 meter，以及小秘会话内模型选择。相应 task JSON 已逐项保留状态和证据边界。

合并与推送结果追加于下方。
- `python3 scripts/check_docs.py`：通过，检查 108 份 Markdown、228 个本地链接、JPEG 标记和 FR-01..100。
- Docker `aide:local` 镜像内 `go build -mod=vendor -buildvcs=false -o /tmp/aide-merge-check ./cmd/aide`：通过。首次启用 VCS build stamp 时挂载仓库的 Git 状态读取失败；关闭 buildvcs 后完成构建检查。
- 未运行完整 Go 测试套件或官方发布门禁。Safari/UI 交互未验收：Mac 桌面锁定，自动解锁失败。真实外部 SFTP 服务器、完整 MCP 服务和所有新 UI 路径仍不应视为本轮端到端验收。

## 版本与部署

本次只合入并推送源码，不变更 `version.md`，不创建 tag，不构建/发布镜像，不重启或替换正在运行的 8097 服务。后续完整验证和发布应独立执行并记录对应代码指纹。

代码集成提交 `2054b04c46dddadf3808c367caa31624a6d722e8` 已 fast-forward 合入 `main` 并推送至 `origin/main`；推送后本地 `main` 与远端 SHA 一致。
