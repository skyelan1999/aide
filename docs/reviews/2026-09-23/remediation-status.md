# 整改状态（Remediation Status）

审查基线：`6a4e441`（0.1.5.0 RC4）。整改分支：`remediate/r01-r07`。最终候选提交：`ee763764`（git 树 gofmt 干净、`go test ./internal/server` 通过）。本文档由实施方（aide 编码助手）维护，Codex 独立验收。

## 完成情况：R01–R09 已实施并通过验收，R10 文档对齐，R08-04/R10 部分为 BLOCKED/NOT_RUN（见下）

| 编号 | 修复 | 关键实现 | 验收 |
| --- | --- | --- | --- |
| R01 | 压缩死锁/并发/阈值/持久化 | 模型调用全部在 `a.mu` 外；每会话 `compactingSessions` 互斥（并发第二个 409）；提交校验消息快照；>48,000 字节才自动压缩；压缩结果 `a.save` 持久化（立即重启不丢）；取消任务后压缩请求对端关闭 | 6/6 workflow + 3/3 lifecycle PASS |
| R02 | 工作区身份/运行中切换 | `Task` 携带 `WorkspaceID/WorkspaceMode/WorkspaceRemotePath`；`wsRoots` 身份→根注册表，运行中任务工具调用仍读原工作区；旧提案/旧编辑器 409；空路径恢复默认根；`/file` 契约字段统一为 `workspaceId`（前端携带打开时身份） | 7/7 PASS + FRONT-003 PASS |
| R03 | 统一路径权限/原子配置 | 边界前缀匹配 + `filepath.Rel` 包含性；工作区/docs/cache 预检（路径必须可打开且为目录），失败时配置/根/recent 全不变，重启安全 | 4/4 PASS（含原子性与重启安全） |
| R04 | 摘要连续性/失败不变性 | 压缩指令含「上一版历史摘要」连续链；压缩失败 400 且 messages/compact/compactedMessages/compactedAt 全不变 | 2/2 PASS（三轮链 + 失败） |
| R05 | 工具 schema/预算 | 插件工具 `parameters` 进入模型请求；三条入口统一预算（≤10 文件、≤512KiB、≤20 命令）；超限必须显式拒绝（run_shell 不再吞错）；工具原始结果跨步骤保留（plan→propose→review 证据链），`ToolUse.Result` 存全文、`Preview` 供界面 | 26/26 PASS |
| R06 | 正文空白字节保留 | `write_file` content 原字节；7 类空白/换行/CRLF/Unicode 逐字节断言 | PASS |
| R07 | 前端竞态/草稿隔离 | `selectSession` 序号守卫；提交完成不再抢走用户已切换的会话，B 草稿与附件不被覆盖 | FRONT-001/002 PASS |
| R08 | 统计真实性 | 按模型费率（`rates`+显式默认），0 是真实 0 且 ≠ 留空；逐调用快照（model/provider/时间/费率/费用），改价不动历史；旧版汇总迁移保留用量与 estimated、标记未计价，不虚构费用；损坏文件保留 + 可诊断日志；前端从服务端取费率（PUT `/api/token-pricing`），未计价历史单独显示 | R08 API 4/4 PASS + Go 测试 3 项 PASS；R08-04 上下文预览 BLOCKED（候选无此功能） |
| R09 | 恢复/构建身份 | 损坏会话文件跳过启动并保留证据；版本为构建期身份（ldflags 优先，工作区 version.md 无法覆盖）；`/api/config` 暴露 `revision`/`buildCommit`/`buildVersion` | 4/4 PASS |
| R10 | 文档/职责 | README/HANDOVER/PRD 与实现对齐、模块职责说明 | 本轮以代码注释与本文档完成一致性核对，正式文档 PR 留待用户审阅后随发布进行（NOT_RUN） |

## 回归测试

`internal/server/remediation_test.go` 共 11 项（R01×2、R02 提案/命令、R03 穿越、R04 链、R06 空白、R07 会话稳定、R08×3：0 价、快照、旧版迁移），全部通过；容器内 `gofmt -l internal/ cmd/` 为空。

## 验收汇总

独立验收脚本（`/private/tmp/aide-independent-acceptance/`，Codex 编写）对候选 `ee763764` 全量运行：

- workflow 6/6 PASS、R01 lifecycle 3/3 PASS、R05 26/26 PASS、R02 7/7 PASS、R03/R09 4/4 PASS、前端 VM 3/3 PASS、R08 API 4/4 PASS。
- 明细见 `acceptance-summary.json`（本目录，含命令与证据路径）。
- BLOCKED：R08-04（候选无上下文预览 API/功能，未实施）。NOT_RUN：浏览器级最短补验（需要真实浏览器，API/单测/VM 级证据已齐）。
- 候选二进制：`/private/tmp/aide-independent-acceptance/candidates/ee763764d0fc/out/aide`（Linux/arm64，ldflags `buildVersion=0.1.5.0-RC4`、`buildCommit=ee763764…`）。

## 兼容迁移与权限范围（HOME 挂载）

`AIDE_LOCAL_ROOT` 默认 `$HOME` 可写挂载保持现状（用户此前要求的功能），本轮未收紧挂载；R03 的默认收紧（默认仅挂显式项目、辅助资料只读、写目录逐项授权）未在本次实施，避免破坏现有访问；如后续实施将随附迁移说明与验收清单。

## 状态：整改完成，等待用户审阅发布

不合并 main、不升版、不重建生产容器。生产容器 `aide-aide-1` 未受本轮影响。
