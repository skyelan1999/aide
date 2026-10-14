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


## 开发分批交付与镜像更新

Git push 只同步源码，不包含本目录被忽略的大型镜像归档。开发分支每批功能通过验证后应同步构建镜像，再使用现有 `bash scripts/aide.sh export-bundle` 生成带镜像身份的离线启动包。另一端解压后运行包内 `start.command`（或 `bash scripts/aide.sh start-bundle`），首次只导入自带镜像，后续按镜像 ID 复用；不拉基础镜像、不装依赖、不编译源码。不同 CPU 架构需要对应镜像包，不可把 ARM64 包当作 AMD64 交付。

保留原有 `SHA256SUMS` 条目，新增归档校验行；不要让一次开发导出丢掉历史包的校验记录。源码分支 push、镜像本机构建、离线包生成、外部镜像仓库 push 是独立状态；没有配置仓库和目标标签时不能宣称已经推送镜像。当前仓库未配置镜像 registry 发布工作流。稳定依赖层位于应用二进制层之前，普通代码变更应复用现有构建缓存；离线更新路径始终避免在接收端重新构建。


`plugins/surface.json` 是插件宿主启动时重写的能力快照（含生成时间），已从源码指纹及Docker构建上下文排除；插件真实源码仍计入。这样重启不会仅因快照时间变化就误判镜像过期。可用只读命令 `bash scripts/aide.sh source-sha` 检查当前构建指纹，该命令不启动Docker；离线包导出继续严格核对镜像label与当前指纹，不绕过一致性门禁。


2026-10-14 星图分批更新包：`aide-0.1.17.0-RC2-linux-arm64-ab75ecc66e3d-offline.tar.gz`。镜像 `sha256:ab75ecc66e3dc9eff663fd757d3b72e869111ef6087efe6a6ee1af8b6bfb207d`，SHA-256 `68c110e614b2cccca0047c689858b52bd77e904a53c39b5b848dda84fad7a3be`。本机8097已部署，包内镜像校验与 `docker image load` 已验证；另一端仍需导入验收。详见[批次验收](../docs/tasks/starmap-progressive-latency-20261014.json)。


2026-10-14 汇总包（星图分批 + 5x 文档规划 + 插件写入队列）：`aide-0.1.17.0-RC2-linux-arm64-ff1553153a55-offline.tar.gz`。镜像 `sha256:ff1553153a558f1002ffc0df84a8aa03e25921a2025c312b650f7db59c6eea39`，SHA-256 `9af6d3c0115241669dfe1a8def3558732b0e8da75bce896082ea09f600765ca5`。本机8097健康，包内外校验与实际 `docker image load` 通过；另一端请使用此包导入。含前述星图修复。详见[ROS2与插件队列复测](../docs/tasks/parallel-document-plugin-scheduling-20261014.json)。


上一批包（2026-10-14 22:50，含常驻插件超时实例停止修复及之前的星图/5x改动）：`aide-0.1.17.0-RC2-linux-arm64-7f0281ea4ceb-offline.tar.gz`。镜像 `sha256:7f0281ea4ceb3076170f46edfc3a13c3b0f00d9c52cc7095db9b7441c1c376f0`，SHA-256 `8e47db108507525c8964a745ce1e13963dc73f5604b9b6494096322f859af611`；本机8097健康，包内外校验及实际导入通过。保留此前包作为回滚材料；本段保留作回滚。

上一批包（2026-10-14 23:13，增加星图首屏缓存/扫描失败回退修复）：`aide-0.1.17.0-RC2-linux-arm64-05ba9e1b9c84-offline.tar.gz`。镜像 `sha256:05ba9e1b9c84cf7cac389e9dd65fb34c8c7c1a4b8ddceef4056f5a670387ca04`，SHA-256 `59b191e5e8e301854e3bbd5e561534099ea14b31fa9270f73dd1ecf56e1e9a06`；内外哈希、实际docker load和两工作树副本校验通过。本机8097原生Safari两次刷新在约1.16秒/0.91秒观察点均可见1563节点。本段保留作回滚；另一设备导入和冷启动尚未验证。

当前最新包（2026-10-14 23:36，增加插件声明资源预约与取消进程组收尾，包含此前星图首屏修复）：`aide-0.1.17.0-RC2-linux-arm64-9fa963d2209b-offline.tar.gz`。镜像 `sha256:9fa963d2209b59bd72c5a69c4108c7a99f209ca3be6305e3ba1600259ed811dc`，SHA-256 `bf5094ebd911301b5021dad9d71b382ea101a95c4c3943d465923a27df871f29`；内外哈希、实际docker load及两工作树副本校验通过。8097健康，设置/挂载/原加密历史保留。该包尚未实现5x插件工具完整依赖图；另一端优先导入本包，另一设备导入待用户实际验证。


2026-10-15 00:03后当前候选包（包含最终并行倍率列表与此前星图/插件修复）：`aide-0.1.17.0-RC2-linux-arm64-6ac93ca10a8d-offline.tar.gz`。镜像 `sha256:6ac93ca10a8d3b90b5d813e0ad89894afc0a6f16307e27da15c73c483c585289`，SHA-256 `1e1171d0590fb1a8dd5b1b8cb3d3d49bccd1bab2f76446bac051f2b9297ef4fd`，561613174 字节。8097原生Safari已验证推理强度与五条倍率同列滚动、选择持久化；包内外校验及实际 `docker image load` 通过，归档复制到两工作树。另一端导入待验；用户新报“引用aide目录无节点”尚未在该实例复现，本包不宣称解决此新问题。详见[布局验收](../docs/tasks/composer-parallel-placement-20261014.json)和[引用排查](../docs/tasks/starmap-reference-empty-20261015.json)。
