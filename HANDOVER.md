# Aide 开发与运维交接

维护日期：2026-11-12；[v0.1.19.0-RC2 预发布](https://github.com/skyelan1999/aide/releases/tag/v0.1.19.0-RC2)已公开，发行源码为 `a9f8f412`，七个附件与完整门禁见[发布回执](docs/reviews/2026-11-12-release-preparation/publication-rc2.json)。SQLite 1.0.3、StarLink 0.1.6 及市场登记已更新。本次未替换维护者的生产服务或已安装插件；主干、安装包与正在运行的实例分别核对，升级保留旧包及数据备份。

## 接手顺序

在实际 Aide 源码目录进行只读检查：

```bash
git status --short --branch
git log -5 --oneline --decorate
git remote -v
bash scripts/version.sh show
bash scripts/aide.sh status
```

先保留未提交改动，确认 `.env` 的端口、Compose 项目名、工作目录、数据卷和当前运行身份，再执行构建或切换。[文档中心](docs/README.md)提供产品入口；[技术白皮书](docs/architecture/aide-technical-whitepaper.md)解释执行原理；维护流程见 [Agent 工作流](docs/agent/WORKFLOW.md)。

## 启动和应用源码

| 操作 | 行为 |
| --- | --- |
| `./start.command` / `bash scripts/aide.sh start` | 复用或启动已安装镜像；健康服务直接打开，不因 Git 变化自动编译 |
| `./build.command` / `bash scripts/aide.sh build` | 从源码构建，校验后启动相应产物；Windows 入口为 `build.bat` |
| `bash scripts/aide.sh status` / `logs` | 查询本安装的状态和日志 |
| `clean` / `docker_clean` | 不同的缓存、镜像与环境维护操作；执行前核对[精确契约](docs/architecture/launcher-contract.md)和[数据边界](docs/docker-maintenance.md) |

默认端口为 `8097`，由 `.env` 中 `AIDE_PORT` 决定；报告中的 `9999` 等端口只属于对应实例。前端经 `go:embed` 编入 Go 服务；只刷新页面不能把新源码装入旧二进制。

本地验收优先遵守用户对当前实例的约定。日常操作保留现有生产 Aide；本次已授权的发布使用专用临时目录、容器和 CI 夹具，不覆盖生产数据。其他确需隔离实例的测试不能悄悄启动，应说明依赖和未执行范围。不要为普通文档更新重建服务。

## 验证与结果记录

```bash
python3 scripts/check_docs.py
python3 scripts/agent-route.py check
python3 scripts/agent-route.py verify quick
```

按改动选择验证，不把所有命令当作每次必须运行的清单。文档修改先检查链接和 diff；代码修改按模块运行聚焦测试，界面改动再验收实际本地页面。`verify full` 和 `bash scripts/aide.sh test` 涉及更完整的 Go/race/vet 及测试环境，先读[Release 验收](docs/testing/release-validation.md)并确认资源条件；不要在本次约定下另起实例。

报告写明提交/脏树、测试命令与结果、运行身份、界面操作范围及未验证项。源码检查、单元测试、服务健康、浏览器操作、真机/远端测试与发行交付各有范围。历史输出只证明原报告对应环境。

## 数据与恢复

- `/workspace` 是项目，`/context` 通常为只读参考，`/data` 保存会话、配置、凭据等状态；宿主映射以实际配置为准。[目录说明](docs/workspace-paths.md)。
- Git 源码、未提交文件、项目数据、数据卷、设置密钥、镜像与离线包分别备份。不要把业务数据或访问令牌写入仓库。
- 不用 `git clean -fdx`、删除数据卷或覆盖唯一副本来清理验收环境。
- 代码恢复优先保留现场后 `git revert`；镜像回退不等于数据库或文件回退。数据恢复先检查备份内容，避免用旧镜像覆盖唯一数据。
- 文件提案、逐文件替换和外部命令并非整组跨资源事务；部分执行后以实际回执及文件读回确定状态。

## 发布与交接

合并、推送、版本声明、构建、tag、Release、镜像分发、市场条目、运行实例重建分别记录。已有程序 Release 不会自动发布新写的论文；推送文档也不代表重新构建安装包。

本次仅维护 Aide 文档，不修改插件、市场、运行实例或发行版本。后续发布时记录对应 Git 提交、公开时间、附件摘要及文档版本；历史记录见 [releases](docs/releases/) 和 [reviews](docs/reviews/)。

## 常用专题

[执行策略](docs/architecture/execution-policy.md) · [上下文恢复](docs/architecture/context-recovery.md) · [知识星图](docs/architecture/knowledge-map.md) · [插件协议](docs/plugin-protocol.md) · [电脑与浏览器控制](docs/plugins/computer-browser-control.md) · [语音](docs/architecture/assistant-openai-speech.md) · [SSH 与存储](docs/architecture/ssh-storage.md) · [诊断](docs/architecture/diagnostics.md)

旧版交接保留在 [archive](docs/archive/)；[2026-10-17 截图](docs/ui-evidence.md)不是当前运行环境截图，不用它判断今天的界面或发布状态。


[0.1.19 使用与迁移](docs/release-guide-0.1.19.md)。
