# 原生安装运行实验（不合并）

分支：`experiment/native-runtime-no-docker-20261104`，基于 `main` 的 `3db8498c`。本实验验证 Go 服务能否在宿主机直接运行，不替换正式 Docker/Compose 启动器。

## 可复现方式

宿主机安装 Go 后，在仓库执行：

```sh
scripts/build-native-runtime.sh
AIDE_WORKSPACE="$PWD" AIDE_CONTEXT="$PWD/context" scripts/run-native-runtime.sh
```

`run-native-runtime.sh` 直接执行 `build/native/aide`，不调用 Docker、Compose、不读取 `.env`，默认数据目录与 Docker 安装隔离。可用 `AIDE_NATIVE_BIN`、`AIDE_WORKSPACE`、`AIDE_CONTEXT`、`AIDE_DATA`、`AIDE_ADDR` 指定二进制和目录/监听地址。`scripts/test-native-runtime.sh` 在隐藏 Docker CLI 的 PATH 下启动临时实例，检查健康接口和工作台首页，并在退出时终止实例、清理测试数据。

## macOS arm64 安装包试验

- 使用本机 Docker 内的 Go 1.26 工具链交叉编译出 macOS arm64 Mach-O；Docker 仅用于本次构建，运行时 PATH 不含 Docker CLI。
- `scripts/package-native-macos.sh` 生成 `.pkg` 安装包和 `.zip` App；检查了 PKG payload，并从解出的 `Aide.app` 直接运行验证。安装包使用项目现有的 Mac 桌面窗口和生物认证桥，保留原生标签、关窗/重开、面板、麦克风和 WebAuthn 检查。
- 原生工作台启动成功，页面标题为 `aide · AI 开发工作台`；Go 服务 `/healthz` 返回 `status=ok`、`integrity=ok`，首页、设置、WebSocket、原生标签和媒体能力检查通过；测试使用临时独立数据、工作区和引用目录。
- App 从本机证书目录读取 TLS 证书 DER 指纹；WebKit 仅对 `localhost`、该随机端口和完全匹配的本机证书 pin 放行，不安装系统 CA。测试确认服务随窗口退出而终止。
- 本轮从仓库脚本重新生成 `.pkg`（242 MiB）与 `.zip`（250 MiB），Installer metadata 与载荷路径检查通过；从新 `.pkg` 解出的 App 在独立临时目录启动，健康状态为 `ok/integrity=ok`，服务只监听回环地址，正常退出后监听端口关闭。
- 最初的 component package 在 Installer 卷检查阶段异常退出；手工改用 `productbuild --component` 生成标准 product archive 后，经 macOS Installer 真实安装至 `/Applications/Aide.app`，安装日志为 `Install Succeeded`。安装过程由本机系统授权界面完成，未覆盖现有 `/Users/skyelan/Applications/Aide.app`。
- `.pkg` 载荷包含原生服务、Node/npm 24、可重定位 Python 3.12 和镜像内固定的 Office Python 包。对解出的安装包检查了 Node v24.19.0、npm、`python3.12 -I` 下的 python-docx/openpyxl/python-pptx/ezdxf/pypdf 导入，以及 WebView 首屏。
- `scripts/package-native-macos.sh` 已使用 `productbuild --component`；本次真实安装所用的标准 product archive 由相同方式手工构建。构建脚本尚未在本轮重新全量产包；已安装包是 arm64、本机临时 ad-hoc 签名，正式公开分发仍需 Developer ID 签名和 notarization。
- 使用全新临时工作区、引用目录和数据目录，测试后清理；未读取或迁移 Docker 安装的 `.env`、数据卷、插件、工作区和凭证。
- 试验 App 有一次在加入隔离路径参数前启动，曾写入 `~/Library/Application Support/Aide`。没有删除该路径，也没有将它用作之后的测试数据目录；其内容未做差异盘点，应由用户决定是否检查或清理。
- 用隔离的 `/tmp/aide-native-installed-smoke` 数据、工作区和引用目录启动已安装 App；窗口显示本机服务已连接，`/healthz` 返回 `status=ok`、`integrity=ok`，并确认服务仅监听 `127.0.0.1`。从 Aide 菜单正常退出后，服务进程和测试端口均关闭；原有 Aide 进程未受影响。

## Linux Debian 安装包试验

