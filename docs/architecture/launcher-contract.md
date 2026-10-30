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

## 构建与资源归属

Windows 源码入口修复见 [#646](https://github.com/skyelan1999/aide/issues/646)。根目录 BAT 经统一 CMD 入口调用原生 EXE，并显示操作和退出码；通过双击（`cmd /c`）启动时会停留等待确认，自动化设置 `AIDE_LAUNCHER_NO_PAUSE=1` 可禁用停留，仍保留真实退出码。

源码更新后运行 `build.bat`，不必手动删除 EXE。显式 `build/clean` 总是从当前源码编译原生启动器，在成功编译且本安装后台控制器确认停止后替换旧 EXE，然后执行运行镜像构建。失败保留旧 EXE 和持久数据，不转用 Release。普通 `start/stop/verify` 等入口检测旧版启动器并提示先 build；`start` 不编译。旧编译收据不再使 build/clean 回到旧 EXE；普通委派也会检查原生命令版本。

活动根目录及 `scripts/` 中的 PS1 已迁入 `scripts/fixtures/legacy-windows/`，仅供历史兼容回归，不进入 Docker 上下文、Release 包或源码构建身份。当前 Git 源码的 `.bat`、原生 EXE 和新打包入口都不调用 PowerShell。原生失败信息写入 `.aide-data/native/launcher.log`；启动器尚未编译或 Docker bootstrap 失败时，CMD 窗口显示实际错误和失败退出码。

源码 build 需要 Git、Docker Compose、Buildx；POSIX 协调器还需要 Bash/Python 3。Windows 首次 `build.bat` 通过 Docker 的 `native-launcher` 目标生成引导 EXE，再由原生启动器编排当前运行镜像和当前原生宿主构建；宿主无需安装 PowerShell、Python 或 C++ 编译器。当前编译产物使用校验过的不可变 `launcher-<sha256>.exe`，入口按本安装收据转交，避免覆盖正在执行的 EXE。

每个安装目录拥有独立的 `aide-<目录摘要>` docker-container Buildx 构建器、`aide:source-<目录摘要>` 镜像和 `.aide-data/build/owner.json` 归属收据。已有同名构建器但无匹配收据时拒绝接管。构建器限制为 4 GiB / 2 核；增量层和 cache mount 留在该构建器中。冷启动需要获取 BuildKit、基础镜像、固定工具链及依赖；默认 Docker 引擎已有基础镜像 tag 不代表专用构建器已有离线缓存。无网络的机器优先使用完整 Release 包，或预先准备该专用构建环境。

实际 build 结果先核对镜像 ID、源码指纹和本安装标签，再更新 `.env` 的镜像选择。构建失败保留此前启动配置。Windows 还核对当前宿主产物后才选择新运行镜像。编译和临时提取容器属于构建流程，不属于普通 start。

Docker 的专用缓存与驱动行为见 [docker-container driver](https://docs.docker.com/build/builders/drivers/docker-container/)；移除专用构建器会移除其缓存，本接口不使用 `--keep-state`，见 [buildx rm](https://docs.docker.com/reference/cli/docker/buildx/rm/)。

## 停止、隐私与清理

停止先发出终止 SSE 事件，页面同步清空标题和文档，再尝试关闭标签；浏览器拒绝脚本关闭用户标签时转为 `about:blank`。清理连接、任务流、语音和本地登录令牌，不删除草稿或用户设置。只能保证收到停止事件的在线页面执行此流程；断网、旧版页面、浏览器崩溃或强制杀进程不能被声明为已关页。此机制不删除浏览器历史记录。

服务端停止拒绝新工作、取消执行、停止插件守护进程并等待退出。宿主 stop 停止当前目录登记的桥接/后台组件和全部同根 Compose 服务，保留手动停止标记，避免监视器立即重启。组件未确认退出则报告失败，继续尝试停止其余组件。

`docker_clean` / `clean` 不执行全局 prune、强制删共享镜像或 `down -v`。容器/项目必须能追溯到当前目录；混入外部安装时拒绝清理。镜像仍被其他容器使用或存在其他引用则保留并报告。`.env`、`.aide-data` 的用户存储、工作区、会话、命名数据卷及离线镜像归档保留。不要把“Docker 环境清理”理解为清空 Docker Desktop 或删除用户数据。

旧 `clean-docker-cache.*` 是历史兼容工具，仍可能清理默认构建器的共享缓存；不属于此规范，也不随新 Release 标准入口分发。日常维护使用 `docker_clean` / `clean`。

## 诊断、升级与发布

`verify` 不构建、不拉镜像、不启动服务；未运行或故障返回非零并报告检查结果。POSIX 输出最近启动日志位置，Windows 输出原生日志位置。启动失败应先保留配置、数据和归属收据，再根据诊断恢复。

源码安装通过 `AIDE_INSTALLATION_KIND=source` 禁用应用 A/B 安装/激活和宿主更新 worker，即使当前镜像来自 Release。页面仍可查看公开 Release 信息。Release 保留既有槽位和共享数据的健康验证/回滚协议。

`release` 复用 `scripts/version.sh` 的现有版本规则，不自行发明版本号。准备干净 main、指向 HEAD 的已推送版本 tag，以及 `docs/releases/<tag>.md` 的完整发布说明；当前远端 main/tag 必须一致且 required gate 成功。命令分派既有 `aide-delivery.yml`，后续仍执行原交付中的架构构建、发布说明、发布节奏和环境审批。分派成功不等于已发布。实现该入口本身不授权升版、推送 tag、公开发行或部署。
