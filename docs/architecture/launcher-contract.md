# 标准启动器接口（#640）

本规范落实 [issue #640](https://github.com/skyelan1999/aide/issues/640)，取代旧的“start 自动比较源码并编译”规则。接口实现、局部测试、真实平台验收和公开发行分别记录在[任务账本](../tasks/launcher-contract-640.json)。

## 命令与安装类型

macOS 使用根目录的 `名称.command`，Linux 使用 `bash 名称.sh`，Windows 使用 `名称.bat`，BAT 只调用 C++ `aide-launcher.exe`。POSIX 也可执行 `bash scripts/aide.sh 名称`。源码包由 `.git` 或 `go.mod` 与 `Dockerfile` 的组合识别；遗留 `.aide-image` 或旧构建环境变量不能把源码目录变成 Release 安装。

| 接口 | Git / 源码包 | Release 包 |
| --- | --- | --- |
| `start` | 重开健康实例；否则校验并启动已有镜像，缺镜像提示先 build | 重开健康实例；否则校验/导入随包镜像并启动 |
| `stop` | 通知已连接页面清空、取消任务，停止自托管插件、已登记宿主组件及本安装容器 | 相同 |
| `build` | 增量编排运行镜像及所需宿主组件，校验成功后自动应用并启动 | 不分发 |
| `clean` | 清除本安装构建缓存和可移除的 Docker 镜像，再调用 build | 不分发 |
| `docker_clean` | 停止并移除本安装容器、网络、可移除镜像及专用构建器 | 相同；离线归档保留供再次导入 |
| `verify` | 诊断版本、Docker、Compose、配置、运行健康和启动日志位置 | 相同 |
| `release` | 核对当前版本/主分支/tag/发布说明与 gate，分派现有交付 workflow | 不分发 |

`start` 永不编译代码或镜像；`AIDE_BUILD_FROM_SOURCE=1`、`AIDE_FORCE_REBUILD=1` 也不改变这一点。健康实例的普通重复 start 不重新创建容器或刷新后台组件。显式 build 完成后的内部启动路径则必须应用新镜像，不能只打开旧实例。源码更新流程为 `git pull` → `build` → 自动启动；失败不切换成公开 Release 来掩盖构建错误。

Mac 源码版的显式 `build` 同步编译安装 WKWebView 桌面宿主；日常 `start` 优先打开本安装已编译的 `Aide.app`，不存在时保留浏览器入口。独立的 `desktop.command` 可只构建安装 Mac 窗口，不重编译后台镜像。参见 [macOS 独立工作台](macos-desktop.md)。

### POSIX 启动就绪等待（#981）

macOS/Linux 的构建后启动、普通启动和 Mac 桌面启动，等待正在运行的本安装实例完成初始化，默认就绪预算为 600 秒。容器 HTTP 健康响应和宿主 HTTPS/实例身份核对共用一个截止时间；每次网络探测有连接及请求超时，每 15 秒显示等待阶段与已用时间，就绪后自动继续打开工作台，无需手动重试。可在启动环境设置 `AIDE_STARTUP_TIMEOUT=1..1800`（秒）；构建协调器与 Mac 桌面外层调用保留相应等待余量。

初始化期间 Docker 暂时标记 `unhealthy` 不等于服务进程退出；进程仍运行时继续等待。容器退出、重启或归属不符会停止等待，宿主响应身份不符拒绝打开。真正到达截止时间才报告等待超时并保留实例和数据；普通重复 start 不重建正在初始化的容器。显式 Release 激活继续保留既有失败回退规则。Windows 原生启动器有独立流程，本次未修改。

### 大会话库启动性能（#982）

未启用按需加载时，启动归位、数据完整性检查、会话加载与首次镜像升级确认，共用仅限本次启动的会话解析结果。每次仍读取当前完整文件并计算 SHA256；只有字节摘要完全相同时才复用已完成的完整类型解析，文件变化后重新解析，不使用文件名、长度或修改时间代替内容校验。程序二进制完整性仍每次重新读取计算。首次就绪健康响应后释放启动缓存，后续周期巡检保持原有独立验证路径。

加载后仅在中断任务、停止意图或审批状态实际需要恢复时保存会话；未变化的历史保持原字节与修改时间。编号修复、归档桶迁移、回滚及小秘结果唤醒仍按原规则执行。服务日志的 `startup:` 记录各阶段耗时、会话数量、恢复写入数量及解析复用次数，只输出汇总信息，不包含会话正文或凭据。性能验收见 `docs/tasks/startup-performance-20261122.json`。

### 会话历史按需加载（#983）

标准 Compose 安装默认启用 `AIDE_LAZY_SESSIONS=1`。启动先从可再生目录 `cache/session-catalogue-v1` 读取经过本机访问令牌 HMAC 认证的会话与任务摘要，核对原文件尺寸、修改时间和会话身份，列表不加载完整正文。打开或操作某个会话时，重新读取并完整解析原始文件，核对读取期间的文件身份，恢复该会话的中断/停止状态后再提供历史或写入。多个 HTTP 请求合并读取同一会话，磁盘读取期间不持有全局会话锁；内部工具的同步读取也有完整加载保护。

索引不是权威历史。索引缺失、过期、签名不符或版本不匹配时，完整读取原件并重建索引；旧会话库首次迁移需要一次全量解析，之后启动只读摘要。未加载正文的对象在保存入口被拒绝，不能用空消息或摘要覆盖原件。归档、编号修复与恢复写入先读取完整会话；全文搜索和导出会按请求读取涉及的历史。原始 JSON 保留，索引可以重新生成。

按需模式的历史正文校验被明确标记为 `integrity=pending`，报告含 `deferredSessions`；这不等同于全部历史已经检查完成。程序二进制、配置与目录仍在启动和周期检查中验证，索引匹配的旧正文在真正读取时校验，发生变化的文件回退完整检查。可在启动环境或 `.env` 设置 `AIDE_LAZY_SESSIONS=0` 回到完整加载；不经 Compose 的单独二进制保持兼容路径。验收和一次性迁移耗时记录在 `docs/tasks/lazy-session-startup-20261122.json`。

## 构建与资源归属

Windows 源码入口修复见 [#646](https://github.com/skyelan1999/aide/issues/646)。根目录 BAT 经统一 CMD 入口调用原生 EXE，并显示操作和退出码；通过双击（`cmd /c`）启动时会停留等待确认，自动化设置 `AIDE_LAUNCHER_NO_PAUSE=1` 可禁用停留，仍保留真实退出码。

源码更新后运行 `build.bat`，不必手动删除 EXE。显式 `build/clean` 总是从当前源码编译原生启动器，在替换旧 EXE 前会关闭本安装桌面并等待其登记的 PID/创建时间对应进程退出，同时确认后台控制器停止，然后执行运行镜像构建。旧桌面仍在启动时也会等待窗口出现并发送关闭请求；超时或未保存确认未处理时保留旧 EXE 和数据，不强制终止进程，也不转用 Release。普通 `start/stop/verify` 等入口检测旧版启动器并提示先 build；`start` 不编译。旧编译收据不再使 build/clean 回到旧 EXE；普通委派也会检查原生命令版本。

活动根目录及 `scripts/` 中的 PS1 已迁入 `scripts/fixtures/legacy-windows/`，仅供历史兼容回归，不进入 Docker 上下文、Release 包或源码构建身份。当前 Git 源码的 `.bat`、原生 EXE 和新打包入口都不调用 PowerShell。原生失败信息写入 `.aide-data/native/launcher.log`；启动器尚未编译或 Docker bootstrap 失败时，CMD 窗口显示实际错误和失败退出码。

源码 build 需要 Git、Docker Compose、Buildx；POSIX 协调器还需要 Bash/Python 3。Windows 首次 `build.bat` 通过 Docker 的 `native-launcher` 目标生成引导 EXE，再由原生启动器编排当前运行镜像和当前原生宿主构建；宿主无需安装 PowerShell、Python 或 C++ 编译器。当前编译产物使用校验过的不可变 `launcher-<sha256>.exe`，入口按本安装收据转交，避免覆盖正在执行的 EXE。

每个安装目录拥有独立的 `aide-<目录摘要>` docker-container Buildx 构建器、`aide:source-<目录摘要>` 镜像和 `.aide-data/build/owner.json` 归属收据。已有同名构建器但无匹配收据时拒绝接管。构建器限制为 4 GiB / 2 核；增量层和 cache mount 留在该构建器中。冷启动需要获取 BuildKit、基础镜像、固定工具链及依赖；默认 Docker 引擎已有基础镜像 tag 不代表专用构建器已有离线缓存。无网络的机器优先使用完整 Release 包，或预先准备该专用构建环境。

### macOS / Linux 构建前网络选源

POSIX 源码 `build` 首先用宿主 Node.js/npm 编译 TypeScript（CI 使用 Node.js 24），首次执行按锁文件安装依赖，再运行 `npm run build:ts`。预构建运行包的 `start` 不做这一步。源码镜像保留 Go CLI 用于命令面板；POSIX、Windows 原生启动器和隔离 selfdev A/B 在应用新构建前须核对 `source` 镜像标签、源码指纹和安装归属；启动阶段只有源码检出可加载 `source`，发行包和更新包不会将它当成发行镜像。发行镜像仍须为 `slim`，不包含 Go SDK；需开发工具时使用独立 SDK。Linux 的 Codex Bridge 在本地 Docker 网桥上使用受认证的管理请求；停止失败仍须显式报错。Linux 干净安装验收仅显式引入这两个工具，不继承宿主的完整环境。

源码构建在计算指纹、创建构建器及下载依赖之前，并发检测 Docker 标准源与 DaoCloud 镜像、PyPI 与清华镜像、Debian 与清华镜像。Docker 检测包含匿名令牌与清单读取；APT、PyPI 检查有效的源响应。标准源可用时优先使用标准源，失败而国内源可用时直接预选国内源。设置 `AIDE_BUILD_SOURCE=standard` 或 `china` 可明确指定，默认 `auto`。探测结果写入本安装 `.aide-data/build/network.json`，不包含凭据。

存在可用的本地 Python 运行镜像时，包源另在 Docker 网络中探测，以容器结果决定初始包源；冷机器无可用镜像时明确报告仅有宿主探测结果。Docker 引擎和 BuildKit 的实际拉取继续校验镜像；探测成功不证明所有镜像层或依赖均可下载。两端均未探测成功时保留有效原配置并报告，后续失败恢复仍适用。私有源、额外索引、离线安装和镜像摘要继续遵循各自配置。Windows 原生宿主目前沿用下述失败恢复流程。

macOS 启动或重新打开 Aide 时，会唤起已安装的 `com.skyelan.aide.pp-offline-license` 用户服务。服务启动使用其既有安装配置，不强制重启正在运行的核心。准备容器时，仅将已存在的标准 `bridge` 目录只读映射至 `/run/pp-license`，不映射设备状态目录；健康实例的普通重开不重建容器，新映射需在正常应用镜像时生效。缺失或失败的组件会给出提示，不阻止工作台启动。PP 商业插件需要另外安装本机授权组件，Aide 源码编译不包含私有核心和用户授权。

### 构建失败后的中国大陆源恢复

`build.command` / `build.sh`、Windows 原生源码构建及首次 `build.bat` 引导默认启用 `AIDE_CN_SOURCE_FALLBACK=1`。先使用当前配置；识别到下载失败后，按依赖类型选择备用源，保留已指定版本、镜像摘要和构建平台：

| 下载对象 | 自动恢复 | 次数上限 |
| --- | --- | --- |
| Docker Hub 官方 Python / Node / Go 基础镜像 | 从实际 Dockerfile 默认值或有效 build args 提取失败引用，改用 DaoCloud 对应 `library/` 路径 | 每个基础镜像一次，整个构建最多三次换源重试 |
| 在线 PyPI 公共索引 | 当前索引失败后尝试清华 TUNA、华为云；不增加 extra-index | 含首次安装共三次 |
| Debian APT | 容器内标准 Debian 源失败后尝试清华 TUNA、阿里云；兼容 `.list` 和 DEB822 `.sources`，保留 suites、组件及 Signed-By | 含首次安装共三次 |
| GNU LibreDWG 源码归档 | GNU 官方下载发生连接/传输/HTTP 错误后，尝试清华 GNU 镜像的同一路径 | 一次换源；仍强制原 SHA256 校验 |

Docker 映射只覆盖 `PYTHON_BASE`、`NODE_BASE`、`GO_BASE` 中的官方公共镜像。缺少本地 `aide:dependencies-*`、私有镜像或自定义仓库时，不替换成其他产品或版本。代码编译错误、磁盘错误不会触发基础镜像换源。PyPI 私有索引、extra-index 和 `--no-index` 离线安装保持原选择；APT 不改写其他域名的自定义源。所有请求继续使用 HTTPS 与既有包/镜像校验，不关闭证书或签名验证。

POSIX 与原生 Windows 构建在镜像身份核验通过后才沿用既有启动流程；重试失败保留原 `.env` 镜像选择。不会修改宿主 Docker daemon、系统全局源、注册表或用户代理设置，也不会重启 Docker。POSIX 实时显示 BuildKit 输出，只保留有限大小的诊断尾部；取消或超时会终止其进程组。首次 Windows 引导复用其原有 Windows PowerShell 前提；已安装的原生构建器直接在 C++ 中匹配，不调用宿主 Python/PowerShell。

需要禁用自动换源时，在执行构建的环境中设置 `AIDE_CN_SOURCE_FALLBACK=0`；标准 Compose 构建还可从 `.env` 读取此值。首次 Windows 引导发生在 Compose/native host 前，应使用当前 CMD 环境变量。直接 `docker build` 则传 `--build-arg AIDE_CN_SOURCE_FALLBACK=0`，仅控制容器内下载包装器；基础镜像恢复由 Aide 启动器负责。

BuildKit 自身启动镜像、GitHub Codex Bridge 源码、WebView/Windows SDK 和可选 sherpa 下载沿用原有缓存、固定摘要和错误处理，本次没有把任意第三方代理加入其下载链。镜像服务可能受网络、同步延迟和限流影响；换源失败仍明确报错，不能保证任意网络均能构建。

来源与路径按维护方文档核对：[DaoCloud](https://github.com/DaoCloud/public-image-mirror)、[TUNA PyPI](https://mirrors.tuna.tsinghua.edu.cn/help/pypi/)、[TUNA Debian](https://mirrors.tuna.tsinghua.edu.cn/help/debian/)、[华为 PyPI](https://support.huaweicloud.com/mirrors_faq/mirrors_faq_0010.html)、[阿里云 Debian](https://developer.aliyun.com/mirror/debian)。实际检查和未验证范围见[任务记录](../tasks/build-cn-source-fallback-20261115.json)。

### POSIX 构建自动回收（#745）

macOS `build.command` 与 Linux `build.sh` 在镜像身份校验、应用及健康检查成功后（Docker 状态为 starting 时最多等候 30 秒），自动回收本安装不再被容器引用的过时 `source` 运行镜像及旧版 `slim` 运行镜像。归属标签逐项复核，移除过时 tag 后按完整 ID 处理遗留的无 tag 镜像，不使用强制镜像删除。保留当前运行镜像、任何运行中或已退出容器仍引用的镜像、其他安装镜像、构建参数指定的依赖镜像、`dependencies/dependency-seed/sdk-seed` 及 toolbox 编译/验收工具镜像；不停止容器、不删除数据卷或工作区。

随后仅对有匹配归属收据的专用构建器执行 `type=regular`、`until=24h`、`--max-used-space 20GB` 的定向缓存回收。保留最近 24 小时使用的增量层以及所有编译/下载 cache mount；因此 20GB 是回收目标，不是缓存总量的硬上限。使用 Docker 默认构建器时跳过缓存回收，避免清理共享构建器。Docker 过滤及空间策略见 [Buildx prune](https://docs.docker.com/reference/cli/docker/buildx/prune/)。

低磁盘空间预检首次失败时，若现有本安装实例健康，会先尝试上述定向回收，再按原 20 GiB 门槛复查；仍不足时停止，不降低保护阈值。新镜像编译或启动失败时不执行构建后的回收。回收失败会显示独立警告，健康的新实例继续运行；成功结果记录在 `.aide-data/build/recycle-last.json`。成功交接后过时镜像会被删除，需要历史源码回滚时应重新编译。

本轮只接入 POSIX 标准源码构建协调器；Windows 原生启动器和隔离 selfdev A/B 流程沿用各自既有生命周期，未接入这项自动回收。

### Windows 源码构建代理（#649）

`build.bat` / `clean.bat` 在编译第一个 EXE 前读取当前用户的 Windows 手动系统代理；同一次调用中的 Docker bootstrap 和后续原生源码构建继承相同的进程环境。直接执行原生 EXE 的 `build/clean` 时也会读取当前用户设置。宿主仍无需 PowerShell、Python 或额外编译器。

已有非空 `HTTP_PROXY`、`HTTPS_PROXY` 或 `ALL_PROXY` 时，保持整组显式代理配置，不再混入系统代理；`NO_PROXY` 始终优先保留。没有显式代理时，支持单一 HTTP/HTTPS 代理地址、`http=...;https=...` 分协议地址以及方括号 IPv6 地址，不硬编码端口。系统代理的等价绕过规则转换为 `NO_PROXY`；`<local>` 只映射本机回环地址，短主机名及不兼容通配规则会提示显式设置 `NO_PROXY`。凭据、复杂地址或无法识别的手动格式应使用显式环境变量，诊断不输出代理值。

只有 PAC URL 而没有手动地址时，入口明确提示配置显式代理。构建客户端不执行 PAC/WPAD；无手动代理且无 PAC URL 时继续使用现有 Docker Desktop 网络设置，自动发现代理的环境可用以下方式指定地址。把 `HOST:PORT` 换成自己的实际 HTTP 代理地址和端口，在同一个 CMD 中运行：

```bat
set "HTTP_PROXY=http://HOST:PORT"
set "HTTPS_PROXY=http://HOST:PORT"
call build.bat
```

自动读取仅影响本次构建进程及其子进程；不执行 `setx`、不修改注册表、Docker 全局设置或镜像中的 `ENV`。`--dry-run` 不查询系统代理、不联网、不创建构建状态；普通 `start/stop/verify` 不因系统 PAC 设置而被构建代理检查阻止。

这里处理的是 **Windows Docker CLI / Buildx 客户端** 的认证请求。`docker pull` 成功不能证明 Buildx 的令牌请求可用。BuildKit 容器与 Dockerfile `RUN` 联网另由 Docker Desktop 容器代理、已有 Docker 配置或构建参数负责；入口不将宿主机 `127.0.0.1` 地址原样注入 Linux 容器，也不改动已有专用构建器的代理环境和缓存。需要容器访问宿主服务时，按 Docker Desktop 网络配置使用可达地址（例如 `host.docker.internal`），并检查代理的监听范围。认证/连接超时会提示检查代理是否运行、`HTTPS_PROXY` / `NO_PROXY` 以及容器代理。

代理作用域见 [Docker Desktop networking](https://docs.docker.com/desktop/features/networking/)、[Docker CLI proxy](https://docs.docker.com/engine/cli/proxy/) 和 [container-to-host networking](https://docs.docker.com/desktop/features/networking/networking-how-tos/)。本问题的实际测试及受影响 Windows 完整构建状态见[任务账本](../tasks/issue-649-windows-build-proxy.json)。

实际 build 结果先核对镜像 ID、源码指纹和本安装标签，再更新 `.env` 的镜像选择。构建失败保留此前启动配置。Windows 还核对当前宿主产物后才选择新运行镜像。编译和临时提取容器属于构建流程，不属于普通 start。

Docker 的专用缓存与驱动行为见 [docker-container driver](https://docs.docker.com/build/builders/drivers/docker-container/)；移除专用构建器会移除其缓存，本接口不使用 `--keep-state`，见 [buildx rm](https://docs.docker.com/reference/cli/docker/buildx/rm/)。

### 宿主消息桥与服务镜像兼容边界

局域网消息桥由宿主启动器管理，端口 `45840`（消息）和 `45841`（消息采集）必须绑定到 loopback；`45842` 是可选的、受密码保护的工作台远程访问转发入口，也只能绑定 loopback。`-remote-target` 能力或 `45842` 映射缺失时，启动器必须明确记录远程访问不可用，同时保留消息桥和工作台其他功能；不能把可选远程访问故障升级为消息服务整体启动失败，也不能静默显示为已就绪。

macOS/Linux 从正在运行的不可变镜像提取并校验对应 helper；Windows 将 Go helper 嵌入原生 EXE，并按 helper 摘要刷新安装缓存。Windows launcher/helper 与服务镜像是分别构建和更新的产物，跨版本协议变更必须保持兼容或显式协商；能力探测不能替代后端 API 协议兼容。旧 PowerShell helper 仅在 `scripts/fixtures/legacy-windows/` 中用于回归，不构成当前发布路径。跨版本与真实 Windows 网络验收边界记录在[远程访问任务](../tasks/remote-access-win-launcher-20261104.json)和[兼容审计任务](../tasks/launcher-compat-audit-20261104.json)。

当前协议清单通过 helper `-capabilities` 返回 `protocol`、`message`、`capture`、`remoteAccess`；服务 `/healthz` 公布 `messageBridgeProtocol` 与 `remoteAccessProtocol`。Go 服务/helper 共用协议常量，Windows launcher 构建时从同一源码生成 C++ 头文件常量。POSIX 与 Windows launcher 都在传递可选 `-remote-target` 前比较版本，并验证 `45842` 只映射到 loopback。旧 helper 可由帮助输出兼容探测，旧服务或不匹配时只停用远程访问并保留消息桥；替换 POSIX helper 前先完成探测。协议版本改变必须同步更新服务、helper 与 launcher 并加入组合测试。

Release A/B 更新通过 `AIDE_FORCE_IMAGE_ACTIVATION=1` 明确绕过健康旧实例复用；普通 `start` 仍只重开当前健康实例。启动器检查实际运行容器镜像 ID 与目标槽一致并通过健康检查后才算激活成功；更新代理等待相同镜像身份，否则回滚旧槽。此开关仅允许 `start-bundle` 更新路径使用。

## 干净环境验收（#654）

POSIX 构建协调器把实际项目路径作为独立、带引号的参数传入磁盘检查子 shell，不依赖调用者导出 `PROJECT_DIR`。资源检查也支持显式路径和当前目录回退；低磁盘空间和无法识别的磁盘报告仍拒绝继续。在线 Python 依赖安装发生中途下载失败时，保留原版本与其余参数最多重试三次完整安装，并按上述公共索引规则选择备用源；离线安装不改为联网。

`scripts/test_launcher_clean_environment.py` 是显式启用的真实 Docker 验收工具。它要求已提交、没有运行数据的独立源码 checkout，并为 HOME、Docker 客户端配置、工作区/资料/本地目录、应用数据、项目名、端口、镜像和 Buildx 缓存建立新状态。宿主 Docker 引擎和已安装 CLI 属于测试前提；工具不会清空共享引擎或复制原安装的数据、代理环境及模型凭据。

```sh
python3 scripts/test_launcher_clean_environment.py \
  --root "/path/to/fresh source checkout" \
  --evidence "/path/to/private-evidence" \
  --allow-real-build
```

验收实际执行七个接口：缺镜像时 start 拒绝、首次冷 build、健康实例重开、运行/停止状态 verify、stop、删除专用缓存后的 clean 冷重建、保留数据的 docker_clean、清理后再次拒绝启动。HTTPS 健康、HTML 与带令牌的只读 API 均通过真实服务请求检查。release 在非 main 的验收 checkout 中验证真实拒绝门禁，不创建 tag 或分派发行。

每组记录包含 17 次入口调用、两次冷构建、数据/工作区哨兵与退出码，输出为私有目录中的 `result.json` 和逐项日志。Linux CI 的 `launcher` job 是汇总 gate 的必需依赖，使用全新 checkout 并显式选择已有的 PyPI 索引配置；macOS 使用仓库默认索引在本机实测。Windows SDK/CMD 夹具回归与 Windows Docker Desktop 完整实机验收分别记录。完整证据和限制见 [#654 验收记录](../reviews/2026-10-31-launcher-clean-acceptance-654.md)。

## 停止、隐私与清理

停止先发出终止 SSE 事件，页面同步清空标题和文档，再尝试关闭标签；浏览器拒绝脚本关闭用户标签时转为 `about:blank`。清理连接、任务流、语音和本地登录令牌，不删除草稿或用户设置。只能保证收到停止事件的在线页面执行此流程；断网、旧版页面、浏览器崩溃或强制杀进程不能被声明为已关页。此机制不删除浏览器历史记录。

服务端停止拒绝新工作、取消执行、停止插件守护进程并等待退出。宿主 stop 停止当前目录登记的桥接/后台组件和全部同根 Compose 服务，保留手动停止标记，避免监视器立即重启。组件未确认退出则报告失败，继续尝试停止其余组件。

`docker_clean` / `clean` 不执行全局 prune、强制删共享镜像或 `down -v`。容器/项目必须能追溯到当前目录；混入外部安装时拒绝清理。镜像仍被其他容器使用或存在其他引用则保留并报告。`.env`、`.aide-data` 的用户存储、工作区、会话、命名数据卷及离线镜像归档保留。不要把“Docker 环境清理”理解为清空 Docker Desktop 或删除用户数据。

旧 `clean-docker-cache.*` 是历史兼容工具，仍可能清理默认构建器的共享缓存；不属于此规范，也不随新 Release 标准入口分发。日常维护使用 `docker_clean` / `clean`。

## 诊断、升级与发布

`verify` 不构建、不拉镜像、不启动服务；未运行或故障返回非零并报告检查结果。POSIX 输出最近启动日志位置，Windows 输出原生日志位置。启动失败应先保留配置、数据和归属收据，再根据诊断恢复。

源码安装通过 `AIDE_INSTALLATION_KIND=source` 禁用应用 A/B 安装/激活和宿主更新 worker，即使当前镜像来自 Release。页面仍可查看公开 Release 信息。Release 保留既有槽位和共享数据的健康验证/回滚协议。

`release` 复用 `scripts/version.sh` 的现有版本规则，不自行发明版本号。准备干净 main、指向 HEAD 的已推送版本 tag，以及 `docs/releases/<tag>.md` 的完整发布说明；当前远端 main/tag 必须一致且 required gate 成功。命令分派既有 `aide-delivery.yml`，后续仍执行原交付中的架构构建、发布说明、发布节奏和环境审批。分派成功不等于已发布。实现该入口本身不授权升版、推送 tag、公开发行或部署。

## Selected directory object bridge (#773)

Windows can refuse renaming an opened directory or its parent while the bridge owns the handle. Tests distinguish this from successful pathname replacement: the same operation must succeed after closing the retained root. Remove the selection and normally apply/stop, then confirm the bridge has exited before changing the host directory name. This is separate from complete Windows Docker Desktop/removable-drive acceptance.

Selected directories retain their `/mounts/<id>` paths. The native helper holds each directory with `os.Root` and exposes that object through a loopback-only NFSv3 service and Docker local named volume. Selected host pathnames are no longer passed to Docker as bind sources. Replacing a directory or its parent after preparation leaves the current export on the original object. A subsequent preparation rejects a changed identity until the directory is deliberately removed and selected again; unavailable drives retain the last confirmed identity. Existing global workspace/installation mounts are outside this change.

The native helper and launcher must be updated together. The Windows resident host launches an immutable helper copy and checks its preparation receipt. Workers remain while Docker uses their volumes; retired generations stop only when those owned volumes are unused. Preserve configuration and identity receipts for recovery. `active.json` has no export secrets and uses mode 0644 for the container UID; its private host parent is 0700. Export keys and other bridge receipts remain private.

RO is enforced by the host server. Writes synchronize before stable acknowledgment; bounded soft NFS timeouts mean helper failure or host suspension can cause I/O errors that require rereading before assuming a save. `nolock` does not coordinate native-host database writers: stop concurrent writers before opening the same database through the bridge. Special files and foreign ownership changes are refused. See the [source and isolated acceptance record](../reviews/2026-11-12-directory-object-bridge/README.md) for tested scope and hardware limits.


### 认证 Python 包索引

`PIP_INDEX_URL` 和 Docker/Compose 构建参数只接收无凭据的索引，用于公开镜像等场景。认证索引请把完整 HTTPS URL 单独写在构建目录外只有自己可读的文件中，把文件路径（不是 URL）设置为宿主环境变量 `AIDE_PIP_INDEX_FILE`，然后运行源码构建入口。不要把认证 URL 写进仓库、`.env` 或命令行。
启动器通过 BuildKit secret `aide_pip_index` 传入文件，不将 URL 作为构建参数。认证安装不输出原始 pip 日志、不切换镜像、不使用公共 pip 缓存，也不能和 `PIP_OFFLINE=1` 一起启用。手动调用 Buildx 时使用 `--secret id=aide_pip_index,src=/path/outside/context`、`--build-arg AIDE_PRIVATE_PIP=1` 和 `--no-cache-filter dev,build,runtime-base`，不要把认证 URL 填到 `--build-arg PIP_INDEX_URL=...`。
