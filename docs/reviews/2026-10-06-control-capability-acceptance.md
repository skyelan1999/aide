# 浏览器与电脑控制能力验收 · 2026-10-06

任务：`computer-browser-control-20261006`。范围是 Aide 控制插件、宿主桥接、工具结果及确认流程；不代表具备全部 Codex 能力。

## 实际测试结果

| 检查 | 结果 | 证据与限制 |
| --- | --- | --- |
| 生产插件加载 | 通过 | localhost:9999 显示 19 项，包含 browser-control / computer-control；两个控制插件仍停用。 |
| Safari 中设置保存及重开 | 通过 | 保存非授权 qaVerification 标记，重开读取成功，随后恢复原配置 `{}`。 |
| 容器到本机桥接 | 通过 | 实际插件 browser_status / computer_status 返回 ok；Safari session=false。健康状态不证明实际操作可用。 |
| 真实 Safari 导航 | 失败 | 用户授权并亲自完成系统确认后，已在 Safari UI 核实“允许远程自动化和外部代理”值为 1。Aide 导航仍超时；独立 4445 端口的 WebDriver 返回 session not created：连接 Safari 实例、创建自动化会话超时。尚未完成导航、输入、点击验收。 |
| 真实电脑截图 | 失败 | 本机桥接返回 `could not create image from display`；屏幕录制等宿主权限尚待检查和授权。 |
| 浏览器集成脚本 | 通过 | browser-control-check.cjs；使用假 WebDriver，覆盖域名范围、鉴权、导航、读取、点击、输入。 |
| 电脑插件边界脚本 | 通过 | computer-control-check.cjs；覆盖应用范围、确认门禁、按键和输入长度。不是实机桌面验收。 |
| 工作流确认回归 | 通过 | 六种动作乘确认、拒绝、取消、等待期间停用，共 24 组；模型参数 approved=true 不绕过用户确认。 |
| 聚焦 Go race / vet | 通过 | control/browser/computer/bundled 测试 19.473s；go vet ./internal/server exit 0。 |
| 真实付费模型选择并调用工具 | 未运行 | 本轮没有调用账户模型；工作流测试使用模拟插件执行器。 |
| 最终完整回归 | 通过 | full 门禁 pass，18 个子命令 exit 0，含全部 Go race / vet；日志 `.agent-state/verify-20261006T154107236604Z.log`，指纹 `a36df123449aad3039f6dbf057270fa41d6a9e78b8d369d88b975c1b697be656`。第一轮因测试期间代码变化导致指纹失败，已重新完整运行。 |

## 本轮修复

1. 工作流上下文传入短期插件子进程，取消任务能够终止本轮启动的 Node 执行；已发出的外部动作不能保证撤销。
2. 控制插件在等待确认后、执行前复查是否仍启用。请求发出后的撤权不具备原子中止保证。
3. 结构化状态结果以 JSON 返回模型，避免只回复“插件工具已执行”。截图 base64 不作为文本塞给模型，并明确提示当前没有视觉输入。

## 尚未具备或未验收的能力

- Safari 远程自动化权限已开启，但会话创建超时；已请求用户保存内容并重启 Safari 后继续测试。
- 电脑截图失败，真实点击、输入、按键尚未验收。
- 电脑截图未接入模型视觉输入，不能据此声称模型能看见屏幕。
- 浏览器当前只有 status、snapshot、navigate、click、fill；滚动、截图、下载、上传尚未实现。
- 设置界面仍是通用 JSON 编辑器，缺少专门的授权表单和系统权限诊断。
- 电脑桥接是独立 Node 进程，调用 screencapture / osascript，没有专门的 macOS 应用权限登记及申请界面；用户反馈系统设置中找不到授权入口。
- 本轮 Go 修复尚未重新构建并部署到生产实例；生产 UI 测试与源码测试分别记录。

结论：插件注册、连接和确认流程已有测试证据，但完整电脑及浏览器控制能力尚未达到验收标准。

## 2026-10-07 Headless / native bridge acceptance

