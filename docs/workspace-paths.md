# macOS 工作目录选择 / Workspace selection

工作目录与 Docker 可访问范围是两项不同配置。浏览器可以选择已挂载范围内的任意目录；保存工作目录不会自动添加新的 Docker 挂载。

## 从根目录导航

Docker Desktop 的 Linux 虚拟机根目录不是 macOS 根目录。不要直接用 `/:/local`。
本项目提供组合配置，按真实路径展示已共享的 `/Users`、`/Volumes`、`/private`：

```sh
python3 scripts/configure-local-root.py --path /
bash scripts/aide.sh start
```

配置脚本将组合配置写入 `.env` 的 `COMPOSE_FILE`。以后双击 `start.command`、运行 `scripts/aide.sh` 或 `docker compose` 均读取同一设置，无需另记启动命令。端口由 `.env` 的 `AIDE_PORT` 决定，默认 8097。

这会重建 aide 容器并保留命名数据卷，不重启 Docker 或其他项目。此模式使 aide 的命令、可信插件可访问上述共享目录，请按实际用途启用。若 Docker 拒绝某一目录，先在 Docker Desktop 的文件共享设置中开放该目录。

打开工作空间配置 → 浏览，可输入绝对路径、点击“前往”或逐级点击“上级”。只有到达 `/` 才禁用“上级”。选择目录后点击“保存配置”生效。当前工作目录保持不变，直到用户保存。

这不是完整 macOS 文件系统映射：未配置的 `/Applications`、`/System` 等目录不会出现在列表中。需要其他目录时，在组合配置中添加同名宿主机路径，例如将 `/Applications` 挂载至 `/local/Applications`，并确保 Docker 允许共享。系统与文件权限仍然适用。

回退：`python3 scripts/configure-local-root.py --path "/你的项目目录"` 后再次启动，恢复单目录挂载；如当前工作目录超出该范围，先在界面将工作目录改回可访问位置。

## English

The working directory is separate from Docker's accessible mount boundary. The configuration helper stores the selected Compose files in `.env`; `start.command`, `scripts/aide.sh`, and `docker compose` use this same configuration and port (8097 by default). Use the commands above to browse shared macOS `/Users`, `/Volumes`, and `/private` from a common `/` view. The Linux VM root is never presented as the macOS root. Other system directories require explicit additional mounts and Docker file-sharing permission. Select a folder, then save the workspace configuration. Data volumes are preserved; only aide is recreated.
