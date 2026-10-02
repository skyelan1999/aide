# aide 0.1.14.0 RC13

RC13 发布统一的完整运行与应用内升级包。macOS、Windows 和 Ubuntu 启动文件与一个 Linux ARM64 镜像归档打在同一个 ZIP 中；可用该包首次启动，也可在「设置 → 软件升级」上传到非活动槽。macOS 自动解压后，选择解压出的完整包文件夹即可。

升级页显示上传字节进度和 A/B 切换阶段。上传并校验后，新版本先暂存到非活动槽，需要用户手动激活；失败时启动器会尝试恢复原槽。会话、设置、模型配置和容器 home 使用原数据卷。

## 使用说明

- 全新安装：解压统一完整包并运行 `start.command`（macOS）、`start.ps1`/`start.bat`（Windows）或 `start.sh`（Ubuntu）。需要 Docker Desktop 或带 Compose v2 的 Docker Engine。
- 从 RC12 升级：在升级设置选择 `aide-v0.1.14.0-RC13-full-linux-arm64.zip`，或选择 macOS 解压后的完整发行文件夹；检查非活动槽版本后再手动激活。
- RC10/RC11 的 macOS 安装需先按 RC12 发布说明更新宿主启动器脚本，再进行应用内升级。容器镜像升级不会替换安装目录中的宿主脚本。
- 该版本是测试 prerelease。macOS 启动脚本未使用 Apple Developer ID 签名或公证，首次打开可能出现 Gatekeeper 提示。

## 验证状态

构建、全量测试、健康检查、资产校验、上传/升级验收和远端 Release 复核见 [RC13 资产验证报告](release-assets-2026-10-02-RC13.md)。
