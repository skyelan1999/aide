本轮已发布 [Terminal 1.5.2 预发布](https://github.com/skyelan1999/aide-pkg-terminal/releases/tag/v1.5.2) 与 Compare 0.3.3 私有预发布。市场更新已推送到 [PR #15](https://github.com/skyelan1999/aide-pkg-shop/pull/15)，尚未合并或部署。时间按 2026-10-29 Asia/Shanghai 记录；账号已确认是 skyelan1999。

盘点了 39 个插件仓库（28 个公开、11 个私有），见 [公开仓库盘点及私有汇总](inventory.json)。Computer Control 1.2.0 等条目已是最新版；仅有 CODEOWNERS、归档托管、文档或测试变化的仓库没有重复发版。私有仓库没有新验收候选的项目也没有凭脚手架创建发行。

| 交付 | 状态 | 身份/边界 |
| --- | --- | --- |
| Terminal 1.5.2 | 已推送修复分支、打 tag、发布预发布 | Release 固定提交 `17227d72ef3017858de3f9e0c30bd06a9941668e`；随后 `3bf0502` 只修正仓库 README 安装入口 |
| Terminal ZIP | 已验证远端固定归档与本地市场包一致 | 1,063,504 bytes，SHA-256 `36984e8e16e33f785f7128a08097271ade036b31cd7223c39be1a01a9fa6a10c` |
| Compare 0.3.3 | 已发布私有预发布 | 二进制和验证详情仅在私有仓库；公开市场保持 coming_soon，无安装、下载或购买入口 |
| 市场更新 | 已验证、提交并推送，待合并 | `release/terminal-1.5.2-20261029` / `43f12cab7e9b311bbfa6bf74959a1cf5e5de5192` |
| 市场线上 | 新版尚未部署 | 当前 main `6d425fb1332f1db01610d630d7c08d5ba18b1f88`，Terminal 仍为 1.5.1 |
| Aide 主程序 | 无产品改动、未合 main、未发版 | 本轮只将公开证据推送到原 issue-review 分支 |

Release 附件上传接口返回 HTTP 401，沿用已有的固定提交归档下载方式。两个 Release 均没有附件；Terminal 的公开下载链接已独立经 HTTP 下载核验，两个仓库的固定提交归档也经 GitHub blob API 解码校验。Compare 的归档保存在私有仓库，不复制到公开市场或本报告。详见 [远端 Release 收据](releases.json)。

Terminal 修复 #565 的输入请求排队：输入与中断优先于尚未执行的后台请求，连续最多 8 项输入后为后台请求让行。已经执行的请求仍需完成，不增加并发或自动重发结果不确定的写入。升级结束现有连接，配置与日志保留。固定归档内的 README 安装介绍仍沿用先前版本文字，当前仓库 README、Release 下载链接及市场说明已指向 1.5.2；未替换已发布字节或历史归档。

实际验证：

- Terminal：`npm run build`、`npm test` 的 34 项测试、`python3 scripts/verify_package.py dist/aide-pkg-terminal-v1.5.2-candidate.zip` 的 25 项解压包通信回归通过。最终 ZIP 的运行文件与验收源码逐字节一致，见 [发行验证](terminal-release.json)。测试使用临时 pyserial 依赖，未改动宿主配置。
- Terminal 浏览器：`node test/browser-latency.cjs` 使用真实 Chromium/xterm、OpenSSH 和生成的 Python TLS1.3 中继，直连及中继各 10 次回显；中位数分别 119ms/107ms，单请求在途且无自动探测。使用串行 12ms 宿主桥夹具，远端回显延迟 15ms，仅回环环境；不能据此推断用户网络延迟或声称完整宿主验收。见 [延迟结果](terminal-latency.json)。
- Compare：Linux AMD64 原生执行 20 项 `go test -race`、`go vet`、6 项 Node 测试及现有 `scripts/check.py` 解压包验收通过，包含实际 pypdf 提取、比较、证据归档、越界/篡改拒绝、测试离线许可证和 Aide plugin_host 调用。使用 Go 1.26.0 及已有构建参数；Linux ARM64 仅交叉编译和 ELF/摘要检查，本轮没有执行 ARM64，未将历史验收算作本轮通过。临时许可证不进入发行包。
- 市场：私有/公开目录和路由共 10 项测试、`node --check market.js`、路由搜索边界及 `scripts/verify_catalog.py` 全部 29 个公开包校验通过。catalog 和 routing 中仅 Terminal 内容改变，Compare 只更新筹备状态说明，其他条目内容及顺序保持不变。
- 市场真实 Chromium：搜索 SSH、打开 Terminal 1.5.2 详情、点击下载并等待页面 SHA 校验成功，文件大小及摘要与发行归档一致；Compare 无下载或购买按钮，无页面异常。见 [浏览器结果](market-browser.json) 与 [本地候选下载截图](market-download-local.png)，截图不是线上新版部署证明。
- 当前线上市场的 catalog/routing/Terminal ZIP 已下载核验，但版本仍为 1.5.1，见 [线上现状](current-live.json)；最近成功的 [Pages 工作流](https://github.com/skyelan1999/aide-pkg-shop/actions/runs/40695819677) 对应旧 main。

阻塞来自 GitHub 分支保护：PR #15 为 `REVIEW_REQUIRED`，普通合并被分支策略拒绝。PR 作者及唯一 CODEOWNER 都是 skyelan1999，作者不能自审；自动合并也被仓库禁用。没有使用 `--admin`、修改保护规则、直接推 main 或从候选分支触发线上部署。见 [PR 状态](market-pr.json)。需所有者决定本次管理员合并，或提供能完成有效审核的策略路径；之后由既有 Pages 工作流部署，再核对线上 catalog、routing 和浏览器下载摘要。

完整 start.command 工作台、真实串口硬件及 Windows/macOS 原生运行本轮均未验收，Terminal 保持预发布及既有宿主能力要求。无生产服务启停、Aide 核心版本变更、模型付费调用或工作区数据迁移。自有浏览器与临时 HTTP 服务已退出，原有未跟踪文件和离线镜像残留未清理。Aide 产品源码指纹仍为 `afc4da50ae380ee8d5c0edb5bc0e0a9430063ff65b2692ab1a44a3899a5ac8a9`，见 [指纹收据](aide-fingerprint.json)。

回滚：市场尚未部署，可关闭或修改 PR #15，旧 1.5.1 包保持可用。Terminal 降级前备份状态并核对宿主兼容；Compare 按私有二进制交付说明备份并干净安装。本轮未替换生产数据。完整状态见 [验证清单](verification.json) 和 [任务账本](../../tasks/plugin-release-market-20261029.json)。