| Check | Actual result |
|---|---|
| macOS Bash 3.2 launchers | Unicode-adjacent variable expansion reproduced and fixed with braces; 8 PID lifecycle cases passed |
| Real Chromium fixture | Read, isolation, navigation, fill, click, snapshot, scroll passed; auth, scope, redirects and file denial passed |
| Production Aide browser plugin | Enabled, 7 executable tools. Container browser_read returned Example Domain and DJI Chinese homepage, 10350 text characters and 100 links |
| Native desktop app | Signed Aide Computer Bridge.app runs on 17778; screenRecording=false, accessibility=false |
| Real desktop operations | NOT_RUN, awaiting macOS permissions |
| Safari engine | WebDriver session creation still times out; headless engine used instead |
| Paid model end-to-end | NOT_RUN; handler success does not prove model tool selection |

The independent Chromium session does not use Safari tabs or cookies. Native bridge startup and shutdown only target its own executable; Codex and Clash remain open. New settings form pending build and real UI acceptance. Screenshot results are not yet routed to model vision input. Download/upload tools remain unimplemented.

### Production UI and model acceptance

- Built and deployed image config sha256:8b032f553e6b37829293a942a23c9ee2b5bbaa0b253de8cdd5de361a36884260. Existing data volumes retained; no running tasks before restart. Old-image rollback tag attempt failed because screenshot digest did not match an available image; no rollback tag was created.
- Real Safari UI: browser settings now shows headless engine selector and allowed domain list; saved existing settings successfully, browser plugin shows enabled. Registry metadata remains version1.0.0/old description, while executable source is current.
- Aide session #31: actual DeepSeek conversation completed with one browser_read call to https://example.com/. Expanded tool trace contains title Example Domain and returned body sentence. Model-to-plugin-to-browser-to-model path passed.
- Real Docker-to-native health returned screenRecording=false/accessibility=false. Both computer_inspect and computer_snapshot correctly rejected an application outside allowedApps. No user screen was read.
- Full regression on2026-10-07 initially failed: stale browser tool count5 vs7 and unsynchronized test hook read of daemon stopCh. Updated count and captured the generation stop channel under p.mu before publishing readiness. Full rerun pending.

### 2026-10-07 final native desktop acceptance

This section supersedes earlier permission-pending records.

- Both native permissions now true. Removing the stale Aide entry and adding the current app restored accessibility; only Aide's permission entry was changed.
- Actual computer plugin handlers clicked inside a newly created blank TextEdit document, typed `Aide desktop acceptance 中文`, pressed ENTER, and typed `PASS`. `computer_inspect` read back the exact two lines. Native scoped window capture was saved and visually inspected at `.cache/control-acceptance/textedit-native-after.png`.
- Production computer plugin enabled; persisted allowedApps are 文本编辑, TextEdit, Safari. Browser remains enabled with headless engine and configured DJI/example domains.
- Actual model session #32 completed with successful computer_status and computer_inspect calls, returning the two test lines. Session #31 already proved browser_read end to end.
- Source full gate passed all 19 commands, including Go race and vet; final persisted settings changed the source fingerprint, so another full gate is running.
- Limitations: Safari WebDriver session still unavailable; isolated headless Chromium handles web content. Screenshot image payloads are not routed to model vision. Download/upload not implemented. Desktop operations require the allowed target app to be foreground; blank AX controls with no label/value can be absent from inspect. Click/type/key still require Aide action approval.
- Codex and Clash were not closed. Test text was entered only into the new blank document; no existing user document was modified. Registry display metadata still shows version1.0.0 while executable manifests are1.1.0.

- Final real Safari reload: plugin panel search 控制 shows both browser-control and computer-control enabled/使用中, and declares seven browser tools plus six computer tools.
- Real UI computer settings reopened after page reload and displayed persisted 文本编辑/TextEdit/Safari scope; canceled without modification.

### Final gate result

PASS: all 19 commands exit0, including all Go race tests and vet. Receipt `.agent-state/full.json`; log `.agent-state/verify-20261007T031544277890Z.log`; fingerprint `b1369412e8e3ec2b93b13f5fa555b1734ced8b415778443330cd5c8868744807` matched final persisted plugin configuration. Both controls remain enabled after real Safari page reload.