- 新增 `scripts/build-native-python-linux.sh`、`scripts/build-native-node-linux.sh` 和 `scripts/package-native-linux.sh`。脚本下载并校验 Python 3.12.16 与 Node 24.19.0，生成自带 Go 服务、Node/npm、Python Office 依赖和 Office 脚本的 Debian 包。构建阶段可用 Docker 交叉编译，安装后的运行过程不调用 Docker。
- 当前输出为 Linux `amd64` 与 `arm64` v4 `.deb`，分别约 233 MiB 与 219 MiB，声明 `libc6 >= 2.34`（Python Office wheel 的最低 glibc 要求）。ARM64 v4 在干净 Debian Bookworm arm64 容器中安装；AMD64 v4 在 Docker Desktop x86_64 仿真容器中安装。两个全新容器都确认 Docker CLI 不存在，服务 `/healthz` 返回 `ok/integrity=ok`，Node v24.19、Python Office 模块导入通过。它们仍不是实体 Linux 桌面安装验收。
- Debian 冒烟测试显式确认 Docker CLI 不存在。打包服务直接运行，健康接口为 `status=ok`、`integrity=ok`；验证了有令牌鉴权的 `/api/config`、默认浏览器启动 URL（由 `xdg-open` 测试替身截获）、Node v24、Python Office 模块，以及生成 DOCX 后读取标题/段落/表格的往返。
- 测试还占用了默认端口，确认桌面启动器只在端口属于当前实例时复用；否则选新回环端口。重复打开会复用服务，`aide-native-stop` 可停止由启动器创建的服务。中间一次端口冲突验收发现条件反转，已修正并用新的 v3 包复验通过。
- Linux 包的本机网页服务仅在明确的 `AIDE_NATIVE_LOCAL_HTTP=1` 且监听地址为 loopback 时用 HTTP，避免系统浏览器首次遇到自签名 TLS 拦截；服务拒绝将该选项用于非回环地址。首页返回 HTTP 200，不重定向 HTTPS；LAN 远程访问服务仍保持独立 TLS 路径。当前启动器调用系统默认浏览器，尚未做真实桌面浏览器/媒体权限验收。
- 新增 `nativeLoopbackHTTPEnabled` 边界单测；Go 服务针对这项行为的测试通过。修改后的同一 Go 服务再次交叉编译为 macOS arm64、Linux amd64/arm64 和 Windows amd64；Windows 编译结果仍未在真实 Windows 上执行。
- Windows amd64 服务已从相同源码交叉编译成功；此前发现的 POSIX 进程组与磁盘统计调用已抽成 Windows 实现。新增 `scripts/build-native-python-windows.sh`、`scripts/package-native-windows.sh`，组成 x64 per-user NSIS 安装链：内置 Aide Go 服务、Python 3.12.16、Node 24.19.0 和 Office 脚本。PowerShell 启动器将数据放在用户目录、只监听 loopback、健康检查后打开默认浏览器、不需要管理员权限，也不调用 Docker；安装前和卸载前都会停止本安装实例的服务。Python/Node 官方归档 SHA-256 校验通过；交叉编译 PE 和 NSIS 安装器静态生成成功，7-Zip 实际解包并核对了服务、Python、Node、启动/停止脚本、Office 脚本及卸载器，PowerShell parser 对启动/停止脚本解析通过。安装器为 181 MiB。当前没有可用 Windows 主机，因此安装、启动、升级、卸载和浏览器联调仍未在 Windows 真机验收。
- 后续检查发现 Windows 启动器没有自带 Git/SSH，且 Office 子进程使用 `python3` 而 Python 归档默认名是 `python.exe`；native 子进程环境也过滤掉了 Windows 的 `SYSTEMROOT`/`USERPROFILE` 等必要变量。已修正环境白名单和单测，并给包内 Python 增加 `python3.exe`；安装器现在附带校验过 SHA-256 的 MinGit 2.56.0.4 与 Win32-OpenSSH 10.4 preview，`PATH`/`GIT_SSH` 指向随包工具。第三版安装包目标为 `/tmp/aide-native-win64-v3/output/Aide-Native-Windows-x64-Setup.exe`；包内的服务、Node、Python、Git、SSH 客户端、`ssh-keygen` 和 Office 脚本在 7-Zip 解包后逐项检查，PowerShell 启动/停止脚本语法解析通过。新增 Windows native 环境单测和 Windows amd64 服务交叉编译通过；完整 `go test ./internal/server` 执行超过 4 分钟仍在 CPU 密集阶段，本轮终止，不能记作通过。OpenSSH 上游所发版本标为 preview；尚未在 Windows 实机验证安装、SSH、插件子进程或浏览器启动。
- 进一步核对 Win32-OpenSSH 上游功能范围后确认：Windows 客户端不支持 `ControlMaster`，而既有 Aide SSH 工作区/SFTP 主连接逻辑依赖该功能。现已为 Windows 改为逐命令建立直连，不带 `-S`、`ControlPath` 或 `ControlMaster` 参数；SSH/SFTP known_hosts 路径切到用户目录；远程命令、Git 历史、流式 SFTP 和来源浏览参数均改用平台 helper。SSH agent 管道仅传给 SSH 客户端，不进入插件/通用子进程环境。加入 Windows SSH 参数测试，连同环境测试及若干 Linux SSH 生命周期回归测试通过，Windows amd64 服务重新交叉编译通过。Windows 当前只支持引用密钥和可用的系统 SSH agent；Aide vault 中的复制密钥、SFTP 来源密钥、密码认证与密钥口令 askpass 尚未移植，且逐命令直连没有连接复用。新版安装包和 Windows 实机网络验收仍待完成。
- Windows 运行路径还发现知识来源 SFTP 子进程固定启动 `/bin/sh`；现在改用平台 shell 选择器，Windows 从 PATH 运行随 MinGit 安装的 `sh.exe`，macOS/Linux 仍用 `/bin/sh`。对应平台选择单测与 SSH 回归用例通过，Windows 服务交叉编译通过。第六版安装器 `/tmp/aide-native-win64-v6/output/Aide-Native-Windows-x64-Setup.exe`（208 MiB，SHA-256 `dac419eddf1ca4085aa2a06302f0ec495ff94e51257543753b5e76b5d9f684e2`）已生成并 7-Zip 完整解包；确认含服务、python3 别名、Node、MinGit `sh.exe`、Git、OpenSSH、SSH keygen、Office 脚本、启动/停止和卸载文件。包内服务 SHA-256 与构建产物一致；启动/停止脚本 PowerShell 解析通过。仍未在 Windows 主机执行安装与启动。
- 当前宿主 Python 3.9 只有标准库，没有 `python-docx`、`openpyxl`、`python-pptx`、`ezdxf`、`pypdf`、Pillow 或 NumPy；依赖这些模块的工具不会与容器环境等价。Node 存在，但测试没有安装或验收任一插件。

