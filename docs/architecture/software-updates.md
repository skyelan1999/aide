# 软件升级与 A/B 槽

## 当前启动优先级（2026-10-19）

当前策略以[启动与回退说明](../runtime-bundle.md)为准：Git 源码工作区默认核对当前源码哈希，变化时重新构建；显式设置 AIDE_BUILD_FROM_SOURCE=0 或发行安装模式才优先复用完整本地镜像。构建失败后按官方 Release 元数据校验回退。严格源码模式可设置 `AIDE_RELEASE_FALLBACK=0`。Git LFS 总包及当前属性已移除；旧版本在历史提交中的记录不会被强制重写。下文涉及更早默认行为的记录属于历史证据。

源码目录中残留的 `.aide-image` 不再自动覆盖源码模式；显式的 `start-bundle` 或 `AIDE_BUILD_FROM_SOURCE=0` 仍保留发行镜像模式。Windows 原生启动器同样按源码目录与配置选择模式，切换源码分支不会自动替换磁盘上既有的 EXE。

2026-10-30 起，macOS/Linux 源码启动器在源码构建失败且 Release fallback 开启时，优先从 GitHub 官方 Releases API 选择该 Docker 架构最新发布的完整运行 ZIP。它核对官方资产 SHA256、ZIP CRC、包内 Release tag/平台/镜像 ID、镜像归档 SHA256，以及导入后的 Docker 镜像 ID、平台、源码与 CAD 标签；容器健康检查通过后才记录版本切换完成。Compose 启动或容器健康检查失败时恢复原镜像标签。若官方元数据不可用或校验/导入失败，启动会停止，不再静默退到旧的静态清单镜像。设置 `AIDE_RELEASE_FALLBACK=0` 可禁用此路径。下载包和验证后的镜像归档缓存于 `.cache/aide/releases/`；健康确认后删除冗余 ZIP，缓存中只保留当前镜像归档。Windows 原生 EXE 在源码目录按源码指纹构建并从 version.md 注入版本；已有可信运行镜像时，构建失败后可通过原生 HTTPS 校验最新完整 ZIP，再用已验证镜像内的固定 Python helper 解出镜像。Windows 宿主无需 PowerShell 或 Python。完整发行包仍按随包镜像与应用内升级流程工作。旧 EXE 不会随 Git 分支切换自动重新编译，必须替换为包含此修复的构建。

## 适用范围

应用内 A/B 升级仅适用于通过正式 Release 镜像启动、并由随包宿主启动器管理的 aide。源码开发模式仍可检查公开 Release，但不显示应用内发行升级控件；普通源码升级仍由显式 `build` 执行。另提供宿主侧 `scripts/selfdev-ab.py` 开发候选 A/B：`stage A|B` 用独立构建镜像完成全量 Go 测试、构建并临时启动健康检查；`activate A|B` 在候选专用回环端口运行所选槽，失败时恢复此前候选槽。该开发候选与正式服务使用不同 Compose 项目、端口、数据卷、home 和本地目录；它不读写普通 `.env`，不挂载真实 `/local` 或 `/context`，不迁移账户配置，也不会调用正式服务端口。候选工作区只挂载当前 Aide 源码仓库。槽数据互相独立，切换不复制数据。已启用设备终端插件时，Aide 可经 `terminal-run` 在宿主执行该脚本并遵循该插件的授权；内置 `run_shell` 仍不访问 Docker socket，只在任务绑定的项目沙箱中执行。没有宿主终端能力时，用户可从源码仓库手动运行脚本。包身份仍通过 `AIDE_INSTALLATION_KIND` 区分。参见[标准启动器接口](launcher-contract.md)。

升级服务负责访问令牌鉴权、发行包解析与校验、槽状态持久化和进度报告。它不访问 Docker socket，也不执行上传包中的脚本。宿主启动器仅在用户手动激活槽后导入镜像并重启服务。

## 启动、重编译和版本升级的区别

