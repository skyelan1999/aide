# 工作台协调与动态背景验收

任务：`workbench-polish-20261016`。基线 main `0f691833766d3fa71640f0501132dafd07bbe803`，开发目录为移动硬盘 `aide_ws/aide`。未修改旧 iCloud 项目。

共用样式收敛导航条目、按钮高度、SVG 图标比例、圆角、正文宽度、输入区间距及文件列表。模型设置、新建会话、上一级目录、命令面板的字符图标改用现有 SVG 风格。清理首页 SVG 多余闭合标签。保持既有侧栏拖拽宽度和会话运行逻辑。

外观新增“工作台背景”：默认关闭、内置流光、本地素材。支持 MP4/WebM/GIF/PNG/JPEG/WebP，最大30 MiB，素材仅保存在当前浏览器；切换、关闭和离开页面释放对象URL及播放器。后台/锁屏暂停，减少动态效果时流光静止、视频暂停且隐藏、GIF/WebP卸载。CSP 只给图片/视频开放本地 blob，外部媒体、脚本与连接规则不扩张。

## 已执行

- `node scripts/workbench-background.test.cjs`：PASS。覆盖默认关闭、stale IndexedDB读取、视频静音/暂停/恢复、GIF卸载、对象URL释放、30 MiB/格式校验、保存失败保留旧素材、成功替换、删除和跨标签偏好规范化。
- `node scripts/test_i18n.cjs`、`node scripts/session-run-controls.test.cjs`、JS语法与 diff检查：PASS。
- `python3 scripts/agent-route.py verify quick`：84项 PASS，完整检查随后绑定最终内容指纹。
- 实际 IAB 页面：使用生产首页标记、全部共用 CSS、真实背景模块和真实分段设置渲染函数，配合合成会话/文件数据；无模型调用。1440×960 专业/舒适/经典明暗6组无页面横向溢出，设置内容均未横向溢出。
- 390×844 发现导航宽度与旧横排规则冲突，已修复为横向滚动；复查页面/设置内容均无横向溢出，正文、输入框可见。窄屏预览见[截图](workbench-polish/narrow.png)，桌面稳定状态见[截图](workbench-polish/desktop.png)。
- 通过真实文件选择器加载自建 GIF；刷新恢复本地素材，减少动态效果时 `src` 卸载。浏览器实际加载合成 MP4，`readyState=4`、宽160、静音、未暂停，减少动态效果后 paused=true；单装饰层。
- 所有自建浏览器标签已关闭，视口已复位，预览服务器已停止。无新临时工作树，无删除用户数据。再次进入外观设置时清理已离开的背景设置节点，避免反复访问累积引用；对应回归通过。
- 从 `start.command` 启动并更新本机9999；鉴权健康检查、响应资源与源码对齐，更新前后的认证/配置/来源/全部会话哈希与挂载一致。

## 验收边界

上述是实际浏览器中的生产组件验收，含真实 IndexedDB 和 MP4 播放；不等同于完整认证 Safari 实例操作。Safari 验收沿用用户此前暂缓决定；WebM 的实际编解码兼容、各浏览器/移动设备和另一端离线导入尚待验证。锁屏与后台生命周期有回归覆盖，未新增线上锁屏/多标签压力测试。未新增付费模型请求。全量检查、独立main门禁和远端SHA随后补实际结果，不预记PASS。

Final runtime sha256:4cf57ac959708c9c41f60caddae63c9913901497fb138c5dd10c30c0e046c432, source a90c65e66ac6c4205ffe9df784ec31c382bf43c696d748a212b01b7329f6eca6. Offline aide-0.1.17.0-RC2-linux-arm64-4cf57ac95970-offline.tar.gz, 705765015 bytes, SHA256 c24e21ef14f128a66b37ea1ad91cd837a9331d02d86771d821a0b8c4ee60b3d3. Outer/inner hashes, app/dependency tags, platform, launchers and actual docker load PASS. Own b7ba intermediate archive/image removed;60f rollback preserved.

首次 main full 的前85项通过，最后 Go race 在新增 TestWorkbenchBackgroundAssetPolicy 中因直接访问尚未初始化的 a.handler 而 panic（535.394秒）。这是本轮测试装配错误；已改为标准 a.Handler() 入口。未推送远端；先单独执行该策略测试及vet，再重跑完整full并以新收据为准。

修正后，单独的 TestWorkbenchBackgroundAssetPolicy race 与 go vet 全包均已通过；原错误不是工作台运行时崩溃。最终full仍须重跑。

策略测试装配修正后重新从start.command构建，最终候选取代4cf：sha256:4cc2308dbb4be83159ddb92cb664b0f1d78095630858e3f87927ab139db6abfe，source 800a235afe888f8a61ca00da1179fa3bdac3b033456e5596b3d89bc3f960a73f。完整包 aide-0.1.17.0-RC2-linux-arm64-4cc2308dbb4b-offline.tar.gz，705763057字节，SHA256 bcaea979f89c67d596dcbe636b361aaad2e8b3a466b1ce8990ea61165d3b8eb3；完整导入/摘要/平台/依赖/启动器再次通过。4cf本轮中间包与镜像已清理，60f回滚保留。产品界面代码与已验收4cf相同。

Final clean-main full86 PASS: {"status": "pass", "count": 86, "fingerprint": "fc3d2b98af58fe9d417bba53765429ad994cc8fff6f3232ea0b1064945f76a6a", "sourceCommit": "cc114734a2d03921742ee0b0b2ae0e54dbf69ba0", "serverRaceSeconds": 552.597}

Independent main gate PASS; no-force main push 7f0e876802905b9327c666b4c76510909ad5e035 exactly verified remotely. Final receipt commit follows. No version/tag/GitHub Release.