## 2026-11-05 最新源码包复验

- 从当前实验分支同一源码分别交叉构建 macOS arm64、Linux amd64、Linux arm64、Windows amd64 服务。文件格式检查均符合目标平台；Windows amd64 SHA-256 为 `fe810f5583c2740d7393d03a6522f8507523ddf24af0571860163e0bcaaa9d06`，与 Windows v6 安装器载荷中的服务一致。
- 重新生成 macOS arm64 `.pkg`（SHA-256 `490dfadc9a2a1483332094beb9476763e9971d6bba71a864ceeb5054c755de12`）和 `.zip`（`eb7930d64c90106f22e8413d357c74a9a8577b261b0f41a7f636ec3987866d1d`）。从新 `.pkg` 解出 App 后直接启动，隔离数据/工作区/引用目录；本机 HTTPS `/healthz` 返回 `ok/integrity=ok`，访问令牌鉴权配置请求成功，工作台首页 HTTP 200，服务监听回环地址。手动停止服务并退出测试进程后，监听端口关闭。系统日志出现 macOS 对 `/tmp` 解出 App 的 sandbox extension 提示，但服务启动和上述 HTTP 验收均成功；本次没有把新版本安装覆盖到 `/Applications`。
- 从当前源码新生成 Linux arm64 `.deb`：`/tmp/aide-native-linux-latest/aide-native-experiment.deb`，SHA-256 `2ec4ae3f3f256cd3cb8e854a80c3e4b74a3b5f18ddde6487a4d5c42865c4c2ea`。在干净 Debian Bookworm arm64 容器内通过 `dpkg -i` 安装；运行容器没有 Docker CLI。真实执行了桌面启动器（用 `xdg-open` 替身捕获 URL）、本机服务启动、健康与令牌鉴权、首页 HTTP 200、随包 Python Office 模块导入、DOCX 创建与文本回读；停止后确认端口关闭。该验证使用容器作为隔离 Debian 主机，不等于实体 Linux 桌面验收。
- 随后将上述旧 Git 断言更新为当前审批安全策略，并让 SSH 测试夹具建立真实 Unix socket 路径，继续验证任务固定到已认证主连接的安全要求。三个定向用例通过；完整 `go test -json ./internal/server -count=1` 再运行 551 秒，2434 项通过、125 项跳过、0 项失败。
- Windows 安装器 v6 完整解包、文件清点和 PowerShell 语法分析已在前文记录；当前服务二进制与该安装器中的二进制哈希一致。此环境没有 Windows 主机、虚拟机或 Wine，仍未实际运行安装、升级、卸载、桌面浏览器和 Windows SSH。
- 产物与测试结果仍只用于本实验分支，未合并到 `main`。当前 Linux arm64 包、macOS arm64 包以及 Windows x64 安装器包含自带 Go/Node/Python/Office 依赖，但完整插件市场、AI 会话、浏览器桥、TTS、电脑控制、隔离命令和 SSH 凭据方案未在三端逐项端到端验收；原生运行仍不具有 Docker 沙箱的安全等价性，不能据此宣称已达到功能/安全完全等价的正式发行标准。
- 为补上安装器级 Windows 验收，新增仅监听本实验分支的 `Native Runtime Experiment` GitHub Actions：Linux/macOS runner 从当前源码构建、安装并执行健康/令牌/Office/停止/无 Docker 冒烟，Windows runner 实际执行 NSIS 安装、启动、HTTP 鉴权、停止、卸载及数据保留；构建产物限期保存。工作流通过 actionlint，Linux 安装冒烟脚本已在 Debian arm64 测试容器通过，Windows PowerShell 脚本语法通过；远程 runner 尚未运行，因此 Windows 实际安装结果仍待 CI。
- 工作流复核还发现 Linux 启动器的端口探测依赖 PATH 中的 `python3` 别名，并且 Linux 打包脚本没有正确使用自定义 Python/Node 运行时输出目录；现已改为明确调用随安装包提供的 Python 3.12，同时让构建脚本尊重配置的运行时目录。重新构建的 Linux amd64 包 SHA-256 为 `213ac988f5fcbd3826daf349ea9fa098f5cc9d7527175c40426bdd36719736da`；在干净 Debian Bookworm amd64 容器内成功安装、启动、鉴权、首页访问、Node/Python Office 导入、DOCX 创建/回读、停止并确认无 Docker 调用。首次远程 CI 的 Linux 安装和 Windows runner 安装/启动/鉴权/停止/卸载/保留用户数据均通过；macOS runner 暴露了 CI 对 Node 24 补丁版的过严断言，现已改为校验 24 主版本并等待重跑。工作流通过 actionlint，PowerShell 语法检查通过。
- 2026-11-05 更新：Aide CI 的 quick gate、Go 编译/关键回归和 required gate 全部通过（run `41874455290`）。原生包工作流 run `41875022680` 三端所有必需作业均通过：Linux `.deb` 在 hosted Linux runner 安装、启动、健康/鉴权、Office 模块与 DOCX 往返、停止和无 Docker 检查通过；macOS arm64 `.pkg` 在 macOS runner 安装，Node/Python、服务 TLS 健康、鉴权、首页、停止及无 Docker 检查通过；Windows x64 NSIS 安装器在 Windows 2025 runner 实际安装、核验随包运行时、启动、健康/鉴权/首页、停止、卸载并验证用户数据保留通过。`required` gate 全绿。此前 macOS 补丁版本断言已放宽为 Node 24 主版本，重跑后闭环成功。Aide CI 另修复了星图测试 VM 夹具缺少 `cosmicLensAssignmentScene`/`Camera` 两个状态字段；该改动不涉及生产实现。本机完整 server suite 2434 通过、125 跳过、0 失败。当前新增的 `runtimeMode` 标识将经后续三端 CI 再验证。
- 为避免独立包仍在工作台标成“Docker 环境”，`/api/config` 现在按运行配置报告 `runtimeMode=native|container`，前端分别显示“本机运行”或“Docker 环境”；Linux/macOS/Windows 安装冒烟都断言 native 模式。定向 Go 测试、Node 前端语法检查、PowerShell 解析和 shell 语法检查已通过；这项新改动尚未进入刚结束的三端 runner，推送后会触发新一轮验证。

