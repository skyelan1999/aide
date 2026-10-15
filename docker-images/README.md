# Docker 镜像交付

发行镜像提供 aide（AI + IDE 专业工作台）的完整运行环境：内置 Go、Python、Node.js、Git 与已编译应用。场景定制使用源码和 [Agent 工作流](../docs/customization.md)，完成后重建自己的镜像。

## 统一校验清单 SHA256SUMS

本目录**只使用一份 `SHA256SUMS` 清单**（与镜像归档同目录、相对路径）：

```bash
shasum -a 256 -c SHA256SUMS
```

历史零散的 `aide-*.sha256` / `aide-*.tar.gz.sha256` 单文件校验和**已废弃**，仅供回溯旧版；新发布一律以 `SHA256SUMS` 为准。`install.sh --image` 也只读取归档旁的 `SHA256SUMS`。

## 离线 / 空气 Gap 使用要点

- **运行离线包使用包内 `start.command` 或 `start-bundle`**，不执行构建。源码 `start` 仅在完整本地依赖镜像就绪时支持无网络重建；缺少本地镜像的首次构建仍需联网。
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


2026-10-15 上一批启动恢复包（已由下述资源权限修正版替代）：`aide-0.1.17.0-RC2-linux-arm64-f154cce63fed-offline.tar.gz`（linux/arm64，705564170 字节），SHA-256 `7c202d09a8148d95c2511df597004933c405111a7e609f137455db172dcd6037`。应用镜像 `sha256:f154cce63fed211234c668eeb550718100ced70ff3df4eccddde20ebe2f6e579`；同一包内 image tar 同时包含 `aide:dependencies-v1`，已验证两个标签及运行平台 manifest、镜像包内外哈希和实际 docker load，并复制至两个工作树。原包保留作回滚。按用户要求从 canonical `start.command` 实测启动及清构建缓存后本地重建；实际端口9999，Safari已自动打开但证书提示仍待用户手动处理，尚未把工作台UI验收记作通过。

仅需补充源码构建依赖时，可使用 `aide-dependencies-v1-linux-arm64.tar.gz`，SHA-256 `520baf83d7bc5fc1f20e7b0b8a9efd55976d22fa5bd4b6d2f713566eda905b76`，直接 `docker load -i` 导入。运行离线包不需要再下载此文件或重新构建。首次联网从零构建、Docker缓存与保留镜像的边界见[本地镜像说明](../docs/local-dependency-image.md)及[本批任务](../docs/tasks/launcher-local-image-20261015.json)。


2026-10-15 当前资源权限修正版：`aide-0.1.17.0-RC2-linux-arm64-05c7a9ee2663-offline.tar.gz`（linux/arm64，705587320 字节），SHA-256 `ecb62181f5efc32029e509d00ebbd59de335d0eeb2ede7117272e444374379e5`。应用镜像 `sha256:05c7a9ee2663aa94e49752a67d221babe69deca51c7d97c4da39305ee983ac14`，源码指纹 `5be86528b20af58d36940751e2b56b614ea5de25057c8ad4043cb96c54ab5ca0`。保留同一完整依赖镜像，修复私有源码模式复制后普通运行用户无法读取 Office 和内置插件的问题；工作台实际端口9999，Safari工作台与原配置保留已观测。镜像包内外校验、实际导入和副本状态以[本批任务](../docs/tasks/launcher-local-image-20261015.json)为准。旧包保留追溯，另一设备请优先使用此修正版；接收端导入仍待实际验收。


## 2026-10-15 incremental local snapshot

The canonical `aide/start.command` rebuilt the current local candidate using `aide:dependencies-v1` with build networking disabled and opened Safari on configured port9999. Latest offline bundle: `aide-0.1.17.0-RC2-linux-arm64-29c59cea57b5-offline.tar.gz`; use the adjacent SHA256SUMS. It includes both application and complete dependency image tags for one local import. Runtime/source startup and cache-only cleanup were verified in the launcher task. The current map additionally retains verified observations on source failures, but some host directory opens still wait; complete map coverage is not accepted. This is a development snapshot, not a formal release. Other-device import and Windows native acceptance remain external.


Latest plugin admission-order development snapshot: `aide-0.1.17.0-RC2-linux-arm64-ab31e79ca6c3-offline.tar.gz` (linux/arm64, 705581306 bytes), SHA256 `b49180ef4ce23a6d0ca73d51d248e71d62907f90b9ac38d17be864abbe877683`. Source648fa40 fixes cross-plugin readers overtaking writers hidden behind plugin FIFO. Resource/call race twice and vet PASS; quick68 PASS. Canonical start.command opened healthy Safari9999. Both application/dependency tags, actual docker load and both local copies verified. The prior full70 is historical evidence; no new full/formal release/real-model or receiving-device acceptance claimed.


