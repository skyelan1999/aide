# 安装与首次启动

> **启动配置以 `.env` 为准**：`start.command` → `scripts/aide.sh` → Docker Compose，统一读取 `AIDE_PORT`（默认 8097）和 `COMPOSE_FILE`。临时验收端口不是用户启动入口。目录范围和 macOS 共享根模式见 [工作目录配置](workspace-paths.md)。

aide 融合 AI 与 IDE，让你在本地更专注地处理专业任务。可直接使用发行版，需要扩展时再按 [定制指南](customization.md) 修改源码。安装脚本负责环境检查、首次配置、校验镜像和启动应用；不会静默安装收费软件、请求 sudo、覆盖已有 .env 或删除数据卷。

## 1. 安装宿主环境

| 平台 | 前置环境 | 注意 |
| --- | --- | --- |
| macOS Apple Silicon | Docker Desktop、Git；镜像校验需要 Python 3 | 打开 Docker Desktop，等待引擎就绪；本次镜像为 linux/arm64 |
| Linux ARM64 | Docker Engine、Compose v2、Git、Python 3 | 当前用户须有访问 Docker 引擎的权限 |
| Linux/macOS x86 | 同上 | 使用源码构建；不要直接使用 ARM 归档 |
| Windows | WSL2 Linux 环境与 Docker Desktop WSL 集成、Git、Python 3 | 在 WSL 的 Bash 中运行；本轮未在 Windows 实机验证 |

安装入口：[Docker Desktop](https://docs.docker.com/desktop/) / [Docker Engine](https://docs.docker.com/engine/install/) / [Git](https://git-scm.com/downloads) / [Python](https://www.python.org/downloads/)。Docker Desktop 的适用许可由使用者按其组织情况确认；aide 自身使用 MIT 许可证。不需要在宿主机安装 Go；日常运行无需 npm。开发路由检查另外需要 Node.js 与 Python 3。

## 2. 下载同一版本的源码

```bash
git clone https://github.com/skyelan1999/aide.git
cd aide
git checkout v0.1.6.0-RC4
bash scripts/install.sh --check
```

也可解压 Release 源码 ZIP 后进入根目录。镜像安装脚本需要同版源码中的 Compose、version.md 和 scripts；Docker 归档本身不是双击安装程序。

## 3A. 发行镜像安装（Apple Silicon / ARM64）

从 [Release](https://github.com/skyelan1999/aide/releases) 下载镜像和 SHA256SUMS，放在同一目录，例如 aide/docker-images/：

```bash
bash scripts/install.sh --image docker-images/aide-0.1.6.0-RC4-linux-arm64.tar.gz
```

脚本先检查校验和，再导入镜像并检查架构，最后使用 `--no-build --pull never` 启动。版本不匹配、校验失败或架构不匹配会停止，不会偷偷重建。

## 3B. 源码安装（含 x86）

```bash
bash scripts/install.sh --source
```

首次构建需联网下载基础镜像。脚本创建 context/，仅在 .env 不存在时复制模板，并把本地浏览范围限制到仓库目录。镜像内包含应用与工具链，宿主目录通过挂载提供。

## 4. 配置自己的项目

首次默认使用源码目录作为工作区。如需其他目录，修改 .env 后重新启动；路径必须存在，推荐绝对路径：

```dotenv
AIDE_IMAGE=aide:0.1.6.0-RC4
AIDE_PORT=8097
AIDE_WORKSPACE=/absolute/path/to/project
AIDE_CONTEXT=/absolute/path/to/reference
AIDE_LOCAL_ROOT=/absolute/path/to/projects
```

发行镜像后续运行用 `bash scripts/aide.sh start-image`；自定义源码改动后将 AIDE_IMAGE 设为自己的镜像名，用 `bash scripts/aide.sh start` 重建。不要使用 Docker 命令直接覆盖当前业务目录；升级前备份数据卷。

## 5. 登录与连接模型

macOS 脚本自动打开带本地登录令牌的浏览器；Linux 有 xdg-open 时同样处理。无桌面时访问 http://127.0.0.1:8097，在本机终端运行 `docker compose exec -T aide cat /data/access-token`，复制令牌到登录框。不要把令牌或带令牌的 URL 分享出去。

然后在模型设置填写 Base URL、API Key 和模型 ID。安装不会调用模型；模型费用取决于提供商。先用非敏感问题测试连接，再打开实际资料。

## 常见问题

| 现象 | 处理 |
| --- | --- |
| docker 不存在 / 引擎未就绪 | 安装并启动 Docker，重新运行 --check |
| 端口 8097 被占用 | 修改 AIDE_PORT，不要停掉不属于本项目的服务 |
| bind source path 不存在 | 检查 .env 目录；已有 .env 不会被脚本自动纠正 |
| 镜像标签找不到 | 使用同版源码/归档，确认 docker load 成功及 AIDE_IMAGE |
| checksum mismatch | 停止使用该归档，重新下载同版文件和 SHA256SUMS |
| 版本显示 dev/unknown | 使用正式附件或在 Git 仓库内运行 start 注入版本/SHA |
| 连接模型失败 | 检查提供商配置；安装成功不等于模型已连通 |
| 重启后没有旧会话 | 检查 Compose 项目名及 /data 卷，不要创建新卷代替恢复 |

停止：`bash scripts/aide.sh stop`。查看状态：`bash scripts/aide.sh status`。日志：`bash scripts/aide.sh logs`。不要使用 `docker compose down -v` 清除数据。备份与回滚见 [交接手册](../HANDOVER.md)。

### 配置可浏览的本地目录

工作空间配置中的「浏览」只能访问挂载到 `/local` 的宿主目录。在 aide 仓库根目录执行：

```bash
# 将当前 aide 仓库设为本地浏览根目录
python3 scripts/configure-local-root.py
# 或指定其他已存在的目录（含空格时加引号）
python3 scripts/configure-local-root.py --path "/absolute/path/to/projects"
```

脚本更新 `.env` 的 `AIDE_LOCAL_ROOT` 和 `COMPOSE_FILE`，保留其他配置，不自动重启服务。确认当前任务结束后，执行 `docker compose up -d --no-build --pull never` 重建容器以应用挂载；服务会短暂中断，命名数据卷保留。使用「工作空间配置 → 本机路径 → 浏览」选择目录并保存。浏览器显示并回填电脑上的真实路径；不能通过「上一级」越过配置的浏览根目录；macOS 要从 `/` 浏览已共享目录，运行 `python3 scripts/configure-local-root.py --path /` 后照常启动。应用内切换目录只能选择已挂载范围内的位置，扩大范围须重新配置并重建容器。