## 尚未达到“独立自主运行”的部分

浏览器和本地 sherpa 离线 TTS 工具链仍未随 App 打包；插件 daemon、消息桥、电脑/浏览器桥接、AI/TTS 以及 Office 真实文件转换尚未端到端验收。Python/Node/Git/SSH 主机工具已纳入实验安装包，但尚未验证真实市场插件的安装、启动和依赖行为。Windows SSH 的 Aide vault 密钥、SFTP 来源密钥、密码认证和安全 askpass 尚未实现；当前不能宣称 Windows SSH 与 macOS/Linux 功能对等。桌面冒烟的完整插件页断言因隔离数据目录没有 Compare/ProfessionalProject 插件而为 false；系统平台认证器检查也为 false，原生 Touch ID bridge 本身可用。

`internal/execsandbox` 只有 Linux Landlock 实现；macOS/Windows 的 restricted 命令继续 fail-closed，不会因为改成原生启动就扩大为不受限的宿主机 shell。原生运行时需先设计并验证系统级隔离，否则不能宣称与 Docker 工作台具有同等的安全能力。

Linux/Windows 安装包已经在隔离的 Debian 容器及 Windows hosted runner 做过安装 smoke，但还没有用户实体 Linux/Windows 桌面验收。尚未完成 macOS 重装/升级/卸载、真实 Linux 浏览器首启、登录后启动、全量 AI 会话、真实插件/TTS、远程访问及三端对等验收。Linux 桌面仍依赖系统默认浏览器、`xdg-open` 和 Debian 系统库。macOS 本轮已增加并实测受限 shell 沙箱；Windows `internal/execsandbox` 仍 fail-closed，restricted 宿主 shell 不会因为原生启动自动得到支持。原生包还未达到与 Docker 工作台同等的功能和安全隔离能力；本分支只用于继续实验，不替换 Docker/Compose 启动器，也未合并到 main。