Plugin dependency-plan development candidate: `aide-0.1.17.0-RC2-linux-arm64-2b4c7e19c77a-offline.tar.gz` (linux/arm64, 705608875 bytes), SHA256 `2aeebb09e34b6d0115512eff7ef9db6a97bc1e7efc61cef5ea57d805ec67ee93`. Source d8f7059 adds explicit plugin DAG dispatch, selected concurrency, serialized confirmations, durable node receipts and conservative recovery. Scoped race/vet and native model-protocol fixture pass; final full gate pending. Canonical start.command opens Safari9999. App/dependency tags, archive hashes and actual local import/copies verified. Dedicated graph UI, uncertain-effect reconciliation and real model/receiving-device acceptance remain open.


Latest readonly plugin graph UI development snapshot (2026-10-15): `aide-0.1.17.0-RC2-linux-arm64-045bdfce262f-offline.tar.gz` (linux/arm64, 705616235 bytes), SHA256 `b046c9a2772d13d0e097460e2e48bc8ffe44626d702a9278d0d01f04bbaa4f69`. Source e54f390; canonical `start.command` opened healthy Safari9999 using local complete dependencies with build networking disabled. App and dependency tags, inner/outer hashes, launcher bytes, actual local docker load and both local copies verified. The included dependency image supports local source rebuilds after cache-only cleanup; offline image startup imports directly. Synthetic display-only Safari desktop/current-dark, actual581x971 and keyboard/evidence navigation checks pass; no live model/plugin execution or receiving-device import claimed. See [current UI task](../docs/tasks/plugin-plan-ui-20261015.json). The previous backend full70 is historical; this UI batch has its own quick69 receipt. No formal release.


Latest chat draft navigation development snapshot (2026-10-15): `aide-0.1.17.0-RC2-linux-arm64-659d44949a0b-offline.tar.gz` (linux/arm64, 705613537 bytes), SHA256 `c193bf748ce6b40d93f42c0eefd31d29f06d256c8493f6d637255b73957111fe`. Source0cb5e42 fixes outgoing draft cancellation and destination input cached under the previous conversation. Canonical `start.command` rebuilt with complete local dependencies and opens healthy Safari9999. Actual typed A/B switching/reload and empty new-session reload pass; lock/workspace guards have focused regression evidence. App/dependency tags, launcher bytes, inner/outer hashes, actual local import and both local copies verified. See [draft navigation task](../docs/tasks/chat-draft-navigation-20261015.json). This replaces the prior local candidate; no new full71/formal release, browser crash/quota or receiving-device acceptance claimed.


Latest per-file star-map body isolation development snapshot (2026-10-15): `aide-0.1.17.0-RC2-linux-arm64-a187ed7d3c6a-offline.tar.gz` (linux/arm64, 705619992 bytes), SHA256 `72aa6fa24cf4e2dd27e0b362ae036e8de1f5dc8ff0a009caa93fded2306e60bb`. Sourcec4f9295 continues indexing peers when a local body is unreadable, shows metadata-only nodes and body-gap counts, and removes obsolete body/code evidence. Canonical `start.command` opened healthy Safari9999 using complete local dependencies. All Knowledge/local-root scoped race73.040s+vet and actual mounted four-batch probe pass; native Safari reference grew2 to3875 nodes with truthful unavailable/readable details. Two sampled cloud placeholders required normal host reads before their bodies became available; full body availability and all latency/host syscall issues remain open. App/dependency tags, launcher bytes, inner/outer hashes, actual local import and both local copies verified. See [body isolation task](../docs/tasks/starmap-file-read-isolation-20261015.json). No new full71/formal release, narrow/other-theme or receiving-device acceptance claimed.


Latest per-batch star-map material reuse development snapshot (2026-10-15): `aide-0.1.17.0-RC2-linux-arm64-1e34815bcb87-offline.tar.gz` (linux/arm64, 705617146 bytes), SHA256 `dec8cb657ed51b5309cfc34d860cb9f8990cc95107dd11c3265a585ffd17552a`. Sourcea3824e5 reuses stamp-validated body success/error within one candidate across aliases; errors are retried on the next candidate, and source IDs/origins remain distinct. Code upgrade reuses text; code-only error does not deny a text-only alias. Scoped all Knowledge/local-root race79.549s+vet and mounted counted probe pass; instrumented old algorithm reproduces four reads per fixture body versus one after repair. Canonical start.command opened healthy Safari9999 and actual reference counters progressed beyond5800. This is reader-call reduction, not a fixed wall-clock speedup or directory-discovery deduplication. Cloud body gaps, first-load selection reset and full latency acceptance remain open. App/dependency tags, inner/outer hashes, exact launchers, actual local import and both local copies verified. See [material reuse task](../docs/tasks/starmap-batch-material-reuse-20261015.json). No new full71/formal release or receiving-device acceptance claimed.