| 场景 | 实际行为 | 更新方式 |
| --- | --- | --- |
| Git源码目录，AIDE_BUILD_FROM_SOURCE=auto | 检测.git后走源码模式；镜像source-sha相同则跳过编译，缺镜像或不同则编译 | 先同步目标分支，保留依赖，重新构建并核对commit及源码身份 |
| Aide 自我开发候选 A/B | 设备终端 `terminal-run` 经授权执行 `python3 scripts/selfdev-ab.py stage A` 或 `stage B`，在专用受限 Buildx builder 中跑全量 Go 测试、vet、编译，并健康启动隔离候选；`activate A|B` 显式切换候选端口 | 候选服务、数据、镜像、工作区与常规运行完全分开；`stop` 保留两槽数据与镜像。内置 `run_shell` 仍限于当前项目沙箱 |
| 完整离线启动目录，含.aide-image | 校验平台与包内镜像摘要，按本地归档导入；日常启动复用镜像 | 若应用也是release-image构建模式且有宿主代理，可使用正式升级包；不能仅凭.aide-image推断A/B可用 |
| 正式Release镜像及升级代理 | 服务器校验并暂存升级包，手动切换，健康失败尝试恢复旧槽 | 同平台的完整升级ZIP／升级ZIP；保留旧镜像与数据备份 |

不要通过清空Docker实现升级：清缓存会降低后续构建命中，清运行镜像还会丢失依赖与回退点。程序镜像包含工具链、运行依赖与编译程序；API密钥、会话、保险库、私有配置及可选下载模型不随新镜像分发。数据格式迁移须另做兼容性与备份检查，镜像回退不等于数据恢复。

`export-bundle`生成的`*-offline.tar.gz`是启动归档，不能直接作为应用内ZIP升级包上传。`package-release-assets.sh`还生成含manifest的统一完整ZIP与升级ZIP；打包脚本本身不保证输入镜像的buildRuntimeMode为release-image，发行验收必须通过运行API独立核对，不能仅因包名是Release就宣称支持A/B。

2026-10-18实机曾复现：Git源码目录清掉镜像后运行start.command，因缺CAD基础镜像中止；当时通过[人工恢复与重编译](../reviews/2026-10-18/startup-runtime-recovery.md)恢复本机。后续Bash启动器增加[本地依赖seed恢复](../reviews/2026-10-18/source-offline-recovery.md)：仅当配置的应用镜像不完整且基础依赖缺失时，按受控默认清单选择本地已有完整包或已导入镜像，校验后放入独立依赖标签，供prepare-local/CAD使用；旧包不直接变成当前程序，仍构建当前源码。完整bases已有时不额外导入。损坏摘要、符号链接、架构／身份错误和导入失败均停止，不转联网掩盖失败。

恢复依赖本身不下载、不选择最新文件或扫描任意tar包；只使用`docker/prebuilt-images.tsv`登记的平台与身份。对应本地包缺失返回明确不可用，既有首次源码构建流程仍可能走原始联网构建；需要完全离线时应预先放入完整包，不能把“本地恢复不联网”扩大为“所有首次源码构建都离线”。`AIDE_LOCAL_DEPENDENCIES=0`保留原构建方式，`AIDE_FORCE_REBUILD=1`仍请求构建；普通相同源码启动仍跳过编译。此恢复改动适用于macOS/Linux的Bash源码启动器；PowerShell显式源码重建未加入同等恢复路径，Windows原生验收仍由用户进行。

## 安装方式

| 方式 | 输入 | 适用场景 |
| --- | --- | --- |
| 离线安装 | 完整发行 ZIP、应用内升级 ZIP，或 macOS 解压后的发行文件夹 | 网络受限、已提前下载发行包，或 Finder 自动解压 ZIP |
| 在线安装 | 设置页中选择官方 Release 版本 | 直接从 Release 下载到服务器，无需先把大文件下载到本机 |

两种方式都会把经过校验的包暂存到非活动槽，不会自动切换或重启当前工作台。完成后，用户可手动激活该槽。版本不要求严格递增，因此支持同版本重装和低版本回退；活动槽只有在手动切换成功后才会变化。

在线安装只接受公开 GitHub Release API 中列出的官方 GitHub 下载地址，并按当前 Linux 架构选择 `*-update-linux-<arch>.zip`；Release 缺少独立升级包时，使用 `*-full-linux-<arch>.zip`。服务端限制下载体积，校验 ZIP、manifest、Release tag、平台与镜像 SHA256，再写入 `/data/updates/packages`。容器需能访问 GitHub API 和 Release 下载服务。

