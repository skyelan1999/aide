# aide 运行时端到端走查清单（runtime walkthrough）

- 目标应用：https://127.0.0.1:8097 （容器 aide-aide-1，宿主 8097→容器 8080，healthy）
- 走查时间：2026-09-28
- 走查方式：Playwright 真实浏览器 + curl 接口核验 + docker logs 对照
- 总项数：9 大项（含子项共 22 个检查点）
- 结果：PASS 18 / FAIL 4（其中 3 项 FAIL 为「主流程 PASS 但存在缺陷」，已记入 findings；无 P0/P1）

## 逐项结果

| # | 走查项 | 结果 | 一句话结论 |
|---|--------|------|-----------|
| 1 | 登录/鉴权（坏token/无token/好token） | PASS | 坏 token 与无 Authorization 均 401；正确 token 登录成功；清 localStorage 后弹「连接你的工作台」令牌框。证据 01/01b。 |
| 2 | 普通对话 + 流式 SSE 逐字 | PASS | POST /runs→202，EventSource /events?access_token= 逐字流式渲染，Markdown 正常，会话自动命名。证据 02/02b/02c。 |
| 3 | 运行中插话(steer)/排队 | PASS | 长任务流式中再发消息，POST /runs→202 被接受无报错，后续回答纳入插话内容；输入区有「排队」开关。证据 03-running/03b-interject。 |
| 4 | 工具调用（run_shell/read/write） | PASS | 模型调用 run_shell 落盘并执行 greeting.py（exit 0，输出「你好QA 123」），工具活动行可展开。证据 04。 |
| 5 | 文件提案审批（批准落盘/冲突态） | **FAIL(有缺陷)** | 批准 happy-path 200 落盘正确；但冲突 apply→409 时前端无任何提示、按钮残留（见 findings-001）；卡片未见显式「拒绝」按钮（findings-005）。证据 05/05b/05c。 |
| 6 | 文件查看器（md/txt/图片/pdf） | PASS | txt(a.txt)、md(TASK 渲染标题/列表/引用/提示块)、png(中秋海报缩放)、pdf(2页翻页) 均模态弹窗正常。证据 06a-06d。 |
| 7 | SQLite 查看与修改 | PASS(只读设计) | 打开 demo.db 列出 orders/users/sqlite_sequence；容器内改 orders.amount 后重开查看器即回显（读快照+持久化可见）；后端仅 GET tables/data，无写接口。证据 07/07b。 |
| 8 | 设置与权限面板 | PASS | 沙箱三档/工具开关可读写；切「工作区可写」后 /data/settings.json sandboxMode 持久化；模型参数只读预设。证据 08/08b/08c。 |
| 9 | 会话三点菜单（置顶/归档/删除） | PASS | 置顶排序正确(📌在上)；归档后从活跃列表消失、设置→归档视图可「恢复」(pinned 状态保留)；删除弹原生确认框，删后 GET 404。证据 09/09b/09c。 |

## 子项失败/缺陷汇总（详见 findings/runtime-functional.json）
- FAIL-5a：apply 409 无前端反馈（P2, high）—— findings-001
- FAIL-5b：提案卡片无显式拒绝按钮（P3, low）—— findings-005
- FAIL-console：CSP 拦截动态内联样式，console 洪水（P3, medium）—— findings-002
- FAIL-static：highlight.js ruby/php/matlab 语言包 404+MIME 拒绝（P3, high）—— findings-003
- 加固项：SSE token 走 query 参数（P3, high）—— findings-004

## 证据截图清单（evidence/runtime/）
01-login-dialog, 01b-authenticated, 02-chat-streamed, 02b-streaming-mid, 02c-streaming-partial,
03-running, 03b-interject, 04-tool-call-file-proposal, 05-after-apply-click, 05b-reject-proposal,
05c-apply-200, 06a-txt-viewer, 06b-md-viewer, 06c-image-viewer, 06d-pdf-viewer,
07-sqlite-viewer, 07b-sqlite-after-update, 08-settings, 08b-permissions, 08c-model-settings,
09-after-archive, 09b-archive-view, 09c-after-delete

## 环境与约束备注
- QA 造数集中在容器 /local/Users/skyelan/debug/__qa_walkthrough__/（含 greeting.py，apply 落盘，已按要求保留未删）。
- 未做恢复出厂/docker build·down/改配置；未删除用户既有会话（删除的是 QA 自建「新会话」f09c7cf2）。
- 容器 INFO 级日志不记录单条 HTTP，401/409 等状态以浏览器 network 与 curl 取证。
