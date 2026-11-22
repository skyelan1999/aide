# 图纸直接测量按钮与操作期间误锁屏修复

任务 `dwg-native-controls-idle-20261115`，继承 `dwg-spline-recovery-measure-20261115`。原始图纸及归档检查结果保持不变，本轮不修改转换器/绘制几何。

## 问题与结果

旧的空闲监听只包含 mousemove、keydown、click、scroll、touchstart。图纸使用 Pointer/Wheel 完成拖动、测量和滚轮缩放，并会阻止默认鼠标/滚动行为；部分操作没有进入原监听。内层滚动也可能因不冒泡而漏记。现改为捕获阶段的被动监听，补齐 pointerdown/move/up 和 wheel，覆盖查看器取消默认行为/阻止冒泡的操作。

回归测试另发现跨页通知仅节流首条 ping，停止操作前的末次输入可能不送达工作台，主计时会提前到期。现在至多排一个尾次通知，携带实际输入时间；主计时钳制未来时间并拒绝旧输入回退时间。真实输入才续期，轮询/心跳不续期。退出页面、晋升主页面和手动锁定时清理尾次通知。

空闲回调同时验证当前 timer 身份、锁状态、主页面角色和当前空闲截止时间；已被新输入替代、但曾经排队的回调不能误锁屏。未改变密码、配置的超时值、后台任务或手动锁定/代际验证规则。

原测量工具选择框改成直接显示的七个按钮：平移、距离、连续长度、角度、面积/周长、半径/直径、坐标。当前按钮高亮并提供 aria-pressed；工具栏支持方向键/Home/End，窄屏自动换行。单位、捕捉、完成、撤销和清空仍保留。

## 实际验证

- `scripts/lock-idle-activity.test.cjs`：执行当前 app 的真实监听/计时代码及真实 LockCluster，覆盖取消默认/停止冒泡的 Pointer/Wheel/内层滚动，文件页→主页面通知、尾次输入、已取消但排队的回调、真正空闲、手动锁及关闭空闲计时。原先首条通知节流会使“末次输入后未满超时”的断言失败，修复后通过。
- `scripts/lock-cluster-state.test.js`、`lock-entry.test.cjs`、`lock-recovery.test.cjs` 及 quick 通过；没有把全量 Go 测试或长达实际生产超时的观测写成已运行。
- 隔离真实工作台 fixture 使用合成密码、临时目录与 4 秒超时，不读取/修改生产账户。实际 Chromium 两页（工作台为主、图纸为从）：只滚轮 7.2 秒或持续 Pointer 拖动 7.2 秒，两页均不锁；随后停止操作，两页正常锁定。
- 实际六个直接测量按钮均生成结果（包括 90°、面积/周长、圆半径/直径），方向键可切换工具；390px 宽度所有按钮都在视口内，工具组 366px 宽且 scrollWidth=366。没有页面 JS 错误。
- 原生 Aide 已看到七个工具按钮，点击“距离”后两次点选生成 7,009.5102 图纸单位结果；原图未声明单位，界面没有推断毫米。测试结果已清空，恢复平移、原来的舒适/跟随系统主题；两页连接恢复。原生仅做控件/点测验收，短超时空闲行为由真实浏览器隔离 fixture 验证。
- 标准本地构建后 health/integrity 均 ok；86 会话、36 插件、194 项保护文件和原有草稿核对通过。原配置的空闲时间未改变。

原始日志/截图在 `.agent-state/dwg-native-controls-idle-20261115/`：`idle-tests.log`、`cluster-tests.log`、`recovery-tests.log`、`quick.log`、`browser-results.json`、`direct-buttons-narrow.png`、`direct-buttons-desktop.png`、`native-acceptance.json`、`production-verification.json`。客户图纸、访问令牌和用户密码不进入公开记录；fixture 的公开合成密码只用于独立测试实例。

## 本地运行状态

- Aide 版本保持 `0.1.19.0 RC2`，没有提交/tag、推送或公共 Release。
- `buildCommit`: `bd5c6456162e48346e499dadb41f9e8939bd541f-dirty.dcfa3f35e4adbcafbe716908b80247045a8e7cd2995bedf8fb979317435e102e`。
- 镜像：`sha256:d1fb5d2401f987869484fd6a793d340ebb55f0b6327232fdac6abfcf984efd20`。
- 回滚标签 `aide-rollback:dwg-native-controls-idle-20261115`，备份 `/Users/skyelan/WorkTrees/aide-runtime-backups/20261115-dwg-native-controls-idle`；不以还原锁状态文件代替账户解锁。
- 原图纸报告的 453 文件、361 通过、86 提示、5 超限和 1 结构异常没有重新分类；本轮没有重跑全部 425 次转换请求，因为转换路径未改变。
