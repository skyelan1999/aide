# Docker 恢复与启动资源保护验收

2026-10-17，工作目录 `/Volumes/MY PASSPORT/aide_ws/aide`。用户明确授权强制恢复、清理构建缓存，并要求重新创建前清理旧 AIDE 内容、在脚本预设资源保护。

## 已验证的恢复

移动盘已重新挂载。恢复后 Docker 引擎响应正常，原容器、镜像、卷、构建缓存均为零；没有再次执行清盘、删卷或强制结束当前后台。原 Docker.raw 已被重建，故障前失败的 `.partial` 备份目录目前不存在，不能声称存在有效虚拟磁盘备份。保全的历史临时源码档案仍在。

宿主 bind 数据中20个会话／配置／认证／密钥库保护条目与故障前精确一致。原 start.command（AIDE_OPEN_BROWSER=0）从已验证的本地 ARM64 完整包导入6ad1d1179c2c镜像，未构建、未拉取。服务健康，14个会话可经鉴权API读取。再次启动后两容器ID与StartedAt保持一致。Safari原有星图页已重新同步；没有新开标签页。用户正在操作浏览器，未进一步改变其页面。

Docker Desktop本机已设为4核、6GiB，实际info为6210576384字节/4核。缩小虚拟磁盘时出现删除全部镜像／容器／卷提示，已取消，未实施缩容；全局虚拟磁盘容量仍约228GiB。日常启动脚本不修改Docker Desktop／WSL全局设置。结束了9个父进程为1且路径精确匹配旧Docker agent的孤立进程，当前后台及其子agent保留。

## 代码行为

- Bash与PowerShell入口在导入镜像、迁移数据前检查磁盘：普通启动至少10GiB；显式源码构建在准备依赖前至少20GiB。无法读取容量也会停止，不自动删除文件。Bash检查系统卷和项目卷；Windows检查项目、系统、LOCALAPPDATA所在卷。远程daemon或自定义VM磁盘路径的可用容量不在这些宿主检查覆盖内。
- Docker引擎至少4GiB；输出实际预算，高于8GiB或4核给出提示。现有服务内存上限保留应用2GiB、CAD4GiB，各2核。两份Compose均增加json-file轮转日志，每服务10MiB乘3份。
- 完整回归的一次性Go容器新增4GiB内存、4GiB内存加交换总额、2核、512进程和轮转日志限制；该参数修改尚未跑完新完整race/vet，不宣称全量通过。
- 离线导出明确携带两个resource-guard辅助脚本；纳入源码指纹。已生成旧发行包保持不变，新的发行包和远端main仍待完整门禁与公开附件准备。

## 实际检查

资源保护5项、维护9项、源码指纹2项、启动持久化21项专项通过。首次quick因当前shell找不到Node失败；随后把桌面自带Node加入PATH重跑，quick全部105项通过（退出0）。Windows原生验收由用户执行，本机没有伪称Windows通过。

实际容器inspect确认2GiB/4GiB限制、日志轮转、重复启动无重建；故障前完整回归进程已消失，旧full.log停在Go快照，不视为通过，也不复用前一轮收据。复测时应用约603MiB、CAD约12MiB；系统盘空闲约114GiB，外接盘约358GiB。数值为采样快照，不能相加Docker内部占用与Docker.raw作为两份独立占用。

私有证据：`.agent-state/docker-resource-guard-20261017/` 的start.log、repeat.log、runtime-proof.json、orphan-cleanup.json与quick-final.log。未公开会话、密钥或原始截图。


## 后续终态核对

资源保护这轮完整回归已终态通过，不再处于上文记录时的等待状态：108项，指纹 `d2fe7e746bc9f7e96c9a7cedd1bf37dd03f31acc905f6116f6c8daf5c4da05ed`，server race 799.188秒及全包vet/CAD通过。原始收据 `.agent-state/docker-resource-guard-20261017/full-final-proof.json` 记录HEAD5ab36ca2；产品改动为66b0a89a，随后普通文档未改变输入指纹。

这份收据不覆盖后续MCP源码。MCP另有独立全量进程，不能借用本轮PASS。既有6ad/0c完整包生成于资源保护改动之前，保留为已验收旧包；未重新导出前，不能声称它们已携带新增的资源保护启动脚本。MCP当前正式候选构建/导出已准备精确输入门禁，等待新全量完成后串行执行。
