# aide 离线启动包

将启动包解压到目标电脑的普通目录。需要 Docker Desktop / Docker Engine 与 Compose v2；应用、Go、Python、Node.js 与文档解析依赖已包含在镜像中。首次启动时，若 Docker 中尚无目标镜像，启动器会从同一 GitHub Release 自动下载镜像与 `SHA256SUMS`，校验 SHA256 和镜像 ID 后导入。首次下载约 500 MB，需联网；离线安装可预先将匹配镜像归档和 `SHA256SUMS` 放入包内 `docker-images/`。

macOS 双击 `start.command`；Windows ARM64 双击 `start.bat`；Ubuntu ARM64 执行 `./start.sh`。首次运行会检查 CPU 架构，按需下载、校验并导入镜像，创建 workspace / context 目录和默认 .env，然后启动应用。后续运行复用匹配的镜像。全程不构建、不拉取基础镜像；包内没有 Dockerfile 或 Compose build 配置。

通常在浏览器访问 https://localhost:8097。若该端口已被占用，启动脚本会自动选择并保存下一个可用端口，并在终端显示实际端口。首次使用的自签证书提示需要由使用者确认。启动脚本在桌面环境中会自动打开携带本机登录令牌的页面。服务器环境从容器 `/data/auth/access-token` 获取令牌，勿公开令牌。

「设置 → 无障碍」可修改宿主机绑定端口；设置保存到持久数据卷，后台启动监视器会自动更新 Compose 端口并重启服务。原设置页会等待新端口就绪后在当前标签页切换过去，避免留下失效旧页或额外打开重复标签。首次启动时，脚本优先使用 8097；若被占用，会自动选择并保存后续可用端口。

应用默认仅浏览包内 workspace 目录，辅助资料放在 context。需要使用其他目录时编辑 .env 中的路径，并再次启动。端口冲突时修改 AIDE_PORT。包使用独立的 `aide-offline` Compose 项目和持久数据卷。

RC2 当前 Release 仅提供 `linux/arm64` 镜像：适用于 Apple Silicon、Ubuntu ARM64，以及使用 ARM64 Linux Docker 引擎的 Windows ARM64。Windows x64 与 Ubuntu x64 需要 `linux/amd64` 镜像；当前构建主机没有该架构的本地基础镜像，因此本次不发布虚假的兼容包。运行 Aide 本身可离线启动；AI 对话仍需配置目标环境可访问的模型服务。可选语音模型存于数据卷，未包含在镜像中。

包不包含原电脑的会话、API 密钥、浏览器令牌、宿主项目文件或数据卷。首次启动为空环境；会话与配置以后保存在目标机的 Docker 数据卷中。

停止应用：`bash scripts/aide.sh stop`。不要用 `down -v`，它会删除会话与配置。迁移已有会话需要另行备份和恢复数据卷，镜像包只负责程序运行环境。

BUILD.txt 记录版本、镜像 ID、平台和构建输入指纹；docker-images/SHA256SUMS 校验包内镜像。外层归档的校验文件随交付提供。此包为当前工作树构建快照，正式版本发布状态以仓库发布记录为准。
