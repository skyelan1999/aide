# 本地依赖镜像与启动恢复

本轮桌面观测统一从仓库根 `start.command` 打开。当前用户指定的入口位于 `aide/start.command`，不以另一份兼容工作树、单独 HTML 夹具或直接打开旧页面代替启动验收。Go/Node 等定向回归仍可通过测试命令执行，但不能算作桌面观测。

## 清构建缓存后保留完整环境

`start.command` 仍先比对运行镜像的源码指纹；一致时直接启动，不重建、不拉取。需要重建时，优先使用 `aide:dependencies-v1`，通过 `Dockerfile.local` 编译应用，并将构建步骤的网络设为 `none`。Go 模块来自仓库 vendor，Office 包和本地 TTS 来自完整依赖镜像，源码构建无需重新下载这些依赖。缓存清除后重新编译仍可能耗时，但不需要重装依赖。

依赖镜像由已经完整安装的本地 AIDE 镜像生成。`docker/Dockerfile.local-base` 验证 Go 1.26、Node 24、Python 3.12、固定版本 Office/加密模块、Git、SSH、curl 和 sherpa 程序，再移除旧应用资产，保留工具链及依赖层。它是有标签的镜像，独立于 BuildKit 构建缓存；`clean-docker-cache` 不会删除它。自定义插件、外部模型服务及数据卷中的语音模型不属于构建依赖镜像，不承诺模型服务本身离线可用。

```bash
# 源码目录：保留完整本地镜像；没有镜像时尝试校验、导入本地归档
bash scripts/prepare-local-base.sh
# 导出依赖镜像，保留历史 SHA256SUMS 条目
bash scripts/prepare-local-base.sh --export
# 另一台同架构电脑：校验归档后直接导入
cd docker-images
shasum -a 256 -c SHA256SUMS
docker load -i aide-dependencies-v1-linux-arm64.tar.gz
```

macOS/Linux 源码启动自动检测、校验并导入归档；Windows 源码启动在上述导入完成后识别依赖镜像并启用本地构建，Windows 原生流程仍由用户验收。镜像 CPU 架构必须与 Docker 引擎匹配。若本地没有依赖镜像、对应归档和完整旧运行镜像，启动器会明确说明首次构建仍需联网；若归档校验失败或本地准备失败，则报错，不静默退为联网构建。可用 `AIDE_LOCAL_DEPENDENCIES=0` 显式选择原始构建路径。

存在匹配架构的依赖镜像时，新 `export-bundle` 同时保存应用镜像和该依赖镜像标签到同一个 image tar；Docker save 对共享层去重，接收端一次 load 即可导入两者。包内带 Mac/Windows/Linux 启动及维护入口、端口监视器与宿主控制脚本。既有旧包不会因源码 push 自动变化。正式 Release 仍需独立门禁。

## 不弹窗与路径恢复

启动器在健康检查成功后才打开浏览器。本机原配置中的 `/Users/skyelan/debug` 不存在，之前服务反复退出，因此未走到打开浏览器。

现在仅在启动时恢复失效的本地工作区：临时使用挂载的 `/workspace` 和默认缓存位置，原 `workspace-config.json` 字节保持不变。工作台显示修复提示，配置表单保留原路径、文档与缓存设置；用户选择有效目录并成功保存后才清除临时状态。无效的运行中路径修改仍返回错误，损坏的 JSON 配置仍明确失败，不静默重置。恢复模式不表示失效目录中的文件已找回。

每次启动日志保存到 `.agent-state/startup/start-时间-进程号.log`，权限遵守私有 umask；启动失败也可追查原因。Shell 返回原失败码并保留 stdout/stderr。`AIDE_FORCE_REBUILD=1 bash start.command` 用于强制重建验收，日常启动不需要。

2026-10-15 本机观测：从 Finder 打开指定入口，服务健康、Safari 自动打开；实际配置端口为 9999，挂载与原工作区配置保留。清理约 4.903 GB 构建缓存后，同入口强制本地重建并恢复健康，依赖镜像身份不变。本轮后续 Safari 已显示工作台，恢复提示可见；打开配置表单确认原工作区和文档路径保留，未保存或改写配置。浏览器安全提示仍须由用户手动处理，不能自动绕过。后续具体镜像、包和验收范围以[任务记录](tasks/launcher-local-image-20261015.json)为准。

## 镜像内资源权限

本机 iCloud 工作树包含 0700 目录和 0600 文件。Docker COPY 会保留文件模式，导致镜像中的普通用户无法读取 Office 脚本和内置插件。两个应用 Dockerfile 在复制后，仅为 `/opt/aide` 内的公开应用资源增加读取权限和目录遍历权限，不修改宿主源码、配置和用户数据。

`python3 scripts/test_runtime_assets.py` 以镜像默认非 root 用户、`--network none` 检查全部内置资源可读，并执行实际 DOCX 生成和回读。它已纳入 full 验证；旧镜像能复现权限错误，修正镜像通过 18 个资源读取和文档回读。首次 full 的 Office 权限失败保留为失败证据，不能计为通过；最终结果见任务记录。
