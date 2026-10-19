# 工作台响应延迟排查与修复 · 2026-10-19

任务 [workbench-response-latency-20261019](../../tasks/workbench-response-latency-20261019.json)，基线 `d4c32418`，分支 `fix/workbench-response-latency-20261019`。主流程采用前端 skill，未启用子代理、付费模型或新服务账号。

## 定位与边界

用户尚未回答最明显的操作类型与实际设备／端口。已证实两处网络依赖：设置打开 await 定义下载后才显示；会话子组折叠／展开调用 loadSessions，发出活动与归档两个请求。旧源码分别以“设置必须先于下载显示”“折叠不应发两个请求”断言失败。当前源码通过，同时覆盖乱序请求、工作区／令牌变化、相同快照 DOM 不替换、关闭／重开、下载失败重试和请求共享。

本机 Docker 空闲容器 CPU 低，不能据此证明交互没有卡顿。本机正式 9999 `/api/config` 返回构建 `8f6640646099a94a0ebbf196ec3235577744d9ec`，并非近几轮源码。正式容器读取 `/data`、`/workspace`、`/local`、`/home/aide` 报 Bad file descriptor，宿主目录存在且可 stat；隔离 QA 挂载正常。简单已连接 HTTPS 请求在正式实例约 0.3–8 ms，QA 约 0.1–1.2 ms，只反映内存型接口，不含 TLS 建连、模型生成或文件 I/O。额外只读文件列表探针 HTTP 200、约 22.9 ms，没有在该响应中出现 Bad file descriptor。因此不能把部分挂载检查失败推断为全部接口不可用或用户延迟的已证实根因。14 个正式会话摘要没有 running 状态。没有因此重建正式容器或推断用户一定在此实例上；实际目标仍待用户回复。

## 实现

设置先显示加载反馈，下载共享且带当前打开序号；缓存命中直接渲染。子组本地重绘；列表离屏构建、相同快照保留节点；请求序号及 workspace/token 守卫拒绝旧响应。保留现有自动刷新、选中高亮、助手槽位和归档操作。

## 验证

`node scripts/ui-response-latency.test.cjs`、原 `test_ui_click_latency.cjs`、`lock-recovery.test.cjs`、路由自检通过。新回归加入标准 quick/full。测试最初对子组全展开的总行数漏算“收起”控制，修正预期为 8 行，产品断言仍保留。基线失败原日志在私有任务目录；没有将基线失败记成最终通过。

真实工作台通过原有 `TestLockBrowserFixture`、2 GiB/2 CPU 临时容器及 HTTP loopback 18620 打开。使用既有合成密码 123456，实际 Handler 与当前内嵌前端，创建 100 个合成会话，不调用模型。已验首次／缓存后设置打开关闭、100 项列表、会话选择后的标题／高亮一致、390×760 设置面板和 Escape。旧密码／用户生产数据未改变。浏览器性能计时对象不在 DOM 只读接口中开放，未给出浏览器毫秒级提升百分比；网络等待消除由延迟 Promise 的实际函数回归证明。子组快照操作由产品函数 VM 回归覆盖，未伪称真实模型生成了子会话。

验收页已关闭、视口覆盖已复原；临时 fixture 服务随后停止并清理。截图在私有 `.agent-state/workbench-response-latency-20261019/settings-100-sessions.png`、`settings-narrow.png`。

原启动入口候选已通过：既有隔离 QA 工程运行原 `start.command`，AIDE_BUILD_FROM_SOURCE=1、AIDE_OPEN_BROWSER=0、QA 标记 `d4c32418-response-qa`，退出 0，https://localhost:59197/。镜像 `aide:qa-response-latency-20261019`（`sha256:449fceb487c1079fb397a82fdbe67f636e85b15ac7e2c2f7af26185c28007403`）。每次请求先固定完整 DER 证书，再发鉴权：健康 200、锁态、文件文本／6 个原始夹具以及 4 个内嵌 JS/CSS/HTML 摘要均匹配当前源码。没有绕过浏览器证书告警，也没有修改生产配置。

完整回归从相同内容、独立 Git 元数据的内部源码副本执行，避开已知移动卷 Docker bind mount 问题；收据按当前源码指纹核对后才进入 main 门禁。最终 138 条检查全部 exit 0：含新增 UI 回归、12 个服务器 race 分组、其他包与 vet、原 CAD 几何测试。未修改官方验证器或收据。原始日志 `.agent-state/verify-20261019T101400851866Z.log`，源码指纹 `561b69381cad1ceea72ff8b5d5b1e87a83514e81c725f8f3adc7fd3e45b17f63`，收据 SHA256 `55a2c07c72d71f0b560739d191ca8f39251793a022a17358a4bbec4f7f8f1b35`。与实际项目重新计算的指纹一致。本机正式挂载异常和实际使用端口待对准，未替换生产或发布新版镜像附件。


## 清理与剩余范围

已删除本轮内部临时源码副本、关闭验收页并恢复视口、停止并移除 fixture 容器及其合成数据。已移除无容器引用的旧 QA 镜像标签，保留当前 QA 项目与本地依赖。没有清理用户数据、镜像总库或构建缓存，也没有重建正式容器。

实际卡顿操作／设备／端口仍待用户回答。已完成的源代码改进不代表正式 9999 已替换，也不代表所有卡顿根因都已找到。触控、Safari 原生、Windows、真实模型流和用户长会话性能本轮未验收；没有编造延迟提升百分比。


## 源码 main 交付

功能提交 `d1bc85ad`，main 合并提交 `bb55a086cea5bce0fd2edbcf6e5d1110c7b0d70f`。`python3 scripts/agent-route.py release-check workbench-response-latency-20261019` 通过当前 full、干净 main、各前置阶段及既有默认公开镜像附件元数据门禁。`git push origin main` 成功，`git ls-remote` 与合并 SHA 一致。随后只追加任务／本文交付记录，不修改产品指纹。此门禁不是本轮新镜像发布；未创建 Release／版本／tag，也未替换正式服务。

当前验收候选保留在 59197。正式部署若需要从当前源码构建，应在正确设备／项目目录运行 `AIDE_BUILD_FROM_SOURCE=1 ./start.command` 后刷新；普通启动仍可能复用旧的有效本地镜像。旧正式实例的挂载问题及实际卡顿需要对准用户正在使用的页面继续处理，不把这一步标为已完成。源码回滚：`git revert -m 1 bb55a086cea5bce0fd2edbcf6e5d1110c7b0d70f` 后重建，无数据格式迁移。
