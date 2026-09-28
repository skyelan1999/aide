# Docker 镜像交付

发行镜像提供 aide（AI + IDE 专业工作台）的完整运行环境：内置 Go、Python、Node.js、Git 与已编译应用。场景定制使用源码和 [Agent 工作流](../docs/customization.md)，完成后重建自己的镜像。

## 统一校验清单 SHA256SUMS

本目录**只使用一份 `SHA256SUMS` 清单**（与镜像归档同目录、相对路径）：

```bash
shasum -a 256 -c SHA256SUMS
```

历史零散的 `aide-*.sha256` / `aide-*.tar.gz.sha256` 单文件校验和**已废弃**，仅供回溯旧版；新发布一律以 `SHA256SUMS` 为准。`install.sh --image` 也只读取归档旁的 `SHA256SUMS`。

## 离线 / 空气 Gap 使用要点

- **目标离线机绝不执行 `bash scripts/aide.sh start`**：`start` = `--build`，会触发 `docker build` 拉取基础镜像，在无外网机器上必然失败或挂起。
- 离线路径固定为 `bash scripts/aide.sh start-image`（= `--no-build --pull never`），只启动已 `docker load` 的镜像。
- **双保险防 pull**：Compose 中 aide 服务已设 `pull_policy: never`，叠加 `start-image` 的 `--pull never`。即使裸跑 `docker compose up` 也不会联网拉取；缺镜像会立即报错。
- 镜像自包含运行工具链，无 CDN/外部字体依赖；离线时模型指向局域网地址，`web_search` 在未配置端点时优雅降级为本地 `search_text`。完整离线流程见 [docs/installation.md §3C](../docs/installation.md)。

## 联网构建机：产出离线镜像

在有外网、可拉取基础镜像的机器上，于 Git 仓库根目录执行（复用 `scripts/version.sh` 版本号，不另造）：

```bash
bash scripts/docker-release.sh
```

脚本流程：读取版本/commit → 固定 tag `aide:<VER>` → `docker build`（注入版本/commit）→ 容器内 `healthz` 冒烟 → `docker save | gzip` → 生成统一 `SHA256SUMS`。产物命名：

```
docker-images/aide-<VER>-linux-<arch>.tar.gz   # arch = aarch64 | amd64
docker-images/SHA256SUMS                        # 单文件清单（相对路径）
```

交付三件套：同 tag 源码 + 上述 tar.gz + SHA256SUMS，随介质拷贝到目标机。

## 下载与启动

从 [GitHub Releases](https://github.com/skyelan1999/aide/releases) 下载同版源码、`aide-0.1.10.2-RC1-linux-aarch64.tar.gz` 与 `SHA256SUMS`，放在本目录。此次提供 **linux/aarch64（Apple Silicon）** 镜像；x86/amd64 请从源码本机构建，不把 ARM 镜像当作原生 x86 版本。

```bash
# 在归档所在目录验证，再导入
shasum -a 256 -c SHA256SUMS
docker load -i aide-0.1.10.2-RC1-linux-aarch64.tar.gz
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

`bash scripts/aide.sh export` 将本地开发镜像 aide:local 导出为 aide-local.tar.gz 与 SHA256SUMS；`load` 导入该开发归档。正式 Release 使用 `scripts/docker-release.sh` 产出的版本化归档和 `SHA256SUMS`，避免混淆。

需要换电脑直接启动时，先通过 `start.command` 构建当前工作树，再执行 `bash scripts/aide.sh export-bundle`。这会导出当前 Compose 配置实际使用的镜像，并生成 `aide-<版本>-linux-<架构>-<镜像ID前12位>-offline.tar.gz` 与 SHA256SUMS。构建输入与镜像不一致时拒绝导出，防止误交付旧镜像。

离线包包含运行镜像、独立 Compose、启动脚本、空工作目录模板和使用说明。目标电脑解压后双击包内 `start.command`（Linux/WSL：`bash start.command`），自动校验、导入并启动；无需源码、Go、Node 或 Python 宿主环境，不执行 build/pull。Docker/Compose、Bash、shasum 仍需由目标电脑提供。CPU 架构不符时明确停止。包使用独立的 `aide-offline` 项目；默认端口仍为 8097，和源码版同时运行时应在包内 .env 修改端口。

这是当前构建快照的离线交付，不会自动打 Git 标签、推送或发布 GitHub Release。详见 [包内说明模板](../docker/OFFLINE.md)。

大型归档保留在 aide/docker-images，上传到 Release 附件，不进入 Git 或 Docker 构建上下文（`docker-images/*.tar*`、`*.sha256` 已 gitignore）。镜像不包含宿主工作文件、辅助目录、API 密钥、访问令牌或会话数据。数据卷另行备份；加载镜像不会恢复数据卷。升级/回滚前阅读 [交接手册](../HANDOVER.md)。
