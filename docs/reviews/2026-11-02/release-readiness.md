# Release 与异常定位验收进度 · 2026-11-02

最新状态：**[v0.1.18.0-RC1 已正式公开并标为 Latest](https://github.com/skyelan1999/aide/releases/tag/v0.1.18.0-RC1)**，完整同源 Delivery 10/10 门禁通过。下文按发生顺序保留失败、待授权与阶段候选记录；最终事实见[正式发行回执](#正式发行回执)及[受控核对摘要](release-publication.json)。

任务：[release-ci-hardening-20261101](../../tasks/release-ci-hardening-20261101.json)。下文保留最初候选验收阶段的记录；最终发行状态见文末正式发行回执，生产服务始终未替换。测试范围见[完整矩阵](../../testing/release-validation.md)，诊断接口和隐私边界见[异常定位](../../architecture/diagnostics.md)。

## 已执行的检查

| 对象 | 实际结果 | 身份与限制 |
| --- | --- | --- |
| 日常 CI | quick、关键 Go/vet、gate 通过 | [a0ff1acb 的 CI](https://github.com/skyelan1999/aide/actions/runs/41331751852)；被新运行取消的重复 push 不作为通过依据 |
| 本地完整 Go race/vet | 1,587 个注册项：1,509 通过、78 跳过；24 个分片无失败；其余 Go 包和 11 项平台合约通过 | `0aa765a2` 阶段封存的源码快照，Linux ARM64 SDK、单 worker；选择性/环境相关跳过单独保留，不能当作执行通过。后续源码已有变更，不能拿此报告当作最终 Release 收据 |
| 结构化异常故障注入 | 8 项 `TestIncidents*` 在上述完整 race 中通过；后续当前源码的同组定向 race/vet 也通过 | 实际插件 TypeError/EIO 与加载错误、显式失败结果、请求鉴权关联、并发与重启、轮转权限、不可写/链接目录、摘要失败/取消、panic 原语义与正文不泄露 |
| 浏览器 | 48/48 真实交互场景通过 | [16b48d08 云端运行](https://github.com/skyelan1999/aide/actions/runs/41330840791)；三引擎、桌面/窄屏、专业/经典明暗。每格独立浏览器进程，无失败重试；Linux WebKit 不代表原生 Safari |
| 原生宿主 | Windows AMD64 10 项、macOS ARM64/Intel 各 8 项通过 | [d6cc5a81 云端运行](https://github.com/skyelan1999/aide/actions/runs/41330227847)；Windows 实际 Win32、BAT、PowerShell 5.1/7，macOS 原生桥接合成自检。未改变用户系统隐私授权，未替换生产服务 |
| 当前市场 30 个 public 原 ZIP | 首轮逐包检查、实际安装及功能用例通过；新增否定用例后发现 SQLite/Terminal 旧包缺陷 | 首轮报告不再作为最终放行依据。其余 28 包保持字节不变；六个 DSH 预设按其无独立 handler 的契约验证实际安装与能力声明 |
| SQLite 修复候选 1.0.1 | 33 项 CRUD + 25 项边界用例通过；原生 ARM64/AMD64 CI 通过；真实 Aide ZIP 导入及否定回归通过 | [PR #2](https://github.com/skyelan1999/aide-pkg-sqlite/pull/2)，提交 `8098ba73c1ca588f037805dba1aa51f239615c71`；非公开发布回执 |
| Terminal 修复候选 1.5.3 | 36 项源码 + 27 项解包测试通过；原生 ARM64/AMD64 CI 通过；真实 Aide ZIP 导入及已安装 TCP 模块双向拥塞回归通过 | [PR #2](https://github.com/skyelan1999/aide-pkg-terminal/pull/2)，提交 `a7e790c10eee389f99ba646f99ca60648f1d56f9`；双向 32 MiB 慢读校验字节数和 SHA，非公开发布回执 |
| 市场修复候选 | 33 个展示条目、30 个 public ZIP、3 个 private 展示条目检查通过；11 项 Python 测试和 Node 路由测试通过；CI 通过 | [PR #32](https://github.com/skyelan1999/aide-pkg-shop/pull/32)；仅替换上述 2 包，其余 public ZIP 和 private 展示元数据保持，未包含 private 安装包 |

本地原始日志、截图和完整报告在忽略的 `.agent-state/release-ci-hardening-20261101/`；这里只保留脱敏结论。候选 ZIP 的私有测试目录明确标记 `candidate-not-published-20261102`，不得冒充已发布市场快照。

## 已发现的问题和修复状态

- [#696](https://github.com/skyelan1999/aide/issues/696)：旧测试仍要求已取消的图片视觉能力门禁。改为验证实际 `image_url` 发送，CI/本地完整检查通过，待主程序集成。
- [#699](https://github.com/skyelan1999/aide/issues/699)：文档链接指向忽略的本地证据，干净 checkout 不可用。保留本地路径说明并修复链接，干净云端检查通过。
- [#702](https://github.com/skyelan1999/aide/issues/702)：双架构 Release Docker 构建遗漏工程 benchmark 夹具，导致 4 个真实测试失败。已在两个 Docker 构建阶段补精确 COPY，夹具不进入运行镜像；**修复后的实际完整构建尚待执行**。
- SQLite [#583](https://github.com/skyelan1999/aide/issues/583)、[#584](https://github.com/skyelan1999/aide/issues/584)、[#586](https://github.com/skyelan1999/aide/issues/586)：原 public ZIP 的删除确认、外部 ATTACH、CSV 路径边界缺陷已实际复现，候选修复包通过；目前公开市场仍是旧包，Issue 保持打开。
- Terminal [#588](https://github.com/skyelan1999/aide/issues/588)：原 public ZIP 代理未传递背压，慢读下观察到约 31 MiB 待发队列且来源未暂停。修复候选实际双向测试通过，原公开版本尚未替换，Issue 保持打开。

Windows 首轮误用了 POSIX 专属测试、Linux WebKit 同进程连续场景出现引擎内部错误，分别调整为对应原生宿主检查、每格独立进程后实际复验通过；没有添加忽略异常规则。一次本地并行重型验证受到内存压力，保留失败日志，降低到单 worker 后完整执行通过，不将环境中断记为产品验收通过。

## 剩余发布步骤

1. SQLite、Terminal、市场三个 PR 都受 `main` 的一次 owner 审阅保护；唯一 owner 与作者为同一账户。已提出**仅限这三个 PR** 的管理员例外授权请求，尚未收到回复，不修改保护规则、不推定授权。
2. 获得授权后，先集成并发布两个插件，再验证公开 tag、ZIP 与 SHA，最后集成市场并固定其新 main 提交。候选摘要：SQLite `641ed1d4d86e2a83482f93f2465b7803521dd0fbf077914da4f66a4bef04f789`；Terminal `0f6810cf1e637621aca25b414af5fafb1db089134653dd8e7b8aedef4df43813`。
3. 更新 Aide public lock，在同一最终源码上重新执行完整 Delivery：双架构完整 Docker 构建、race/vet、30 个正式 public ZIP 实装/功能、48 格浏览器、原生宿主和干净安装生命周期。host/browser 子范围通过不能替代完整运行。
4. [Aide PR #698](https://github.com/skyelan1999/aide/pull/698) 仍是草稿；它的集成需满足自身分支保护，三个插件/市场的管理员例外不延伸到该 PR。满足后沿用干净 main 的 full、release-check 和 version.sh，实际发布并核对远端附件才算完成。

暂存、推送、PR、测试通过、合并和 Release 发布分别记录。当前没有给新版本打正式 tag、上传或公开 Release，也没有重启本机 9999 生产服务。

## 收到授权后的执行回执

用户随后明确授权上述三个 PR 管理员例外合并，未修改分支保护规则。SQLite PR #2 已合并到 `9d9cbd9a4291c1ddcb6f0d7ef310ff21f6efa374`，Terminal PR #2 合并到 `221147d2f33cff377f2c854a5b97dbbfbe1525c6`；分别发布 [SQLite v1.0.1](https://github.com/skyelan1999/aide-pkg-sqlite/releases/tag/v1.0.1) 和 [Terminal v1.5.3 预发布](https://github.com/skyelan1999/aide-pkg-terminal/releases/tag/v1.5.3)。两个公开 tag 指向对应合并提交，远端 ZIP 字节数和 SHA256SUMS 均与上文候选摘要相符，沿用仓库固定提交下载，没有声称上传 Release 二进制附件。

市场 PR #32 已合并到 `943f6a90f08c4f5ba09e4d09031247b64ee29295`；Aide public lock 固定此提交和两个新版本，另外 28 项身份保持不变。市场源码检查再次确认 33 个展示条目、30 个 public 包、3 个 private 展示条目；Aide 的 34 项 CI 合约通过。Aide 主程序尚未发布，随后以更新后的正式包启动完整候选验收；此前的旧市场报告不作为最终放行依据。

## 新市场与完整候选验收

公开市场 Pages 发布运行 `41356294112` 通过；实际公开 `catalog.json` 已核对两个版本和摘要。Aide `eb97d1446bf28c7568175c3b4570b2ffa4bae877` 在 Linux ARM64 隔离宿主下载、检查并真实安装全部 30 个正式 public ZIP，逐包功能检查通过，包含 SQLite 的全部新增否定回归和 Terminal 已安装模块双向 32 MiB 背压/字节 SHA 校验。报告的 marketRevision、commit、architecture、30 项身份与必需阶段均另经验证器核对。

完整 [Delivery 41356355299](https://github.com/skyelan1999/aide/actions/runs/41356355299) 的源码门禁、Windows 10 项、macOS ARM64/Intel 各 8 项通过；下载的原生报告已独立核对提交与实际 Go 断言。此运行浏览器在第 47 格失败，登记 [#709](https://github.com/skyelan1999/aide/issues/709)：WebKit 刷新旧页面时，测试只识别已观察到的 GET 取消，未识别旧页面实际发出的 context-preview POST 和 session-check PATCH。该场景已完成审批、附件、聊天、刷新持久化及权限断言，失败截图完整渲染，服务/脚本/CSS 独立探测均为 200。原失败保留，不能因为功能断言通过就放行。

修复按文档代际跟踪实际方法/路径，并保留导航提交前旧页面最后发出的请求；仅精确匹配 WebKit 旧代际刷新取消，POST/PATCH 取消不算写入成功。活动页面、新代际、未观察路径、跨域及普通脚本/引擎错误均由专门反例继续拒绝。真实 48 格复验尚在进行；双架构构建继续执行，不复用失败运行作为最终 Release 收据。

### 浏览器修复复验回执

修复提交 `a0265d4d42c4567134d592ad366bb6600715bed8` 的基础 CI 通过。本地完整真实矩阵 48/48 通过，耗时 193.29 秒；云端 [41357546514](https://github.com/skyelan1999/aide/actions/runs/41357546514) 也通过 48/48，下载后的提交身份、48 格集合和每格实际断言经独立验证器核对。20 条旧文档导航取消仍在报告中，记录观察到的请求方法集合和文档代际，不推断某个被取消写请求已经完成。

此提交只改浏览器测试运行器、CI 检查条目和文档，Aide 产品源码及公开插件安装测试与 `eb97d144` 相同。Windows、macOS ARM/Intel 与干净 Linux 生命周期报告均绑定 `eb97d144`，后者包含 17 个实际命令及 4 次 HTTPS 页面/鉴权接口健康采样。这些分阶段证据用于审阅修复，**不能拼装成最终同源 Release 收据**；正式打 tag 后仍需完整 Delivery 重新验证。

### 双架构候选最终核对

ARM64、AMD64 原生候选任务及 public_plugins 汇总门禁均通过。每架构的完整 race/vet 回执都是 1,587 个注册项、1,509 通过、78 明确跳过、24 个完整分片，其余 Go 包/平台合约/vet 通过；每架构 30/30 个正式 public ZIP 实际安装与功能通过。两组回执均经独立验证器核对，没有把跳过项或安装成功当作功能通过。

四个完整运行/可选开发工具 ZIP 已下载，文件大小、SHA-256、CRC 与候选回执相符。两架构和两种包的提交/平台/导入/冒烟/逐层隐私记录一致，共同源码指纹为 `dfaac9616ea4c869a2e93f27394e0e0fb651f0cc3be6557aacce8a61563e2f6d`。脱敏的可审阅摘要见 [release-candidate-acceptance.json](release-candidate-acceptance.json)。这是打正式版本之前的候选，版本仍是工作区当时的 `v0.1.17.6-RC1`，没有覆盖或发布同名旧 Release。

SQLite/Terminal 四个集中缺陷 #583/#584/#586/#588 已依据正式插件发布和双架构实装回归关闭。#696/#699/#702/#709 仍等待 Aide 主程序集成/正式发行闭环。原完整运行的浏览器失败记录保留；修复后的独立运行通过不改变原运行结论。

用户随后独立授权本次 Aide 发行例外，PR #698 已合并为 `f7f3a615`。近十次代码提交审阅发现的 #711/#712/#713 已在 [修复 PR #714](https://github.com/skyelan1999/aide/pull/714) 实现；35 项定向 Go race、全包 basic/vet、314 项 quick、35 项 Python CI 合约及全部 30 个当前市场 ZIP 的加强检查通过。远端 [CI 41366268506](https://github.com/skyelan1999/aide/actions/runs/41366268506) 的 quick、go、gate 全通过后，依据用户针对 #714 的补充授权合并为 `418a6ce0`。主工作区已检出该干净 main，保护规则未改；修复后完整 Go 验收与正式版本 Delivery 仍需完成。详见[审阅修复记录](release-review-remediation.md)。

### 审阅后修复与当前 main 完整检查

后续完整检查发现的测试夹具唯一匹配 #715、运行资产镜像选择 #719、relay socket/context 截止时间通知顺序 #722，分别由 [PR #716](https://github.com/skyelan1999/aide/pull/716)、[PR #720](https://github.com/skyelan1999/aide/pull/720)、[PR #724](https://github.com/skyelan1999/aide/pull/724) 修复；每个 PR 的远端 quick/go/gate 均通过，按用户“本次发行必要修复 PR”明确授权使用管理员例外合并，保护规则未改。旧逻辑故障注入/负向对照证据和失败运行均保留，没有降低断言或延长原取消测试截止时间。

当前干净 main `b9c78eed3ed27c8b6fb5ed30a91a98a2fcf36db1` 的本地 full 已完整通过，内容指纹 `8242b62963a44563c9338b57e3040ce09daa653281002e9f74f708056f4c8257`：318 项 profile 检查；Go server 1,592 个注册项中 1,514 通过、78 明确跳过、24/24 race 分片通过，其余 Go 包、平台合约与 vet 通过。使用显式不可变依赖运行镜像和 SDK，在独立 QA 容器以 2 workers、6 GiB 上限执行；依赖镜像资产检查不替代新发行镜像验收。原始日志在忽略目录 `.agent-state/release-ci-hardening-20261101/post-relay-full/`，受控结论由本记录和任务账本保存。

这完成的是正式版本前置检查；升版/tag 后仍必须在同一源码上重新完成双架构构建、30 个现行 public 原包实装/功能、48 格浏览器、原生宿主及安装生命周期门禁，上传并核对七个正式附件后才算发布。本地 9999 生产服务未替换。

用户指出 Codex Bridge 未给出 API 网址，已补到[宿主接口说明](../../architecture/codex-plugin-host.md)：Docker Desktop 使用 `http://host.docker.internal:8787/v1`，本机原生使用 `http://127.0.0.1:8787/v1`，按实际配置端口替换；17888 是管理端口。实际本机 `/health` 返回桥接服务 0.4.0；未读取密钥、调用推理或修改服务。这是地址文档补充，未声称修复插件 UI。

## 正式发行回执

既有 `version.sh bump feature` 生成并原子推送版本提交 `e86ce5b5d6e5d119e91381dc802b8f4201e89dab` 和 annotated tag `v0.1.18.0-RC1`，没有改写旧 tag。版本提交基础 CI [41375786514](https://github.com/skyelan1999/aide/actions/runs/41375786514) 通过；[正式同源 Delivery 41375800455](https://github.com/skyelan1999/aide/actions/runs/41375800455) 的 10/10 门禁全部通过。精确源码 SHA256 为 `dcd5df95d6163781897614aba7917c51d56f6a1f0fdc9965c80c7424c5c864f7`，它与上文 Agent 路由输入指纹用途不同，不混用。用户明确要求本次完整发行，使用现有一次性发行频率例外参数，保留正常每周发布策略。

- Linux ARM64、AMD64：每架构 server 注册项 1,592，其中 1,514 通过、78 明确跳过、24 个完整 race 分片通过；其余 Go 包、平台合约与 vet 通过。跳过项保留在发行回执中，不能算作实际功能通过。
- 两架构各自安装并验证当前冻结市场全部 30 个正式 public ZIP 的检查、安装和功能；版本、字节数、路径、SHA 均核对。市场固定 `943f6a90f08c4f5ba09e4d09031247b64ee29295`，没有重编译全部插件，没有捆绑公有或私有插件到主程序镜像。
- 三浏览器 48/48 格、Windows 10 项、macOS ARM64/Intel 各 8 项、干净 Linux 生命周期 17 个命令和 4 次 HTTPS 页面/鉴权健康采样通过。各报告已下载并独立核对实际提交与必需断言。
- 两个运行包和两个可选开发工具包的实际 CRC、导入、冒烟和逐层隐私检查通过；发布前门禁检查七个远端附件摘要。发布后又下载三份小型清单/回执，核对实际字节及服务端摘要、六项 SHA256SUMS 记录、完整七项集合、标签提交、共同源码身份及实际功能回执。未重复下载本轮约 1.97 GB 四个 ZIP；实际导入/CRC/冒烟由本次同源原生流水线执行，服务端字节身份由独立核验绑定。

[Release](https://github.com/skyelan1999/aide/releases/tag/v0.1.18.0-RC1) 于 `2026-11-02T03:17:30Z` 公开。用户明确“把这个版本推做正式的release 标签吧”：保留仓库既有版本 tag，将 GitHub Release 标为正式发行和 Latest；远端实际复核 `draft=false`、`prerelease=false`、Latest ID 相同。没有把 RC 命名改写为另一个版本，附件及已验证源码未改变。精确七项字节数与摘要见[受控核对摘要](release-publication.json)及 Release 附件 `SHA256SUMS`、`RELEASE-ASSETS.txt`、`RELEASE-VALIDATION.json`。

发行标签的测试用例源码目录与当时 checkout 逐项一致：726 个文件、2,143 个静态命名用例。静态目录不是执行或覆盖率证明。真实付费提供商/账户、物理设备、原生 Windows/macOS Docker Desktop 完整升级生命周期及原生 Safari 仍不在本轮实际验收范围；合成插件功能测试和 Linux WebKit 不代替这些环境。

本机 9999 生产容器未被停止、重启或替换，仍使用原镜像。保留旧完整运行包、原镜像和数据，按既有 A/B 槽/启动器流程回退，不删除 `.aide-data` 或工作区。只移除本任务空闲 QA 容器；原始失败日志、成功回执及可复用缓存保留。

本机 GitHub 域名解析曾返回连接拒绝地址；通过一次性进程内 CONNECT 转发及 TLS 验证的公开 DNS 查询恢复访问，保持 GitHub 端到端证书校验，没有改系统 DNS、代理、Git 全局配置或 SSH 主机校验。临时本机辅助程序不进入源码或发行包。

## 发行后的独立跟进

最终文档 PR 的 CI 发现原加密存储测试存在偶发夹具错误，已登记 [#729](https://github.com/skyelan1999/aide/issues/729)：固定把 Base64 首字符设为 A 时，随机密文约有 1/64 已以 A 开头，实际内容没有改变。修复改为翻转真实密文字节，保留原有加密恢复、错误密钥、错误作用域和认证失败断言。256 种首字节控制通过，恢复旧操作的负向对照被新用例正确拒绝；Node 24 隔离验证重复 32 轮通过，相关 102 项用例与本地 quick 315 项通过。仅改测试和目录，不改变运行加密代码、已公开 tag 或附件。修复后的静态目录为 726 个文件、2144 个命名用例；发行标签仍是上文的 2143 项静态目录。

Terminal [新增能力 #726](https://github.com/skyelan1999/aide/issues/726) 和本次开发发现的 [1.5.3 侧栏分组缺陷 #728](https://github.com/skyelan1999/aide/issues/728) 由独立子任务在后续分支处理，未进入本次已发布源码、插件包或市场。现有发行的插件安装/功能回执只证明其实际列出的用例，不代表插件所有 UI 状态均已覆盖。
