# 星图文件预览使用独立选项卡

任务：[starmap-file-new-tab-20261019](../../tasks/starmap-file-new-tab-20261019.json)。基线 main `e4dfec19`，修复分支 `fix/starmap-file-new-tab-20261019`。

## 当前行为

用户要求在星图打开文件时使用新选项卡。原链接已经有 `target=_blank`，但 `starmap.js` 的统一点击处理传入了 `navigate: location.assign`，预览模块优先执行该分支，实际替换了星图页。

现在移除同页导航模式，普通点击同步创建独立的只读预览选项卡。节点详情、原文、源码、关系来源和 AI 找到的文件均继续使用同一预览入口。原星图没有发生页面跳转，因此保留当前搜索、视角和节点状态；后续文件复用同一个属于本星图的只读预览标签，避免每次点击累积一页。浏览器可能根据用户偏好把 `_blank` 显示为独立窗口，产品不覆盖系统偏好。

原有来源、工作区、只读、行号、哈希参数原样保留。本地与 SSH 引用复用同一文件定位链。锁屏、来源过期、工作区变化仍拒绝或关闭受控预览；用户已将预览页导航到其他网站或编辑路由时，不强行复用或关闭该页。弹窗被拦截时提示用户允许打开预览，不退回替换星图页。旧文件视图的安全返回参数解析保留兼容，新预览不携带同页返回参数。

## 已执行验证

- `node scripts/starmap-file-preview.test.cjs`：实际 map 初始化片段不调用当前页导航；第一次 `_blank` 打开、后续复用、只读与本地／SSH 定位参数、窗口所有权、弹窗拦截、锁屏、过期工作区／来源、原文行号与内容哈希检查通过。
- `node scripts/file-route-location.test.cjs`：UTF-8／CRLF 原文前缀、完整哈希、旧路由和晚到读取／摘要结果隔离通过。
- `node --check internal/server/web/starmap.js` 与 `node --check internal/server/web/starmap-preview.js` 通过。
- Safari 原生交互组件验收：页面加载真实产品预览模块与从真实 map 代码提取的初始化片段，文件内容页为明确标注的合成定位页。打开本地文件后，Safari 标签数从 5 到 6；原测试星图标签仍存在，搜索框值“保留这个搜索条件”不变。第二次打开 SSH 引用后仍为 6 个标签，原预览 URL 的 `previewRequest` 从 1 变为 2；定位显示 `source=ssh-reference`、`path=docs/ssh.md`、`line=42`、`codeHash=fixture-hash` 和 `readOnly=true`。
- 截图保留在私有 `.agent-state/starmap-file-new-tab-20261019/safari-preview.png`。仅合成定位数据，不包含真实消息、SSH 凭据或文件正文。

内置浏览器组件首次返回了打开成功提示，但标签清单未暴露其弹出页，因此未以该提示作为多标签通过证据；最终以 Safari 的实际标签和定位内容为准。

## 验证与交付边界

沿用用户对小范围前端修改的简单测试与合入 main 要求，不重跑全库 Go race/full，也不把旧 full 收据用于本次变更；依赖 full 的常规 release-check 不运行，不修改仓库工作流。文档链接和 diff 检查单独执行。

本轮没有重建／替换运行镜像，没有通过原始启动器为新代码建立完整工作台候选。既有原始 `start.command` 启动的 `https://localhost:59197/` 仍停在 Safari 证书提示；完整工作台、实际文件正文渲染和真实 SSH 服务未新增验收。组件验证证明标签打开、复用和参数传递，不等同于这些内容读取验收。

本轮临时 HTTP 服务 59198、内置浏览器组件标签和两个 Safari 测试标签均已关闭。保留用户原页面、未保存的 Release 编辑表单及原有证书交接页；没有关闭整个浏览器或删除运行数据。回滚可撤销本轮前端合并，不涉及消息、通知配置、索引或原文件迁移。

源码 `fe5298dd` 已合入 main（`2c2d74d6`）并推送至 origin/main。运行镜像未更新，需后续重建／更新实例后实际工作台才使用这份前端代码。源码回滚可在复核后续变更后使用 `git revert -m 1 2c2d74d6`。