## 发行包结构

`bash scripts/package-release-assets.sh <tag> <image> [output-dir]` 生成轻量平台启动 ZIP、镜像归档、兼容旧版的 `aide-<tag>-update-linux-<arch>.zip` 和统一完整包 `aide-<tag>-full-linux-<arch>.zip`。

统一完整 ZIP 可用于首次启动，也可用于应用内离线升级。根目录包含 `.aide-image`、macOS/Windows/Ubuntu 启动文件、Compose 配置、脚本、`workspace/` 与 `context/`；Docker 镜像和升级清单位于 `docker-images/`。升级三件套为：

- `manifest.json`：格式 `aide-update-package`、版本 1、Release tag、Docker 平台、镜像归档文件名和 image ID。
- `SHA256SUMS`：镜像归档的 SHA256。
- `aide-<tag>-linux-<arch>-image.tar.gz`：Docker 镜像归档。

离线选择完整 ZIP 时，服务只提取三件套并重新封装，不会把整个启动目录解压到服务端。选择 macOS 解压后的文件夹时，浏览器从目录树定位同一 `docker-images/` 下的三件套，只上传这三个文件，并忽略 Finder 元数据、启动器、工作区等其他内容。旧版升级 ZIP 和仅含三件套的目录仍兼容。外层 Release `SHA256SUMS` 记录发行附件及升级 ZIP。

## 版本与槽状态

槽 A/B 使用稳定镜像引用 `aide:slot-a`、`aide:slot-b`。新包始终写入非活动槽；同版本包会替换非活动槽内容，较低版本包可用于回退。服务器不比较版本大小来拒绝包，而是校验 manifest、镜像 ID、平台和 SHA256。

切换由用户在目标槽点击「手动激活」后开始。服务器创建待处理操作；宿主代理拉取受令牌保护的命令、复核 SHA256、导入镜像，并使用原 Compose project 重建唯一 aide 服务。新服务 `/healthz` 检查通过后确认切换；超时或失败时恢复旧 `.aide-image` 并启动原槽。

当前启动器的A/B两槽使用同一Compose project，并复用启动目录 `.aide-data/storage/data` 与 `.aide-data/storage/home` 的宿主绑定，因此会话、设置、模型配置与容器home共用；较早named-volume安装须先经启动器校验迁移，不能直接另建空卷替换。`workspace/` 和 `context/` 仍由启动目录挂载。切换按停旧再启新执行，期间服务会短暂不可用，但不会有两个 aide 实例同时写数据。

## 正常升级后的完整性基线

镜像构建在复制最终主程序后生成只读、root 所有的 `/usr/local/bin/aide.sha256`，该文件不在持久化数据卷中。正式镜像和本地编译镜像均生成它；不是通过环境变量、上传内容或 API 参数指定可信摘要。它绑定镜像内的二进制内容，不替代发行包 SHA256、镜像身份及来源校验。

启动时仅准备候选转换，不替换已有基线。条件是：原基线可解析且有有效摘要，只有程序哈希不一致，版本或完整 commit 身份发生变化，正在运行的二进制与镜像内只读、root 所有的摘要一致，而且没有其他数据完整性错误。应用初始化完成并收到首次 `/healthz` 请求时，再次核对原基线未变、镜像摘要未变、当前程序和数据完整性。通过后先按内容摘要备份原基线为 `.integrity/baseline-before-<摘要>.json`，再原子更新 `baseline.json`，同次健康响应返回更新后的完整性状态。备份失败或冲突时不替换原基线。

后台巡检与该转换串行执行；巡检本身不批准版本转换。启动失败、缺失或可写/非 root 的镜像摘要、相同构建身份下的程序变化、候选确认前发生变化或数据异常，均不能自动覆盖旧基线。正常降版/槽回退走相同验证；首次安装仍沿用原有首次建基线行为。未携带此实现或摘要的旧镜像不会获得自动更新能力；非 Linux 镜像运行和裸 `go run` 不使用该机制。

隔离验收脚本：`scripts/test_integrity_upgrade_docker.py`，使用专用新旧测试二进制与临时数据，覆盖正常升级、首次健康请求前不更新、启动失败、摘要不匹配、回退、备份以及数据字节不变。该测试不挂载现有9999数据，不代表已发布新主程序。

