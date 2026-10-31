Final status: merged, market published, local 9999 installed/enabled and four USD quote checks passed. Earlier sections preserve the pre-merge checkpoint; the final acceptance supplement supersedes its pending deployment status.

# OpenAISupporter 源码兼容验收 · Issue #661

问题：源码构建已包含 USD 支持，但版本身份仍为 0.1.17.6 RC1。市场为避免不含 USD 的已发布 RC1 安装该插件，将最低发行版本设为 RC2，因此也拦截了具备能力的源码构建。

修复：目录增加可选 `sourceCompatibility`，宿主仅在 source runtime 下按明确最低版本和内置能力检查替代发行版本条件。`provider-support-usd-v1` 由宿主 USD 支持判断。未知能力拒绝；原平台和 feature 检查继续执行。市场浏览、AI 发现、安装、路由、升级统一判断，并在详情中解释源码兼容。已发布 RC1 仍不可安装；不改变发行身份。

## 实测

- macOS Docker 隔离 Go 1.26 / Node 环境：quick 全部成功；46 项相关 Go race 测试成功，涵盖市场、路由、provider support 与分币种费用。
- 版本矩阵：发行 RC1 拒绝、未知 runtime RC1 拒绝、具备能力的源码 RC1 允许、较低源码版本拒绝、发行 RC2 允许；未知能力与原 feature 条件不能绕过。
- 实际公开 OpenAISupporter 0.1.0 ZIP：SHA-256 `dae0cbbf00cf955df0c086575583e90ccabbc45e72c1a495d27b4dcda71672c1`。使用配套目录元数据验证市场安装返回 201、USD 报价、合成 USD 用量费用与停用后报价移除。ZIP 字节未改。
- Safari 实际打开独立测试实例 127.0.0.1:18979，详情保留 RC2 最低发行版本、显示源码能力说明且安装按钮可用；点击安装后，已安装页显示 OpenAISupporter v0.1.0「启用」「使用中」。测试页仅有临时配置和合成凭证。目录刷新网络由 fixture 隔离，使用已校验快照；这不是线上市场发布验收。
- 配套目录：public/private/routing 测试、目录完整性、市场 JS 语法和路由搜索回归通过；33 条登记、30 个公开包、3 个私有登记，原公开包保持。

原始证据仅保存在本地 `.agent-state/openai-compat/`：`tests-final-combined.log`、`quick.log`、`browser-final.log`、`safari-installed.png`；不提交测试包、缓存或凭证。

## 交付边界

宿主修复与市场目录需配套合并。当前用户 9999 未部署本修复，未变更真实来源、会话或插件。未调用真实 OpenAI 账户或付费模型，未升版、打 tag 或发布新 Aide。测试标签和独立服务已关闭；CI 在 PR 上单独执行。Issue 在合并与发布验收完成前保持打开。

## Authorized merge and actual local acceptance

User authorized both admin merges, catalog publication and local acceptance. Host #663 merged as 9af5077f29ed1055a6981fd2e122cc436406d57a after quick/go/windows/launcher/gate all succeeded. Catalog #29 merged as 3fd69a3f6936133fe83864989b5c24fc3e9b5ef9; Pages run41155991922 succeeded. Public catalog and actual 6430-byte ZIP with the original SHA-256 reverified.

Nine code files were checked against their expected baseline, backed up and applied from the merged tree. The original build.command completed with exit0 and started the new runtime. verify.command: six checks pass, buildRequired=false. Runtime image: sha256:265e6fc2affb25c20e268a226fcf434c446a15adab007fff802e4358cd2f95fc. Container healthy; healthz status=ok. Existing integrity=degraded retained. Mounts and unrelated local-file hashes matched the before snapshot. The original dirty checkout was preserved rather than reset or forcibly advanced.

Actual Safari https://localhost:9999: source compatibility explanation and enabled install button observed; marketplace install succeeded; installed view showed OpenAISupporter v0.1.0 enabled/in use. Four exact model token-pricing GETs returned official=true, USD, and the catalog display name from the unchanged published ZIP. No model requests were initiated.

The combined provider/model snapshot changed while the user actively used the app; it is not evidence that every model selection stayed unchanged. This task made no settings writes or attempted settings restoration. Local file/mount preservation assertions passed.

Final local evidence: .agent-state/openai-compat-deploy/runtime.json, verify.json, pricing-after.json, safari-compatible.png and safari-installed.png. One current workbench tab retained as handoff; user tabs were not closed.
