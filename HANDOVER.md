# aide 开发与运维入口

> **当前源码基线**：`v0.1.14.0-RC2`，tag 对应的 `main` 提交为本次发布记录所列版本提交。此版本只标记源码；本次未创建 GitHub Release、未发布镜像附件、未部署生产。运行实例身份须查看 `/api/config` 的版本与 revision。

## 快速接手

```bash
git status --short --branch
git log -5 --oneline --decorate
git remote -v
bash scripts/version.sh show
bash scripts/aide.sh status
```

先读 [Agent 工作流](docs/agent/WORKFLOW.md)、[路由配置](docs/agent/router.json) 和相关 [任务验收记录](docs/tasks/)。所有现行产品说明从 [文档中心](docs/README.md) 进入；旧交接与方案放在 [历史档案](docs/archive/)。

## 本地运行与诊断

- 常规启动：`./start.command` 或 `bash scripts/aide.sh start`。这会构建/启动 Compose 服务，使用前确认工作树和目标实例。
- 只读状态：`bash scripts/aide.sh status`；日志：`bash scripts/aide.sh logs`。
- 默认端口 `8097`，由 `.env` 的 `AIDE_PORT` 控制。生产或用户正在使用的服务不得作为测试实例；在临时实例上验证。
- Go 服务嵌入前端。改动后必须重建目标测试镜像；仅刷新页面不会更新旧二进制。
- `/workspace` 是工作目录，`/context` 为只读参考资料，`/data` 保存会话、凭据和配置。不要将密钥、访问令牌或业务数据写入 Git。

## 验证入口

```bash
python3 scripts/agent-route.py check
python3 scripts/agent-route.py verify quick
python3 scripts/agent-route.py verify full
python3 scripts/agent-route.py audit
```

`full` 包含 `bash scripts/aide.sh test`，在 Docker 中运行 `go test -race -count=1 ./...` 与 `go vet ./...`。涉及 UI 的改动还要在浏览器实际检查；组件脚本通过不等于 UI 验收通过。验证记录位于 [docs/verification.md](docs/verification.md) 和相应任务 JSON。

## 数据保护与发布边界

- Git 源码、未提交改动、工作区、`/data`、模型配置与镜像归档是不同的数据，需要分别备份。
- 不运行 `git clean -fdx`、`docker compose down -v`，不覆盖唯一数据副本。
- 合并、推送、版本升级、tag、GitHub Release、镜像发布、生产重建与服务重启分别记录；只执行用户明确授权的步骤。
- 本次用户授权按发布工作流升 RC、创建源码 tag 并推送 `main` 与 tag；GitHub Release、镜像附件与生产部署仍为独立动作，本次不包含。

## 回滚

代码回滚优先使用 `git revert <merge-or-fix-commit>` 并重新运行 quick/full；数据恢复使用独立卷和工作区副本先验证，确认后再切换。不要以回滚镜像覆盖唯一数据副本。

## 历史记录

- [旧版运维与交接档案（截至 2026-09-30）](docs/archive/HANDOVER-2026-09-30-legacy.md)
- [2026-09-26 RC3 交接记录](docs/archive/2026-09-26-HANDOFF-RC3.md)
- [版本记录](version.md) · [安装指南](docs/installation.md) · [用户指南](docs/user-guide.md)
- [0.1.14.0 RC2 发布记录与限制](docs/reviews/2026-10-01-v0.1.14.0-RC2.md) · [发布任务验收](docs/tasks/release-2026-10-01.json)
