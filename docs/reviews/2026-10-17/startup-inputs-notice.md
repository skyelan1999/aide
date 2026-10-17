# 启动指纹与重启提示修复

任务：[startup-inputs-notice-20261017](../../tasks/startup-inputs-notice-20261017.json)。代码a331b6f8；离线包清单冻结b4cb4afb。已完成下面的局部验收，新的完整回归尚无终态，不能复用上一轮e35收据，也未发布到远端main或Release。

## 修复与直接证据

启动哈希原先遍历被Git忽略的插件node_modules，导致本机与Git快照指纹不同。现在与Docker上下文一起排除生成依赖目录，继续计算插件代码与锁文件。旧脚本在新增缓存稳定性测试中失败，修正后两项测试通过；实际scratch镜像上下文检查确认插件manifest保留、宿主依赖目录排除，原缓存没有删除。工作树与干净Git快照镜像的源码指纹均为`2b0f9c65000ccee3d1537d9018480fd975f97883cb417444d1fe737c631eda07`。

星图通用aside样式曾把共享重启提示拉成右侧贯穿页面的大白卡。改为限定到节点详情#detail，共享提示显式限制位置和尺寸，并更新资源版本。原生Safari实际复现旧白卡；载入新样式后，经真实服务实例切换触发的提示恢复为顶部短条。诊断记录、节点详情及关闭交互通过；1325像素与约707像素窗口通过并恢复原尺寸，小于700像素未完成原生验收。首轮重启未显示通知的画面不作为修复证据，随后实际出现的通知及对应诊断才计入通过。

ARM6ad1d1179c2c／AMD0c173c73296e完整离线包的外层、内层哈希、架构和真实导入通过。全新ARM运行包内原始start.command，无构建或拉取，文档CRLF空行后的第3行／原始哈希和原生CAD120三角形通过，临时服务已停止移除。更新本机候选清单后，普通`AIDE_OPEN_BROWSER=0 bash start.command`选择当前支持压缩历史的镜像，无源码构建参数；重复启动时容器ID和启动时间完全不变。

局部证明见[startup-inputs-notice-proof.json](startup-inputs-notice-proof.json)。私有原始日志及截图位于`.agent-state/startup-inputs-notice-20261017/`，不随Git发布。原生窗口、main39及星图面板已恢复，未新增标签或调用模型。Windows由用户验收，真实向量测试仍暂缓。

## 完整回归期间的主机空间故障

本轮新full启动后，终端报告`can't create temp file for here document: no space left on device`。系统卷可用空间约113–116 MiB，移动盘仍余358 GiB；Docker.raw实际磁盘占用约115 GiB，系统交换空间使用约3.35 GiB。这些是同阶段观测，尚不能把它们当作Docker停止的唯一原因。

随后Docker API超时、9999健康端点连接被拒绝，Docker Desktop显示Engine stopping且正常Restart按钮禁用。正常退出Dashboard后，三个后台进程仍在，不能据此宣称引擎已完成停止。新的回归外层进程和Docker run客户端仍在等待，没有终态，不启动另一轮替代它。缓存prune请求也因Docker API超时未执行成功。

已把两个旧/private/tmp/aide-resume-*临时副本迁到移动盘`/Volumes/MY PASSPORT/aide_ws/preserved-temp-workspaces-20261017/`，共11,957项经过SHA-256、权限和软链接检查，原路径保留链接。该目录是保全副本，不能当作可再生agent缓存删除。系统卷恢复到约890 MiB可用；本轮可再生源码快照及已停止的全新启动目录另行清理，完整镜像包与历史回滚证据保留。

Docker不可达期间，直接从宿主持久化目录读取并核对20个保护条目，原14会话、配置、auth与vault状态保持升级前字节身份。实际Safari显示紧凑的AIDE-SERVICE-UNREACHABLE提示，原会话及已输出内容仍可见；没有自动重发模型任务或刷新丢弃页面。

强制退出后台可能中断尚在运行的容器并丢失未落盘数据，因此已请求用户确认；尚未收到答复，不实施强制操作。Safari开发者功能许可也仍未收到答复，没有更改该偏好。

## 继续条件

先按当前用户答复恢复Docker，重新核对引擎、原容器、持久化数据及待验证容器，再从原始start.command恢复工作台。观察当前full句柄的真实终态；仅在确认终止、排除空间风险后，决定是否需要重跑。之后完成当前指纹的full、干净main门禁和公开附件校验。新包URL尚未公开可用，远端main仍保持5fac564d，不能把本机导入与局部通过写成发布完成。