Latest star-map startup selection development snapshot (2026-10-15): `aide-0.1.17.0-RC2-linux-arm64-b687ad3e321d-offline.tar.gz` (linux/arm64, 705620605 bytes), SHA256 `9035e2265ac5e2b3691a3fd9c80761298d5de65167908fd3c71ecca3c7ed129d`. Final sourceee39d65 retains current filter/query/camera edits during cached startup synchronization and identity/network failure retries; same-workspace graph replacement preserves view, workspace/lock/mode boundaries remain. Deterministic old/intermediate negative proofs and final source VM cases pass. Canonical start.command opened Safari9999; native old reset reproduced, final context/query persisted through actual updates6787 to7542 and normal reload. Timing-specific retries/lock/mode/camera are VM evidence, not forced native error/crash acceptance. Complete app/dependency tags, exact launchers, inner/outer hashes, running platform manifest, actual local import and both local copies verified. See [startup selection task](../docs/tasks/starmap-startup-selection-20261015.json). Large-source latency/cloud body gaps remain; no new full71/formal release or receiving-device acceptance claimed.


Latest bounded star-map material development snapshot (2026-10-15): `aide-0.1.17.0-RC2-linux-arm64-abca7611229b-offline.tar.gz` (linux/arm64, 705638919 bytes), SHA256 `131ce82df4f86e6f8244cc56759a0bf6050f6728a5b47bea0ab83aef5b4b3e9b`. Source5ffaaa3 applies fair quota/time-sliced file and directory material validation, explicit pending/unavailable metadata and durable queue cursors. Encrypted payload v2 reads v1; older images safely rescan v2 instead of restoring it. All Knowledge/local-root scoped race99.729s+vet PASS; actual readonly945-node frozen prefix proves bounded reader calls, not full scan latency. Canonical start.command opened healthy Safari9999; native reference grew10744 to13156 nodes, pending detail remained explicit and current selection survived updates. Complete app/dependency tags, exact launchers, inner/outer hashes, running platform manifest, actual local import and both local copies verified. See [material batching task](../docs/tasks/starmap-material-batches-20261015.json). Cloud body gaps and single OS-call blocking remain; no new full71/formal release or receiving-device acceptance claimed.


Latest operator plugin-effect check development snapshot (2026-10-15): `aide-0.1.17.0-RC2-linux-arm64-e5998ca4b4ef-offline.tar.gz` (linux/arm64, 705643413 bytes), SHA256 `19f23a2b501c749ac19b3f3779b1610b1ffa1cd20f419f40a5ed51d6b738053a`. Source060088e adds append-only operator evidence to stopped uncertain nodes with digest/revision guards and save rollback. Operator observations do not replace execution receipts or authorize replay/recovery. Outcome exports omit private check evidence. Scoped graph/effect/outcome race38.884s+vet and quick69 PASS; canonical start.command Safari fixture save, concurrent revision rejection with draft preservation, reload and real stopped/started service restore2 checks PASS. Fixture only: no provider or external plugin action executed. Complete app/dependency tags, launchers, inner/outer hashes, running platform manifest, actual local import and both local copies verified. See [operator check task](../docs/tasks/plugin-effect-checks-20261015.json). Whole-repository Go race (server743.173s) and vet PASS on the frozen Go snapshot. The full71 wrapper is FAIL: it ran an exact-body legacy lock assertion before its behavioral-test repair, and delivery metadata also changed during the run; corrected final quick69 PASS. This is not a passing current full release gate. No formal release or receiving-device acceptance claimed.


Latest reviewed-recovery continuation development snapshot (2026-10-15): `aide-0.1.17.0-RC2-linux-arm64-b5c31b79e1a7-offline.tar.gz` (linux/arm64, 705655682 bytes), SHA256 `2a216d336b1e929bd66d385c88881159f9f641e748d97a811bd976f4fdec8d28`. Final source3248686 (with e446ea9/4994835) adds session/workspace/epoch-bound explicit continue-without-replay decisions, current-policy new work admission, immutable old graph history and exact unresolved-operation retry refusal. Cached graph results and outcome summaries exclude private check/decision notes. Scoped graph/harness/effect/outcome/resume race/vet and final guards16.065s+vet PASS. Native canonical Safari e446 simulated-provider fixture verified missing checks, required note/ack, stale409 retaining note and successful successor; one Docker-internal model request without private markers/credentials, no plugin action. Backend-only owner/ordinary-checkpoint hardening has current API race proof; final source rebuilt by exact start.command with original settings/config/mounts preserved. Complete app+dependency tags, launchers, inner/outer hashes, running platform manifest, actual local import and both local copies verified. Gate result is recorded in the [continuation task](../docs/tasks/plugin-recovery-continuation-20261015.json). No real provider/plugin-effect, receiving-device or formal-release acceptance claimed.

