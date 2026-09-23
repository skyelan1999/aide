# 整改状态（Remediation Status）

审查基线：`6a4e441`（0.1.5.0 RC4）。整改分支：`remediate/r01-r07`。本文档由实施方（aide 编码助手）维护，Codex 独立验收。

## 第一批（R01/R02/R03 边界/R04/R06/R07）—— 已完成，等待验收

| 提交 | 内容 |
| --- | --- |
| 分支 `remediate/r01-r07`（未合并 main、未升版、未重建生产容器） | 见下 |

### 修复项

| 编号 | 修复 | 关键实现 |
| --- | --- | --- |
| R01 | 压缩死锁/并发 | `recordTokenUsage`/`tokenStatsHandler` 改用独立 `tokenStatsMu`；`execute()` 释放 `a.mu` 后再 `maybeAutoCompact`；压缩重构：模型调用全部在锁外、每会话 `compactingSessions` 互斥（并发第二个返回 409）、提交时校验消息快照前缀一致、切片索引绝不越界 |
| R02 | 工作区身份 | `Task` 增 `WorkspaceID`（模式+路径+主机）；`applyTask` 校验身份不一致返回 409「工作区已切换」；命令 cwd 跟随当前工作区根（`a.workspace.Name()`）；SSH 远程命令进入配置的远程目录（`cd <path> && …`） |
| R03（边界部分） | 路径穿越/前缀 | `resolveHostPath` 对 `/workspace`、`/context`、`/local` 做边界前缀匹配（`/workspaceXYZ` 不再误配）与 `filepath.Rel` 包含性校验（`../` 穿越拒绝）；远程路径/相似前缀/符号链接与内容策略统一列入第二批 |
| R04 | 摘要连续性 | `buildCompactionSummary` 输入含「上一版历史摘要」，指令要求保留既有约束/事实/待办；`CompactedMessages` 累加；压缩失败不删除原消息（提交前置校验失败即放弃） |
| R06 | 正文空白 | `write_file` 的 `content` 按原字节保留（新增 `rawStr`，仅 path/command 等字段 TrimSpace） |
| R07 | 前端竞态 | `selectSession` 递增请求序号、过期响应丢弃；发送过程捕获 `draftSession`，仅在仍停留在发送会话时清空草稿/滚动 |

### 回归测试（将审查附件缺陷观察改写为正确行为断言）

新增 `internal/server/remediation_test.go`，8 个用例：

- `TestRemediationAutoCompactNotBlockedByHeldMutex`：全局锁被持续争用时压缩必须在 10s 内完成并折叠消息
- `TestRemediationConcurrentCompactionNoPanicNoDeadlock`：两个并发压缩 → 一个 200、一个 409；随后 `/api/config` 立即响应
- `TestRemediationOldProposalRejectedOnNewWorkspace`：A 提案切 B 应用 → 409 且 B 无文件；切回 A 应用成功且内容正确
- `TestRemediationCommandFollowsCurrentWorkspace`：切 B 后 `pwd` 返回 B
- `TestRemediationVirtualRootTraversalRejected`：`/workspace/../data`、`/workspaceXYZ/etc`、`../escape` 均 400
- `TestRemediationCompactionChainRetainsPreviousSummary`：两轮压缩，第二轮请求携带上一版摘要/唯一约束
- `TestRemediationWriteToolPreservesWhitespace`：前导空格/制表/末尾换行逐字节保留（含应用落盘）
- `TestRemediationSessionAPIStableDuringRun`：运行期间会话读取始终 200

前端 R07 序号/草稿守卫：逻辑审查 + `node --check` 通过；Node VM 乱序复现已不适用（旧响应被序号丢弃），待 Codex 前端审计复核。

### 测试命令与结果

```
bash scripts/aide.sh test   # 容器内 go test -race -count=1 ./... + go vet
ok aide/internal/server 3.239s   # 既有 36 项 + 新增 8 项回归，全部通过
```

### 兼容迁移与权限范围（HOME 挂载，本轮未改动）

本轮未调整 compose 挂载。`AIDE_LOCAL_ROOT` 默认 `$HOME` 可写挂载仍是用户此前要求的功能，保持现状；R03 的默认收紧（默认仅挂显式项目、辅助资料只读、写目录逐项授权）安排在第二批实施，届时随附迁移说明与验收清单。

### 残留问题（下一批）

- R03 剩余：远程驱动统一内容策略（256KiB/UTF-8/文本限制、`..` 与符号链接处理）、HOME 挂载收紧与 UI 权限展示、插件执行隔离边界（文档化现状）
- R05：工具统一注册表（schema/权限/预算），插件 parameters 进入模型请求，三条入口统一累计限制
- R08：后端预算预览构造器、逐调用 usage 明细（provider/model/来源）、费率带币种/版本、0 费率合法化
- R09：坏文件隔离启动、编译注入版本/commit、镜像 tag 与归档 manifest、独立恢复演练
- R10：README/HANDOVER/PRD 与实现对齐、状态所有权拆分与文档化
- R01 验收补充：压缩与「新任务交错」场景测试（当前覆盖锁争用与并发压缩）
- 前端 R07：Node VM 回归脚本随第二批补充进仓库测试目录

## 状态：等待 Codex 验收（第一批）

不合并 main、不升版、不重建生产容器。
