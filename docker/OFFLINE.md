# aide 离线启动包

将完整启动包解压到目标电脑的普通目录。需要 Docker Desktop / Docker Engine 与 Compose v2；应用、Go、Python、Node.js、文档解析与原生 CAD 运行时已包含在镜像中。包内 `docker-images/` 带有匹配镜像及 `SHA256SUMS`，首次启动从本地校验并导入，后续复用已导入的镜像，不构建、不拉取基础镜像、不在线安装 Python 依赖。

macOS 双击 `start.command`；Windows 双击 `start.bat`；Ubuntu 执行 `bash start.sh`。启动器检查镜像与 Docker 引擎架构是否匹配，创建 workspace/context 目录和默认 `.env`。启动包没有 Dockerfile 或 Compose build 配置。保留包内镜像归档，可以在清理 Docker 镜像后再次本地导入。

通常在浏览器访问 https://localhost:8097。端口被占用时，启动器选择并保存下一个可用端口，终端显示实际端口。自签证书首次提示由使用者确认。桌面启动器自动打开携带本机登录令牌的页面；服务器可从应用容器 `/data/auth/access-token` 获取令牌，勿公开令牌。

「设置 → 无障碍」可修改宿主机绑定端口。后台监视器更新端口并重启服务，原页面等待新端口就绪后在当前标签页切换。验收时可以设置 `AIDE_OPEN_BROWSER=0` 并复用已有工作台页，避免重复打开标签。

核心会话、配置、认证资料位于启动目录 `.aide-data/storage/data/`，工具用户目录位于 `.aide-data/storage/home/`；私有启动配置备份也保存在 `.aide-data/`。这些是宿主目录绑定，删除容器、镜像或构建缓存不会删除它们。搬迁、更新启动包时保留整个 `.aide-data/` 与自己的 `.env`，不要用新包覆盖旧电脑的私有配置。旧版命名卷迁移会校验并保留原卷，迁移记录在 `.aide-data/storage/migration.json`。

原生 STEP 预览由同一应用镜像启动的 `cad` 服务完成，使用包内新版 Compose；不需要另拉 CAD 镜像或向云端上传模型。服务只连接内部网络，没有宿主发布端口，只读挂载 CAD 专用派生凭据目录；临时输入在转换结束或取消后清理。工作台限额 2 GiB，CAD 服务限额 4 GiB、一次一个转换；本次 204 MiB 样例在约 7.75 GiB Docker 引擎内存的环境验收，峰值采样约 2.21 GiB。体积相同的不同复杂模型并不保证相同内存与耗时。完整曲面、装配位置、单位和颜色由几何验收检查；测量界面与导出会明确注明预览网格近似精度。

辅助资料放在 context，项目放在 workspace；其他目录通过 `.env` 或工作台工作区配置选择。包使用独立的 `aide-offline` Compose 项目。包不含原电脑的会话、API 密钥、浏览器令牌、宿主项目或数据目录；在新目录首次启动为空环境，镜像包负责程序运行环境，已有数据需另行复制或恢复。

本次快照为 `linux/arm64` 镜像，适用于 Apple Silicon、Ubuntu ARM64，以及使用 ARM64 Linux Docker 引擎的 Windows ARM64。x64 引擎需要另构建并验收 `linux/amd64` 镜像，不能混用架构。Aide 启动及 CAD 预览可离线使用；AI 对话仍需配置可访问的模型服务。可选语音模型在私有数据目录，未包含在镜像中。

停止应用：macOS 双击 `stop.command`，Windows 双击 `stop.bat`，Linux 执行 `bash stop.sh`；也可运行 `bash scripts/aide.sh stop`。停止脚本按当前启动目录定位并停止工作台及 CAD 服务。`clean-docker-cache` 入口仅清理当前引擎默认构建器的未使用构建缓存，不删除镜像、数据目录或工作区。

`BUILD.txt` 记录版本、镜像 ID、平台、构建输入指纹及 CAD 运行时版本；`docker-images/SHA256SUMS` 校验包内镜像，外层归档摘要随交付提供。此包是已验收的构建快照；是否创建正式版本、标签或 GitHub Release，以仓库发布记录为准。

## 标准启动器入口（#640）

新 Release 完整包仅提供各平台的 start、stop、docker_clean、verify 与生成的 Windows 原生 EXE。start 校验/导入镜像，不编译；docker_clean 保留归档与用户数据，便于下次重新导入。build、clean、release 仅在源码包提供。源码更新使用 git pull 后执行 build，不再由 start 隐式编译。参见[标准启动器规范](../docs/architecture/launcher-contract.md)。
