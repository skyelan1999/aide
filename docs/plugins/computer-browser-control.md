# Aide 浏览器与电脑控制插件设计

> 当前状态（2026-10-07）：生产 Aide 已启用浏览器控制和电脑控制。浏览器使用独立无头 Chromium；真实网页读取与模型调用通过。原生电脑桥接已获屏幕录制、辅助功能权限，窗口截图、点击、中文输入、回车、文字读回及模型调用通过。下方早期“未验收/停用”描述为阶段记录，以最新验收报告为准。Safari WebDriver、模型视觉输入、下载/上传仍未完成。


状态：浏览器控制和电脑控制插件及 macOS 桥接均已实现，默认停用。Safari 真实联调需要在 Safari 开启远程自动化；电脑真实联调需要 macOS 为启动器授予屏幕录制与辅助功能权限。尚未完成这些权限下的实机验收。浏览器操作与桌面操作是两个独立授权面。

## 目标

- 为 Aide 提供可单独启用、配置和停用的浏览器控制与电脑控制插件。
- 复用插件 ZIP 包、Python 工具运行时和每插件设置页。
- 浏览器插件可检查网页内容并执行明确授权的导航/交互；电脑插件可查看屏幕并执行明确授权的输入操作。
- 让工具结果和网页内容始终作为不可信资料处理，不把页面或截图中的文字提升为指令。

## 当前架构事实

- 插件宿主、Node.js、Python 和辅助资料 MCP 客户端都运行在 Linux 容器中。
- `/local` 是文件挂载，不提供 macOS Accessibility、AppleScript 或宿主浏览器调试协议。
- `browser-control` 插件通过独立 macOS 进程 `scripts/safari-bridge.js` 调用 Safari WebDriver；桥接进程仅提供健康检查、页面快照、导航、点击和输入 API，不提供任意命令或文件访问。
- `computer-control` 插件通过独立 macOS 进程 `scripts/computer-bridge.js` 调用屏幕捕获与辅助功能；桥接只提供屏幕截图、坐标点击、文本键入和有限按键，不提供任意命令或文件访问。
- 当前 MCP 来源只支持 stdio，并只允许模型调用服务标注为只读的工具。
- 插件 `ctx.settings` 是工作区明文 JSON 配置；不能存访问密钥，也不会自动授予网络、浏览器或桌面权限。
- Python 工具包执行在 Aide 容器内。仅增加 Python 包不能获得 Mac 屏幕、Safari 标签页或键鼠权限。
- 电脑截图当前捕获整屏；检查前台应用名称不能阻止截图包含屏幕上可见的其他窗口。使用者必须了解此限制，并显式配置允许控制的前台应用。

## 参考的 Codex 权限原则

Codex 将浏览器使用与电脑使用分开控制；企业策略可按站点和应用收窄范围，并由拒绝规则覆盖允许规则。对 Aide 应采用同样的分权思路：

1. 默认关闭两个插件，未配置本机桥接服务时不注册可执行工具。
2. 浏览器权限单独配置精确站点；桌面权限单独配置应用名单。
3. 截图、页面文本和无副作用读取可在已授权范围内运行；点击、输入、提交、下载、文件选择、快捷键和系统设置变更须逐次产生用户可见的确认。
4. 不能以插件设置 JSON 作为秘密仓库或单独的授权机制；权限必须在 Aide 服务端校验，不能只靠前端隐藏按钮。
5. 确认问题中的输入摘要只记录字符数；模型工具轨迹仍可能包含实际输入与返回内容，不能承诺整个会话日志已脱敏。桥接令牌不写入确认摘要。
6. 权限撤销、超时、取消与桥接断连必须立即停止后续动作。

