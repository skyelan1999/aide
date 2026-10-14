# AIDE 停止与 Docker 构建缓存清理

把脚本放在对应 AIDE 根目录使用；源码目录与离线安装目录都支持。执行停止不会删除工作区、会话、配置、数据卷或镜像，下次使用原来的 start 入口恢复。

| 系统 | 停止这套 AIDE | 清理构建缓存 |
| --- | --- | --- |
| macOS 双击 | `stop.command` | `clean-docker-cache.command` |
| Linux | `bash stop.sh` | `bash clean-docker-cache.sh` |
| Windows 双击 | `stop.bat` | `clean-docker-cache.bat` |
| PowerShell | `./stop.ps1` | `./clean-docker-cache.ps1` |

Shell 入口支持 `--dry-run`，PowerShell 支持 `-DryRun`，只展示目标和动作。原命令入口也支持 `bash scripts/aide.sh stop` 与 `bash scripts/aide.sh clean-cache`。Docker 引擎未运行时明确退出，不自动启动 Docker Desktop。

停止依据 Compose 的安装目录与 aide 服务标签查找容器，兼容自定义项目名及 macOS 物理/逻辑路径别名，不按端口猜测、不停止其他目录中的 AIDE。没有匹配的运行容器时重复执行也成功。macOS 同时停止当前安装目录登记的宿主桥接；`.cache/aide-manual-stop` 阻止端口监视器随后重建容器，正常启动自动清除此标记。不建议在 A/B 升级切换过程中并行执行停止。

清理实际执行 `docker builder prune --all --force`：删除当前 Docker 引擎默认构建器的未使用构建缓存。这些缓存由项目共享，因此也可能包含其他项目缓存，之后源码构建可能重新下载或编译依赖。不会执行 system/image/container/volume prune，不清理镜像、容器、数据卷、`.env`、浏览器记录、工作区文件或离线镜像包。自定义 buildx 构建器的缓存不在此次范围内；Docker Desktop 磁盘文件大小也不保证立即缩小。

新导出的离线包和 Release 启动包会包含对应平台的两个入口及共用维护脚本。既有离线包不会因 Git push 自动更新；本次另提供可复制到旧安装目录的维护脚本补充 ZIP。

验收范围与待办见[任务记录](tasks/docker-maintenance-20261015.json)。Windows 原生行为由用户验收。
