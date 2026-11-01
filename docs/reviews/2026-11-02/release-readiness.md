# Release 与异常定位验收进度 · 2026-11-02

任务：[release-ci-hardening-20261101](../../tasks/release-ci-hardening-20261101.json)。这是候选验收记录，**新 Release 尚未发布，当前生产服务未替换**。测试范围见[完整矩阵](../../testing/release-validation.md)，诊断接口和隐私边界见[异常定位](../../architecture/diagnostics.md)。

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
