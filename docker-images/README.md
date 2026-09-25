# Docker 镜像交付

发行镜像提供 aide（AI + IDE 专业工作台）的完整运行环境：内置 Go、Python、Node.js、Git 与已编译应用。场景定制使用源码和 [Agent 工作流](../docs/customization.md)，完成后重建自己的镜像。

## 下载与启动

从 [GitHub Releases](https://github.com/skyelan1999/aide/releases) 下载同版源码、`aide-0.1.10.2-RC1-linux-arm64.tar.gz` 与 `SHA256SUMS`，将归档放在本目录。此次提供 **linux/arm64（Apple Silicon）** 镜像；x86/amd64 请从源码本机构建，不把 ARM 镜像当作原生 x86 版本。

```bash
# 在归档所在目录验证，再导入
shasum -a 256 -c SHA256SUMS
docker load -i aide-0.1.10.2-RC1-linux-arm64.tar.gz
```

进入源码根目录，首次复制 `.env.example` 为 `.env`，创建 context 目录，设置已存在的目录：

```dotenv
AIDE_IMAGE=aide:0.1.10.2-RC1
AIDE_PORT=8097
AIDE_WORKSPACE=.
AIDE_CONTEXT=./context
AIDE_LOCAL_ROOT=/absolute/path/to/your/projects
```

```bash
mkdir -p context
bash scripts/aide.sh start-image
```

`start-image` 只启动已导入的镜像，不重建、不拉取，并沿用浏览器令牌登录流程。`start` 是源码重建入口。不要用新版本标签构建未验证的自定义代码；定制时将 AIDE_IMAGE 改为自己的名称（如 my-aide:local）。

## 本地导出与数据边界

`bash scripts/aide.sh export` 仍将本地开发镜像 aide:local 导出为 aide-local.tar.gz 与校验和；`load` 导入该开发归档。正式 Release 使用独立的版本化归档和标签，避免混淆。

大型归档保留在 aide/docker-images，上传到 Release 附件，不进入 Git 或 Docker 构建上下文。镜像不包含宿主工作文件、辅助目录、API 密钥、访问令牌或会话数据。数据卷另行备份；加载镜像不会恢复数据卷。升级/回滚前阅读 [交接手册](../HANDOVER.md)。
