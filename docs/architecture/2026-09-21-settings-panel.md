# 设置中心 · 系统设计（增量）

> **历史设计（2026-09-21）**：设置已演进为导航面板，主题为专业/经典两排，thumb 动画不是当前布局要求。 当前状态见 [现行 PRD](../PRD.md) 与 [架构](../architecture.md)。下文“待实施/已验证”仅代表原设计时间点，不能用于今天的发布判断。

> **现状核对（2026-09-25，版本 0.1.10.2 RC1）**：设置中心已从「仅收纳主题」扩展为**导航式 12 分区面板**（`internal/server/web/settings-schema.json`），存储仍由 `settings-init.js` 管理：
> - 分区顺序：`stats`(消耗统计) → `appearance`(外观) → `language`(语言) → `model`(模型参数) → `sessions-data`(归档) → `permissions`(权限管理：沙箱模式+工具权限+工具轮数) → `account`(账户/锁屏) → `persona`(aide 性格) → `voice`(语音小秘) → `accessibility`(无障碍) → `backup`(配置备份) → `about`(关于)。
> - `localStorage['aide.ui']` 文档现为 `{"version":1,"theme":"system","palette":"blue","language":...}`：在本文 `{version,theme}` 基础上**新增 `palette`（blue=专业 / green=经典，默认 blue）与 `language`**；`<html>` 上除 `data-theme`/`data-theme-pref` 外另写 `data-palette`。
> - 服务端持久化的设置（模型列表、沙箱、人格、语音、账户、无障碍、工具开关、推理强度等）经 `PUT /api/settings` 落库，**不在** localStorage；localStorage 只承载外观/语言等纯界面偏好。
> - 配置备份分区走 `POST /api/config/export`、`POST /api/config/import`。

| 项 | 值 |
| --- | --- |
| 文档类型 | 系统设计（System Design） |
| 日期 | 2026-09-21 |
| 上游依据 | `docs/prd/2026-09-21-settings-panel.md`（增量 PRD）+ `docs/PRD.md` v1.2 |
| 对应分支 | `feat/settings-panel` |
| 功能基线 | `a5f2032` |
| 作者 | 编码助手 |
| 状态 | 已落地，并后续扩展为 12 分区 |

## 1. 分层架构

```text
① 存储层  settings-init.js（<head> 首位同步外链脚本）
   localStorage['aide.ui']  ← JSON 文档 {"version":1,"theme":"system"}
   迁移：旧键 aide.theme 存在时读入 → 写 JSON → 删除旧键
   校验：theme ∈ {light,dark,system}，非法回退 system 并回写
   产出：html[data-theme] / html[data-theme-pref]（首帧前就位）
   契约：window.aideUI（JSON 设置）/ window.aideTheme（主题门面，兼容旧契约）

② 面板定义  web/settings-schema.json（go:embed 随二进制，GET /settings-schema.json）
   {"version":1,"sections":[{id,title,description,controls:[{id,type,label,options}]}]}

③ 渲染层  app.js（defer）
   brand / 顶栏迷你入口 → loadSettingsSchema()（懒加载、缓存）→ renderSettingsSheet()
   controlRenderers.segmented：轨道 + thumb + 按钮；点击 → aideUI.set('theme', v)
   aideUI.subscribe → aria-pressed + thumb 位移；resize 时重算 thumb
   开合：.open 类切换 + 焦点管理（开→关闭按钮，关→入口）

④ 视觉层  style.css（追加玻璃面板块；颜色一律用既有令牌，FR-49 归零不变）
   backdrop：var(--backdrop) + backdrop-filter: blur(10px)
   sheet：color-mix(var(--panel) 70%, transparent) + blur(28px) saturate(1.6)
          + 1px var(--line-soft) 描边 + inset 高光 + var(--shadow-3)
   thumb：var(--soft) + var(--line-soft) + var(--shadow-1)
```

## 2. 存储契约（LIM-21）

