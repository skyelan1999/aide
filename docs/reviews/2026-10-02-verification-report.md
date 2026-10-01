# Aide 代码整理、Skill 路由与 UI 更新验证报告

日期：2026-10-02（Asia/Shanghai）

基线：`e1848c9d6fbf99f1e7faa1d5e668dff575188e63`
范围：代码目录导航与 mock 归类、Aide Agent skill 路由、UI 克制化样式。

## 用例与结果

| 用例 | 验证项 | 执行方式 | 结果 |
| --- | --- | --- | --- |
| LAYOUT-01 | 目录导航、架构入口及本地链接存在 | `python3 scripts/test_code_layout.py` | PASS |
| LAYOUT-02 | `server`/`tts` package、Go embed 与目录图一致 | 同上 | PASS |
| LAYOUT-03 | 稳定启动入口保留，mock 已迁入 fixtures，Docker COPY 和活动文档路径一致 | 同上 | PASS |
| ROUTE-01 | 路由上限、全部 skill 文件和输出入口有效 | `python3 scripts/test_agent_skill_routing.py` | PASS |
| ROUTE-02 | 后端、前端、运行发行、安全、验证文档五类请求命中正确主 skill；无匹配回退协调者 | 同上 | PASS |
| ROUTE-03 | `route` 与 `prompt --request` 输出技能正文/建议，且不启动 agent | 同上 | PASS |
| UI-01 | 主题令牌名不变、macOS CSS 缓存号递增 | `python3 scripts/test_ui_calm.py` | PASS |
| UI-02 | 宽屏设置面板玻璃效果由后置规则覆盖；减少透明效果模式也覆盖宽屏面板 | 同上 | PASS |
| UI-03 | 键盘焦点、减少动效和桌面三列布局规则存在 | 同上 | PASS |
| UI-04 | 深色桌面首页、设置对话框和真实浏览器视觉确认 | 系统浏览器 / 隔离预览 | 未完成：Mac 锁定，浏览器自动化无法解锁；IAB 对 `https://localhost:9999/` 报 `ERR_CERT_AUTHORITY_INVALID`。未绕过证书或登录用户实例。 |
| GATE-QUICK | 仓库 quick 门禁 | `python3 scripts/agent-route.py verify quick` | PASS（15 项） |
| GATE-FULL | 仓库 full 门禁（含 Docker Go race/vet） | `python3 scripts/agent-route.py verify full` | PASS（15 项 quick + `go test -race -count=1 ./...` + `go vet ./...`） |

## 结果与修正

- 专项用例最终复跑：目录 5/5、路由 7/7、UI 样式 5/5，合计 17/17 PASS。
- Quick：PASS，收据指纹 `d4c7246e3db44f64923798bb676bde439adc75f26ddf42968bedff363523806b`。Full：PASS，收据指纹 `715c88d116b397f4a145d05c9786cffd2a426c685d3e5718b62f573e2c462419`；本机日志 `.agent-state/verify-20261001T194023588666Z.log`。

- 三组专用回归测试覆盖目录布局、路由行为和 UI 样式声明。具体用例分别位于 `scripts/test_code_layout.py`、`scripts/test_agent_skill_routing.py`、`scripts/test_ui_calm.py`。
- 检查发现 `.settings-sheet.sheet-wide` 的旧规则比新增普通面板选择器更具体，导致 22px 模糊可能胜过预期的 12px。已提高后置规则的选择器覆盖范围，并为减少透明模式添加同一宽屏面板覆盖；UI-02 对此作回归检查。
- UI 的静态/规则检查不能代替浏览器视觉验收。此前 UI 任务的隔离预览记录只覆盖 1280×720 深色首页及模型设置；本次无法在浏览器中复核当前修正。

## 环境与限制

- 用户数据与 9999 实例未登录、未写入；未绕过 TLS 证书错误。
- 静态/自动化验证全部通过；UI-04 保持未完成，不以源码检查代替浏览器目视。
- 提交 SHA、远端推送结果将在 Git 操作后补记。
