# 移动硬盘开发目录切换与产物清理

任务：[external-workspace-cleanup-20261015](../../tasks/external-workspace-cleanup-20261015.json)。用户明确要求后续开发使用新目录，释放旧编译产物和无用缓存，保持分支干净。客户端为macOS本地Codex；不创建新浏览器页，不调用模型。

## 当前开发与运行位置

开发根目录为 `/Volumes/MY PASSPORT/aide_ws/aide`。已同步历史完整性修复及私有证据和完整离线包；后续修改、测试、提交均使用此根目录。此前复制校验收据保留，代表复制时点；用户后续授权开发后，该目录不再是冻结备份。原目录源码保留。

五项继承vendor删除分别保存在两处仓库的stash，以及仓库外 `.migration-state/original-vendor-deletions.patch`、`external-vendor-deletions.patch`。随后恢复受控文件，使新开发树与干净main一致，没有丢弃原改动。六个旧预览夹具绝对软链接已改指移动硬盘源码，插件相对软链接保留。

使用指定 `AIDE_OPEN_BROWSER=0 bash ./start.command` 从新目录构建并观察9999；运行镜像 `sha256:b11bb97fed59692804b1177fe3b11dacf1a2f458df7c6f5d4cc1150792ba2d3b`，source SHA `4f32f3fc9aeb01a59b159be9e4e82d78ac1fcfd39b33aaef843c3cda70c5bb00`。`/workspace`与宿主根代理已切到新目录，原Harness上下文路径及Docker data/home卷保持。13份会话加4份配置／来源／鉴权条目的摘要不变，健康及三项实际网页资源与新目录一致。Docker Desktop的 `/host_mnt` 前缀规范化后比较物理宿主路径；首次未规范化字符串比较仅在 `/local/private` 处失败，不是挂载内容变化。

## 清理边界与结果

逐份读取原目录与移动硬盘对应归档并比较SHA-256后，才删除原目录中的重复构建归档；完整发布记录、镜像包及回滚材料留在移动硬盘。清理两处目录中经文件头和名称确认的旧AIDE／server.test可执行文件、明确登记的旧AIDE可执行文件gzip备份、Swift模块缓存。保留源码、插件依赖、日志、脚本、测试数据、会话、密钥和设置。没有执行宽泛Docker prune，不清理依赖基础镜像／用户卷。

最初清理进程在归档阶段被主动中断，以将随后可执行文件选择进一步限于AIDE和server.test；已删归档均有逐文件外置副本校验记录，续跑保留原收据，未执行泛ELF删除。完整原始记录在私有 `.agent-state/external-workspace-cleanup/cleanup.json`；[脱敏收据](external-workspace-cleanup-proof.json)记录分类计数、分目录逻辑字节数及原始清单SHA。

合计删除253项，逻辑大小约89.20 GiB；这是文件逻辑大小，不等同APFS即时物理回收量。清理后本机可用约78 GiB，移动硬盘约395 GiB。旧源目录保留源码和用户记录；旧归档的受控SHA清单保留为历史记录，不再表示这些包仍在旧目录。

## 验证与交接

新目录quick82通过，输入指纹与干净full84精确一致：`b38daba67a0367402712624686c75f7cb3e263a93a0a50573b8dc9e5c7442b13`。full源快照 `55381ed6afeba189be27cf078ff79a11e8f1dada`，server race562.526秒及vet通过；本轮迁移和清理不改变受控产品输入，因此复用该收据，没有声称再跑一次full。完整已鉴权Safari按用户当前安排暂缓，Windows用户验收，真实向量暂缓。

本轮没有另导出相同产品逻辑的镜像包；原历史修复完整包9bc517eba16f与更早包及依赖包留在移动硬盘。新运行b11bb97fed59按恢复后的干净vendor源码构建，不能把旧包说成该新镜像的导出。后续产品修改继续按既有镜像交付流程更新。当前启动仍引用原父目录的Harness上下文；本次只迁移aide项目，没有迁移Harness或Docker Desktop数据卷。

最终main门禁通过，推送后远端精确核对 `1a48d081aae453630a000b4d25d4eab2bc1665f8`；本条普通文档补完成收据，不改变full输入。原12工作流继续，迁移与清理不替代全部功能验收；不升版、tag或创建正式Release。
