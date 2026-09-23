# aide 开发与运维交接

更新：2026-09-23。本文是现行操作入口。历史测试结果保留在 [验证记录](docs/verification.md)，不能据此推定今天的运行服务状态。

## 接手时先确认什么

```bash
git status --short --branch
git log -3 --oneline
git remote -v
bash scripts/version.sh show
bash scripts/aide.sh status
```

本次交付目标 `v0.1.6.0-RC3`：新版 UI、主题、关于、Agent 开发路由、统一 docs，以及 Docker 镜像附件。aide 定位为 AI + IDE 专业工作台；开发 Agent 路由用于维护与场景扩展，见 [定制指南](docs/customization.md)。

发布只交付 GitHub 源码/tag/Release/镜像；不自动替换本机 8097 的生产服务。镜像导入启动见 [镜像说明](docker-images/README.md)。

开发从 [AGENTS.md](AGENTS.md) 开始，恢复 [任务记录](docs/tasks/agent-workflow-docs.json)。日常操作见 [使用指南](docs/user-guide.md)。

## 配置与运行

- Dockerfile：Go 1.26、Python 3.12、Node.js 24 工具链，Git/curl/bash，非 root 用户；精确版本由构建镜像决定。
- Compose：本机端口、cap_drop、no-new-privileges、2 CPU / 2 GiB / 256 PID。
- `/workspace` 可写；`/context` 只读；`/local` 默认为可写 HOME。`.env` 可以缩小本地根目录范围。
- `/data` 和 `/home/aide` 为命名卷；切换挂载目录不是迁移数据卷。
- API Key、访问令牌与会话可能包含私密数据，不进 Git，不贴到公开日志。
- Go embed 前端，修改源码要重建；只刷新页面不能换掉旧镜像。

`start.command` / `scripts/aide.sh start` 会构建并启动，不适合当作只读检查命令。状态与日志使用 status/logs。

## 检查代码与文档

```bash
python3 scripts/agent-route.py check
python3 scripts/agent-route.py verify quick
python3 scripts/agent-route.py verify full
python3 scripts/agent-route.py audit
```

full 在一次性 Docker 容器运行 race/vet，需要本地 `aide:local` 镜像与 Docker 引擎。浏览器检查与变化相关 API 验收另行执行并记录。脚本自身的回归通过不意味着所有客户端已自动加载路由。

## 发布

先完成文档工作流中的前置阶段、测试证据和发布授权记录。生产重建前结束任务并保存恢复点。不要自动混入其他人的未提交文件。

1. 审阅本次任务、diff、测试与未验证项；按授权合并到 main。
2. main 工作树干净后运行 full 与 `release-check <任务号>`。
3. 使用 `scripts/version.sh bump <档位> -m "说明"` 或 `patch`。它会提交版本文件并打 tag；只允许 main。
4. 构建时明确注入身份，避免默认 dev/unknown：

```bash
export AIDE_VERSION="$(bash scripts/version.sh show)"
export AIDE_COMMIT="$(git rev-parse HEAD)"
docker compose build aide
```

5. 保留旧镜像回滚标签和数据备份后，在授权维护窗口执行 `docker compose up -d --no-build`。
6. 检查 `/healthz`、认证后的 `/api/config` version/revision、登录、文件、任务与需要的界面。对应用于这一数据集的 `scripts/verify_runtime.py --check` 才能据其结果判定持久化；默认脚本会创建测试会话/文件，不能随意对业务目录运行。
7. 需要迁移时再 export；推送和 GitHub Release 各自记录实际状态。备份/构建/tag/健康检查都不能单独代表完整发布成功。

## 备份与恢复

源码 Git 历史、未提交改动、项目文件、镜像归档、配置及两个数据卷需要分别备份。Git bundle 不包含未提交文件；镜像不含卷。不要运行 `docker compose down -v` 来做普通升级。

下面是维护窗口参考命令，**不是本次已执行的动作**。执行前检查实际卷名称；不要覆盖已有备份。

```bash
umask 077
AIDE_BACKUP_DIR="$HOME/aide-backups/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$AIDE_BACKUP_DIR"
git bundle create "$AIDE_BACKUP_DIR/aide-source.bundle" --all
docker image tag aide:local aide:rollback
docker compose stop aide
docker run --rm --user 0 --entrypoint tar -v aide_aide-data:/source:ro aide:local -C /source -czf - . > "$AIDE_BACKUP_DIR/aide-data.tgz"
docker run --rm --user 0 --entrypoint tar -v aide_aide-home:/source:ro aide:local -C /source -czf - . > "$AIDE_BACKUP_DIR/aide-home.tgz"
docker compose start aide
shasum -a 256 "$AIDE_BACKUP_DIR"/*.tgz "$AIDE_BACKUP_DIR/aide-source.bundle"
```

确认 tar 可读取、校验和已保存；任一步失败都不能将半成品当恢复点，并及时恢复服务。业务文件与 `.env` 另做私有备份。

恢复时使用独立项目名、端口、空数据卷和工作区副本，保留 UID/GID 1000 可访问权限。检查会话、模型配置、费率、来源、文件与只读约束后再切正式入口。

回滚优先用旧镜像和配套备份，在隔离环境验证数据兼容；若恢复卷，不覆盖仅有的数据副本。镜像回滚不会撤销已经写入宿主工作区的文件。导入旧归档后应 `--no-build` 启动，不能紧接着重新构建新源码。

## 排障

| 现象 | 首要核对 |
| --- | --- |
| Docker 不可达 | Docker Desktop / docker info / CLI 路径 |
| 401 或重新要求登录 | 当前实例的本地令牌、浏览器存储、端口；不要使用 API Key 代替 |
| 模型已配置但失败 | Base URL、模型 ID、提供商权限/网络与实际错误 |
| 409 文件冲突 | 工作区身份、文件哈希、外部编辑；重新读取而非绕过保护 |
| 页面没有更新 | 实际镜像构建身份和资源内容，再排查缓存 |
| cd 不保留或 vim 卡住 | 非交互独立 shell；单条命令内组织工作目录 |
| 统计费用不同于账单 | 本地费率快照、估算/未计价、缓存折扣与上游结算 |
| 压缩后缺细节 | 回查原文件、历史任务和摘要；不把压缩当无损归档 |

## 交接输出

报告任务 ID、修改范围、代码/镜像身份、测试命令与证据、未验证范围、数据迁移、当前服务、发布和回滚状态。不要写“全部完成”掩盖未发布或未实测的步骤。
