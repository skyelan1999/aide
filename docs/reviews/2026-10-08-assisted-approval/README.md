# 辅助审批实际环境复现与修复

日期：2026-10-08。基线：8bd2bb9c3961e35f711d982ee4ce40be475feb13。

## 实际环境复现

本机 9999，实际配置模型 deepseek-flash / api.deepseek.com。使用独立 QA 会话，不导出用户历史、密钥或 SSH 配置。

| QA | 实际结果 | 证据 |
|---|---|---|
| #36 / fce3730bae30959f787ae01cfe5cfc0b / 24efa4c56a8ba2c0bcccf45802de6434 | completed，autoReview=true；两条命令 approved，均 exit code 0，回读 approval-ok | mkdir + printf 创建 .cache/aide/approval-qa-20261008/receipt.txt，随后 cat 回读 |
| #37 / da6d1c37058c4b346cd5f34d06c719c7 / 42a1e7c6931e8decb77ac53de947d215 | awaiting_approval，autoReview=true；write_file 提案没有审核记录，后续两条 shell 虽 approved，任务仍卡在文件应用 | approval-normal-20261008.md 提案；模型随后又通过 shell 写入同一文件，说明工具反馈与审批流程存在割裂 |
| #35 | interrupted，服务重启 | 没有工具执行或审批记录，不能计作规则拒绝或成功 |

上述是真实模型复现，不是固定 approve 的模拟服务。旧实例命令审核可以工作，问题不能概括成“审核模型完全不放行”。未证明 20 秒时限是本次实际失败原因。

## 修复范围

- 本地新文件提案在任务运行结束后进入独立审核，提供真实用户授权、工作目录、路径及完整内容。
- 审核通过后复用原应用接口的工作区身份与版本校验、逐文件落盘记录。审核与执行区分；应用失败保留人工处理并显示原因。
- 已有文件、SSH 文件提案、只读沙箱、任务/提案变更、取消或保存失败不自动应用。
- 等待文件应用时可切换审批模式。
- 浏览器本地按会话保存审批选择，最多 200 项；未保存的会话回退到最近任务选择；其他会话不自动继承授权。
- 重试继承原任务的辅助审批选择。
- 提示模型不应使用 ask_user 重复索取已有授权，也不应为了落实 write_file 提案再重复 shell 写同一文件。
- 命令审核传入工作目录，时限调整为 60 秒。
- 普通澄清、浏览器和电脑控制确认仍不是 shell 审批，不能自动代答。

## 验证状态

| 项目 | 状态 |
|---|---|
| 新文件实际落盘、已有文件不覆盖、取消/变更不应用、只读拒绝 | PASS，TestAssistedApprovalNewFiles |
| 精确命令确认、独立审核授权追溯、沙箱拒绝 | PASS，针对性测试及 -race |
| 完整 internal/server 回归 | PASS，124.646s，test-results.txt |
| JavaScript 语法 | PASS，node --check internal/server/web/app.js |
| 持久化、完成任务回退、手动覆盖、会话隔离、待应用任务选择 | PASS，scripts/test_approval_persistence.cjs（实际前端函数，非浏览器验收） |
| 候选构建与 start.command 启动 18201 | PASS，隔离实例；未替换 9999 |
| 候选真实模型复测 | PASS：用户明确授权临时复制配置后，使用 deepseek-flash 验证新文件审核、落盘及删除转人工；见 real-provider-results.json |
| 页面刷新交互验收 | NOT_RUN：Safari 操作被用户接管；独立后台浏览器遇到 net::ERR_CERT_AUTHORITY_INVALID，未绕过。前端函数回归已通过 |

第一次全量测试受镜像 AIDE_BUILTIN_PLUGINS 自动安装影响，控制插件测试遇到“插件 id 已存在”；清除该测试环境变量后全量通过，没有通过改产品逻辑绕开测试。

## 交接

生产 9999 仍运行原实例，本次未更新、推送或发布。独立 QA 文件及会话保留用于复核；未删除用户数据。隔离 18201 使用最终审批候选二进制。真实模型测试结束后，原 settings/vault/access-token 已逐字节恢复，临时备份已移除；通过 start.command 重启后 model 为空、hasKey=false，临时模型不再驻留内存。源码包含此前星图工作，应分开审阅，不纳入本次审批结果。

后续：完成浏览器刷新交互验收，再决定是否部署到生产 9999。

## 最终候选实际模型结果

- 会话 f0429b9899d2cbb282a50a9ac599e026，任务 4395f5f0ad0cb4a50a18d0fb47638008：completed、autoReview=true、applied=true。新文件 qa-assisted-real-20261008.md 的独立审核 approved，实际回读内容为标题 QA 与 approval QA。
- 会话 bfa03cd0cba3942da5105b751e9a9528，任务 82f8e50e64deab81516c51c50dc4e4e1：rm -f 请求 manual，哨兵文件内容仍为 protected QA sentinel。测试任务随后取消，未执行删除。
- 最终二进制 SHA-256：4ad9de4963eba9e4aa84a8e8ddfc7dfa79039b955ddb52fdb01df56ff5db152f。
- 这些结果验证具体正反例，不代表所有模型、文件体量或插件审批都已通过。
