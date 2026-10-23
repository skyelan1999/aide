# 精简运行镜像与可选开发工具

默认 `Dockerfile` 最终阶段从 Python slim 基础镜像重新开始，不继承 Go 工具链层。运行镜像保留 Python 3.12、Node 24、Git/SSH、Office/DXF Python 库和原有离线 TTS 引擎安装路径；不包含 Go SDK、GCC/G++、make 或 npm/npx。预编译插件仍通过 Node 或自身二进制执行。需要编译本地扩展的插件应提供预编译安装包，或由开发工具包单独构建。

开发工具为独立、可选镜像，**不是另一个 Aide 服务**。普通聊天、模型调用、文件浏览和预编译插件不需要安装工具包。现有主程序不会自动把 AI 命令转发进工具包；直接在精简运行环境执行 `go`、`gcc`、`make`、`npm` 会找不到命令。

## 构建与离线交付

联网的开发/打包机器执行（宿主辅助脚本需要 Python 3）：

```sh
docker build -t aide:local .
python3 scripts/development-toolbox.py build --runtime aide:local --image aide:toolbox
python3 scripts/development-toolbox.py export --image aide:toolbox --output /absolute/output/aide-toolbox.tar.gz
```

工具包继承已选运行镜像的精确本地 image ID，再增加 Go 1.26、C/C++ 编译器、make、pkg-config 和 npm。构建需要基础镜像/软件源，**离线用户安装不需要编译**。导出只包含镜像层，不导出容器、卷、API Key 或用户文件；目标已存在时拒绝覆盖。

离线机器先校验随附 `.sha256` 文件，再导入：

```sh
cd /absolute/output
shasum -a 256 -c aide-toolbox.tar.gz.sha256
docker load --input aide-toolbox.tar.gz
```

导出使用 image ID，导入后可能没有 tag；使用 `docker load` 返回的 ID，或给该 ID 设置自己的 `aide:toolbox` tag。工具包只支持对应的 Linux ARM64/AMD64 容器架构。运行镜像与开发工具包完整离线包各自包含共享层，分别下载时不能将两者体积相加称为节省。

## 使用

```sh
python3 scripts/development-toolbox.py run --image aide:toolbox --workspace /absolute/project -- go version
python3 scripts/development-toolbox.py run --image aide:toolbox --workspace /absolute/project -- bash -c 'cc main.c -o app && ./app'
```

只把显式指定的项目目录挂载为 `/workspace`；该目录内文件可被命令修改。默认无网络、非 root、只读镜像根目录、临时 HOME/缓存、资源限制和 no-new-privileges；不挂载 Aide 数据目录或 Docker socket。下载依赖时显式添加 `--network`。容器退出后临时缓存消失，项目目录里的输出保留。Linux 宿主项目目录需要允许 UID/GID 1000 写入。

## 开发与验证边界

- 源码开发环境继续使用 `docker build --target dev -t aide:dev .`。精简发行镜像不能直接作为 `Dockerfile.local` 的源码依赖种子；现有缓存或完整开发镜像仍可使用。
- `test_go_snapshot.py` 优先读取 `AIDE_TEST_TOOLCHAIN_IMAGE`，兼容旧 `AIDE_TEST_IMAGE`，默认 `aide:dev`。Go race/vet 不在精简运行镜像里执行。
- 原生发布候选流程从精简候选构建同源码的可选工具包，Go race/vet 在工具包内运行；健康、鉴权、Office、DXF、原生辅助程序和 STEP 可选边界仍检查实际运行候选镜像。
- 镜像层审计拒绝已在后续层删除的 Go/GCC/npm 载荷，防止表面删文件后仍发布编译器层。
- Python 依赖和预编译引擎没有拆出；助手本地执行 Go/C/npm 构建的便利性有所变化。后续可以增加明确的工具包执行入口，但本次没有自动授权 Docker 操作或扩大工作区权限。

回滚时切回原有镜像/A/B 槽；数据挂载、端口、启动入口和数据格式不变。卸载可选工具镜像不会删除主程序和项目文件。

## 同版发行附件

周更 Release 同时提供 `aide-<tag>-full-linux-<arch>.zip` 普通包和 `aide-<tag>-devtools-linux-<arch>.zip` 可选开发包；两类均有 ARM64/AMD64。开发包包含镜像、内层校验和、身份清单、使用说明、辅助脚本与许可证。外层 ZIP SHA256 与主程序一起列入 Release 的 SHA256SUMS。普通运行包不嵌入开发包。

候选流程在每个原生架构上执行实际 Go/C++/npm 检查、所有层的插件隐私边界审计、归档校验和导入后的重复编译检查；任何一架构或任一类附件缺失都会阻止发布。开发包允许编译器层，但仍拒绝安装插件或用户状态进入镜像。公开发布遵循[每周发布政策](release-cadence.md)。
