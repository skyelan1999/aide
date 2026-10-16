# 思考过程最新预览与折叠交互

任务：`reasoning-live-preview-20261016`。基线 `ac8e1dcc`，外置盘开发分支 `feat/reasoning-live-preview-20261016`。

用户报告折叠只见首行、无法展开。旧实现明确取第一行，且每个流式事件替换 summary 全部子节点，点击目标可能在指针按下与松开间消失；展开意图只等异步 toggle 写入，轮询重建之间存在状态窗口。脱离页面的旧卡片也能写回状态。这些是代码路径确认的失效条件，不能据此断言用户每次失败都由同一时序触发。

现在显示最新非空行，长行保留末尾110字符。summary 子节点保持稳定，点击同步保存状态，原生键盘激活仍可用；脱离页面的旧卡片无权写回。终态最新步骤继承展开状态，早先步骤独立控制。展开正文对保留记录变化做替换，不再假设记录永远只增长；折叠不保留隐藏大段正文。

实际验证：

- 扩展 `node scripts/run-activity-cards.test.cjs`：旧源码在最新行断言失败；修改后通过。覆盖稳定点击子节点、同步意图、失效旧节点、旧步骤隔离与已有单卡片/轮询恢复回归。
- `python3 scripts/agent-route.py verify quick`：88项通过，日志 `.agent-state/reasoning-live-preview/quick.log`。
- `python3 scripts/fixtures/render-thinking-preview.py` 生成直接抽取生产函数/CSS的组件。唯一IAB页在75毫秒持续更新下点击展开成功，停止时已显示第60条，轮询重建仍展开。保留正文替换后Enter折叠，隐藏正文0字符，Space展开，再完成任务仍展开；390px明暗主题无横向溢出。组件使用合成数据，未调用真实模型，不能代替服务端模型能力测试。
- 截图 `.agent-state/reasoning-live-preview/component-light-390.png`、`component-dark-390.png`。

本机镜像与完整离线包已验证，full90已通过，main独立门禁、推送与远端精确核对已通过。保留原7b6321af镜像和离线包作为回滚，不升版、不打tag、不创建正式Release；接收端导入待用户验证。此任务只关闭思考区问题，不关闭连续开发主账本的12项工作流。

本机已通过指定 `start.command` 更新至473463bc镜像，15份网页资源与源码一致，健康、许可证、原14会话/配置/认证/挂载逐项校验通过。首次观察发生于构建尚未完成时，指纹断言未通过，等启动实际完成后重新校验通过；未把该中间观察算成成功。Safari原页面刷新后预览呈现最新末尾，点击展开已有记录，Return收起通过；未新建模型任务。

完整离线包706096093字节，SHA-256为4b4061064745d6a0d2dccac1f2a2368d337f8e9a1f91b4dde56c5c29257ce247。应用+依赖、内外摘要、启动器、许可证与本机实际导入通过，原镜像7b6321af保留。组件页41/18766服务已关闭，viewport已恢复，用户原3个标签及原会话现场保留。完整结构化证据见同目录 `reasoning-live-preview-{runtime,bundle,browser}-proof.json`。

完整验证 `20261016T042027736104Z`：90项全部通过（含隔离Go race/vet），源码指纹 `e04e49e30088f16bed1859687d570c026ed3206dfdea2c261ca6b5373d8c2eb6`，冻结提交 `124714d27b026bc9b594403a0ed04e486cfd4c2a`。结构化结果见 `reasoning-live-preview-full-proof.json`。

已授权快进main，独立release-check通过，推送与远端精确核对 `2c9bb88d5047d3394bc6d66922b19e1af0913a54` 一致。server race638.182秒，TTS1.523秒；最终文档回执另在干净main运行门禁并推送，精确最终HEAD保留在私有交付收据。未升版、打tag或创建Release，接收设备导入待验证。
