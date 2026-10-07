# 模型驱动的执行循环

## 当前改动

自动策略下的自动 AI 工作流改为 `agent` 工具循环，模型选择步骤与顺序。手动策略、明确指定的阶段、已经开始固定三阶段的旧暂停任务仍保留原流程。重试保留 `workflowPhase`，不丢失用户选择。

`update_plan` 维护每轮任务的计划：1至12项，状态为 pending / in_progress / completed / blocked，同一时间最多一个执行中步骤。`evidence` 可引用已有工具记录的1基编号；编号存在仅证明引用有效，不证明结论正确。计划在工具结果检查点一起持久化，恢复时继续原对话链。计划是模型评估，不能替代授权、文件应用状态或验证证据。简单问题无需计划。

聊天与新的自动工作流在有工具结果、模型准备结束且预算允许时，追加一次模型收尾自检：结合原对话、计划与最近16条实际工具结果摘要，模型决定继续行动、报告阻塞或交付。摘要每条最多400字，完整工具结果仍在前文。不会按固定业务路径调度，也不会绕过工具轮数、取消、网站和命令权限。自检增加一次模型请求，复杂任务可能增加延迟与费用；状态持久化避免暂停恢复重复自检。

自动模式不再强制按需求/设计/实施/验证四类子智能体分工，也不要求为了流程选择齐参数配置。模型仍可在用户允许且确有必要时使用既有委派工具。

入口和接口：`internal/server/workflow.go` 的 `execute` / `toolLoop` / `executeToolCall`，`internal/server/model_led_agent.go` 的计划及收尾检查；任务 JSON 增加可选 `workflowPhase`、`agentPlan`、`agentReviewDone` 字段。现有文件提案、附件范围、命令确认和禁用工具执行检查保持有效。界面复用工具轨迹展示计划更新，新工作流标题显示“自主执行”。

## 参考与适用边界

- Codex：本地源码 `Harness/openai--codex/codex-rs/core/src/tools/handlers/plan.rs` 的模型计划工具；[官方 harness 说明](https://developers.openai.com/blog/codex-as-a-platform)中的会话、工具、权限分工。
- DSH：本地源码 `Harness/deepseek-ai--deepseek-harness/packages/core/agent-loop/src/index.ts` 的循环与持久会话恢复。引用当前本地文件，不声称与上游最新版本一致。
- Claude Code：[官方执行循环文档](https://code.claude.com/docs/en/how-claude-code-works)描述由模型根据工具反馈决定下一步，调查、行动、验证交错进行。未复制其未公开实现。

这是 Aide 原有模型和工具上的 harness 改造，没有接入三款产品的账号或收费服务，没有替换当前模型。更好的执行框架不能保证较弱模型获得更强模型的推理能力。收尾自检仍可能判断错误，不能把模型的完成状态当作产品验收通过。

2026-10-07 已补充模型计划、兼容入口、工具编号与一次收尾自检回归，并通过实际 DeepSeek / Safari 只读目录任务检查连续工具执行及页面显示。详细结果与局限见[验收记录](../reviews/2026-10-07-model-led-acceptance.md)。该场景不能代表所有任务成功率；跨模型质量对比、跨设备首装和全部主题/窄屏组合仍未验收。

## 回滚

本地构建前保留镜像 `aide:model-led-before-20261007`。本次任务状态新增字段为可选字段；回滚旧程序会忽略它们。含 `agent` 步骤的进行中任务应先暂停并保留会话数据，旧版本不支持续跑新的单循环工作流。本次验收通过后以 0.1.15.0 RC2 交付；发布与制品状态见对应任务记录及发布说明。
