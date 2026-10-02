# 软件升级与 A/B 槽

设置中的「软件升级」可检查公开 aide Release、上传完整发行包并切换 A/B 软件槽。容器应用负责访问令牌鉴权、ZIP 结构/manifest/平台/版本/SHA256 校验以及将包和槽状态写入 `/data/updates`。它不会访问 Docker socket，也不会执行上传内容。统一完整包同时包含首次启动所需的宿主启动文件和 Linux 镜像、以及 A/B 升级所需的校验清单；轻量平台启动 ZIP仍只用于联网安装，不能上传到 A/B 槽。

运行方式由独立构建标记区分。普通源码/开发镜像默认 `AIDE_RUNTIME_MODE=source`，即使构建时注入正式版本号也仍按源码运行：可以检查公开 Release，但不显示 A/B 上传/切换控件，上传、切换及宿主代理的轮询、同步、结果回报和包下载 API 也都会拒绝请求；更新源码后按开发流程重新启动即可，不需要应用内升级。正式 Release 构建脚本显式传入 `AIDE_RUNTIME_MODE=release-image` 后，镜像才启用 A/B 槽、升级包暂存、手动激活和健康失败回滚。

## 升级包

`bash scripts/package-release-assets.sh <tag> <image> [output-dir]` 生成轻量平台启动 ZIP、镜像归档、兼容旧版的 `aide-<tag>-update-linux-<arch>.zip`，以及推荐的统一完整包 `aide-<tag>-full-linux-<arch>.zip`。统一完整包包含 macOS、Windows、Ubuntu 启动文件和一个平台匹配的 Docker 镜像；启动器校验后可离线导入并启动。升级页面从该完整 ZIP 或 macOS 解压后的目录中定位镜像升级三件套：

- `manifest.json`：格式 `aide-update-package`、版本 1、Release tag、Docker 平台、镜像归档文件名和 image ID。
- `SHA256SUMS`：镜像归档 SHA256。
- `aide-<tag>-linux-<arch>-image.tar.gz`：Docker 镜像归档。

统一完整 ZIP 根目录含 `.aide-image`、启动器、Compose 配置、脚本、`workspace/`、`context/`；升级三件套位于 `docker-images/`。服务器上传 ZIP 时只提取并重新封装该三件套，再进行严格校验；不会把完整运行目录解压到服务端磁盘。`SHA256SUMS` 校验镜像归档，外层 Release `SHA256SUMS` 同时记录统一包和其他发布附件。保留独立升级 ZIP 是为了兼容旧版本安装；新用户和升级用户优先使用统一完整包。

服务器拒绝多余/重复文件、未知平台、非递增版本、超限或校验不匹配的包。发布资产还会在外层 `SHA256SUMS` 中记录升级 ZIP。离线安装可以在发布流水线结束后下载该 ZIP，再通过本页上传。

## 安装和切换

1. Release 镜像运行时，打开「设置 → 软件升级」，选择同一 Release 的 `*-full-linux-*.zip`。若 macOS 已自动解压，选择完整发行包文件夹；文件选择器会从目录树定位同一 `docker-images/` 中的 `manifest.json`、`SHA256SUMS` 和镜像归档，只上传这三个必需文件，并忽略 Finder 元数据和启动器、工作区等其他内容。也兼容旧版 `*-update-linux-*.zip` 与仅含升级三件套的文件夹。服务端仍重新封装并执行完整校验。上传进度按浏览器实际发送字节显示；服务器完成校验后，包暂存到非活动槽并显示「待手动激活」，不会自动重启或切换当前工作台。
2. 用户点击非活动槽上的「手动激活槽」并确认后，服务器写入待处理操作；随 launcher 分发的 macOS/Ubuntu shell 或 Windows PowerShell 宿主代理轮询受令牌保护的 API。未点击前，当前版本继续运行。
3. 宿主代理下载已验证包、复核 SHA256、导入 Docker 镜像并标记目标槽，然后用原 Compose project 重建唯一的 aide 服务。
4. 新服务的 `/healthz` 在期限内成功后，代理确认切换；否则恢复旧 `.aide-image` 并启动原槽，再记录失败/回滚。

切换期间，宿主代理会向受令牌保护的进度 API 报告准备、下载、校验、导入镜像、切换/重启、健康检查与回滚阶段。设置页展示当前阶段和进度百分比；这些阶段百分比是操作里程碑，Docker 镜像导入阶段并非 Docker 实际字节计数。服务短暂重启时，页面轮询失败会继续等待，重新连接后可从共享槽状态恢复进度。若请求长时间仍为 `requested`，页面会提示启动器尚未响应，并提醒检查本机启动脚本。已安装镜像的手动切换不需要下载和导入，进度会直接进入切换阶段。

宿主代理命令协议对可选的 package ID 与镜像 SHA256 使用占位值，并且 Bash 侧采用非空白分隔符解析。Bash 会折叠连续制表符；若依赖空字段，已安装镜像的手动切换会导致后续字段错位，代理便会忽略命令而留下永久的等待状态。

macOS 使用系统 Bash 3.2，因此宿主 shell 代理避免 Bash 4 才支持的 `${value,,}` / `${value^^}` 大小写转换。代理从当前运行容器的 Compose 标签发现项目名，确保 Finder 启动、升级重启与显式 Compose 项目一致。`start-bundle` 检测到本 bundle 已在运行时沿用 `.env` 端口；只有首次启动且目标端口被其他程序占用时才选择下一个可用端口。这样切换槽不会把同一工作台误判成端口冲突，也不会悄悄更改访问地址。

槽 A/B 使用稳定镜像引用 `aide:slot-a` 与 `aide:slot-b`。`docker/compose.offline.yaml` 固定同一 Compose project，并使用相同的 `aide-data` 和 `aide-home` named volumes；会话、设置、模型配置与容器 home 共用。`workspace/`、`context/` 仍由启动目录挂载。切换采用停旧再启新，任何时刻只允许一个 aide 实例写数据；切换期间页面可能短暂断开。两个镜像槽保留各自版本，失败可回滚，也可在升级页面主动切回旧槽。

## 运行边界

- 更新 API 与宿主代理 API 均要求访问令牌。
- Docker 操作仅在用户宿主启动包里的代理中运行；不挂载 Docker socket 到 aide 容器。
- 页面关闭不会中止已提交的切换；启动代理在宿主后台轮询。
- 服务重启后代理读取共享 `/data/updates/slots.json` 恢复槽状态；本地 `.aide-image` 记录当前启动槽。
- 无待处理切换时，启动器同步会更新活动槽并清除过期的切换错误文案；存在待处理操作时保留该操作，等待代理回报结果。
- 切换操作的当前阶段、百分比和提示会持久化到 `/data/updates/slots.json`，供设置页刷新或服务恢复后继续显示。
- 检查公开 Release 需要联网；包上传和镜像导入可离线执行。
- 当前 Release builder 对应当前 Docker Linux 镜像架构（arm64 或 amd64）。安装机的 Docker engine 架构必须匹配；不兼容平台包会在上传时被拒绝。
