# 模型自主执行验收 · 2026-10-07

## 范围与环境

任务：model-led-agent-20261007。用户授权：验收、更新文档并发布。
当前本地模型 deepseek-chat；Safari 访问 https://localhost:9999，使用独立会话 #34 做只读目录任务。验收不读文件正文、不修改用户工作文件。

## 已执行的检查

- 新增 TestModelLedWorkflowCompatibility：自动入口、手动策略、明确阶段及旧任务兼容。
- 新增 TestModelLedPlanValidationAndJSONPersistence：有效计划、非法状态/不存在证据/多个进行中项拒绝；任务 JSON 保存恢复。
- 新增 TestModelLedLoopPlansReceiptsAndSingleReview：实际 HTTP 模拟模型调用链、工具记录编号、计划更新、一次收尾自检、恢复后不重复自检。
- 暂停恢复、重启恢复、预算、文件批准与路径权限沿用现有回归。暂停恢复测试增加一次收尾检查预期，保留不重复已完成工具调用的断言。
- 实际 DeepSeek / Safari：新自动工作流只包含自主执行阶段；工具为 update_plan、list_files、update_plan 共3次；页面显示完成、3条工具记录。无 ask_user、write_file 或读取文件正文动作。随后直接检查该验收会话的磁盘 JSON：status=completed、steps=[agent]、agentReviewDone=true，三项计划及证据编号均已保存。

## 观察与限制

模型将要求的“两步计划”拆为3项（包含总结），说明细粒度指令遵循仍有偏差。本次证明该只读场景可连续自主执行，不证明所有未来研究、代码修改、噪声仿真任务都能成功。页面收尾答案偏长，且部分描述只靠名称推断；不能把模型自检当成独立正确性证明。

桌面 Safari 已实际查看工作流及工具展开交互；未遍历全部主题和窄屏组合。内置浏览器受本地证书限制，未绕过告警，转用已正常访问的 Safari。

首次完整测试在取诊断堆栈时被人工中断，结果为 FAIL，不作为通过证据；另一次被系统终止、一次失效重复实例被停止，均保留失败记录。最终单实例完整回归通过：19项检查退出码均为0，go test -race 的 server 包耗时472.341秒，go vet通过；有效收据为 verify-20261007T051106700665Z.log，源码指纹 68b7f26d7959f19bc31fcbae9b9beaabebb9a10adaa30495522652d09037eadb。

发布包补入既有 macOS 浏览器/电脑桥接脚本、Swift 源码与浏览器 runtime 的依赖清单；不包含本地令牌、会话、权限授权或 node_modules。新设备仍须安装浏览器 runtime 并按文档授予系统权限；本次未进行跨设备首装验收。

## 证据

- 测试源码：internal/server/model_led_agent_test.go、internal/server/workflow_pause_test.go。
- 本地截图：.agent-state/model-led-acceptance.png（用户目录信息，仅本地保留，不上传发行包）。
- 完整回归：.agent-state/full.json 及其 log，按内容指纹判断最终有效收据。
- 架构与回滚：[模型驱动执行循环](../architecture/model-led-agent.md)。
