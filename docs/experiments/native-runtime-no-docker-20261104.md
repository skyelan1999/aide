# 原生安装运行实验（不合并）

分支：`experiment/native-runtime-no-docker-20261104`，基于 `main` 的 `3db8498c`。本实验验证 Go 服务能否在宿主机直接运行，不替换正式 Docker/Compose 启动器。

## 可复现方式

宿主机安装 Go 后，在仓库执行：

```sh
scripts/build-native-runtime.sh
AIDE_WORKSPACE="$PWD" AIDE_CONTEXT="$PWD/context" scripts/run-native-runtime.sh
```

`run-native-runtime.sh` 直接执行 `build/native/aide`，不调用 Docker、Compose、不读取 `.env`，默认数据目录与 Docker 安装隔离。可用 `AIDE_NATIVE_BIN`、`AIDE_WORKSPACE`、`AIDE_CONTEXT`、`AIDE_DATA`、`AIDE_ADDR` 指定二进制和目录/监听地址。`scripts/test-native-runtime.sh` 在隐藏 Docker CLI 的 PATH 下启动临时实例，检查健康接口和工作台首页，并在退出时终止实例、清理测试数据。

## 本机实测（macOS arm64）

- 使用本机 Docker 内的 Go 1.26 工具链交叉编译出 macOS arm64 Mach-O；Docker 仅用于本次构建，运行时 PATH 不含 Docker CLI。
- `scripts/package-native-macos.sh` 生成 `.pkg` 安装包和 `.zip` App；检查了 PKG payload，并从解出的 `Aide.app` 直接运行验证。安装包使用项目现有的 Mac 桌面窗口和生物认证桥，保留原生标签、关窗/重开、面板、麦克风和 WebAuthn 检查。
- 原生工作台启动成功，页面标题为 `aide · AI 开发工作台`；Go 服务 `/healthz` 返回 `status=ok`、`integrity=ok`，首页、设置、WebSocket、原生标签和媒体能力检查通过；测试使用临时独立数据、工作区和引用目录。
- App 从本机证书目录读取 TLS 证书 DER 指纹；WebKit 仅对 `localhost`、该随机端口和完全匹配的本机证书 pin 放行，不安装系统 CA。测试确认服务随窗口退出而终止。
- 本机安装器执行需要管理员凭据；当前环境没有可用的 sudo 凭据，因此没有对 `/Applications` 或用户系统执行安装。已检查安装包载荷并从其中运行 App，未覆盖现有 `/Users/skyelan/Applications/Aide.app`。
- `.pkg` 载荷包含原生服务、Node/npm 24、可重定位 Python 3.12 和镜像内固定的 Office Python 包。对解出的安装包检查了 Node v24.19.0、npm、`python3.12 -I` 下的 python-docx/openpyxl/python-pptx/ezdxf/pypdf 导入，以及 WebView 首屏。
- 当前 `.pkg` 为本机临时 ad-hoc 签名 App + 未签名安装包；正式公开分发仍需 Developer ID 签名和 notarization。安装器实际写入 `/Applications` 需要管理员凭据，本机没有可用 sudo 凭据。
- 使用全新临时工作区、引用目录和数据目录，测试后清理；未读取或迁移 Docker 安装的 `.env`、数据卷、插件、工作区和凭证。
- 独立运行过 Linux amd64 ELF 服务；在 Linux 内核的临时测试容器中将 Docker CLI 从 PATH 移除后，`/healthz` 和首页仍通过。这个结果是 Linux 内核运行验收，不是实体 Linux 桌面安装验收。
- Windows amd64 服务已从相同源码交叉编译成功；此前发现的 POSIX 进程组与磁盘统计调用已抽成 Windows 实现，但没有真实 Windows 主机、桌面启动器或安装器验收。
- 当前宿主 Python 3.9 只有标准库，没有 `python-docx`、`openpyxl`、`python-pptx`、`ezdxf`、`pypdf`、Pillow 或 NumPy；依赖这些模块的工具不会与容器环境等价。Node 存在，但测试没有安装或验收任一插件。

## 尚未达到“独立自主运行”的部分

浏览器和本地 sherpa 离线 TTS 工具链仍未随 App 打包；插件 daemon、消息桥、电脑/浏览器桥接、AI/TTS 以及 Office 真实文件转换尚未端到端验收。Python/Node 主进程运行时已随实验包提供，但尚未验证真实市场插件的安装、启动和依赖行为。桌面冒烟的完整插件页断言因隔离数据目录没有 Compare/ProfessionalProject 插件而为 false；系统平台认证器检查也为 false，原生 Touch ID bridge 本身可用。

`internal/execsandbox` 只有 Linux Landlock 实现；macOS/Windows 的 restricted 命令继续 fail-closed，不会因为改成原生启动就扩大为不受限的宿主机 shell。原生运行时需先设计并验证系统级隔离，否则不能宣称与 Docker 工作台具有同等的安全能力。

Linux/Windows 安装包尚未构建或验证。尚未完成管理员实际安装、首次启动、升级/卸载、登录后启动、全量 AI 会话、真实插件/TTS、Office 文件往返、远程访问及三端对等验收。macOS 安装器本机实际安装需要管理员凭据；此分支仍是实验，不应替代正式 Docker/Compose 启动，也未合并到 main。