## 2026-10-15 启动错误恢复与并发关闭修复候选

最新本机 ARM64 包：`aide-0.1.17.0-RC2-linux-arm64-0d67b0b6bbfc-offline.tar.gz`，SHA-256 `e18e8f3b54e603eb531547ebd219fc59ef3f074ddb72f0fafd67e4676c3eb0fe`。镜像 `sha256:0d67b0b6bbfc1907d92b419508182da098ab8d19c60995b24454e72b15d894aa`，构建源码提交 `334237a`，源内容指纹 `0759c1e5cc41c0a9c91a7b5cc0b96a031e72f449407239af08e28c16f03105a7`。包含完整应用及 `aide:dependencies-v1` 依赖镜像；内外哈希、镜像标签、运行平台、实际docker load、启动脚本及两工作树本地副本均校验通过。包内入口沿用 `start.command`，仅导入本地镜像再启动；源码入口可使用依赖镜像在无网络模式重建。

本机仍使用保存的9999配置；原不可用工作区配置保留并明确提示临时工作区。修复不可用引用目录误回登录框及HTTP/TLS监听器并发关闭panic。定向race/vet与系统Safari恢复验收通过；完整72项门禁待最终收据。开发分支推送和本机镜像交付不代表正式main/tag发布或接收设备导入成功。


## 2026-10-15 会话编号及独立语音偏好修复候选

当前本机 ARM64 包：`aide-0.1.17.0-RC2-linux-arm64-00c6011ad9f4-offline.tar.gz`，705659485 字节，SHA-256 `fb39935fe3d0be3e09fb8181629a5d9c6db3d69a737ca10f1bad948da47d7ab2`。源码提交 `57e1579`（含 `d6dbf55`），源内容指纹 `eb8d7f89e3ca583c3aa2bc52d4729cceb21bd823967653f36a62f75611066cf6`。应用及完整本地依赖镜像均在包内；内外哈希、启动脚本、运行平台、实际流式 docker load 和两个本地工作树副本校验通过。沿用包内 `start.command` 直接导入；接收设备尚未验收。

规范路径持久化编号、旧配置仅提升编号、独立设置保存保留语音派发偏好已修复。定向 race/vet、quick70、系统 Safari 编号重启与语音读回通过；完整72项门禁单独记录于[任务账本](../docs/tasks/session-counter-canonical-20261015.json)，当前不声明全量通过。六主题桌面及574像素窗口显示检查通过，390像素与真实模型执行仍未验收。开发分支交付，不代表main/tag正式发布。


## 2026-10-15 星图持续扫描快照候选

`aide-0.1.17.0-RC2-linux-arm64-320315d7e14b-offline.tar.gz`，705662934字节，SHA-256 `aafddf23a1d2212b60313af6bd625f7d488c3dd1b2875dd72b9dc42ce0db3c1b`。源码 `6bdcb41`，源指纹 `0220874f51107c9361102fecfbf9112859f12b6b2617b5c15dc562cc59958cd1`。修复持续扫描时缓存写入被不断推迟；quick70、定向计时器回归及系统Safari大引用显示通过。完整应用和本地依赖镜像均包含，流式内外哈希、运行平台、启动器、实际docker load和两个本地副本校验通过。原`start.command`已在本机启动9999；接收设备导入未验收。

新full72结果见[当前任务](../docs/tasks/starmap-current-latency-20261015.json)；前一批全量通过不能代替本批门禁。当前不声明大星域交互或冷启动整体提速，不做main/tag正式发布。


## 2026-10-15 星图元数据布局复用候选

`aide-0.1.17.0-RC2-linux-arm64-fbbbcc0aa4d7-offline.tar.gz`，705661053字节，SHA-256 `dba765290d4f297cf6cb31bcf102bc297d35e60811d9b0ee16ce888307c8d367`。源码`98d6599`，源指纹`e77bddba1abef7056c993ff4aac1c3acfccde32853c9ca9c6df93ec0d6d8bf1c`。仅正文及读取状态等更新时保留空间布局，保持搜索、详情与引用校验更新，结构变化仍重建。quick71和系统Safari自有文件等长改写／删除验收通过，包含完整应用与本地依赖镜像，流式内外哈希／平台／启动器／实际docker load与两个本地副本校验通过。

现有清理脚本实际释放38.37GB默认构建缓存，镜像、卷及本机工作台保留；清理后原`start.command`直接复用本地镜像启动9999。当前full73状态以[任务账本](../docs/tasks/starmap-metadata-layout-20261015.json)为准；不借用前批full72结果。大星域总体交互／接收设备导入和正式main/tag发布未完成。
