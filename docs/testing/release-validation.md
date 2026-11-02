# 日常 CI 与完整 Release 验收

日常提交保留快速反馈；正式发行使用显式的 **Aide Delivery**，全部必需检查和证据校验通过后才允许公开 Release。安装包、测试结果和镜像必须绑定同一源码提交，ARM64/AMD64 使用相同源码指纹。测试失败不能通过改断言、跳过插件或忽略 job 来放行。

## 日常提交

[Aide CI](../../.github/workflows/aide-ci.yml) 在所有分支 push、main PR 和手动运行时触发。`quick` 执行仓库静态、前端行为与流程合约检查，另以 `scripts/ci/basic_go.py` 编译所有 Go 包、运行鉴权/文件边界/上下文恢复/调度等关键回归并运行 vet。分支保护所需的汇总名称仍为 `gate`。

日常 CI 不构建发行镜像，不自动跑完整 race、跨系统、浏览器和公开插件测试，不自动发布。文档站点工作流另按其路径规则运行。历史一次性 RC3 发布工作流已退役，不能绕过 Delivery。

## Release 必需矩阵

公开插件锁定记录必须逐项匹配冻结市场目录的版本、安装包路径、字节数和 SHA-256，并拒绝重复插件 ID；只验证历史 ZIP 自身正确不足以证明测试了现行公开版本。`test_public_plugins.py` 用同时保留有效旧版／新版 ZIP 的夹具，以及版本、路径、大小、摘要和重复 ID 反例验证这一门禁（[#713](https://github.com/skyelan1999/aide/issues/713)）。

| 层次 | 执行入口与环境 | 核心断言 | 通过证据 |
| --- | --- | --- | --- |
| 异常诊断 | 完整 Go 故障注入与 race | 插件 worker/daemon/加载元数据、HTTP 关联、持久化轮转、取消/panic 语义和内容不泄露 | incidents 回归及诊断包，见[模块诊断](../architecture/diagnostics.md) |
| 源码 | Delivery source；干净 checkout | main/tag 对应版本脚本、提交、发布说明；分支只允许无 tag 候选 | 固定 source SHA；quick 收据 |
| 原生 Linux | Ubuntu 24.04 ARM64、AMD64 | 原生编译、普通 Go 全量、独立 race/vet、运行资源、可选引擎边界 | 两套 candidate-proof、Go 分片日志 |
| Go 完整性 | `test_go_snapshot.py` | Git 列表完整快照；注册 Test/Fuzz seed/Example 全部互斥分片，每项恰有一个终态；其他包 race、平台合约、vet | inventory/selected/results/missing/pass/skip 分离记录 |
| 全部公开插件 | `public_plugins.py run`，两个原生工具链容器 | 原 ZIP 字节/摘要/安全结构；真实宿主上传安装、启用、能力声明；实际功能调用和独立结果断言 | 每插件 inspection/installation/function/cases，含已知缺陷的否定回归；缺项或 skip 阻止发布 |
| Windows | Windows 2025 AMD64 | 原生 Go helper、消息/mTLS、Windows xsys 审查断言；启动器编译；PowerShell 5.1 和 7 各自验证目录启动、镜像槽、升级代理 | 10 项宿主命令（Windows 使用真实 Win32/BAT 套件，不执行 POSIX 专用启动器）、日志、平台与 SHA |
| macOS | macOS 15 ARM、Intel | 原生 helper、消息/mTLS、relay/launcher（不在 Darwin 调用无实现的 xsys 子包）；临时目录构建 Swift Computer Bridge，codesign 深度验证 | 每平台 8 项命令与实际 Go 测试终态；不修改系统授权 |
| 浏览器 | Chromium/Firefox/WebKit × 中文/英文 × blue/comfort × 明/暗 × 1440/390 | 48 格真实 App：审批意图/原始命令/拒绝；文件拖入及图片粘贴；附件实际发送；流式 Markdown 与刷新持久化；未授权和路径穿越拒绝；输入框可见和无脚本异常 | 48 个截图与逐格 checks；WebKit 旧文档取消按实际同源 API 方法/路径及文档代际匹配单独记录；旧文档在导航提交前发出的请求仍跟踪，POST/PATCH 取消不算写入成功，新文档/活动阶段/未知路径/引擎异常仍失败；不能用空匹配 Go test 代替报告 |
| 新装与生命周期 | 独立 Linux checkout，路径含空格和中文 | 新 HOME/config/data/project/builder/image、构建启动、停止/重启/清理、数据哨兵、拒绝未经满足门禁的 release | clean-linux-launcher result 与命令/健康记录 |
| 包与镜像 | 已有 packager，两个架构 | 全层隐私扫描；Aide 不带插件和开发工具；SDK 单独交付；ZIP CRC/身份/摘要；导出后移除自建 tag、导入并再次运行健康和鉴权检查 | package/import/privacy/smoke/SDK 证明 |
| 发布 | main 上现有 version.sh tag | 所有 job 成功且具体报告完备；市场冻结未漂移；先草稿上传，远端 API 独立摘要与精确附件集合验证 | RELEASE-VALIDATION.json、RELEASE-ASSETS.txt、SHA256SUMS |

运行器标签见 [GitHub 官方说明](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)；引擎安装见 [Playwright 官方文档](https://playwright.dev/docs/browsers)。Linux WebKit 覆盖 WebKit 引擎，不能代称原生 Safari/macOS 隐私授权验收。

## 当前公开插件安装包范围

[冻结清单](../../scripts/ci/public-plugins.lock.json) 来自公开市场的固定提交，包含当时 **30 个 public 可安装包**。每项保留市场版本、原 ZIP 大小/SHA256、公开实现仓库和来源提交，以及最近公开 tag 供差异审阅。较新源码 tag 不会悄悄替换市场包。私有插件不进入本轮公开验收或发行镜像。

不要求重编译全部插件。测试把原发布 ZIP 直接提交到真实 `/api/plugins/bundle`，验证已安装身份和启用状态，再调用安装后的 handler 或 Aide 的正式 native executor。需要引擎的插件安装现有发布依赖；例如 STEP 测试实际下载并校验预编译 CAD 引擎、产生已知尺寸实体再计算网格。

| 插件 | 必需功能与结果断言 | 外部能力的验证范围 |
| --- | --- | --- |
| current-time | 已知日期周数、非法日期拒绝 | 无外部服务 |
| lunar-calendar | 2024-02-10 正月初一、非法日期拒绝 | 离线历法 |
| almanac | 指定日期实际黄历返回 | 传统资料不作为科学结论 |
| environment-guide | 临时项目目录清单，不读取正文 | 不连接真实远程来源 |
| goal / plan / todo / feedback / subagent / workflow | 六个现行包是能力预设：真实安装并核对各工具/服务声明 | 包无执行 handler；不声称实现了独立任务管理引擎 |
| DeepSeekSupporter / ALiBaiLianSupporter / OpenAISupporter | 安装后的计价匹配、官方端点/模型、外域拒绝 | 不使用客户 Key；不声称查询了真实账户余额 |
| AutoAiModelRouter | 安装后的模型目录可读取 | 路由算法/媒体边界另由 Go 全量回归覆盖 |
| skill | 原生入口读取已安装技能 | 不下载未知第三方 Skill |
| plugin-authoring | 原生制包指南和提案语义 | 不擅自修改真实插件库 |
| office | 原生入口实际生成 DOCX、检查 ZIP 文件 | 使用合成内容 |
| archive-tools | 真实压缩/解压、还原字节一致 | 全量 Go 套件另测路径边界 |
| markdown-history | 实际历史接口记录快照、相同内容去重 | 临时工作区 |
| sqlite | 安装后 CREATE/INSERT/参数化查询；默认只读拒绝写；逐句确认、实际 ATTACH 越界拒绝、CSV 链接哨兵不变 | 私有临时数据库 |
| starlink | 原插件读取本机合成令牌/证书，与真实隔离 Aide HTTPS 宿主完成状态及星图摘要 | 不读取用户星图 |
| terminal | 创建真实本地进程、输入、解码轮询输出、关闭 | SSH/串口/SCPI 需专用设备，不声称真实硬件通过 |
| rviz | 保存配置走实际提案，敏感字段不持久化 | 不连接机器人 |
| video-preview | MLT 字节 begin/write/inspect 协议与格式断言 | 不声称所有视频编解码器兼容 |
| file-preview | 原发布 Python renderer 产生真实 JPEG、原件不变、穿越拒绝 | 先验证图片路径；Office/HTML 转换须另有 LibreOffice/Chromium 环境，不能以此替代 |
| step-viewer | 安装需确认、发布引擎摘要、已知 10×20×30 STEP 实体实际转换、网格与边界 | 原生 ARM/AMD 分别执行 |
| browser-control / computer-control | 已发布 handler 与带鉴权的隔离 bridge 协议互通 | 模拟 bridge 响应，不代替物理桌面点击或系统权限验收 |
| tts-tools | 已安装原生音色入口、非法编码拒绝、合成服务协议夹具的真实请求/下载/WAV 落盘/续跑不重提 | 不使用真实付费合成账户 |
| codex-bridge | 已发布 handler 与本机 manager fixture 完成配对、授权持久化、额度换算、模型目录、断开；秘密不返回 | 不冒称真实 Codex 推理或账户额度验收 |

这些是本轮必需的可重现功能场景，不是所有插件所有硬件、格式和付费服务的穷尽证明。发布记录必须保留这些限制。新增 public 包或改变任一包必须更新冻结清单和相应用例，重新跑两架构；不能只改版本号/摘要来跳过失败。

## 执行命令与证据

```sh
# 日常：无付费调用
python3 scripts/agent-route.py verify quick
python3 scripts/ci/basic_go.py
python3 -m unittest discover -s scripts/ci -p 'test_*.py' -v

# 本机完整 Go（Docker；运行时源码快照与独立 SDK）
AIDE_TEST_TOOLCHAIN_IMAGE=aide:dev AIDE_TEST_WORKERS=1 python3 scripts/test_go_snapshot.py

# 本机完整预检查：先选择并检查已存在的本地运行镜像与 SDK；可使用镜像 ID。
# test_runtime_assets.py/test_optional_step.py 都读取 AIDE_TEST_IMAGE，且禁止隐式 pull。
AIDE_TEST_IMAGE="${AIDE_TEST_IMAGE:?请设置已有本地运行镜像}" \
AIDE_TEST_TOOLCHAIN_IMAGE="${AIDE_TEST_TOOLCHAIN_IMAGE:?请设置已有SDK镜像}" \
AIDE_TEST_WORKERS=1 python3 scripts/agent-route.py verify full

# 只使用当前公开发布包；toolbox 必须带 Go/Node/Python
python3 scripts/ci/public_plugins.py run --image aide:dev --out .agent-state/public-plugins/arm64

# 真实浏览器；先安装 scripts/browser-runtime 锁定依赖与三个引擎
npm ci --prefix scripts/browser-runtime
scripts/browser-runtime/node_modules/.bin/playwright install --with-deps chromium firefox webkit
AIDE_RELEASE_BROWSER=1 AIDE_EVIDENCE_DIR="$PWD/.agent-state/release-browser" go test -mod=vendor -count=1 -timeout=20m -v ./internal/server -run '^TestReleaseBrowserMatrix$'

# 源码目录，不是执行结果
python3 scripts/ci/test_inventory.py --write
```

所有日志/截图留在 `.agent-state/` 或 CI artifacts，公开汇总只含脱敏合成信息。Release artifacts 保留 30 天，Release 附件的 `RELEASE-VALIDATION.json` 长期保留摘要和逐项证明。源码用例目录见 [test-cases.md](test-cases.md) / [JSON](test-catalog.json)。环境变量标识可能需要人工/外部设备的 opt-in 场景；完整 Go 日志的 SKIP 不算功能 PASS，必需的浏览器和公开插件由单独不可跳过门禁补验。

## 失败修复、提交和重跑

本轮用户已授权：不满足检查时主动定位、修复并提交。确认是已有主程序/公开插件缺陷时，先在集中 Aide Issue 记录版本、复现和影响，修复提交关联 Issue；仅当前新增测试接入尚未完成时，直接修正接入。不能把依赖下载失败或漏装环境依赖写成产品逻辑缺陷。

修复后先跑失败场景，再跑受影响回归及完整 Delivery。每次代码/安装包变化都产生新提交和新证据；不能把旧提交的通过报告带给新 Release。只重建确需修复的插件，经过包、安装、功能与市场身份更新后再冻结。没有可验证的安全修复时保留失败，说明所需条件，不放松门禁凑数。

CI 本身不授予任意工作流自动改代码/写仓库的权限。本轮由已获用户授权的开发流程执行修复和提交；后续 CI 默认只报告失败并阻止发布。发布权限只给受全部必需门禁约束的 publish job。

## 发布、升级与回滚

候选可在已包含最新 main 的功能分支显式触发 Delivery，`release_tag` 留空，只产出候选证据。修复原生宿主检查时可显式选择 `validation_scope=hosts`，只运行三套宿主检查并生成报告；浏览器修复可选择 `validation_scope=browser`，每格使用独立浏览器进程并在失败时保留服务健康/资源探针。两种局部模式都禁止携带发布 tag，不能发布，也不能替代完整 Release 验收。完整候选与发行使用默认 `validation_scope=full`。正式发行要求 main、干净工作区、仓库 full/release-check 门禁，以及现有 `scripts/version.sh` 产生的精确 tag，不另造版本机制。

沿用每 7 天至多一个公开版本；用户明确要求本轮即时发行时，可使用已有的手动一次性 `cadence_exception_reason` 并记录原因，不修改默认节奏。先创建全新草稿，禁止覆盖既有 Release/附件。远端 tag、资产大小、SHA256、说明和精确附件集合全部一致后才公开。

此次开发与 CI 不替换当前生产服务。客户升级前备份数据目录与配置，保留旧运行镜像和发布包。失败使用现有 A/B 槽与升级代理回滚，不清理用户工作区、历史、插件、凭据或旧镜像。原生宿主 helper 合约测试不等于 Windows/macOS Docker Desktop 的真实升级全生命周期；未完成的原生设备验收必须在发行报告标为未验证。

### Terminal 1.6.0 paired release

For Terminal 1.6.0 and newer the public-package job additionally invokes
`TestTerminalSessionFeaturesInstalledCandidate` against the exact frozen market
ZIP and expected SHA256. The raw multipart upload is not reconstructed. Assertions
cover independent copy lifecycle, hidden-session isolation, actual loopback
Telnet traffic, and dynamic send denial. The upstream plugin protocol/browser
suite separately validates relay and X11 behavior.

Candidate runtime smoke and image reimport smoke both execute `xauth -V` inside
the disposable runtime container. Publication rejects missing, failed or empty
xauth evidence; SDK presence alone does not satisfy this runtime dependency.
Native X Server and customer-device acceptance remain separate from isolated
Linux fixtures. Terminal 1.6.0 requires market minimum Aide `0.1.18.0 RC2`, without
a source-compatibility bypass for older releases.