## 2026-11-05 macOS 原生命令沙箱续测

- 实现 macOS `sandbox-exec` 后端并将它接入受限本地命令执行；子进程继承安装包的 PATH，Linux Landlock 同样授权包内 runtime。包启动器向服务提供原生 runtime 根目录。沙箱状态接口现在按平台报告 Linux Landlock、macOS sandbox-exec 或 Windows 不支持。
- 在真实 macOS arm64 主机编译并重新生成 PKG/ZIP；从 PKG payload 解包后直接启动包内服务，设置隔离 HOME、数据、工作区和引用目录，PATH 中没有 Docker。健康/配置接口成功，配置报告 `runtimeMode=native`、`backend=macos-sandbox-exec`、`available=true`。通过真实 `/api/command` 验证工作区写入成功、工作区同级文件读取被内核拒绝、工作区外写入被内核拒绝；服务退出后测试结束。
- Darwin execsandbox 测试二进制在本机执行通过，覆盖 workspace-write、read-only、工作区外读写、包内 runtime 和私有临时目录。Linux execsandbox 全量定向测试通过，包含包内 runtime PATH；`TestRuntimeMode` 通过。服务分别交叉编译为 macOS arm64 和 Windows amd64 成功。
- 当前三端 CI run `41877101081`：Linux package install、Windows build 和 Windows install smoke 通过；macOS package 作业由 GitHub runner 取消/abandon，required gate 因该作业未运行而失败，不能记录为 macOS CI 失败或通过。后续提交触发完整新 run。
- 后续 commit `fd19aa5a` 的三端原生包 CI run `41881694517` 全绿：macOS `.pkg` 构建、安装、TLS 服务启动/健康/鉴权/首页/无 Docker 检查通过；Linux Debian 包安装及运行 smoke 通过；Windows NSIS 安装、启动、健康/鉴权/首页、停止、卸载和用户数据保留通过；required gate success。独立 Aide CI run `41881331256` 的 quick、Go 编译/关键回归和 gate 均通过。手动 dispatch 曾被随后自动 push run 按并发策略取消；以最终 push run `41881694517` 为准。
- 注意：测试最初用 `/etc/hosts` 作为“工作区外不可读”样本，macOS profile 允许读取系统配置路径，所以该断言不适用；后续改为工作区同级私有文件，并通过真实接口确认用户文件内容无法读取。系统范围只读授权是当前运行策略的一部分，不能据此宣称完全隔离宿主机系统数据。
- Windows `workspace-write` 的 OS 级沙箱仍未实现，继续 fail-closed。Linux/macOS 沙箱当前允许网络，插件隔离未实现；不等价于 Docker 隔离。包级 TTS、浏览器/电脑控制、完整插件与远程工作区能力尚未因本轮变为已验收。

## 2026-11-05 原生电脑控制依赖续查

