# 2026-09-29 整合与发布验收记录

任务：`consolidate-release-20260929`；客户端：Codex desktop。用户授权整理文档、文件、升版并进入 main；不包含替换生产 8097 服务或创建 GitHub Release。

## 文件归属与范围

| 范围 | 源码与测试 | 文档/任务 |
| --- | --- | --- |
| 文件多选与目录间复制/移动 | `internal/server/file_transfer*`、`server.go`、`ssh_session.go`、`web/app.js`、`web/style.css` | `docs/user-guide.md`、`docs/tasks/file-multiselect-transfer.json` |
| Office 生成、XLSX 编辑、DOCX 原生批注 | `internal/server/office_*`、`plugins/office/`、`scripts/office/`、插件注册表及前端 | `docs/plugins/office.md`、`docs/architecture/office-viewer.md`、两份 Office 任务记录 |
| 小秘控件、点击延迟与 SSH 工具预算 | 前端和 `workflow.go`、相应 Go/JS 回归 | `docs/tasks/assistant-mode-controls.json`、`ui-click-latency.json`、`ssh-tool-budget.json` |
| 发布门禁修复 | `assistant_history_test.go`、`memory_access_test.go`、`workflow.go`、`scripts/agent-route.py`、`scripts/aide.sh` | 本记录及发布任务账本 |

现有 `docs/reviews/` 中两处指向未归档日志的坏链接已改成明确的“未归档”说明，未伪造原始证据。受控源码、插件、测试和文档均按现有目录职责归类；隔离预览和收据留在忽略的 `.agent-state/`，不进版本库。没有删除未知未跟踪文件或用户数据。

## 实测与边界

- `verify quick`：2026-09-29 在一次性 `aide:local` 容器内通过；收据 `.agent-state/quick.json`。Windows 宿主直接运行会误调用 WSL bash/Store Python，增加 `AIDE_BASH`/`AIDE_PYTHON3` 显式解释器入口；容器内 `AIDE_VERIFY_IN_CONTAINER=1` 让 full 复用当前隔离容器而不嵌套 Docker。
- `go test -race -mod=vendor -count=1 ./... && go vet -mod=vendor ./...`：在 `aide:local` 一次性容器内通过，服务端 race 用时 522.725 秒。随后启动的官方 `verify full` 重跑依用户“快速合入，让后续测试去做”的新要求提前停止，不能作为通过收据。
- 实际打开隔离 HTTP 预览 `http://localhost:8098/`（仅本机绑定，测试目录），验证工作目录 Shift 多选、右键“复制到… (2)”及目标目录两份文件可见；“移动到…”入口可见，跨目录移动由 Go 测试覆盖，未在 UI 中移动真实文件。
- 同一隔离预览中，XLSX 查看器显示工作表及单元格；修改 B2、点击保存后，从磁盘重新读取为 `final`。DOCX 正文预览已打开；批注按钮在窄视口的点击行为尚未完成可靠验收，WPS 打开/回写和真实 SSH/SFTP 端到端均未验证。
- 定向 Go 回归修复了 4 项原有全量失败：小秘文字请求失败的现行转交语义、真实工作区切换后的记忆映射、上下文压缩余量。没有把旧失败改成无条件跳过。

## 风险、发布与回滚

这些证据仅覆盖本地测试实例；不等于生产已替换。按用户最新指令，本轮先合入/推送 main，测试和发布后续继续。正式发布必须先在干净 main 上取得当前内容指纹的 full 收据并运行 `release-check consolidate-release-20260929`，然后由 `scripts/version.sh` 升版与打 tag。远端推送、构建镜像和生产部署分别记录；不可相互代替。

若合入后需撤回，使用新的 revert/hotfix 提交和新版本修复，不重写或移动已发布 tag，也不清空用户工作目录、Office 文件或会话缓存。测试预览服务可直接停止；生产 8097 在本任务中未重启。