## 进度与恢复

上传进度按浏览器实际发送字节显示。在线安装进度由服务端持久化在 `/data/updates/slots.json`，包括查找 Release、下载、校验和暂存阶段；下载阶段根据已收字节更新百分比。Docker 镜像导入和健康检查阶段由宿主代理报告里程碑进度，百分比不代表 Docker 实际导入字节数。

页面轮询失败时会继续等待服务恢复；重新打开或刷新设置页后，可从共享槽状态继续查看当前操作。若切换请求长期停留在 `requested`，页面会提示检查宿主启动脚本。在线下载完成后，槽显示「待手动激活」；下载失败会保留错误信息，用户可重新选择 Release 再试。

macOS 使用系统 Bash 3.2，因此宿主 shell 代理避免 Bash 4 才支持的 `${value,,}` / `${value^^}` 大小写转换。代理从当前运行容器的 Compose 标签发现项目名，确保 Finder 启动与升级重启使用相同 Compose project。启动器在已有 bundle 运行时沿用 `.env` 端口；仅首次启动且目标端口被占用时选择下一个可用端口。

## 安全与运行边界

- 更新 API 和宿主代理 API 均要求访问令牌。
- Docker 操作仅在用户宿主启动包中的代理内执行；容器不挂载 Docker socket。
- 页面关闭不会取消已提交的在线下载或切换。
- 源码模式不提供应用内安装；Release 镜像模式提供在线下载、离线上传、手动激活与健康失败回滚。
- 安装机的 Docker Engine 架构必须与镜像平台一致；不匹配的平台包会在校验时被拒绝。

## 启动资源检查与日志

源码与离线包启动前均检查系统盘及启动目录所在盘：启动至少预留 10 GiB，源码构建至少预留 20 GiB；空间无法读取时停止，不迁移数据、不自动清理文件。Docker 引擎内存至少 4 GiB，包含原生 CAD 时建议 6 GiB。全局分配超过 8 GiB 或 4 核会提示检查设置，启动器不会修改 Docker Desktop 或 WSL 的全局配置。

应用保留 2 GiB / 2 核上限，CAD 保留 4 GiB / 2 核上限；两项上限不表示实际同时占用。源码和离线 Compose 均把每个服务的日志轮转限制为 3 × 10 MiB；已有容器需按正常更新流程重建后生效。完整 Go 快照验收使用独立无网络容器，限制为 4 GiB / 2 核 / 512 个进程，并保留分片串行测试和 45 分钟内部超时。macOS、Windows、Ubuntu 启动包及完整包均携带资源检查脚本；Windows 实机启动由用户验收。

这些检查不读取 Docker 虚拟磁盘内部剩余空间；Docker 数据盘位于其他磁盘时，应另行检查该盘及 Desktop 磁盘配额。启动前的检查也不能保证后续整个构建始终有足够空间。

## 默认下载清单的公开门禁

`export-bundle` 只更新本机 `.agent-state/prebuilt-images.candidate.tsv`，不提前修改仓库默认的 `docker/prebuilt-images.tsv`。同一平台再次导出会替换该平台的候选项；两种架构应从同一源码生成，旧候选与新候选混用会被门禁拒绝。

镜像按既有流程验收、上传后，执行 `python3 scripts/verify_prebuilt_assets.py .agent-state/prebuilt-images.candidate.tsv`。该命令只访问公开 GitHub Release 元数据，不使用密钥、不下载镜像、不写清单；要求 ARM64/AMD64 各一项，源码身份和 tag 一致，附件已上传、URL/平台/镜像名称匹配、SHA256 与公开摘要一致。通过后再按普通代码审阅流程更新受控默认清单。`release-check` 在本地前置条件通过后，对受控清单执行同样的在线验证；网络失败也拒绝发布。

附件元数据验证不证明包内镜像身份、CAD 完整性或启动成功，实际导入及运行验收继续保留。当前无效默认地址须由最终已验证并公开的镜像替换，不能用门禁通过声称已经修复下载。

## macOS / Linux 启动就绪判定

`start.command` / `start.sh` 在容器健康后还会校验实际宿主端口。缺失、无效、零端口或多个不一致端口会停止启动交付，不读取登录令牌、不启动宿主观察进程、不打开浏览器；保留容器与数据供排查。合法 IPv4/IPv6 映射必须指向同一端口。

