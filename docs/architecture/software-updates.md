# 软件升级与 A/B 槽

设置中的「软件升级」可检查公开 aide Release、上传完整升级包并切换 A/B 软件槽。容器应用负责访问令牌鉴权、ZIP 结构/manifest/平台/版本/SHA256 校验以及将包和槽状态写入 `/data/updates`。它不会访问 Docker socket，也不会执行上传内容。页面区分“应用内升级包”和“启动器下载”；macOS/Windows/Ubuntu 启动 ZIP 只用于安装启动器，不能上传到 A/B 槽。

运行方式由独立构建标记区分。普通源码/开发镜像默认 `AIDE_RUNTIME_MODE=source`，即使构建时注入正式版本号也仍按源码运行：可以检查公开 Release，但不显示 A/B 上传/切换控件，上传、切换及宿主代理的轮询、同步、结果回报和包下载 API 也都会拒绝请求；更新源码后按开发流程重新启动即可，不需要应用内升级。正式 Release 构建脚本显式传入 `AIDE_RUNTIME_MODE=release-image` 后，镜像才启用 A/B 槽、升级包暂存、手动激活和健康失败回滚。

## 升级包

`bash scripts/package-release-assets.sh <tag> <image> [output-dir]` 除平台启动 ZIP 与镜像归档外，还生成 `aide-<tag>-update-linux-<arch>.zip`。升级包严格包含三个文件：

- `manifest.json`：格式 `aide-update-package`、版本 1、Release tag、Docker 平台、镜像归档文件名和 image ID。
- `SHA256SUMS`：镜像归档 SHA256。
- `aide-<tag>-linux-<arch>-image.tar.gz`：Docker 镜像归档。

服务器拒绝多余/重复文件、未知平台、非递增版本、超限或校验不匹配的包。发布资产还会在外层 `SHA256SUMS` 中记录升级 ZIP。离线安装可以在发布流水线结束后下载该 ZIP，再通过本页上传。

## 安装和切换

1. Release 镜像运行时，打开「设置 → 软件升级」，选择完整的 `*-update-linux-*.zip`，或在 macOS 自动解压后选择该升级包目录。目录选择可以覆盖包含单个 `aide/` 根文件夹的工作目录；页面会在目录树中定位同一子目录下的 `manifest.json`、`SHA256SUMS` 和镜像归档，只上传这三个必需文件，并忽略 Finder 的 `.DS_Store`、`._*`、`__MACOSX` 以及其他无关文件。若选择的是启动器目录或普通工作目录且找不到完整三件套，页面会给出明确提示。服务端仍重新封装成标准 ZIP 并执行完整校验。上传后只会把升级包暂存到非活动槽，显示「待手动激活」，不会自动重启或切换当前工作台。
2. 用户点击非活动槽上的「手动激活槽」并确认后，服务器写入待处理操作；随 launcher 分发的 macOS/Ubuntu shell 或 Windows PowerShell 宿主代理轮询受令牌保护的 API。未点击前，当前版本继续运行。
3. 宿主代理下载已验证包、复核 SHA256、导入 Docker 镜像并标记目标槽，然后用原 Compose project 重建唯一的 aide 服务。
4. 新服务的 `/healthz` 在期限内成功后，代理确认切换；否则恢复旧 `.aide-image` 并启动原槽，再记录失败/回滚。

槽 A/B 使用稳定镜像引用 `aide:slot-a` 与 `aide:slot-b`。`docker/compose.offline.yaml` 固定同一 Compose project，并使用相同的 `aide-data` 和 `aide-home` named volumes；会话、设置、模型配置与容器 home 共用。`workspace/`、`context/` 仍由启动目录挂载。切换采用停旧再启新，任何时刻只允许一个 aide 实例写数据；切换期间页面可能短暂断开。两个镜像槽保留各自版本，失败可回滚，也可在升级页面主动切回旧槽。

## 运行边界

- 更新 API 与宿主代理 API 均要求访问令牌。
- Docker 操作仅在用户宿主启动包里的代理中运行；不挂载 Docker socket 到 aide 容器。
- 页面关闭不会中止已提交的切换；启动代理在宿主后台轮询。
- 服务重启后代理读取共享 `/data/updates/slots.json` 恢复槽状态；本地 `.aide-image` 记录当前启动槽。
- 检查公开 Release 需要联网；包上传和镜像导入可离线执行。
- 当前 Release builder 对应当前 Docker Linux 镜像架构（arm64 或 amd64）。安装机的 Docker engine 架构必须匹配；不兼容平台包会在上传时被拒绝。
