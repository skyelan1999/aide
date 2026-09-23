# 设置中心（品牌入口 + JSON 管理 + 主题收纳）· 增量 PRD

> **历史设计（2026-09-21）**：设置已演进为导航面板，主题为专业/经典两排，thumb 动画不是当前布局要求。 当前状态见 [现行 PRD](../PRD.md) 与 [架构](../architecture.md)。下文“待实施/已验证”仅代表原设计时间点，不能用于今天的发布判断。

| 项 | 值 |
| --- | --- |
| 文档类型 | 增量 PRD（Incremental PRD） |
| 日期 | 2026-09-21 |
| 上游依据 | `docs/PRD.md` v1.2 |
| 对应分支 | `feat/settings-panel` |
| 功能基线 | `a5f2032`（主题令牌系统基线，本分支第 1 个提交） |
| 作者 | 编码助手 |
| 状态 | 待实施 |

## 1. 需求来源（用户原话）

> 把左上角的方框中的 a 做成可点击的，做成设置功能，通过 json 进行管理，先把主题颜色 明 暗 跟随系统这个先放进去，把 UI 做一下，做成类似于苹果质感的 glass 滑动效果。

## 2. 需求分解

| 编号 | 需求 | 验收标准 | 状态 |
| --- | --- | --- | --- |
| FR-58 | 品牌设置入口 | 点击左上角 brand 图标打开设置面板；键盘可达（Enter/Space）；Esc／点击遮罩／关闭按钮均可关闭并归还焦点；≤600px 侧栏隐藏时顶栏提供等效入口 | 已实现·已验证 |
| FR-59 | 设置 JSON 管理 | 界面设置以单一 JSON 文档持久化于 localStorage 键 `aide.ui`（含 `version`，非法值回退默认并回写）；旧键 `aide.theme` 自动迁移；面板由 `settings-schema.json` 数据驱动渲染，新增设置 = 新增 schema 条目 + 一个校验器；主题收纳为「外观」分组首个控件，顶栏独立主题按钮移除 | 已实现·已验证 |
| FR-60 | 玻璃质感滑动 UI | 面板为毛玻璃圆角卡片（backdrop blur + 半透明背景 + 高光描边 + 投影），以弹性缓动滑入／滑出；主题三态为分段控件（segmented control），带滑动指示块（thumb）动画；切换主题 ≤100 ms、无网络请求、不重载页面 | 已实现·已验证 |

## 3. 与既有需求的承接与取代

| 承接 | 说明 |
| --- | --- |
| FR-49~FR-57（主题系统，`feat/theme-switching` 增量 PRD） | 令牌化、明/暗方案、首屏无闪烁、跟随系统等全部保留不变 |
| FR-52 三态切换控件 | 控件位置由顶栏**移入设置面板**；三按钮改为分段控件，键盘可达、aria-pressed 语义不变 |
| FR-53 偏好持久化 | 存储形态由字符串键升级为 JSON 文档，见 LIM-21 |
| LIM-16（`aide.theme` 字符串键） | **被 LIM-21 取代**：键名改为 `aide.ui`，值改为 JSON 对象；旧键自动迁移 |
| LIM-15（主题枚举） | 保留：仍为 `light` / `dark` / `system`，默认 `system`，非法值回退 |

## 4. 关键决策

| 决策 | 结论 | 理由 |
| --- | --- | --- |
| D1 存储 | 单一 JSON 文档 `localStorage['aide.ui']` = `{"version":1,"theme":"system",…}` | 用户要求"通过 json 进行管理"；一个键承载全部界面设置，后续设置只加字段 |
| D2 事实源 | `settings-init.js`（原 `theme-init.js` 更名并扩展）是设置唯一事实源 | 首屏主题必须同步应用（FR-55），故设置存储模块必须留在 `<head>` 首位同步外链脚本 |
| D3 面板数据驱动 | `web/settings-schema.json` 描述分组与控件，app.js 按 schema 渲染 | 新增设置不改 HTML/JS 结构，只改 JSON + 校验器 |
| D4 主题引擎兼容 | 保留 `window.aideTheme`（pref/effective/set/subscribe）契约不变，内部改为读写 JSON 文档；新增 `window.aideUI`（get/set/getAll/subscribe） | 主题契约（主题增量 PRD §10.2）不破坏；面板走新 JSON 契约 |
| D5 动画 | CSS transition + 类切换（`.open`）；thumb 位移用 CSSOM（`el.style.transform`） | CSP `style-src 'self'` 禁内联 style 属性，CSSOM 赋值不受限 |

## 5. 非目标（本期不做）

- 不做设置面板收纳模型配置（模型设置对话框保留现状）。
- 不做主题第 3/4 套方案（仍 2 套）。
- 不做设置云端同步 / 导出导入。
- 不改动 Go 后端任何接口。

## 6. 验证方式

| 项 | 方法 |
| --- | --- |
| 后端门禁 | `bash scripts/aide.sh test`（go test -race + go vet，embed 构建校验） |
| 资源可达 | 容器重建后 curl 页面、`/settings-init.js`、`/settings-schema.json`、`/themes/*/tokens.css` 均 200 |
| 浏览器实测 | 用户视觉确认：入口点击、滑入滑出动画、分段控件 thumb 滑动、明/暗/跟随系统生效、刷新后偏好保持、窄屏等效入口 |
| CSP | 无内联 `<script>`、无 `onclick=`、无内联 style 属性；CSP 不放松 |

## 7. 变更记录

| 日期 | 版本 | 变更 |
| --- | --- | --- |
| 2026-09-21 | v1.0 | 依据用户需求建立本文档 |