```json
{ "version": 1, "theme": "system", "palette": "blue", "language": "system" }
```

> 现状：在本文 `{version, theme}` 基础上已扩展 `palette`（`blue`=专业 / `green`=经典，默认 `blue`）与 `language`（`zh-CN`/`en`/`system`）；其余界面设置经 `PUT /api/settings` 落服务端，不入此键。

- 键名固定 `aide.ui`；`version` 为将来迁移预留。
- 读失败（隐私模式/损坏 JSON）→ 全默认并尽力回写；任何字段缺失 → 用默认值补全。
- 旧键 `aide.theme`（字符串）迁移后删除；旧键再被其他标签页写入时同样吸收。
- 不入 cookie、不入服务端、不入容器卷（LIM-16 被取代）。

## 3. 动画规格（FR-60）

| 元素 | 动画 | 时长/缓动 |
| --- | --- | --- |
| 遮罩 | opacity 0→1 | 280ms ease |
| 面板 | `translateY(-12px) scale(.97)`→none + opacity 0→1 | 450ms `cubic-bezier(.32,.72,0,1)` |
| 面板（关闭） | 反向，visibility 延迟到过渡结束 | 同上 |
| 分段 thumb | CSSOM `transform: translateX(active.offsetLeft)`，宽度=按钮宽 | 350ms `cubic-bezier(.32,.72,0,1)` |

约束：动画只依赖 transition + 类切换；thumb 位置由 JS 测量并写 CSSOM（CSP `style-src 'self'` 禁 style 属性，CSSOM 不受限）；切换主题无网络请求、不重载页面（FR-60/NFR-17 语义）。

## 4. 文件变更清单

| 文件 | 变更 |
| --- | --- |
| `web/settings-init.js` | 新建（原 `theme-init.js` 更名扩展）：JSON 设置存储 + 主题引擎 + 双门面 API |
| `web/theme-init.js` | 删除（被上者取代） |
| `web/settings-schema.json` | 新建：面板定义（外观分组 → 主题 segmented 控件） |
| `web/index.html` | head 脚本更名；brand 改 `<button>`；移除顶栏 theme-switch；顶栏加窄屏迷你入口；新增面板/遮罩标记 |
| `web/app.js` | 删除顶栏主题控件逻辑；新增设置面板模块（schema 渲染 + 开合 + 焦点 + thumb） |
| `web/style.css` | 追加玻璃面板、分段控件、迷你入口样式（只引用既有 79 令牌） |
| `docs/PRD.md` | v1.2：FR-58~60、LIM-21、变更记录 |
| `docs/prd/2026-09-21-settings-panel.md`、`docs/architecture/2026-09-21-settings-panel.md` | 本文档 |

Go 后端零改动；`//go:embed web/*` 已实证递归包含子目录，`settings-schema.json` 自动嵌入。

## 5. 约束自查

- CSP 不放松：无内联 `<script>`、无 `onclick=`、无 style 属性、无 `javascript:`。
- 79 个令牌冻结（LIM-20）：面板样式只组合既有令牌与 `color-mix()`，不新增/改名令牌。
- 首屏无闪烁：设置存储脚本仍为 `<head>` 首位**同步**外链，`defer` 不得前置。
- 键盘可达：brand 为原生 `<button>`；分段控件按钮原生 focusable + `aria-pressed`；面板 `role="dialog" aria-modal="true"`。

## 6. 验证

1. `bash scripts/aide.sh test` → go test -race + go vet + embed 构建全绿。
2. `docker compose up -d --build` 重建后健康检查通过。
3. curl 校验 `/`、`/settings-init.js`、`/settings-schema.json`、`/themes/dark/tokens.css`、`/themes/light/tokens.css` 均 200 且无 404 引用。
4. 浏览器实测（用户视觉确认）：入口开合动画、thumb 滑动、三态生效、刷新保持、跟随系统、窄屏入口。
