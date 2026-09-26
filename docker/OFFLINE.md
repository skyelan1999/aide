# aide 离线启动包

将整个包解压到目标电脑的普通目录。需要 Docker Desktop / Docker Engine、Compose v2、Bash 和 shasum；应用、Go、Python、Node.js 与文档解析依赖已包含在镜像中。

macOS 双击本目录的 `start.command`；Linux 或 Windows WSL 执行 `bash start.command`。首次运行会检查 CPU 架构、校验并导入包内镜像，创建 workspace / context 目录和默认 .env，然后启动应用。后续运行复用匹配的镜像。全程不构建、不拉取基础镜像；本包没有 Dockerfile 或 Compose build 配置。

在浏览器访问 https://localhost:8097。首次使用的自签证书提示需要由使用者确认。启动脚本在桌面环境中会自动打开携带本机登录令牌的页面。服务器环境从容器 `/data/auth/access-token` 获取令牌，勿公开令牌。

应用默认仅浏览包内 workspace 目录，辅助资料放在 context。需要使用其他目录时编辑 .env 中的路径，并再次启动。端口冲突时修改 AIDE_PORT。包使用独立的 `aide-offline` Compose 项目和持久数据卷。

CPU 架构以 BUILD.txt 为准；ARM64 包用于 Apple Silicon / Linux ARM64，x86 主机需要 AMD64 包。本包可离线启动应用；实际 AI 对话仍需在设置中配置目标环境可访问的模型服务。可选语音模型存于数据卷，未包含在本包中。

包不包含原电脑的会话、API 密钥、浏览器令牌、宿主项目文件或数据卷。首次启动为空环境；会话与配置以后保存在目标机的 Docker 数据卷中。

停止应用：`bash scripts/aide.sh stop`。不要用 `down -v`，它会删除会话与配置。迁移已有会话需要另行备份和恢复数据卷，镜像包只负责程序运行环境。

BUILD.txt 记录版本、镜像 ID、平台和构建输入指纹；docker-images/SHA256SUMS 校验包内镜像。外层归档的校验文件随交付提供。此包为当前工作树构建快照，正式版本发布状态以仓库发布记录为准。
