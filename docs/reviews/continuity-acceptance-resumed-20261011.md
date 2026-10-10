# Aide 恢复完整验收 · 2026-10-11

## 分工与基线

用户改为由 Codex 继续完整验收，真实向量测试仍暂缓。Windows 无设备，保留 NOT_RUN。基线 `2240eea60c6bcaf9871d6d96766ab75d471ab46a`；本轮只修复历史 UI 回归的 VM 测试夹具，未改变产品源码。生产 9999 未替换，未 Release。

## 自动回归

`python3 scripts/agent-route.py verify full` 终态 exit0，38 项 PASS，包含 Docker Go race/vet。指纹 `7fbdd76d7a9bce49e29595e01dabb2218ef191b536622876fac4c4436134e03a`，日志 `.agent-state/verify-20261010T160155645421Z.log`。

前一轮 `.agent-state/verify-20261010T155248666849Z.log` 保留 FAIL：历史备份测试抽取的 VM 未提供新管理模块所需 `window`。夹具增加 window 和管理模块契约断言，定向 PASS 后重跑 full。不能把这次 full 当作全部真实交互通过；后续源码变化须重新生成指纹。

## 双服务器 SSH/SFTP

`AIDE_SSH_TEST_EVIDENCE_DIR=.agent-state/ssh-acceptance-2240eea bash scripts/test-ssh-workspace.sh` exit0，隔离两个服务器重复两次通过。日志 `.agent-state/ssh-acceptance-2240eea/aide-ssh-test-20261011000106-70151.log`，输入摘要前后一致；临时容器及网络由脚本清理。

覆盖目录不存在/权限错误、独立 SFTP 引用、陈旧连接恢复、文档绑定和缓存路径、Markdown 正文/资源恢复、远程重命名/归档/解压/删除及属性、搜索/时间线/渐进索引、命令退出与远程进程组取消。它不证明生产服务器凭据、物理断网或用户权限配置正确。旧 token memory 往返测试不代表已停用的记忆神经 UI 验收。

## 系统 Safari

候选 18211 使用 `.agent-state/continuity-runtime/start.command` 启动 exit0，二进制 SHA256 `965f364172410a30d164bc6cb01b449937337dfa48a731c1e5e3fa764eee1a56`。旧二进制已保留。复制的启动目录缺少三份 bridge-control 脚本，启动出现告警，因此桥接能力未验收；系统 Safari 的原生操作不依赖这些候选桥接脚本。

已完成实际按钮操作及独立读回：

- `notes.md` 默认保留 0/0 预览保留全部 7 版本；取消无修改。
- 独立临时目录 `acceptance-20261011/page.md`，正文 A + SVG A 初始归档；Safari 自动保存 B 后读回服务器文件。
- 外部修改 SVG 为 C，打开历史捕获资源变化。选择版本 1、预览并恢复正文和资源，服务器正文 A、SVG A 均读回确认。
- 恢复产生版本 4；相同内容再次手动保存不追加版本。
- 下载历史 ZIP 得到 4 版本、4 去重对象；逐对象 SHA256 与文件名一致。包 SHA256 `1b2c62a98c533b93349ea3f27de077782af10639580fabaec156c47acebba4a2`。
- 同一备份导入预览 0，确认后重新打开仍 4 版本；当前正文和 SVG 未改变。自动保存恢复为测试前关闭状态。

截图保存在 `.agent-state/continuity-runtime/`：`history-management-2240eea-safari.png`、`history-restored-2240eea-safari.png`、`history-import-idempotent-2240eea-safari.png`。临时数据与下载留存用于复核，未混入生产数据。

## 星图观察补充

从工作台真实入口打开星图后，自动索引包含新建 `acceptance-20261011/page.md`，节点原文与服务器一致。节点/名称搜索返回两个同名文件，路径可区分。星际日志的连续观察比较显示插件清单 `surface.json` 的 generatedAt 从重启前变化至本次启动时间；随后扫描轮次从176继续至198，观察保留32/淘汰35保持不变。此次没有复现无内容变化的历史增长，不据此覆盖全部时间线/容量验收。截图 `starmap-current-node-2240eea-safari.png` 留存于候选证据目录。

## 剩余验收

永久裁剪/对象清理、跨工作区导入、其他嵌入类型、恢复容量/强杀/断网/冲突、跨会话审批/规则管理、实际插件和模型、星图路径/时间线/镜头/增量更新、输入输出分页、锁态同步、主题与窄屏仍需逐项证据。按[完整验收清单](luna-acceptance-handoff-20261010.md)和[目标源码核对表](continuity-acceptance-audit-20261010.md)继续，不能用本页的部分 PASS 关闭总目标。

真实向量暂缓；Windows NOT_RUN。发布须补可执行回滚、当前输入 full 和阶段证据，再运行 release-check；本页不是发布收据。