- 发现 `computerControlBridgeURL()` 在 `AIDE_RUNTIME_MODE=native` 时仍默认连接 `host.docker.internal:17778`，与安装包脱离 Docker 的运行方式冲突。已改为原生模式固定访问 `127.0.0.1:17778`，容器模式保持 Docker gateway；新增的三种运行模式回归测试通过。
- Linux Debian 包现复制 `computer-bridge-linux.js`，声明 X11 bridge 的 `xdotool`/ImageMagick 依赖；仅在 X11 且有 `DISPLAY` 时生成独立 256-bit bridge token、启动服务并绑定 loopback，停止器核验 pid 命令路径后关闭 bridge。无头/Wayland 不误启动。CI run `41884702473` 的 Linux 安装 smoke 检查了包内桥接文件与 Node 语法，并通过安装后的 `/api/command` 验证包内 Node 在工作区写入成功、工作区外文件读取和写入被 Landlock 拒绝。hosted Linux runner 无 X11，真实桌面截图/输入仍未验收。
- 这只是 Linux 路径的一部分：macOS 原生 `.app` 与 Windows NSIS 包目前仍未自动附带、启动各自的电脑控制 bridge；截图、点击、输入还未在三平台安装包上做实机闭环。不要据此宣称电脑控制已从容器版完全移植。

## 2026-11-05 三端安装回归

- commit `577e7491` 对应原生包 CI run `41884702473` 的三端包构建、安装/启动 smoke 与 required gate 全部通过：Debian Linux 包安装、鉴权、Office 文件、命令沙箱读写隔离和停止；macOS arm64 PKG 安装、服务 TLS/鉴权、首页、停止与无 Docker；Windows x64 NSIS 安装、服务健康/鉴权、停止、卸载和用户数据保留。run artifact 包含 macOS PKG/ZIP；Windows smoke 明确输出 PASS。
- 同一 commit 的 Aide CI run `41884702501` quick、Go 编译/关键回归和 gate 全通过。此记录不代表 Windows shell 沙箱、macOS/Linux 用户桌面浏览器、三端电脑控制、全市场插件/TTS/AI 会话具备完整原生支持。

## 2026-11-05 macOS 电脑控制桥接集成续测

- macOS 原生 `.app` 现将 `Aide Computer Bridge.app` 编入 `Contents/Resources` 并以稳定 bundle identifier 签名；Aide 启动时创建权限为 0600 的本地随机令牌文件，通过 Launch Services 启动桥接，同时把令牌传给本机 Go 服务；退出或服务启动失败会关闭桥接。桥接支持从令牌文件动态读取新值，以处理 Aide 异常退出后桥接进程仍在的重启情形。
- 原生包启动的桥接只绑定 `127.0.0.1:17778`；独立旧版桥接继续保留既有监听模式。设置页引导屏幕录制和辅助功能授权，因此截图/输入仍取决于用户授予 macOS TCC 权限。
- 本机实际重新构建 `.pkg`/`.zip`，从 ZIP 解出的整个 Aide app 通过 `codesign --verify --deep --strict`；检查了 PKG payload 存在内嵌桥接可执行文件和稳定签名标识。主 App 与桥接 Swift 源通过本机 macOS 14 target 类型检查。
- 直接从临时目录执行解包的桥接二进制被本机进程执行策略拒绝（`sandbox_extension_issue_file_to_process`），所以本轮未能完成真实 HTTP health、截图或输入操作验收；也未安装或覆盖当前 Aide。下一步需在 macOS CI/真实桌面以 Launch Services 启动来验证桥接 listener 和 TCC 流程。
- 本机首次打包还复现了共享构建目录的跨平台缓存污染：Linux ELF Python 被 macOS 当成可执行文件。本轮 macOS 与 Linux 打包脚本现校验服务/Python 的操作系统格式和 CPU 架构，默认缓存不匹配时自动重建；显式指定错误平台的自定义产物则报出清晰错误。macOS 重新打包成功；Linux runner 将在后续 CI 验证。
- 同一提交 CI run `41889384883` 已全绿：Aide quick/Go/required gate、Linux 包 smoke、Windows 安装器构建与实际安装/启动/鉴权/停止/卸载/数据保留、macOS arm64 PKG 安装及内嵌桥接签名检查全部通过。该 Windows smoke 尚未验证桌面截图/输入权限行为。

## 2026-11-05 Windows 安装包电脑控制桥接续接