启动器使用启动目录 `.aide-data/storage/data/certs/cert.pem` 校验宿主 `https://localhost:<端口>/healthz`，绕过代理、不跟随重定向，并比对容器与宿主返回的 instanceId；最多三次，每次连接超时两秒、总超时三秒。缺少 curl、证书或实例身份，以及连接、TLS 或实例不符，均不提示“已就绪”。浏览器证书告警仍由用户自行确认，宿主检查不修改系统信任。此规则适用于 Bash 启动器；Windows 原生运行由用户验收，不能据此认定 PowerShell 已具备同等检查。

## 本次文档的运行边界（2026-10-17）

本轮源码截图环境与生产服务、发行槽隔离；未点击检查更新、上传或切换版本。软件升级是否可执行，应同时核对 runtime mode、构建身份、启动代理和包校验，不能仅凭页面版本文字判断。实际文档证据见[截图范围](../ui-evidence.md)，软件启动后全貌见下图。

![真实源码实例，非发行升级验收](../images/current-20261017/01-workbench.jpg)

## Windows 宿主代理切换修复（2026-10-26）

Windows 代理兼容切换命令可选列中的 `-` 占位符及旧式空列。切换本地已有槽不要求重复下载升级包。目标引用必须与 A/B 槽匹配；升级包继续校验 SHA256，并在 Docker containerd 改写导入 ID 时，仅从本次已校验归档的导入回执解析唯一引用及平台，再持久化实际本地镜像 ID。

宿主启动路径按一个参数引用，支持 Release 解压目录含空格。代理仅等待 `start.ps1` 进程退出，不能使用会等待整个后代进程树的 `Start-Process -Wait`，因为启动器会启动常驻监控。目标容器使用的镜像 ID 与健康检查均通过后才上报成功；失败则恢复旧 marker 并调用同一启动器回退。目录挂载沿用启动器的 Compose 覆盖配置。

更新代理使用操作系统排他文件句柄保持单实例，进程退出后锁自动释放，不再因空的 `.aide-update-agent.lock` 遗留目录永久停用。若首次替换脚本时仍有旧版后台代理运行，新代理不与其并发操作，需先重启 Windows 结束旧代理，再用新脚本启动。不要在切换进行中替换或删除锁文件。

`python scripts/test_windows_update_agent.py --pwsh <PowerShell路径>` 执行真实代理代码，使用隔离的 Docker、HTTP 与子进程替身，覆盖 12 组切换、回退、并发锁及镜像身份场景。Mac 上 PowerShell 7 结果不替代 Windows PowerShell 5.1、Docker Desktop、原生进程树与完整升级实机验收。详见[任务记录](../tasks/release-manual-switch-20261026.json)。

离线镜像发行脚本 `scripts/docker-release.sh` 的冒烟容器使用每次独立名称及私有 cidfile。证书复制、停止与退出清理均使用该次 Docker 写入的完整64位CID；没有有效CID时不按名称强制删除容器。因此名称冲突、未创建成功或CID格式异常不会删除既有 `aide-rel-test` 或其他工作负载。健康验证和镜像导出门槛保持不变。对应回归以临时目录中的模拟Docker执行，不代替真实镜像构建与导入验收。

## Windows 官方回退镜像导入（2026-10-27）

Windows 预构建导入接受 Docker 对受控清单固定引用返回的规范 config ID；固定引用导入后仍无法解析时，只允许本次成功 `docker image load` 回执中的唯一引用，随后严格核对架构、源码指纹及该旧版归档要求的 CAD 标签。不扫描本地任意标签，不修改公开清单。激活以规范 ID 为准，先保留原目标标签的回滚引用。错误分别标明身份、平台、源码或 CAD 字段，避免将合法的 manifest/config ID 差异合并为身份错误。

完整包外层 SHA-256 通过后，.NET GZip 流只读取指定普通文件到本次私有临时目录，不执行外部 tar.exe；支持 USTAR、PAX 路径/尺寸与 GNU 长文件名/正数尺寸，PAX 元数据每项最多64KiB。其他成员只跳过，不按归档路径写盘；目标链接、坏校验头、截断或缺失成员拒绝导入。只导入后验证镜像不等于实际 Windows/Docker Desktop 启动验收，也不修复 Docker Hub 网络超时。

