# 商店插件路由验收

任务 `plugin-router-20261021`，基线 `80542984d28e1d260126ae14ce867c403e9bb861`。客户端日期2026-10-21（Asia/Shanghai）。实现与测试结果、源码及商店发布状态将在本轮验收完成后记录；以下不把正在运行的检查视为通过。

恢复环境后，上一轮末次测试日志为空，已重新运行。已发现当前 Linux 执行环境未提供 Landlock ABI（返回 function not implemented），既有 `TestHarnessSkillsToolsAndHookExecution` 中 pwd Hook 被宿主按原安全规则拒绝，未绕过安全策略。

隔离验收使用 testApp/httptest、模拟官方包下载、模拟模型服务和自有浏览器页面，不访问付费模型或用户设备。实际入口 `start.command` 的部署结果、宿主 full 门禁及镜像状态与源码测试分开记录。

## 最终结果

- PASS：23项Go race测试（含子场景），覆盖工具为空/受限阶段不扩权、路由声明、关键词边界、默认关闭与代数CAS、损坏授权文件不授权、安装与动态工具加载、停用不重启、管理员/代理限制、授权撤销/工作区变化/只读/取消/锁代数/坏包拒绝、私有包和不匹配拒绝、后台默认下载、本地查询不等待网络锁、模拟模型安装后下一轮真实调用插件，以及实际商店浏览器。原有插件执行回归单独通过。
- PASS：既有商店JS回归新增授权与需求匹配断言；相关JS语法检查。商店8项Python单元测试、搜索边界Node断言、21条目与20个公开ZIP摘要/路径/内容校验通过，私有条目没有公开包。
- PASS：真实隔离工作台点击商店需求匹配，开启/关闭自动安装并读取API确认落盘；1440/390、舒适/蓝色明暗无横向溢出。另在同一自有浏览器页打开实际静态商店文件，完整需求“我需要SSH连接设备”命中唯一终端条目；截图见下。截图已实际查看。浏览器、上下文、HTTP服务均按测试finally/defer退出。
- quick：140/142，仍为既有 `scripts/test_launcher_data.py` 和 `scripts/test_offline_bundle.py` 失败；未将范围内测试视为full通过。精确内容指纹与命令见 [verification.json](verification.json)。
- `AIDE_OPEN_BROWSER=0 bash start.command`：源码镜像在CAD依赖 `cadquery-ocp-novtk` 下载时因PyPI TLS证书链校验失败。未关闭TLS校验；既有启动器转入旧镜像回退下载后，已核验本任务PID、工作目录和专属输出日志，只停止此进程树，清除本轮未完成的可再生缓存。未将旧镜像当成当前功能部署。

只交付宿主源码与商店路由索引/页面，不升宿主版本，不发宿主Release/tag/镜像。Linux实际验收；Windows/macOS未实测。保留开始时10个未跟踪账本，不覆盖其他任务。确定性关键词匹配不承诺任意语义理解或固定token节省比例，调用结果和历史仍占上下文。

截图：[窄屏宿主商店](comfort-light-390.png)、[桌面宿主商店](comfort-light-1440.png)、[静态商店需求搜索](store-routing-390.png)。

商店 `327efd2` 已推送，现有 [Pages发布流水线](https://github.com/skyelan1999/aide-pkg-shop/actions/runs/39532234869)成功；线上 routing.json、catalog.json、market.js 与本地对应提交的 SHA-256 完全一致，21条路由已上线。哈希见 verification.json。宿主仍为源码交付，镜像未发布。
