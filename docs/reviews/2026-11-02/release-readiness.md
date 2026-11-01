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