`python scripts/test_windows_prebuilt_import.py --pwsh <PowerShell路径>` 运行真实 PowerShell 解包/校验配合隔离 Docker/HTTP 适配器；加 `--docker <docker路径>` 导入一个带唯一标签的微小空镜像，验证 manifest/config 身份与本次导入回执路径，不运行容器、不替换 Aide 标签，最后移除自建标签。Windows PowerShell5.1、用户机器的网络与完整官方镜像导入仍需分别验收。

## Windows 回滚失败与持久诊断（2026-10-27）

宿主更新代理保留目标启动的原始失败及恢复启动的独立结果。恢复再次抛错由内层保护处理，仍生成失败回执；服务端失败消息不再笼统宣称“已回滚”。同版本槽切换仍允许。

诊断位于启动目录 `.aide-data/update-agent/`：`agent.log` 记录操作及重试，`activation.log` / `recovery.log` 记录对应子启动器输出。固定PowerShell包装器在写盘前脱敏当前令牌、token查询/片段、Bearer及常见密码/密钥字段，不落原始输出临时文件；每项截断为2048字符，每日志保留约1MiB加一份轮转。脱敏针对已知启动输出，不保证识别任意程序自行打印的所有秘密。日志与回执沿用私有`.aide-data`访问边界，拒绝链接及悬空链接。

每次只等待拥有的启动器进程，正常结束不等待常驻监视器。启动器最长5分钟；超时才终止该启动器及其子进程树，最多再等5秒。无法确认终止时不启动恢复，写入`launcher-stop-required`，暂停后续激活；确认该进程及其启动操作确已停止后，才能移除这一阻塞文件再重试。Windows5.1使用taskkill分支，PS7使用Kill(entireProcessTree)；两个平台的原生行为需分别验收。

终态先写入`pending-result.json`，API暂不可达时重新获取本机端口及令牌后重发，不再次执行同一激活。若服务器已处理回执或已有较新操作，409后查询当前operation再丢弃过期回执，不以旧结果完成新任务。此流程兼容既有RC12 API，不要求新增服务端协议；后端回归继续验证旧operation不能清除新pending。令牌轮换后轮询使用刷新后的Authorization。

`python scripts/test_windows_update_agent.py --pwsh <PowerShell路径>` 测协议与失败状态；`python scripts/test_windows_update_launcher_process.py --pwsh <PowerShell路径>` 实际启动隔离PowerShell子进程，检查退出码、异常、常驻后代、超时和日志脱敏/轮转/链接。Linux适配器只省略不支持的WindowStyle，进程与包装器真实执行；Docker/HTTP状态测试使用替身，不等于用户Windows/Docker Desktop实机验收。

原生 Windows 默认入口现由同一个 `aide-launcher.exe` 提供，BAT 不再调用 PS1。当前用户级 controller 监督同 EXE 的 port/update worker，目录/LAN 组件由 EXE 内嵌并落到私有缓存。不同后台任务可以运行，安装目录的生命周期锁串行执行端口、目录 Apply 与 A/B 切换；`host-status` 不等待该锁。它不注册 SCM 服务，不跨用户连接 Docker Desktop。

原生 A/B 的准备、激活、结果和 canonical-ID 同步写入 `.aide-data/native/update/transaction.json`。激活前持久保存旧 marker；目标失败恢复并验证旧镜像，进程终止不确认则持久阻塞，不自动回滚。回执及同步重试不重复激活；过期 operation 不覆盖新状态。下载令牌仅送入固定 loopback API 的 WinHTTP 内存请求，不进入子进程参数。ZIP 固定成员解包/迁移仍在受控 Linux 容器内使用 Python，不要求宿主安装 Python。

原生 controller 持有旧 updater 的安装锁，切换前要求旧代理退出，并接续既有 pending-result。原有 PS1 源码和测试保留为历史兼容对照，前文 PowerShell 命令不再描述默认入口。平台运行证据与交叉编译分开记录；生产事务核心测试不代替 Windows Job/ACL、Docker Desktop、真实 A/B 与数据迁移验收。具体命令和阻塞恢复边界见[原生主机说明](../../native/windows-launcher/README.md)。