- Windows NSIS 包新增 loopback-only PowerShell 桥接，随 `AideNative.ps1` 自动启动并随 stop/uninstall 关闭。它使用 `%LOCALAPPDATA%\AideNative` 下继承用户 ACL 的随机令牌文件，Aide 服务共享该令牌；启动器经授权 `/healthz` 检查 bridge，停止器仅按 PID、PowerShell 可执行文件及安装路径内的脚本命令行共同确认后结束该进程。
- 桥接实现允许应用检查、截图、Aide 已确认后的有限点击/键入/按键；截图仅限前台允许应用窗口，截图或输入前再次检查前台进程，点击坐标限制在窗口矩形。Windows 当前不提供辅助功能控件树读取，`computer_inspect` 返回空控件并说明可用截图。
- Windows 安装 smoke 已扩展为检查桥接脚本 PowerShell parser、安装包载荷、授权 healthz、缺失令牌返回 401，以及 stop 后桥接进程退出。`scripts/test_windows_native_package.py` 11/11；三端包 CI run `41891718340` 全部通过，Windows 2025 runner 的安装、桥接脚本解析、Aide/桥接健康、token 鉴权与未授权拒绝、停止、卸载、数据保留通过。runner 只验收桥接生命周期和协议，未执行桌面截图/点击/键入；System.Drawing 在无头桌面下的截图质量及真实用户交互仍需实体 Windows 验收。

## 2026-11-05 macOS Launch Services 桥接闭环

- 在当前实体 macOS arm64 主机，从本轮生成的 ZIP 解包独立临时 Aide.app，使用 `NSWorkspace.openApplication`（与主应用启动桥接的 API 相同）启动内嵌 `Aide Computer Bridge.app`，没有重启或覆盖用户当前安装的 Aide。
- 使用隔离令牌文件和端口 `17883` 验证桥接返回 healthz `200`、缺少 Bearer token 返回 `401`；热更新令牌文件后，旧令牌立即返回 `401`、新令牌返回 `200`。以 `lsof` 确认监听地址是 `127.0.0.1:17883`，结束桥接进程后确认监听端口关闭。这些检查已固化到 `scripts/test_macos_native_bridge_launch.sh`，由原生包 macOS CI 执行。
- 此主机尚未授予桥接屏幕录制和辅助功能权限；本次仅验证真实 Launch Services 启动、授权协议和 loopback 绑定，响应中两个权限状态均为 false。截图、点击、键入仍需在用户授权后用桌面应用验收。
- 当时由文档变更触发的 Aide CI run `41892954729` 尚在 queued 状态；此记录不把它计为通过。完整 CI 需以后续 run 状态为准。

## 2026-11-05 完整 macOS 原生桌面启动续测

- 从同一个临时解包 Aide.app 通过 Launch Services 启动完整桌面应用，设置独立 workspace、context、data 临时路径；确认应用自动启动包内 `aide-server` 和电脑桥接，服务 TLS healthz 正常，鉴权后的 `/api/config` 报告 `runtimeMode=native`。
- 通过 macOS `NSRunningApplication.terminate()` 请求正常退出，确认桌面进程、服务进程及桥接进程均结束。测试只写临时目录，没有覆盖或重启用户当前安装的 Aide。
- `scripts/test_macos_native_desktop_launch.sh` 已加入原生包 macOS CI，覆盖上述完整应用启动、服务鉴权、桥接启动和正常退出。这是本机真实桌面启动证据；CI 重跑仍需查看触发该测试的后续 run。
- 此次服务状态未提供文件/插件/TTS/浏览器能力完整性证明；macOS TCC 屏幕录制与辅助功能尚未授权，所以仍未验收真实截图和点击输入。
- 随后 commit `fe025102` 的原生包 run `41894891085` 三端及 required gate 全绿。macOS 日志明确出现 bridge Launch Services/auth/token-rotation/loopback/stop PASS、`runtimeMode=native`、完整 Aide.app Launch Services/service/auth/bridge PASS 和 graceful shutdown PASS；Linux package 和 Windows package/install smoke 均通过。相同提交的 Aide CI run `41894891478` 全绿。此 CI 证明干净 hosted runner 上的安装包启动与进程闭环，不证明 Windows/Linux 实体桌面交互、全插件、TTS 或 SSH 对等。
- commit `74a76c56` 为三端安装 smoke 增加同一隔离 daemon 插件的端到端启动/停止验证：测试先向各自临时 workspace 安装插件，再通过受鉴权的 Aide API 启动进程，等待 daemon IPC ready、核对 running 状态并停止。原生包 run `41897737079` 三平台及 required gate 全绿，macOS/Linux/Windows job 均明确输出 `Bundled Node plugin daemon start/IPC-ready/status/stop smoke: PASS`；Aide CI run `41897737166` 全绿。这证明各包将自带 Node 纳入真实插件 daemon 的运行 PATH，并不代表完整市场插件及外部依赖都已打包。

