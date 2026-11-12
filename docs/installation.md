# 安装、升级与首次启动

[v0.1.19.0-RC2 预发布](https://github.com/skyelan1999/aide/releases/tag/v0.1.19.0-RC2)已于 2026-11-12 公开，发行源码为 `a9f8f412`。安装前按该 Release 核对实际架构、SHA-256 和 `RELEASE-VALIDATION.json`；[发布回执](reviews/2026-11-12-release-preparation/publication-rc2.json)记录验证范围。不要把后续 main 的功能视为该包或旧包已有功能。

## 选择安装方式

| 方式 | 适用场景 | 入口 |
| --- | --- | --- |
| 完整发行包 `aide-<tag>-full-linux-arm64.zip` | Docker 引擎为 linux/arm64，包括 Apple Silicon | 校验、解压后使用包内 start 入口 |
| 完整发行包 `aide-<tag>-full-linux-amd64.zip` | Docker 引擎为 linux/amd64，包括常见 x64 Windows/Linux、Intel Mac | 同上；不能使用另一架构的镜像 |
| 可选开发工具包 `aide-<tag>-devtools-linux-<arch>.zip` | 需要编译器和开发依赖 | 单独导入 SDK；日常运行无需安装 |
| 源码构建 | 修改程序或跟随 main | 环境检查后显式 build |

包名中的架构指 **Linux Docker 引擎架构**，不是浏览器架构。完整包包含各宿主的启动文件及对应镜像；仅使用同一 Release 实际存在的附件。历史轻量启动包不包含镜像，首次启动需下载；不能将其作为完整 A/B 升级包。

## 宿主环境

macOS 使用 Docker Desktop；Windows 使用 Docker Desktop 的 Linux 容器引擎，x64 机器通常还需 WSL2；Linux 使用 Docker Engine、Compose 和 Buildx。源码构建另需 Git。宿主 Python 3 用于脚本和校验；日常运行不要求宿主 Go、Node 或 npm。开发与发布检查另需对应工具链。

安装来源：[Docker Desktop](https://docs.docker.com/desktop/)、[Docker Engine](https://docs.docker.com/engine/install/)、[Git](https://git-scm.com/downloads)、[Python](https://www.python.org/downloads/)。Docker Desktop 的组织许可由使用者确认。Aide 使用 MIT，第三方组件保留其许可。

## 使用完整发行包

1. 从同一 Release 下载匹配引擎架构的完整 ZIP 和 `SHA256SUMS`，核对文件名与摘要。不要混合不同 tag 的启动器、镜像和元数据。
2. 解压到自己的安装目录。macOS 可运行 `start.command`，Windows 使用 `aide-launcher.exe` / `start.bat`，Linux 使用 `start.sh`。
3. 启动器校验并导入包内镜像，检查架构、镜像身份和健康状态。安装与启动不调用付费模型。
4. 设置提供商、API Key、模型和项目后开始使用。浏览器与原生桌面窗口可共用同一后台，不必为每个窗口启动容器。

```bash
# 在下载目录执行，SHA256SUMS 必须来自同一 Release
shasum -a 256 -c SHA256SUMS
```

只有下载了清单全部文件时，全清单校验才会全部成功；按需下载者可对所选文件逐项核对。Windows 可用 PowerShell `Get-FileHash -Algorithm SHA256` 核对相应条目。SHA-256 是完整性摘要，不是独立发布者签名。

## 源码安装与更新

```bash
git clone https://github.com/skyelan1999/aide.git
cd aide
# 需要可重复的发行源码时，检出已公开 Release 的精确 tag
bash scripts/install.sh --check
bash scripts/install.sh --source
```

后续操作：

| 操作 | 命令 | 行为 |
| --- | --- | --- |
| 日常启动 | `bash scripts/aide.sh start` | 复用健康实例或启动已安装镜像，不编译源码 |
| 应用源码修改 | `bash scripts/aide.sh build` | 显式构建、校验成功后启动，不静默退回旧产物 |
| 强制冷构建 | `bash scripts/aide.sh clean-build` | 按启动器契约处理本安装的构建缓存，不删除用户数据 |
| 状态 / 日志 | `bash scripts/aide.sh status` / `logs` | 查询本安装 |
| 停止 | `bash scripts/aide.sh stop` | 停止本安装组件，保留配置和数据 |

对应的 `start.*`、`build.*`、`stop.*` 入口具有相同职责。刷新页面不会更新嵌入二进制的前端。启动接口和清理精确范围见[启动器契约](architecture/launcher-contract.md)。

## 配置项目与目录

`.env` 决定源码安装的端口、Compose 组合和已有挂载；默认 HTTPS 端口为 `8097`。发行启动器保存其实际端口，端口冲突按该启动器策略处理。不要停止无关服务解决冲突。

```dotenv
AIDE_PORT=8097
AIDE_WORKSPACE=/absolute/path/to/project
AIDE_CONTEXT=/absolute/path/to/reference
AIDE_LOCAL_ROOT=/absolute/path/to/projects
```

路径须存在；系统权限和 Docker 文件共享继续生效。应用内切换工作区只选择已授权范围。设置 → 文件与知识 → 本地目录可登记额外目录，保存后通过本机确认应用并重启。新启动器持有实际目录对象，经宿主文件桥接提供 `/mounts/<id>`，防止准备后同名路径被替换成其他目录；更换磁盘或目录身份后需移除并重新选择。须配套更新原生启动器与助手；详情与数据库并发限制见[工作目录](workspace-paths.md)和[桥接契约](architecture/launcher-contract.md#selected-directory-object-bridge-773)。

SSH 工作区与引用分别配置连接身份。引用需启用并允许当前工作区 AI 访问，写入还需 RW。插件自己连接的设备 SSH 需要独立凭据，不继承项目权限。

## 登录与模型

首次启动在数据目录生成本地 HTTPS 证书。仅在确认是自己这次启动的 localhost 服务后处理自签证书提示。Touch ID / WebAuthn 使用 `localhost`，不要用 IP 替代 RP ID。无桌面时按启动器输出的本机登录方式操作；令牌及带令牌 URL 不应分享。详见 [TLS](security/tls.md)。

配置 Base URL、命名密钥、模型及支持的 API 协议；先用非敏感内容验证。安装成功不证明模型、麦克风、浏览器控制或客户设备已配置好。云端模型会收到实际发送的上下文、工具结果和图片。

## 升级与插件

升级前备份 `.env`、数据目录和配置，并保留旧完整包与镜像。发行安装在设置 → 软件更新使用同架构完整包，遵守现有 A/B 健康检查和回退。源码安装通过更新源码后显式 build，不启用发行包自动升级代理。具体见[软件更新](architecture/software-updates.md)和[运维交接](../HANDOVER.md)。

公共插件独立发行，主程序升级不自动替换已安装插件。SQLite 1.0.3、StarLink 0.1.6 的市场最低宿主为 Aide `0.1.19.0 RC1`；先更新宿主，再从市场升级插件。旧程序不能通过只换插件获得新宿主能力。SQLite 远端操作要求外部写入停止并完成 WAL checkpoint。

## 离线运行与源码构建

完整包包含镜像，可在目标离线机器校验、导入并 start；已安装镜像的日常 start 不构建、不拉取。不要在离线目标执行 build。离线模型应使用内网/本地提供商；在线搜索、市场刷新及尚未下载的外置语音/插件引擎仍需对应网络或预先准备。运行包自带所需应用资源，开发 SDK 单独分发。

离线源码构建是另一件事：需要预先准备基础镜像、全部构建来源和相应平台 wheelhouse。`scripts/prebuild-wheels.sh --target-platform linux/arm64`（或 amd64）和 `PIP_OFFLINE=1` 只解决 Python wheel 输入，不代表所有构建依赖已准备。见[运行包与离线输入](runtime-bundle.md)。

## 故障恢复

| 现象 | 处理 |
| --- | --- |
| Docker 未就绪 | 先检查引擎，再运行环境检查 |
| 缺镜像 / 架构不匹配 | 核对同 tag 包及引擎架构；源码安装先 build |
| 摘要不符 | 停止使用该文件，重新获取同版本附件，不覆盖旧有效包 |
| 自选目录不可用或身份改变 | 确认磁盘及本机权限，必要时移除后重新选择，不创建空目录替代 |
| 桥接 I/O 错误 | 确认助手和磁盘状态，重新读取确认保存结果；不要假定已写入 |
| 会话或插件丢失 | 核对安装目录、Compose 项目和数据挂载，不新建卷覆盖恢复路径 |
| 仍显示旧功能 | 核对运行版本/commit，源码改动须 build，插件须独立升级 |

不使用 `docker compose down -v` 或全局 prune 作为常规恢复。保护用户工作区、会话、插件、凭据和旧镜像。完整发行检查和未验证设备范围见[Release 验收](testing/release-validation.md)。
