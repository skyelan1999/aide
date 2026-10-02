# 软件升级与 A/B 槽

## 适用范围

应用内 A/B 升级仅适用于通过正式 Release 镜像启动、并由随包宿主启动器管理的 aide。源码开发模式仍可检查公开 Release，但不显示 A/B 安装和切换控件；源码更新使用 Git 与常规开发启动流程。

升级服务负责访问令牌鉴权、发行包解析与校验、槽状态持久化和进度报告。它不访问 Docker socket，也不执行上传包中的脚本。宿主启动器仅在用户手动激活槽后导入镜像并重启服务。

## 安装方式

| 方式 | 输入 | 适用场景 |
| --- | --- | --- |
| 离线安装 | 完整发行 ZIP、应用内升级 ZIP，或 macOS 解压后的发行文件夹 | 网络受限、已提前下载发行包，或 Finder 自动解压 ZIP |
| 在线安装 | 设置页中选择官方 Release 版本 | 直接从 Release 下载到服务器，无需先把大文件下载到本机 |

两种方式都会把经过校验的包暂存到非活动槽，不会自动切换或重启当前工作台。完成后，用户可手动激活该槽。版本不要求严格递增，因此支持同版本重装和低版本回退；活动槽只有在手动切换成功后才会变化。

在线安装只接受公开 GitHub Release API 中列出的官方 GitHub 下载地址，并按当前 Linux 架构选择 `*-update-linux-<arch>.zip`；Release 缺少独立升级包时，使用 `*-full-linux-<arch>.zip`。服务端限制下载体积，校验 ZIP、manifest、Release tag、平台与镜像 SHA256，再写入 `/data/updates/packages`。容器需能访问 GitHub API 和 Release 下载服务。

## 发行包结构

`bash scripts/package-release-assets.sh <tag> <image> [output-dir]` 生成轻量平台启动 ZIP、镜像归档、兼容旧版的 `aide-<tag>-update-linux-<arch>.zip` 和统一完整包 `aide-<tag>-full-linux-<arch>.zip`。

统一完整 ZIP 可用于首次启动，也可用于应用内离线升级。根目录包含 `.aide-image`、macOS/Windows/Ubuntu 启动文件、Compose 配置、脚本、`workspace/` 与 `context/`；Docker 镜像和升级清单位于 `docker-images/`。升级三件套为：

- `manifest.json`：格式 `aide-update-package`、版本 1、Release tag、Docker 平台、镜像归档文件名和 image ID。
- `SHA256SUMS`：镜像归档的 SHA256。
- `aide-<tag>-linux-<arch>-image.tar.gz`：Docker 镜像归档。

离线选择完整 ZIP 时，服务只提取三件套并重新封装，不会把整个启动目录解压到服务端。选择 macOS 解压后的文件夹时，浏览器从目录树定位同一 `docker-images/` 下的三件套，只上传这三个文件，并忽略 Finder 元数据、启动器、工作区等其他内容。旧版升级 ZIP 和仅含三件套的目录仍兼容。外层 Release `SHA256SUMS` 记录发行附件及升级 ZIP。

## 版本与槽状态

槽 A/B 使用稳定镜像引用 `aide:slot-a`、`aide:slot-b`。新包始终写入非活动槽；同版本包会替换非活动槽内容，较低版本包可用于回退。服务器不比较版本大小来拒绝包，而是校验 manifest、镜像 ID、平台和 SHA256。

切换由用户在目标槽点击「手动激活」后开始。服务器创建待处理操作；宿主代理拉取受令牌保护的命令、复核 SHA256、导入镜像，并使用原 Compose project 重建唯一 aide 服务。新服务 `/healthz` 检查通过后确认切换；超时或失败时恢复旧 `.aide-image` 并启动原槽。

A/B 两槽使用同一 Compose project 和相同的 `aide-data`、`aide-home` named volumes，因此会话、设置、模型配置与容器 home 共用。`workspace/` 和 `context/` 仍由启动目录挂载。切换按停旧再启新执行，期间服务会短暂不可用，但不会有两个 aide 实例同时写数据。

## 进度与恢复

上传进度按浏览器实际发送字节显示。在线安装进度由服务端持久化在 `/data/updates/slots.json`，包括查找 Release、下载、校验和暂存阶段；下载阶段根据已收字节更新百分比。Docker 镜像导入和健康检查阶段由宿主代理报告里程碑进度，百分比不代表 Docker 实际导入字节数。

页面轮询失败时会继续等待服务恢复；重新打开或刷新设置页后，可从共享槽状态继续查看当前操作。若切换请求长期停留在 `requested`，页面会提示检查宿主启动脚本。在线下载完成后，槽显示「待手动激活」；下载失败会保留错误信息，用户可重新选择 Release 再试。

macOS 使用系统 Bash 3.2，因此宿主 shell 代理避免 Bash 4 才支持的 `${value,,}` / `${value^^}` 大小写转换。代理从当前运行容器的 Compose 标签发现项目名，确保 Finder 启动与升级重启使用相同 Compose project。启动器在已有 bundle 运行时沿用 `.env` 端口；仅首次启动且目标端口被占用时选择下一个可用端口。

## 安全与运行边界

- 更新 API 和宿主代理 API 均要求访问令牌。
- Docker 操作仅在用户宿主启动包中的代理内执行；容器不挂载 Docker socket。
- 页面关闭不会取消已提交的在线下载或切换。
- 源码模式不提供应用内安装；Release 镜像模式提供在线下载、离线上传、手动激活与健康失败回滚。
- 安装机的 Docker Engine 架构必须与镜像平台一致；不匹配的平台包会在校验时被拒绝。
