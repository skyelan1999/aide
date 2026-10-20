# 终端工作台界面重排 · 2026-10-20

双栏工作台：左侧搜索会话名称、主机、用户名或目录，按全部、运行中、SSH筛选；右侧显示当前连接、分组操作、会话标签和终端。选中状态、后台连接与日志状态使用真实数据。手机使用抽屉导航，Escape关闭搜索或抽屉。改变窗口大小时，活动标签保持可见。终端保持深色，界面随宿主主题变化。

## 验证

- `npm run build`：通过，离线HTML 345201字节，源码与宿主插件镜像一致。
- `node --check`：修改的应用与浏览器脚本通过；`git diff --check`通过。
- 宿主 `node --test --test-isolation=none scripts/plugin-workbench.test.cjs scripts/plugin-workbench-client.test.cjs`：14/14通过。
- `test/browser-workbench.cjs`：真实Chromium，217次调用、1次通信续期，本地PTY、SSH指纹与密码、会话隔离、面板关闭后后台运行、重载恢复、日志下载、搜索筛选、结束删除均通过。此轮后仅调整主题背景混色、布局滚动边界与活动标签可见性；完整功能收据为 `functional-result.json`。
- 最终源码 `test/browser-layout.cjs`：四种专业/经典明暗主题、选中标签对比度、1024/768/390px无横向溢出、手机搜索切换与Escape通过；`layout-result.json`及截图对应最终 `fingerprints.json`。Chromium无页面异常；截图展示真实Shell输出。

浏览器在隔离候选 `https://127.0.0.1:8100` 中运行，未替换生产实例；脚本创建的布局会话已结束并删除。本次未重跑已知受环境阻塞的宿主full门禁，也未进行正式发布。已发布v1.1.0 ZIP与tag保持不变。

## 复现

在独立插件目录设置隔离实例的 `AIDE_TEST_URL`、`AIDE_TEST_DATA`、`AIDE_EVIDENCE_DIR`，运行 `node test/browser-workbench.cjs` 或 `node test/browser-layout.cjs`。前者需要loopback SSH监听权限，后者使用系统Chromium。不要指向生产工作区。

## 新标签页补验

点击插件卡片及工作台外层标题栏的“新标签页打开”，新版工作台自动打开。真实浏览器验证原页关闭后继续输入、刷新恢复、390px与日志下载通过，见 `new-tab-result.json` / `new-tab.png`。URL仅含插件ID。

连续验收期间候选服务的8个临时通信实例配额耗尽，返回429；只重启隔离8100候选清理测试残留，生产8097未变更。浏览器脚本改为显式关闭工作台再关闭测试页，并按本轮创建的会话ID选择，避免历史测试记录干扰。复验全部通过。
