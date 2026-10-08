# aide 开发与运维入口

> **0.1.17.0 RC2 发行与交接入口**：[本轮发布记录](docs/reviews/release-0.1.17.0-RC2-2026-10-08.md) · [任务账本](docs/tasks/release-0171-20261008.json)。用户已授权“整理所有资料和文档并release”，并允许完整验收；当前是否完成标签、镜像与附件交付只查该记录的实际状态。生产运行实例、独立 Pages 与源码发行分别记账，旧候选收据不代替本轮门禁。

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
- 本轮发布授权为“整理所有资料和文档并release”。静态主页的 Pages 部署、发行附件上传及运行实例切换分别记录；提交主页源码不会自动启用 Pages。

## 回滚

代码回滚优先使用 `git revert <merge-or-fix-commit>` 并重新运行 quick/full；数据恢复使用独立卷和工作区副本先验证，确认后再切换。不要以回滚镜像覆盖唯一数据副本。

## 当前工作与证据导航

| 范围 | 当前说明与证据 | 交接重点 |
| --- | --- | --- |
| 本轮发行与当前验收 | [0.1.17.0 RC1发布记录](docs/reviews/release-0.1.17.0-RC1-2026-10-08.md) · [协议联调](docs/reviews/2026-10-08-release-integration.md) | 旧报告留存历史范围；当前门禁、镜像、远端附件与运行状态以本轮收据为准 |
| 审批、界面、直接进入星图与自动差量更新 | [源码交接记录](docs/reviews/2026-10-08-starmap-source-sync.md) · [知识星图架构](docs/architecture/knowledge-map.md) | 历史 Safari 观察只证明当时的直接进入及按钮移除；不能覆盖之后的多来源改动 |
| SSH 目录与生命周期修复、存储配置 | [SSH 检查报告](docs/reviews/2026-10-08-ssh-storage-audit.md) · [存储专题](docs/architecture/ssh-storage.md) | 双服务器隔离验收与用户服务器排错分开；只读断线重试一次，不重放写入 |
| 星图不同引用来源与文档查看／回填 | [多来源候选检查](docs/reviews/2026-10-08-starmap-sources.md) · [任务账本](docs/tasks/starmap-sources-20261008.json) | 来源身份随配置及凭据变化；重启后旧查看页重新打开；真实协议／模型验收看本轮收据 |
| 独立产品主页 | [主页说明](docs/website.md) · [任务账本](docs/tasks/homepage-20261008.json) | `site/` 静态展示，与工作台 API、发行包和运行部署分开；Pages 仅手动触发 |

候选启动沿用 `start.command`。先核对 `.env`、Compose 项目名、端口、镜像与数据卷，保留原实例；历史18189与9999的状态只是对应报告的当时观察。发布后再核对实际容器与鉴权 API，不能仅依靠页面角落版本。

本次资料整理不移动业务工作区、应用数据卷或原始验收日志。现行操作说明集中在用户指南与架构专题；带日期的报告保留当次源码、环境和失败记录，过期收据不得用于本轮发布门禁。

## 历史记录

- [旧版运维与交接档案（截至 2026-09-30）](docs/archive/HANDOVER-2026-09-30-legacy.md)
- [2026-09-26 RC3 交接记录](docs/archive/2026-09-26-HANDOFF-RC3.md)
- [版本记录](version.md) · [安装指南](docs/installation.md) · [用户指南](docs/user-guide.md)
- [0.1.14.0 RC5 发布记录与限制](docs/reviews/release-assets-2026-10-01.md) · [发布任务验收](docs/tasks/release-2026-10-01.json)
