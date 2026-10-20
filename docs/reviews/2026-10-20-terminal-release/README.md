# Terminal 1.1.0 预发布交付

用户授权：“可以，推上去吧，做好release”（2026-10-20）。本次交付包括常驻本地/SSH工作台、日志与新标签页入口。

主仓库基于最新 origin/main 保留上游修改，通过 codex/terminal-workbench 分支及草稿 PR 交付。独立插件发布为 GitHub 1.1.0 prerelease，固定提交托管 ZIP 与 SHA256SUMS；稳定市场暂不更新，旧宿主缺少 bridge.renew / daemon.detach 时会提示升级，不能把此版本当作旧公开镜像的完整兼容更新。

## 验证与正式发布阻塞

已有验收：6项插件测试、14项桥接/客户端测试、相关 Go race、实际 ZIP 安装、Chromium218次调用与续期，以及新标签页独立调试/原页关闭/刷新/窄屏/下载。详见前两项工作台验收记录。

本次实际执行 full，共138条命令。原始结果见 full-gate.json。发现两个 VM 夹具未声明新入口依赖；已补齐并分别重跑通过，同时增加初始化/解锁后打开的顺序断言。没有通过改写产品锁屏行为迎合夹具。

仍未通过的正式发布门禁：test_launcher_data.py 的 UID1000所有权假设、test_offline_bundle.py 的无桌面浏览器启动/平台假设；Docker中 runtime assets、完整Go race/vet和CAD几何测试均因没有 aide:local / aide-cad:local 镜像而无法执行。Docker尝试拉取本地镜像名被仓库拒绝，未获得可运行镜像。完整 full 无成功收据，因此不合并主仓库、不升宿主版本/tag、不发布宿主正式镜像。

没有生产重启或数据卷替换。源码推送、插件预发布、宿主正式Release是不同状态，以本任务最终远端链接为准。


## 最终远端状态

- 插件已推送至 main：`f08f8f199e25b5a58a2127e96316335cd68a4b34`，标签 `v1.1.0`。
- [Terminal v1.1.0 预发布](https://github.com/skyelan1999/aide-pkg-terminal/releases/tag/v1.1.0) 已公开发布，非草稿；稳定Latest仍为1.0.0。
- [配套宿主草稿 PR #7](https://github.com/skyelan1999/aide/pull/7)，分支 codex/terminal-workbench，包含原main的892e0b99更新；未合并main。
- 安装ZIP来自固定提交仓库目录，Release说明提供直链，没有上传单独附件。已匿名下载核验：100397字节，SHA-256 `e417db91e86caf0a6a926de976d9dd2901c81725acbb5d10229c2b570a97b87b`；见 [远端验证](published.json)。
- 合并上游后的定向Go race已通过（99.711秒）；含本地PDF/WebResearch回归。最初普通沙箱拒绝httptest套接字，允许回环后重跑通过，见 [补充验证](followup-verification.json)。
- 主仓库、插件源码与Release分别有远端证据；稳定市场未升级，生产服务未替换。宿主正式Release仍被full门禁阻塞，没有建立宿主新tag或伪造成功收据。

先前100117字节的验证候选已保留为 `/workspace/aide-pkg-terminal/dist/aide-pkg-terminal-v1.1.0-candidate-1f8e0354.zip`。新发布包仅修改了README的预发布/配套宿主/下载说明，运行文件字节保持相同；旧候选验收不能替代新发布包的独立摘要校验。
