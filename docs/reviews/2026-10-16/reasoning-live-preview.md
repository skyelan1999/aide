# 思考过程最新预览与折叠交互

任务：`reasoning-live-preview-20261016`。基线 `ac8e1dcc`，外置盘开发分支 `feat/reasoning-live-preview-20261016`。

用户报告折叠只见首行、无法展开。旧实现明确取第一行，且每个流式事件替换 summary 全部子节点，点击目标可能在指针按下与松开间消失；展开意图只等异步 toggle 写入，轮询重建之间存在状态窗口。脱离页面的旧卡片也能写回状态。这些是代码路径确认的失效条件，不能据此断言用户每次失败都由同一时序触发。

现在显示最新非空行，长行保留末尾110字符。summary 子节点保持稳定，点击同步保存状态，原生键盘激活仍可用；脱离页面的旧卡片无权写回。终态最新步骤继承展开状态，早先步骤独立控制。展开正文对保留记录变化做替换，不再假设记录永远只增长；折叠不保留隐藏大段正文。

实际验证：

- 扩展 `node scripts/run-activity-cards.test.cjs`：旧源码在最新行断言失败；修改后通过。覆盖稳定点击子节点、同步意图、失效旧节点、旧步骤隔离与已有单卡片/轮询恢复回归。
- `python3 scripts/agent-route.py verify quick`：88项通过，日志 `.agent-state/reasoning-live-preview/quick.log`。
- `python3 scripts/fixtures/render-thinking-preview.py` 生成直接抽取生产函数/CSS的组件。唯一IAB页在75毫秒持续更新下点击展开成功，停止时已显示第60条，轮询重建仍展开。保留正文替换后Enter折叠，隐藏正文0字符，Space展开，再完成任务仍展开；390px明暗主题无横向溢出。组件使用合成数据，未调用真实模型，不能代替服务端模型能力测试。
- 截图 `.agent-state/reasoning-live-preview/component-light-390.png`、`component-dark-390.png`。

本机镜像、完整离线包、full门禁与main交付：待本轮收据补齐。保留原7b6321af镜像和离线包作为回滚，不升版、不打tag、不创建正式Release；接收端导入待用户验证。此任务只关闭思考区问题，不关闭连续开发主账本的12项工作流。
