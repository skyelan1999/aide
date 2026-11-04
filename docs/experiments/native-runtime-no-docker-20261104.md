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
- 最初的 component package 在 Installer 卷检查阶段异常退出；手工改用 `productbuild --component` 生成标准 product archive 后，经 macOS Installer 真实安装至 `/Applications/Aide.app`，安装日志为 `Install Succeeded`。安装过程由本机系统授权界面完成，未覆盖现有 `/Users/skyelan/Applications/Aide.app`。
- `.pkg` 载荷包含原生服务、Node/npm 24、可重定位 Python 3.12 和镜像内固定的 Office Python 包。对解出的安装包检查了 Node v24.19.0、npm、`python3.12 -I` 下的 python-docx/openpyxl/python-pptx/ezdxf/pypdf 导入，以及 WebView 首屏。
- `scripts/package-native-macos.sh` 已使用 `productbuild --component`；本次真实安装所用的标准 product archive 由相同方式手工构建。构建脚本尚未在本轮重新全量产包；已安装包是 arm64、本机临时 ad-hoc 签名，正式公开分发仍需 Developer ID 签名和 notarization。
- 使用全新临时工作区、引用目录和数据目录，测试后清理；未读取或迁移 Docker 安装的 `.env`、数据卷、插件、工作区和凭证。
- 试验 App 有一次在加入隔离路径参数前启动，曾写入 `~/Library/Application Support/Aide`。没有删除该路径，也没有将它用作之后的测试数据目录；其内容未做差异盘点，应由用户决定是否检查或清理。
- 用隔离的 `/tmp/aide-native-installed-smoke` 数据、工作区和引用目录启动已安装 App；窗口显示本机服务已连接，`/healthz` 返回 `status=ok`、`integrity=ok`，并确认服务仅监听 `127.0.0.1`。从 Aide 菜单正常退出后，服务进程和测试端口均关闭；原有 Aide 进程未受影响。

## Linux Debian 安装包试验

- 新增 `scripts/build-native-python-linux.sh`、`scripts/build-native-node-linux.sh` 和 `scripts/package-native-linux.sh`。脚本下载并校验 Python 3.12.16 与 Node 24.19.0，生成自带 Go 服务、Node/npm、Python Office 依赖和 Office 脚本的 Debian 包。构建阶段可用 Docker 交叉编译，安装后的运行过程不调用 Docker。
- 当前输出为 Linux `amd64` 与 `arm64` v4 `.deb`，分别约 233 MiB 与 219 MiB，声明 `libc6 >= 2.34`（Python Office wheel 的最低 glibc 要求）。ARM64 v3 载荷在干净 Debian Bookworm 容器中首次 `dpkg -i` 安装；AMD64 v3 载荷在干净容器中由 v2 升级安装。v4 只新增了 Debian 依赖下限元数据，已验证包元数据，尚未重新安装 v4。两者都不是实体 Linux 桌面安装验收。
- Debian 冒烟测试显式确认 Docker CLI 不存在。打包服务直接运行，健康接口为 `status=ok`、`integrity=ok`；验证了有令牌鉴权的 `/api/config`、默认浏览器启动 URL（由 `xdg-open` 测试替身截获）、Node v24、Python Office 模块，以及生成 DOCX 后读取标题/段落/表格的往返。
- 测试还占用了默认端口，确认桌面启动器只在端口属于当前实例时复用；否则选新回环端口。重复打开会复用服务，`aide-native-stop` 可停止由启动器创建的服务。中间一次端口冲突验收发现条件反转，已修正并用新的 v3 包复验通过。
- Linux 包的本机网页服务仅在明确的 `AIDE_NATIVE_LOCAL_HTTP=1` 且监听地址为 loopback 时用 HTTP，避免系统浏览器首次遇到自签名 TLS 拦截；服务拒绝将该选项用于非回环地址。首页返回 HTTP 200，不重定向 HTTPS；LAN 远程访问服务仍保持独立 TLS 路径。当前启动器调用系统默认浏览器，尚未做真实桌面浏览器/媒体权限验收。
- 新增 `nativeLoopbackHTTPEnabled` 边界单测；Go 服务针对这项行为的测试通过。修改后的同一 Go 服务再次交叉编译为 macOS arm64、Linux amd64/arm64 和 Windows amd64；Windows 编译结果仍未在真实 Windows 上执行。
- Windows amd64 服务已从相同源码交叉编译成功；此前发现的 POSIX 进程组与磁盘统计调用已抽成 Windows 实现。新增 `scripts/build-native-python-windows.sh`、`scripts/package-native-windows.sh`，组成 x64 per-user NSIS 安装链：内置 Aide Go 服务、Python 3.12.16、Node 24.19.0 和 Office 脚本。PowerShell 启动器将数据放在用户目录、只监听 loopback、健康检查后打开默认浏览器、不需要管理员权限，也不调用 Docker；安装前和卸载前都会停止本安装实例的服务。Python/Node 官方归档 SHA-256 校验通过；交叉编译 PE 和 NSIS 安装器静态生成成功，7-Zip 实际解包并核对了服务、Python、Node、启动/停止脚本、Office 脚本及卸载器。安装器为 181 MiB。当前没有可用 Windows 主机，因此安装、启动、升级、卸载和浏览器联调仍未在 Windows 真机验收。
- 当前宿主 Python 3.9 只有标准库，没有 `python-docx`、`openpyxl`、`python-pptx`、`ezdxf`、`pypdf`、Pillow 或 NumPy；依赖这些模块的工具不会与容器环境等价。Node 存在，但测试没有安装或验收任一插件。

## 尚未达到“独立自主运行”的部分

浏览器和本地 sherpa 离线 TTS 工具链仍未随 App 打包；插件 daemon、消息桥、电脑/浏览器桥接、AI/TTS 以及 Office 真实文件转换尚未端到端验收。Python/Node 主进程运行时已随实验包提供，但尚未验证真实市场插件的安装、启动和依赖行为。桌面冒烟的完整插件页断言因隔离数据目录没有 Compare/ProfessionalProject 插件而为 false；系统平台认证器检查也为 false，原生 Touch ID bridge 本身可用。

`internal/execsandbox` 只有 Linux Landlock 实现；macOS/Windows 的 restricted 命令继续 fail-closed，不会因为改成原生启动就扩大为不受限的宿主机 shell。原生运行时需先设计并验证系统级隔离，否则不能宣称与 Docker 工作台具有同等的安全能力。

Linux/Windows 安装包尚未在真实用户主机验证；Windows 目前只有 NSIS 生成结果和交叉编译证据。尚未完成 macOS 重装/升级/卸载、真实 Linux 浏览器首启、Windows 安装启动卸载、登录后启动、全量 AI 会话、真实插件/TTS、远程访问及三端对等验收。Linux 桌面仍依赖系统默认浏览器、`xdg-open` 和 Debian 系统库。`internal/execsandbox` 的 macOS/Windows fail-closed 限制仍存在，restricted 宿主 shell 不会因为原生启动自动得到支持。原生包还未达到与 Docker 工作台同等的功能和安全隔离能力；本分支只用于继续实验，不替换 Docker/Compose 启动器，也未合并到 main。
