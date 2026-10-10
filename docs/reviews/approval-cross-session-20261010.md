# 实际环境跨会话审批验收 · 2026-10-10

源码基线：`53c98f9eca090ec4c970f787b251a65f1ff7baab`。实际实例 `https://localhost:9999`，原生 Safari，真实模型 `deepseek-flash`。用户在操作时授权临时切换、精确命令记忆及测试后撤销恢复。

## 实际结果

- 初始辅助审批开启、规则0条、revision1。切换手动后，会话41出现「仅本次允许」「永远记住并允许」。选择记住完整命令，回执 `source=human`，执行退出0。
- 新会话42在手动模式下调用相同命令，直接完成，无人工审批卡。回执 `source=remembered`，规则ID和完整命令指纹与会话41一致。
- 命令为 `printf 'AIDE_REMEMBER_RULE_20261010\n' > .cache/aide/approval-remember-20261010.txt`。独立读回28字节，内容精确匹配，SHA-256 `21f0a67e95d5e4fbc9e9dfbeedac44d4616b44e36d6ce11ac14232b390e8cc4f`。
- 管理审批授权中单独撤销本次规则，UI显示「没有已记住的授权」。恢复辅助模式后，另一个已打开Safari标签无需刷新即显示「辅助审批」。最终API为 `enabled=true`、`rememberedRules=0`、`revision=5`，原模式和规则数量恢复。

规则ID：`69ff7ec41b9efd1a03ab3111ed4918d37d8476b02a9f9efe0c34a6e29557478a`。匹配工作区/根目录 `local|/Users/skyelan/debug|`，命令指纹 `0d993e7302ac4848e19868e724cab82dfd02e1732ca0d11ab22bda184c1f351f`。

## 证据及清理

本地原始证据位于 `.agent-state/continuity-runtime/`（忽略、不发布）：

- `approval-cross-session-20261010.json`：脱敏审批回执、最终策略、精确文件核对。
- `approval-remember-before-safari-20261010.png`：人工审批卡。
- `approval-remember-match-safari-20261010.png`：第二会话规则匹配。
- `approval-global-restored-safari-20261010.png` / `.txt`：另一标签的模式同步。

两个临时标签已关闭，原18211草稿及附件保留；实际测试会话41/42和缓存文件保留作证据。没有新增产品源码，本次只补真实验收及文档。现有36项full回归收据继续有效，不替代未覆盖产品范围。

## 未覆盖范围

撤销经UI和API确认，未再次调用真实模型验证撤销后重新弹卡。辅助→手动发生在第二标签打开前，不能作为反向即时同步证据。SSH规则隔离、执行中到期和并发策略变化仍待实际验收。整个目标保持进行中。