## 2026-11-05 Linux 原生安装后截图回归

- Linux X11 截图 bridge 的第一轮安装 smoke 曾被测试夹具阻断：`xmessage` 创建的窗口没有 `_NET_WM_PID`，`xdotool getwindowpid` 在截图请求之前退出。改用带进程 ID 元数据的 xterm 后，安装包内 bridge 成功识别 `xterm` 前台窗口并返回 PNG；run `41905062418` 三端包构建、Linux/macOS 安装、Windows 安装 smoke 和 required gate 全绿。
- 为避免“有效 PNG 头”掩盖空白或尺寸异常，又让 xterm 显示固定文字，并使用 ImageMagick 校验截图尺寸与颜色数。commit `fdb27816` 对应原生包 run `41906373799` 三端包构建、Linux/macOS 安装、Windows 安装 smoke、required gate 全通过；Linux 日志为 `X11 packaged screenshot: PASS (484x316, 2 colors, 389 PNG bytes)`。同一提交 Aide CI run `41906373881` 的 quick、Go 和 gate 全通过。
- 该截图闭环来自 Debian 安装包运行在 GitHub hosted runner 的 Xvfb/X11 虚拟桌面，验证了授权 bridge 截取指定前台应用窗口；它不是实体 Linux 桌面、Wayland、显示器组合或用户真实应用交互验收。测试用的 xterm 只安装在临时 CI runner，不进入 Aide 安装包依赖。
- 当前工作仍仅在已 push 的 `experiment/native-runtime-no-docker-20261104`，没有合并或改动 `main`。这些结果证明安装包可脱离 Docker 启动及部分插件/桌面 bridge 路径；不等于三端所有插件、TTS、AI 会话、SSH、安全沙箱、权限提示和真实桌面交互均与 Docker 版本等价。

## 2026-11-05 Windows 原生桌面截图安装验收

- Windows NSIS 安装 smoke 增加独立 WinForms 截图夹具：窗口显示固定文字并由 Windows 桥接按前台允许应用规则捕获；验收 PNG 尺寸和多个像素采样颜色，最后清理夹具窗口。
- commit `91252776` 的 Native Runtime Experiment run `41908498259` 全绿；Windows 2025 runner 从安装后的 bridge 获得 `720x420`、77 个采样颜色、13,688 字节 PNG，随后停止服务/bridge、卸载安装包并确认数据保留。该 Windows 安装 smoke 明确输出 `Windows packaged screenshot: PASS` 与 `Native Windows package install/start/auth/browser/stop/uninstall/no-Docker smoke: PASS`。
- 同一提交的 Aide CI run `41908498137` quick、Go 与 gate 全通过。此次确认 Windows hosted runner 提供了可用桌面截图环境，仍不替代实体用户桌面、不同显示缩放/多屏/远程桌面锁屏场景及点击/键入验收。
- 当前 Windows/Linux 安装包桌面截图已分别在真实 Windows runner 与 Xvfb/X11 runner 上通过；macOS 仍只验证 Launch Services 启动桥接、权限状态与 loopback 鉴权，屏幕录制/辅助功能尚未授权，故真实 macOS 截图/输入未验收。

## 2026-11-05 Windows 安装包截图闭环结果

- 上述截图扩展的验证结果已由 commit `91252776` 对应的原生包 CI run `41908498259` 实际执行：四个安装 job（Linux 包安装、macOS PKG 安装、Windows 安装器构建、Windows 安装 smoke）和 required gate 均成功。Windows 日志显示 bridge 返回的 PNG 是 `720x420`，采样 77 种像素颜色，大小 13,688 字节；完整安装 smoke 还通过浏览器首页、Node daemon 插件启动/IPC/停止、bridge 授权、停止、卸载和用户数据保留检查。
- 同一提交的 Aide CI run `41908498137` quick、Go、gate 全通过。此次桌面截图来自 GitHub `windows-2025` runner 的临时 WinForms 可见窗口，仍需留意实体 Windows 在 DPI 缩放、多屏、远程桌面锁屏/权限策略下的差异；点击和键入尚未随此 smoke 实际执行。