参考：[Codex 企业浏览器和电脑使用控制](https://help.openai.com/ur-in/articles/20001510-manage-browser-and-computer-use-in-your-enterprise-workspace)、[Codex 计划中的浏览器控制与审批](https://help.openai.com/en/articles/11369540-using-codex-with-your-chatgpt-plan)、[Codex 沙箱与网络权限介绍](https://openai.com/index/introducing-upgrades-to-codex/)。

## 所需组件

### Aide 插件包

- `browser-control`：页面快照、链接/表单识别、站点内导航、点击和输入；站点范围由用户设置。
- `computer-control`：整屏截图、坐标点击、文本键入和有限按键；前台应用范围由用户设置。
- 两者使用不同的 MCP 服务和不同的 Aide 授权策略，不共享一个“全电脑”开关。
- MCP 工具描述仅列出该插件真正支持且服务端允许的能力。未连接桥接服务时说明缺失原因，不伪装可用。

### 本机桥接服务

浏览器桥接使用 Safari WebDriver；电脑桥接使用 macOS 屏幕捕获和辅助功能 API。Aide 启动器分别生成 `.env` 中的随机令牌并管理两个服务；容器内插件只向结构化 API 发送请求，不获得任意 shell、文件系统或网络转发接口。令牌只注入所属插件的单次工具调用进程。桥接服务校验令牌、来源地址、allowlist 和请求大小。

### Aide 服务端授权与确认

- 增加独立于插件配置的 browser/computer 授权状态和 API。
- 由 Aide 工作流处理高影响动作确认；确认内容应显示具体站点/应用、操作和参数摘要。
- 工具调用执行前再次校验权限、目标范围、有效期和取消状态。
- 用户拒绝或策略不允许时，由服务器拒绝 MCP 调用；插件返回值不能绕过策略。

## 实施顺序与验收

1. 先实现本机桥接服务及安全配对/生命周期；确认容器只能连桥接 API，不能用它访问通用宿主资源。
2. 实现服务端授权、精确目标白名单、逐次确认、取消和审计脱敏。
3. 实现两个独立 ZIP 插件包与各自设置说明。
4. 添加 Go API/授权/拒绝路径测试、MCP 协议测试、插件包安全测试。
5. 在当前 macOS Safari 做真实页面读取和一次导航；在测试应用做屏幕读取和一次经确认的输入；确认拒绝、超时、撤权与容器重启行为。
6. 默认发行镜像、macOS 启动器和离线包须包含兼容组件；旧版升级不得意外开启桌面权限。

## 当前实现与验证状态

- `browser-control` 与 `computer-control` 已作为独立内置插件加入注册表，默认停用；设置 `allowedHosts` 或 `allowedApps` 后注册读取工具。导航、点击、输入和按键由 Aide 工作流在执行前逐次要求用户明确回复“确认”。
- `scripts/browser-control-check.cjs` 用隔离的假 WebDriver 验证桥接认证、站点 allowlist、导航、快照、点击和输入；这不是 Safari 实机验收。
- `scripts/computer-control-check.cjs` 验证桌面工具注册、应用范围、逐次确认、输入长度和按键白名单；未实际调用 macOS 辅助功能。
- Safari WebDriver 实机验收需用户启用远程自动化；电脑桥接实机验收需 macOS 授予屏幕录制和辅助功能权限。未绕过浏览器安全警告，也未代用户修改这些系统权限。
- 两个插件都默认关闭，allowlist 为空。当前 RC15 运行实例未重建或重启，源码功能尚未进入该运行实例。

实现范围与验收：

1. 已实现浏览器和电脑控制插件，分别使用默认关闭、allowlist、桥接令牌与服务端逐次确认。
2. 已添加假 WebDriver、桌面桥接边界和 Go 服务端审批测试；不等同实机验收。
3. 尚待用户启用 Safari WebDriver 与 macOS 屏幕录制/辅助功能权限后，测试真实 Safari 和桌面交互。
4. 当前 RC15 运行实例没有加载此源码版本；尚未发行或部署更新镜像。

因此当前可以称 Aide **源码已包含默认关闭的 Safari 浏览器与电脑控制插件**；不能称运行中的 RC15 已加载这些插件，或声称已通过真实 Safari/桌面验收。

## 2026-10-06：修复镜像分发与旧工作区升级遗漏

实际原因是插件只被复制到 Docker 编译阶段，最终运行镜像没有插件文件；启动时也只读取旧工作区注册表。源代码新增插件不会自动进入已有的 17 项注册表。

- 最终镜像包含 `/opt/aide/builtin-plugins`，通过 `AIDE_BUILTIN_PLUGINS` 指定。开发容器指定 `/src/plugins`。
- 启动时增量注册 browser-control/computer-control，默认关闭，保留已有插件、设置和自定义代码；已明确删除的内置插件记录在 removedBuiltins 中，不会自动复活。
- 本地已有目录但缺少注册项时，校验 manifest 与入口后注册原目录，不替换内容。
- 构建哈希包含 plugins，避免插件变化被启动器误认为无需重建。

验证记录：

- 本次新增升级回归测试与相关插件/API/审批测试：`go test -race -count=1 ./internal/server -run "TestBundledControlPlugins|TestPluginLifecycle|TestPluginBundle|TestPluginSettings|TestBrowser|TestComputer"` 通过（5.070 秒）；`go vet ./internal/server` 通过。
- 独立候选镜像 `aide:control-integration-qa` 构建通过。
- 使用旧 17 项注册表启动无网络隔离容器：`scripts/test_control_plugin_runtime.py` 验证 19 项、两个控制插件初始关闭、启用浏览器后五个 executable 工具；测试容器重启后 `--check` 验证设置与工具列表保留。两次均 PASS。
- 本轮临时测试容器已删除。生产 localhost:9999 未重启、未切换；真实 Safari/桌面操作仍为 NOT_RUN。

## 2026-10-07: Current headless and native architecture

This section supersedes the earlier current-state statements; earlier records remain historical evidence.

- Browser default engine is independent Chromium on17779; `settings.engine=safari` selects the original Safari bridge on17777. `allowedHosts` applies to requests, redirects, frames and resources.
- `browser_read` returns URL, title, body and links from an isolated context, then closes it. Navigation/snapshot/click/fill/scroll use a persistent interaction context. Safari cookies and tabs are not reused. Navigation/click/fill still require Aide confirmation.
- Install using `bash scripts/headless-browser-control.sh install`. Startup launches the installed runtime without downloading. Playwright and lockfile reside in scripts/browser-runtime; browser binaries reside in the user Library/Caches/aide-browser-binaries. First installation requires network access; offline delivery is not implemented.
- Build using `bash scripts/build-native-computer-bridge.sh`. The signed independent app is installed in the user Library/Application Support/Aide directory. It provides Screen Recording and Accessibility request buttons plus live status. OS authorization remains a user action.
- Native capture targets one window belonging to the allowed foreground app. The legacy Node fallback still captures the full screen. `computer_inspect` returns accessible control labels and center coordinates, excluding secure password fields, and requires Accessibility permission.
- Production browser plugin is enabled with seven executable tools; actual Docker handler calls read example.com and DJI Chinese homepage. Dynamic specification tables, downloads and uploads are not verified. Download/upload tools are not implemented.
- Native computer bridge is ready, but both OS permissions are false. Real desktop screenshot, inspection, click and input are NOT_RUN. Service readiness is not operation acceptance.
- Text models can use body text and accessible control text. Computer screenshots are not yet passed as vision input to the model.
