# Aide 停止与 Docker 清理

[标准启动器规范](architecture/launcher-contract.md)区分源码包与 Release 包。停止和清理均按安装目录归属处理，不按端口猜测，不清空 Docker Desktop。

| 动作 | macOS | Linux | Windows |
| --- | --- | --- | --- |
| 停止 | `stop.command` | `bash stop.sh` | `stop.bat` |
| 清理本安装 Docker 环境 | `docker_clean.command` | `bash docker_clean.sh` | `docker_clean.bat` |
| 清理后重新构建（源码包） | `clean.command` | `bash clean.sh` | `clean.bat` |
| 版本/依赖/故障诊断 | `verify.command` | `bash verify.sh` | `verify.bat` |

Windows 标准入口直接调用原生 C++ EXE，无 PowerShell 回退。源码包使用 `build.bat` 在 Docker 内编译当前原生启动器，已有旧 EXE 也会刷新；Release 包应恢复完整包。POSIX 清理与原生接口支持 `--dry-run`；诊断本身不启动或构建服务。

stop 通知在线工作台清空敏感内容并关闭/转为空白页，取消任务并停止自托管插件、登记的宿主桥接和本安装全部 Compose 服务。重复停止可安全执行；组件未确认退出会报告失败，继续停止其余组件。手动停止标记阻止后台监视器自动重建。正常 start 清除此标记。不要在升级事务中并行清理。

docker_clean 先核验归属再停止，移除本安装容器、网络、没有其他使用者的镜像和有匹配归属收据的专用 Buildx 构建器。项目混入其他目录容器会拒绝；共享/多引用镜像保留并提示。它不执行全局 prune、不删除数据卷、`.env`、用户存储、工作区或离线归档。clean 再调用显式 build，因此会启动新构建；普通 docker_clean 不构建。

源码清理后运行 build；完整 Release 包清理后运行 start，可重新校验/导入保留的离线镜像。旧 clean-docker-cache.* 保留为历史兼容工具，会清理默认构建器的共享缓存，可能影响其他项目；它不是新标准清理入口，也不随新 Release 包分发。

验收与限制见[任务记录](tasks/launcher-contract-640.json)。代码、交叉编译和模拟回归不能代替真实 Windows Docker Desktop 与浏览器验收。
