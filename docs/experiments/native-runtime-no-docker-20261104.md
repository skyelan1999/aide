# 原生运行实验（不合并）

分支：`experiment/native-runtime-no-docker-20261104`，基于 `main` 的 `3db8498c`。本实验验证 Go 服务能否在宿主机直接运行，不替换正式 Docker/Compose 启动器。

## 可复现方式

宿主机安装 Go 后，在仓库执行：

```sh
scripts/build-native-runtime.sh
AIDE_WORKSPACE="$PWD" AIDE_CONTEXT="$PWD/context" scripts/run-native-runtime.sh
```

`run-native-runtime.sh` 直接执行 `build/native/aide`，不调用 Docker、Compose、不读取 `.env`，默认数据目录与 Docker 安装隔离。可用 `AIDE_NATIVE_BIN`、`AIDE_WORKSPACE`、`AIDE_CONTEXT`、`AIDE_DATA`、`AIDE_ADDR` 指定二进制和目录/监听地址。`scripts/test-native-runtime.sh` 在隐藏 Docker CLI 的 PATH 下启动临时实例，检查健康接口和工作台首页，并在退出时终止实例、清理测试数据。

## 本机实测（macOS arm64）

- 使用本机 Docker 内的 Go 1.26 工具链交叉编译出 macOS arm64 Mach-O，然后在 macOS 宿主直接执行该二进制；Docker 仅用于本次构建，运行时 PATH 不含 Docker CLI。
- `GET /healthz` 返回 `status=ok`、`integrity=ok`，`GET /` 返回 HTTP 200。
- 使用全新临时工作区、引用目录和数据目录，测试后清理；未读取或迁移 Docker 安装的 `.env`、数据卷、插件、工作区和凭证。
- 去符号后的原生二进制约 249 MiB。此次启动证明服务和嵌入式工作台可以直接运行，不代表插件、AI/TTS、Office 转换和消息/远程桥接已完成原生验收。
- 当前宿主 Python 3.9 只有标准库，没有 `python-docx`、`openpyxl`、`python-pptx`、`ezdxf`、`pypdf`、Pillow 或 NumPy；依赖这些模块的工具不会与容器环境等价。Node 存在，但测试没有安装或验收任一插件。

## 未覆盖及下一步

原生试验暂时只覆盖 macOS arm64 的 Go 服务启动、健康检查和静态工作台。Docker 镜像内的 Python/Node/Office/浏览器工具链、各插件的独立运行时、host local mounts helper、消息桥和电脑/浏览器桥接仍需要逐项发现并决定原生安装、按需下载还是保留可选容器后端。Windows/Linux、完整交互会话、实际模型请求、语音/TTS、文件转换、插件 daemon、LAN 与电脑控制均未验收。当前 Go 可执行文件体积也不适合作为最终安装包。
