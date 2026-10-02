# aide 0.1.14.0 RC10

## 软件升级

RC10 首次提供 Release 镜像模式的 A/B 应用内升级：升级包校验后写入非活动槽，用户可从设置页手动激活；启动代理负责导入镜像、重建服务，并在健康检查失败时回滚。会话和配置数据继续使用共享 Docker volumes。

RC9 的公开启动包早于应用内升级功能，RC9 页面不会显示上传入口。请先下载并运行 RC10 平台启动包；启动器会沿用原有 Compose 项目和数据 volumes。后续版本可从 RC10 的设置页上传应用内升级包。

升级文件夹导入兼容 macOS Finder 自动解压：可选解压后的完整升级包目录，服务端定位并校验 manifest、SHA256 清单和镜像归档。应用内升级包 ZIP 与平台启动 ZIP 是两种不同用途的文件。

## 启动器与平台

Release 包含 macOS Apple Silicon、Windows ARM64 与 Ubuntu ARM64 启动器，以及匹配的 Linux ARM64 镜像和应用内升级 ZIP。启动器包含 `start.command`（macOS）、`start.bat` / `start.ps1`（Windows）或 `start.sh`（Ubuntu）。

macOS 启动器未使用 Apple Developer ID 签名，也未公证；首次打开可能出现 Gatekeeper 提示。包内 README 提供首次启动步骤。请只对从官方 Release 下载并校验过的包执行这些步骤。

## 验证

完整 Docker 构建运行 `go test -mod=vendor -count=1 ./...` 与 `go vet -mod=vendor ./...`；容器 `/healthz` 检查通过。镜像、启动器、升级包 SHA256 见本 Release 的 `SHA256SUMS`。
