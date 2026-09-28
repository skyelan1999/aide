# Web i18n 片段→整句重构方案（合理性推断组 / 含红队）

> **调研组重大更新（已并入本版）**：覆盖率脚本报的 52 个「缺失」**全部是误报**——脚本对 needed key 做 `.strip()`、对 en.js key 不 strip，而拼接式 t() 的 key 自带首尾空格、en.js 也正确保留了空格，运行时本就能命中。52/52 逐条核验均已存在于 en.js。
> 因此：本方案的「片段→整句+占位符」重构方向不变，但 en.js 清单的性质是**用整句 key 替换旧片段 key**（不是补缺失）。另发现 **2 处真实缺口**（见 §1.15），必须修。

适用范围：`internal/server/web/`。本方案**不改动任何现有文件**，仅给出可照做的改动。
涉及文件（绝对路径）：
- `/Users/skyelan/Library/Mobile Documents/com~apple~CloudDocs/WorkStation/AI WorkStation/aide/internal/server/web/app.js`
- `/Users/skyelan/Library/Mobile Documents/com~apple~CloudDocs/WorkStation/AI WorkStation/aide/internal/server/web/locales/en.js`
- `/Users/skyelan/Library/Mobile Documents/com~apple~CloudDocs/WorkStation/AI WorkStation/aide/internal/server/web/index.html`（仅 #47，且为可选）

## 0. 关键机制与设计约束（开发组必读）

`t(key, ...args)`（`internal/server/web/i18n.js`）行为：
- 仅当语言为 `en` **且** `window.aideEnglish` 用**精确字符串**命中 key 时返回英文；否则原样返回中文 key。
- 占位符 `{0}{1}…` 用 `String(args[i])` 替换；`i >= args.length` 时**原样保留** `{i}`。
- `t()` 内部**不做 strip**。

覆盖率脚本 `/tmp/check_i18n_coverage.py` 的不对称（这是本次所有决策的根因）：
- app.js 侧：捕获 `t("中文")` 第一个字面量参数后 **`strip()`**。
- en.js 侧：正则提取 key **不 strip**。

=> **设计铁律：新增的整句 key 一律不得有前导/尾随空格**。否则 app.js 被 strip 后与 en.js 精确 key 永不相等，既报 MISS、英文也回退中文。
- 中间分隔符 ` · ` 出现在 key 内部没问题（如 `输入 {0} · 输出 {1}`）。
- 需要前导分隔空格的条件后缀，把 ` · ` 作为**中性字面量**拼在 t() 外面，不要烤进 key。
- 动态值（模型名/路径/工具名/错误信息/日期）一律用 `{n}`。
- 货币 `¥`、单位 `tokens`、符号 `⚒ · ⚠` 保留在中文 key 内（英文同形）。
- 英文无复数选择能力，统一用 `(s)` / `(n)` / `time(s)` 写法。

---

## 1. 逐处重构表

> 表达式列给出**替换后**的写法；`变量` 直接沿用原代码变量，不改名、不改判断。

### 1.1 模型/状态摘要

| # | 行号 | 原拼接表达式 | 重构后表达式 | 中文整句 key | 英文译文 |
|---|---|---|---|---|---|
| 1 | L63 | `state.config.config.model + t(" · API 已配置")` | `t("{0} · API 已配置", state.config.config.model)` | `{0} · API 已配置` | `{0} · API configured` |

> 仅在 `state.config.configured` 为真分支执行；`model` 由后端保证存在。

### 1.2 会话/子会话

| # | 行号 | 原拼接表达式 | 重构后表达式 | 中文整句 key | 英文译文 |
|---|---|---|---|---|---|
| 2 | L151 | `t("子会话") + ' (' + done.length + ')'` | `t("子会话 ({0})", done.length)` | `子会话 ({0})` | `Sub-sessions ({0})` |

> `done.length` 由外层 `if (done.length)` 守卫，必为 ≥1 整数。

### 1.3 运行阶段

| # | 行号 | 原拼接表达式 | 重构后表达式 | 中文整句 key | 英文译文 |
|---|---|---|---|---|---|
| 3 | L372 | `t('正在调用 ') + (ph.toolName \|\| t('工具'))` | `t('正在调用 {0}', ph.toolName \|\| t('工具'))` | `正在调用 {0}` | `Calling {0}` |
| 46 | L4038 | `t('运行中 · ') + phaseLabel(running) + ' · ' + formatElapsed(running.startedAt)` | `t('运行中 · {0} · {1}', phaseLabel(running), formatElapsed(running.startedAt))` | `运行中 · {0} · {1}` | `Running · {0} · {1}` |

> L372 的 `ph.toolName || t('工具')` 保护原样保留；`phaseLabel()` 返回的是已翻译字符串，作为 `{0}` 传入安全（String 直接插入）。

### 1.4 运行 meta / 无回答

| # | 行号 | 原拼接表达式 | 重构后表达式 | 中文整句 key | 英文译文 |
|---|---|---|---|---|---|
| 4 | L569 | `t("策略: ") + (run.strategy === 'auto' ? t("自动 → ") + profileName(run.profile) : t("手动 · ") + profileName(run.profile))` | `run.strategy === 'auto' ? t("策略: 自动 → {0}", profileName(run.profile)) : t("策略: 手动 · {0}", profileName(run.profile))` | `策略: 自动 → {0}` / `策略: 手动 · {0}` | `Strategy: Auto → {0}` / `Strategy: Manual · {0}` |
| 5 | L594 | `t("未返回回答") + '（' + (run.toolUses?.length ? t("已完成 {0} 次工具调用", run.toolUses.length) : t("模型未生成正文")) + '）'` | `run.toolUses?.length ? t("未返回回答（已完成 {0} 次工具调用）", run.toolUses.length) : t("未返回回答（模型未生成正文）")` | `未返回回答（已完成 {0} 次工具调用）` / `未返回回答（模型未生成正文）` | `No response returned ({0} tool call(s) completed)` / `No response returned (model produced no text)` |

> L594 把“嵌套 t()”拍平成两条整句，去掉运行时二次 t() 嵌套；条件判断 `run.toolUses?.length` 完全不变。

### 1.5 工具/文件调用汇总

| # | 行号 | 原拼接表达式 | 重构后表达式 | 中文整句 key | 英文译文 |
|---|---|---|---|---|---|
| 6 | L666 | `t("⚒ 文件查看 · ") + fileUses.length + t(" 次")` | `t("⚒ 文件查看 · {0} 次", fileUses.length)` | `⚒ 文件查看 · {0} 次` | `⚒ File reads · {0} time(s)` |
| 7 | L676 | `isCommand ? t("⚒ 建议命令") : t("⚒ 工具调用 · ") + use.tool` | `isCommand ? t("⚒ 建议命令") : t("⚒ 工具调用 · {0}", use.tool)` | `⚒ 工具调用 · {0}` | `⚒ Tool call · {0}` |

> `⚒ 建议命令` 分支保持原整句不动；`fileUses.length` 由 `if (fileUses.length)` 守卫。

### 1.6 附件 chip

| # | 行号 | 原拼接表达式 | 重构后表达式 | 中文整句 key | 英文译文 |
|---|---|---|---|---|---|
| 8a | L751 | `(a.root === 'context' ? t("参考 · ") : '') + a.path` | `(a.root === 'context' ? t("参考 · {0}", a.path) : a.path)` | `参考 · {0}` | `Reference · {0}` |
| 8b | L751 | `b.setAttribute('aria-label', t("移除附件 ") + a.path)` | `b.setAttribute('aria-label', t("移除附件 {0}", a.path))` | `移除附件 {0}` | `Remove attachment {0}` |

> 非 context 分支直接用 `a.path`（用户路径，不翻译），与原逻辑一致。

### 1.7 上下文预算（cp-summary / cp-detail）

| # | 行号 | 原拼接表达式 | 重构后表达式 | 中文整句 key | 英文译文 |
|---|---|---|---|---|---|
| 9a | L1033 | `data.overLimit ? t(" · ⚠ 超限 ") + Math.max(0, data.totalEstimate - data.contextWindow) : ''` | `data.overLimit ? ' · ' + t("⚠ 超限 {0}", Math.max(0, data.totalEstimate - data.contextWindow)) : ''` | `⚠ 超限 {0}` | `⚠ Over by {0}` |
| 9b | L1034 | `t("输入估算 ") + data.inputEstimate + t(" tokens + 输出预留 ") + data.outputReserve + ' = ' + data.totalEstimate + t(" / 窗口 ") + data.contextWindow + overText` | `t("输入估算 {0} tokens + 输出预留 {1} = {2} / 窗口 {3}", data.inputEstimate, data.outputReserve, data.totalEstimate, data.contextWindow) + overText` | `输入估算 {0} tokens + 输出预留 {1} = {2} / 窗口 {3}` | `Est. input {0} tokens + output reserve {1} = {2} / window {3}` |
| 10 | L1040 | `t("历史消息 ") + (bd.historyMessages \|\| 0) + t(" 条")` | `t("历史消息 {0} 条", bd.historyMessages \|\| 0)` | `历史消息 {0} 条` | `History messages: {0}` |
| 11 | L1042 | `t("附件 ") + (bd.attachmentFiles \|\| 0) + t(" 个")` | `t("附件 {0} 个", bd.attachmentFiles \|\| 0)` | `附件 {0} 个` | `Attachments: {0}` |
| 12 | L1044 | `t("工具定义 ") + (bd.toolCount \|\| 0) + t(" 个")` | `t("工具定义 {0} 个", bd.toolCount \|\| 0)` | `工具定义 {0} 个` | `Tool definitions: {0}` |
| 13 | L1049 | `chars + t(" 字符 ≈ ") + Math.floor(chars / 4) + ' tokens'` | `t("{0} 字符 ≈ {1} tokens", chars, Math.floor(chars / 4))` | `{0} 字符 ≈ {1} tokens` | `{0} chars ≈ {1} tokens` |

> L1033 的 ` · ` 作为中性字面量外置（避免前导空格 key）；`overText` 变量拼接逻辑不变。

### 1.8 配置（profile）删除

| # | 行号 | 原拼接表达式 | 重构后表达式 | 中文整句 key | 英文译文 |
|---|---|---|---|---|---|
| 14 | L1559 | `del.setAttribute('aria-label', t("删除配置 ") + profile.name)` | `del.setAttribute('aria-label', t("删除配置 {0}", profile.name))` | `删除配置 {0}` | `Delete profile {0}` |

> 注意与已有的 `删除配置「{0}」？`（confirm 弹窗）区分，这是 aria-label，不带书名号。

### 1.9 策略按钮 / 模型选择 / toast

| # | 行号 | 原拼接表达式 | 重构后表达式 | 中文整句 key | 英文译文 |
|---|---|---|---|---|---|
| 15 | L1632 | `(p.strategy === 'auto' ? t("策略 · 自动") : t("策略 · ") + profileName(p.activeProfile)) + (modelName ? ' · ' + modelName : '')` | `(p.strategy === 'auto' ? t("策略 · 自动") : t("策略 · {0}", profileName(p.activeProfile))) + (modelName ? ' · ' + modelName : '')` | `策略 · {0}` | `Strategy · {0}` |
| 16 | L1650 | `m.id + ' · ' + (m.contextWindow \|\| 65536) / 1024 + t("K 上下文")` | `t("{0} · {1}K 上下文", m.id, (m.contextWindow \|\| 65536) / 1024)` | `{0} · {1}K 上下文` | `{0} · {1}K context` |
| 17 | L1678 | `toast(t("已切换模型：") + modelName)` | `toast(t("已切换模型：{0}", modelName))` | `已切换模型：{0}` | `Model switched to: {0}` |
| 18 | L1685 | `toast(t("已切换推理强度：") + ({auto:t("自动"),off:t("关闭"),low:t("低"),medium:t("中"),high:t("高")}[value] \|\| value))` | 先 `const eff = ({auto:t("自动"),off:t("关闭"),low:t("低"),medium:t("中"),high:t("高")}[value] \|\| value);` 再 `toast(t("已切换推理强度：{0}", eff))` | `已切换推理强度：{0}` | `Reasoning effort changed: {0}` |
| 19 | L1692 | `toast(kind === 'auto' ? t("已切换为自动路由策略") : t("已切换为手动策略 · ") + profileName(value))` | `toast(kind === 'auto' ? t("已切换为自动路由策略") : t("已切换为手动策略 · {0}", profileName(value)))` | `已切换为手动策略 · {0}` | `Switched to manual strategy · {0}` |

> L1632 的 `' · ' + modelName` 保留为中性字面量（“ · ”中英同形，无 CJK，不被覆盖率脚本采集）。`策略 · 自动` 已在 en.js，不重复加。

### 1.10 用量统计（chips / tooltip / 日历）

| # | 行号 | 原拼接表达式 | 重构后表达式 | 中文整句 key | 英文译文 |
|---|---|---|---|---|---|
| 20 | L1756 | `metric(t("累计用量"), fmtStatTokens(totalsObj.total \|\| 0), 'tokens · ' + (totalsObj.calls \|\| 0) + t(" 次调用"))` | `metric(t("累计用量"), fmtStatTokens(totalsObj.total \|\| 0), t("tokens · {0} 次调用", totalsObj.calls \|\| 0))` | `tokens · {0} 次调用` | `tokens · {0} call(s)` |
| 21 | L1761 | `t("今日 ") + fmtStatTokens(todayStats.total \|\| 0) + ' tokens' + (todayStats.priced !== false ? ' · ¥' + (dayCost[Object.keys(days).sort().pop()] \|\| 0).toFixed(2) : t(" · 未计价"))` | `t("今日 {0} tokens", fmtStatTokens(todayStats.total \|\| 0)) + (todayStats.priced !== false ? ' · ¥' + (dayCost[Object.keys(days).sort().pop()] \|\| 0).toFixed(2) : ' · ' + t("未计价"))` | `今日 {0} tokens` / `未计价` | `Today {0} tokens` / `Unpriced` |
| 22 | L1762 | `t("调用 ") + (totalsObj.calls \|\| 0) + t(" 次")` | `t("调用 {0} 次", totalsObj.calls \|\| 0)` | `调用 {0} 次` | `{0} calls` |
| 23 | L1770 | `t("未计价历史 ") + fmtStatTokens(unpriced.total) + ' tokens · ' + (unpriced.calls \|\| 0) + t(" 次")` | `t("未计价历史 {0} tokens · {1} 次", fmtStatTokens(unpriced.total), unpriced.calls \|\| 0)` | `未计价历史 {0} tokens · {1} 次` | `Unpriced history {0} tokens · {1} calls` |
| 24 | L1839 | `t("余额 ") + parts` | `t("余额 {0}", parts)` | `余额 {0}` | `Balance {0}` |
| 25 | L1844 | `chip.title = t("查询失败: ") + error.message` | `chip.title = t("查询失败: {0}", error.message)` | `查询失败: {0}` | `Query failed: {0}` |
| 26 | L1870 | `pricedDay ? t("费用 ¥") + (dayCost[date] \|\| 0).toFixed(4) : t("费用未知（旧数据未计价）")` | `pricedDay ? t("费用 ¥{0}", (dayCost[date] \|\| 0).toFixed(4)) : t("费用未知（旧数据未计价）")` | `费用 ¥{0}` | `Cost ¥{0}` |
| 27 | L1874 | `t("输入 ") + fmtStatTokens(day.prompt \|\| 0) + t(" · 输出 ") + fmtStatTokens(day.completion \|\| 0)` | `t("输入 {0} · 输出 {1}", fmtStatTokens(day.prompt \|\| 0), fmtStatTokens(day.completion \|\| 0))` | `输入 {0} · 输出 {1}` | `Input: {0} · Output: {1}` |
| 28 | L1876 | `t("调用 ") + (day.calls \|\| 0) + t(" 次 · ") + fee + (day.estimated ? t("（用量为估算）") : '')` | `t("调用 {0} 次 · {1}", day.calls \|\| 0, fee) + (day.estimated ? t("（用量为估算）") : '')` | `调用 {0} 次 · {1}` | `{0} calls · {1}` |
| 29 | L1878 | `t("所在周合计 ") + fmtStatTokens(weekTotal) + ' tokens'` | `t("所在周合计 {0} tokens", fmtStatTokens(weekTotal))` | `所在周合计 {0} tokens` | `Week total {0} tokens` |
| 30 | L1901 | `key + ' · ' + fmtStatTokens(day.total \|\| 0) + t(" tokens，查看当日明细")` | `t("{0} · {1} tokens，查看当日明细", key, fmtStatTokens(day.total \|\| 0))` | `{0} · {1} tokens，查看当日明细` | `{0} · {1} tokens; view daily details` |
| 31 | L1922 | `pricedDay ? t("费用 ¥") + (dayCost[key] \|\| 0).toFixed(4) + t("（按调用时刻计价快照）") : t("费用未知：旧数据没有逐调用与计价证据，未按当前费率冒充")` | `pricedDay ? t("费用 ¥{0}（按调用时刻计价快照）", (dayCost[key] \|\| 0).toFixed(4)) : t("费用未知：旧数据没有逐调用与计价证据，未按当前费率冒充")` | `费用 ¥{0}（按调用时刻计价快照）` | `Cost ¥{0} (per-call rate snapshots)` |
| 32 | L1925 | `t("输入 ") + fmtStatTokens(day.prompt \|\| 0) + t(" tokens · 输出 ") + fmtStatTokens(day.completion \|\| 0) + ' tokens'` | `t("输入 {0} tokens · 输出 {1} tokens", fmtStatTokens(day.prompt \|\| 0), fmtStatTokens(day.completion \|\| 0))` | `输入 {0} tokens · 输出 {1} tokens` | `Input: {0} tokens · Output: {1} tokens` |
| 33 | L1926 | `t("调用 ") + (day.calls \|\| 0) + t(" 次 · 合计 ") + fmtStatTokens(day.total \|\| 0) + ' tokens' + (day.estimated ? t("（用量为估算）") : '')` | `t("调用 {0} 次 · 合计 {1} tokens", day.calls \|\| 0, fmtStatTokens(day.total \|\| 0)) + (day.estimated ? t("（用量为估算）") : '')` | `调用 {0} 次 · 合计 {1} tokens` | `{0} calls · total {1} tokens` |
| 34 | L1973 | `toast(t("费率保存失败: ") + error.message)` | `toast(t("费率保存失败: {0}", error.message))` | `费率保存失败: {0}` | `Could not save rates: {0}` |

> L1876/L1926 中 `fee`（L1870/L1922 的整句结果）作为 `{1}` 占位符传入，String() 直接插入，安全。
> L1876 与 L1925/L1926 是**两个不同 key**（一个带 tokens、一个不带），不要合并。
> L1870 与 L1922 也是**两个不同 key**（一个带“（按调用时刻计价快照）”后缀、一个不带）。

### 1.11 模型获取 / 上下文卡片标题

| # | 行号 | 原拼接表达式 | 重构后表达式 | 中文整句 key | 英文译文 |
|---|---|---|---|---|---|
| 35 | L2017 | `btn.title = p.label + t(" 上下文")` | `btn.title = t("{0} 上下文", p.label)` | `{0} 上下文` | `{0} context` |
| 36 | L2080 | `t("已获取 ") + (data.models \|\| []).length + t(" 个可用模型，在输入框中选择即可")` | `t("已获取 {0} 个可用模型，在输入框中选择即可", (data.models \|\| []).length)` | `已获取 {0} 个可用模型，在输入框中选择即可` | `Fetched {0} available model(s). Select one in the input field.` |
| 37 | L2106 | `(included ? t("最近 ") + included + t(" 条消息") : t("当前会话暂无内容")) + t(" · 4 字符/词估算 · tokens 已用/窗口")` | `(included ? t("最近 {0} 条消息", included) : t("当前会话暂无内容")) + t(" · 4 字符/词估算 · tokens 已用/窗口")` | `最近 {0} 条消息` | `Recent {0} message(s)` |

> L2106 的 `当前会话暂无内容` 与后缀 ` · 4 字符/词估算 · tokens 已用/窗口` 已在 en.js 且无首尾空格，保持不动，只把 `最近 {0} 条消息` 整句化。

### 1.12 工作空间 / 来源

| # | 行号 | 原拼接表达式 | 重构后表达式 | 中文整句 key | 英文译文 |
|---|---|---|---|---|---|
| 38 | L2195 | `(state.config?.workspaceDisplay \|\| '/workspace') + t(" · 本地")` | `t("{0} · 本地", state.config?.workspaceDisplay \|\| '/workspace')` | `{0} · 本地` | `{0} · Local` |
| 39 | L2375 | `src.type + (src.config.path \|\| src.config.url \|\| src.config.host \|\| '') + (src.rw ? t(" · 读写") : t(" · 只读"))` | `const sp = src.config.path \|\| src.config.url \|\| src.config.host \|\| ''; chip.title = src.rw ? t("{0}{1} · 读写", src.type, sp) : t("{0}{1} · 只读", src.type, sp);` | `{0}{1} · 读写` / `{0}{1} · 只读` | `{0}{1} · Read/write` / `{0}{1} · Read-only` |
| 40 | L2380 | `del.title = t("删除来源 ") + src.name` | `del.title = t("删除来源 {0}", src.name)` | `删除来源 {0}` | `Delete source {0}` |
| 41 | L2443 | `toast(t("已添加来源：") + name)` | `toast(t("已添加来源：{0}", name))` | `已添加来源：{0}` | `Source added: {0}` |

> L2195 只重构**本地分支**；SSH 分支 `(w.host || t("远程")) + ' · SSH/SFTP'` 不在本缺口列表，保持原样。
> L2375 把 `path/url/host` 三元链先存 `sp`，避免在 t() 参数里重复长表达式。

### 1.13 调用记录汇总 / 压缩 / 打开文件

| # | 行号 | 原拼接表达式 | 重构后表达式 | 中文整句 key | 英文译文 |
|---|---|---|---|---|---|
| 42 | L2949 | `t('共 ') + calls.length + t(' 次 · 主') + mainN + t(' 子') + subN + t(' 失败') + failN` | `t('共 {0} 次 · 主{1} 子{2} 失败{3}', calls.length, mainN, subN, failN)` | `共 {0} 次 · 主{1} 子{2} 失败{3}` | `{0} calls · main {1} · sub {2} · failed {3}` |
| 43 | L3084 | `toast(t("打不开文件: ") + path)` | `toast(t("打不开文件: {0}", path))` | `打不开文件: {0}` | `Cannot open file: {0}` |
| 44 | L3092 | `sess?.compactedMessages ? t("已折叠 ") + sess.compactedMessages + t(" 条消息") : ''` | `sess?.compactedMessages ? t("已折叠 {0} 条消息", sess.compactedMessages) : ''` | `已折叠 {0} 条消息` | `{0} messages folded` |
| 45 | L3097 | `toast(res.folded ? t("已压缩 ") + res.folded + t(" 条历史消息") : t("历史未超阈值，无需压缩"))` | `toast(res.folded ? t("已压缩 {0} 条历史消息", res.folded) : t("历史未超阈值，无需压缩"))` | `已压缩 {0} 条历史消息` | `{0} history message(s) compacted` |

> L3092 的 `sess?.compactedMessages` 为真值才进入（已隐式保护 0/undefined）。

### 1.14 index.html 静态占位符

| # | 行号 | 原 | 处理 | 中文 key（coverage 采集到的原始属性串） | 英文译文 |
|---|---|---|---|---|---|
| 47 | index.html L23 | `data-i18n-placeholder="例如：go version &amp;&amp; git status --short"` | **不改 HTML**，仅在 en.js 补一条（见 §2 末尾红队说明） | `例如：go version &amp;&amp; git status --short` | `e.g. go version && git status --short` |

> 运行时浏览器会把属性里的 `&amp;` 解码为 `&`，`applyStatic()` 实际查找的是 `例如：go version && git status --short`（en.js L320 已有）。coverage 脚本读原始文本才看到 `&amp;&amp;`——这是脚本「HTML 实体未解码」bug，见 §3.9。

### 1.15 调研组新发现的真实缺口（脚本看不见，必须修）

| # | 行号 | 现状 | 修复动作 | 说明 |
|---|---|---|---|---|
| 48 | app.js L520（`statuses` 表） | `const statuses = { ..., awaiting_clarification: '等待澄清' };` 经 L569 `t(statuses[run.status] \|\| run.status)` **变量调用**，脚本正则只认 `t("字面量")`，抓不到这个 key；en.js 确无此词条 | **en.js 新增** `"等待澄清": "Awaiting clarification"` | 这是真缺口：English 模式下该状态会回退成中文「等待澄清」。其余 statuses（运行中/已完成/失败/已停止/已中断/等待应用）en.js 均已有。 |
| 49 | app.js L3188 | `voice.name = () => (state.config && state.config.voiceAssistantName) \|\| '小秘';` | 把兜底字面量包成 `\|\| t('小秘')` | en.js:754 已有 `"小秘":"Assistant"`，**无需加词条**，仅包 t()。 |
| 49 | app.js L3701 | `input.placeholder = '小秘';` | `input.placeholder = t('小秘');` | 同上。 |
| 49 | app.js L3702 | `input.value = (state.config && state.config.voiceAssistantName) \|\| '小秘';` | `\|\| t('小秘')` | 同上。 |
| 49 | app.js L3707 | `const name = input.value.trim() \|\| '小秘';` | `\|\| t('小秘')` | ⚠️ 见 §3.7 备注：此处是**写回 config.voiceAssistantName** 的兜底，英文下会把 "Assistant" 持久化，属既有默认名本地化，需开发组确认可接受。 |
| 49 | app.js L4061 | `const xm = (state.config && state.config.voiceAssistantName) \|\| '小秘';` | `\|\| t('小秘')` | 解锁欢迎语里显示用。 |

> 其余 `小秘` 出现处均已是更大的整句 key（如 `t('小秘名字')`、`t('语音弹框标题使用这个名字，默认「小秘」。')`、`t('小秘性格系统')`），且 en.js 已有对应词条，**不在本次改动范围**。

---

## 2. en.js 新增词条完整清单（可直接粘贴）

> 性质说明：前 53 条是**新整句 key**（替换旧片段 key，让脚本 strip 后精确命中）；最后 1 条 `等待澄清` 是**真缺口**；`小秘` 已有词条（L754），**无需新增**，仅在 app.js 包 t()。
> 追加到 `internal/server/web/locales/en.js` 对象内（注意前一条末尾补逗号）：

```js
  "{0} · API 已配置": "{0} · API configured",
  "子会话 ({0})": "Sub-sessions ({0})",
  "正在调用 {0}": "Calling {0}",
  "策略: 自动 → {0}": "Strategy: Auto → {0}",
  "策略: 手动 · {0}": "Strategy: Manual · {0}",
  "未返回回答（已完成 {0} 次工具调用）": "No response returned ({0} tool call(s) completed)",
  "未返回回答（模型未生成正文）": "No response returned (model produced no text)",
  "⚒ 文件查看 · {0} 次": "⚒ File reads · {0} time(s)",
  "⚒ 工具调用 · {0}": "⚒ Tool call · {0}",
  "参考 · {0}": "Reference · {0}",
  "移除附件 {0}": "Remove attachment {0}",
  "⚠ 超限 {0}": "⚠ Over by {0}",
  "输入估算 {0} tokens + 输出预留 {1} = {2} / 窗口 {3}": "Est. input {0} tokens + output reserve {1} = {2} / window {3}",
  "历史消息 {0} 条": "History messages: {0}",
  "附件 {0} 个": "Attachments: {0}",
  "工具定义 {0} 个": "Tool definitions: {0}",
  "{0} 字符 ≈ {1} tokens": "{0} chars ≈ {1} tokens",
  "删除配置 {0}": "Delete profile {0}",
  "策略 · {0}": "Strategy · {0}",
  "{0} · {1}K 上下文": "{0} · {1}K context",
  "已切换模型：{0}": "Model switched to: {0}",
  "已切换推理强度：{0}": "Reasoning effort changed: {0}",
  "已切换为手动策略 · {0}": "Switched to manual strategy · {0}",
  "tokens · {0} 次调用": "tokens · {0} call(s)",
  "今日 {0} tokens": "Today {0} tokens",
  "未计价": "Unpriced",
  "调用 {0} 次": "{0} calls",
  "未计价历史 {0} tokens · {1} 次": "Unpriced history {0} tokens · {1} calls",
  "余额 {0}": "Balance {0}",
  "查询失败: {0}": "Query failed: {0}",
  "费用 ¥{0}": "Cost ¥{0}",
  "输入 {0} · 输出 {1}": "Input: {0} · Output: {1}",
  "调用 {0} 次 · {1}": "{0} calls · {1}",
  "所在周合计 {0} tokens": "Week total {0} tokens",
  "{0} · {1} tokens，查看当日明细": "{0} · {1} tokens; view daily details",
  "费用 ¥{0}（按调用时刻计价快照）": "Cost ¥{0} (per-call rate snapshots)",
  "输入 {0} tokens · 输出 {1} tokens": "Input: {0} tokens · Output: {1} tokens",
  "调用 {0} 次 · 合计 {1} tokens": "{0} calls · total {1} tokens",
  "费率保存失败: {0}": "Could not save rates: {0}",
  "{0} 上下文": "{0} context",
  "已获取 {0} 个可用模型，在输入框中选择即可": "Fetched {0} available model(s). Select one in the input field.",
  "最近 {0} 条消息": "Recent {0} message(s)",
  "{0} · 本地": "{0} · Local",
  "{0}{1} · 读写": "{0}{1} · Read/write",
  "{0}{1} · 只读": "{0}{1} · Read-only",
  "删除来源 {0}": "Delete source {0}",
  "已添加来源：{0}": "Source added: {0}",
  "共 {0} 次 · 主{1} 子{2} 失败{3}": "{0} calls · main {1} · sub {2} · failed {3}",
  "打不开文件: {0}": "Cannot open file: {0}",
  "已折叠 {0} 条消息": "{0} messages folded",
  "已压缩 {0} 条历史消息": "{0} history message(s) compacted",
  "运行中 · {0} · {1}": "Running · {0} · {1}",
  "例如：go version &amp;&amp; git status --short": "e.g. go version && git status --short",
  "等待澄清": "Awaiting clarification"
```

共 54 条（53 条整句替换 + 1 条真缺口 `等待澄清`）。
> `小秘` 不加词条：en.js L754 `"小秘": "Assistant"` 已存在，只改 app.js 包 t()。

### 2.1 旧片段 key（重构后不再被引用，可保留或后续清理）
下列旧 en.js 词条在 app.js 改完整句后变为**死词条**（脚本只查缺、不查多，留着不影响绿灯、运行时也无害）：
` · API 已配置`、`策略: `、`自动 → `、`手动 · `、`正在调用 `、`运行中 · `、`未返回回答`、`已完成 {0} 次工具调用`、`模型未生成正文`、`⚒ 文件查看 · `、` 次`、`⚒ 工具调用 · `、`参考 · `、`移除附件 `、` · ⚠ 超限 `、`输入估算 `、` tokens + 输出预留 `、` / 窗口 `、`历史消息 `、` 条`、`附件 `、` 个`、`工具定义 `、` 字符 ≈ `、`删除配置 `、`策略 · `、`K 上下文`、`已切换模型：`、`已切换推理强度：`、`已切换为手动策略 · `、` 次调用`、`今日 `、` · 未计价`、`调用 `、`未计价历史 `、`余额 `、`查询失败: `、`费用 ¥`、`输入 `、` · 输出 `、` 次 · `、`所在周合计 `、` tokens，查看当日明细`、`（按调用时刻计价快照）`、` tokens · 输出 `、` 次 · 合计 `、`费率保存失败: `、` 上下文`、`已获取 `、` 个可用模型，在输入框中选择即可`、`最近 `、` 条消息`、` · 本地`、` · 读写`、` · 只读`、`删除来源 `、`已添加来源：`、`共 `、` 次 · 主`、` 子`、` 失败`、`打不开文件: `、`已折叠 `、`已压缩 `、` 条历史消息`。
- **本次不清理**，待上线冒烟后单独开 PR 清理并回归。

---

## 3. 红队风险审查

### 3.1 动态值 null / undefined / 0
`t()` 用 `String(args[i])`：`String(null)="null"`、`String(undefined)="undefined"`、`String(0)="0"`。

| 位置 | 风险点 | 原代码保护 | 重构后是否保留 |
|---|---|---|---|
| L63 | `state.config.config.model` | 外层 `if configured` 守卫 | 保留，原样传入 |
| L1650 | `m.contextWindow` | `\|\| 65536` | 保留 |
| L1040/1042/1044 | bd 各计数 | `\|\| 0` | 保留 |
| L1049 | `chars` | 外层 `if (chars)` 守卫；`Math.floor(chars/4)` 恒为数 | 保留 |
| L1756/1762/1770/1874/1876/1878/1901/1925/1926 | tokens/calls | 全部 `\|\| 0` 或 `fmtStatTokens(... \|\| 0)` | 保留 |
| L1839 | `parts` | 外层 `if (!infos.length) return`；内部 `?? '?'`、`\|\| ''` | 保留 |
| L1844/1973 | `error.message` | **无保护**，可能为 undefined → "undefined" | 与原行为一致（原 `+error.message` 也会拼出 "undefined"），**不新增保护**以免改变行为；建议开发组顺手确认 |
| L1559/2380/2443 | `profile.name`/`src.name`/`name` | 表单有默认值；空字符串时 String("")="" 不报错 | 保留 |
| L2375 | `src.type` / `sp` | `sp` 已 `\|\| ''`；`src.type` 若 undefined 会显示 "undefined" | 与原拼接一致，不新增 |
| L372 | `ph.toolName` | `\|\| t('工具')` | 保留 |
| L569/1692 | `profileName()` | 内部 `\|\| id`；id 仍可能 undefined | 与原行为一致 |

**结论**：所有 `\|\| 0` / `\|\| ''` / 外层守卫一律原样保留，不增减。唯一“无保护”的是 `error.message`，但这是**既有行为**，本次不改变（保持功能零改动红线）。

### 3.2 数字与货币格式化
- `fmtStatTokens(n)` 返回 **string**（`<1000` 时 `String(n)`，否则 `(n/1000).toFixed(1)+'K'`）。作为占位符传入不影响格式。
- `.toFixed(2)/(4)` 返回 **string**，如 `"0.00"`、`"0.0000"`。占位符原样插入。
- L1650 `(m.contextWindow || 65536)/1024` 是**裸除法**，可能产生小数（如 195.3125）。**原代码不 round，重构后也不 round**，表达式逐字保留。英文 `{1}K context` 会显示 `195.3K context`——与中文完全一致，无回归。
- `¥` 位置：L1870/L1922 key 为 `费用 ¥{0}`，`¥` 在数字前，中英同形，正确。

### 3.3 拼接空格（strip 不对称的核心）
- 旧片段 key 大量带首尾空格（`"已获取 "`、`" 个"`、`"调用 "`、`" · 未计价"`）。这些在 app.js 侧被脚本 strip，在 en.js 侧不 strip → 既误报 MISS、英文也回退中文。
- **本方案所有新 key 均无首尾空格**（见 §0 铁律）。
- 需要前导分隔空格的两处（L1033 超限后缀、L1761 未计价后缀），把 ` · ` 外置为中性字面量：
  - L1033：`' · ' + t("⚠ 超限 {0}", …)`
  - L1761：`' · ' + t("未计价")`
- L1632 的 `' · ' + modelName` 保持中性字面量（无 CJK，不被采集）。
- 后果：这些新 key 在 app.js 被 strip 后 == en.js 精确 key，coverage 与运行时同时命中。

### 3.4 英文语序
- 多数 “标签 {n}” 中英语序一致（`今日 {0} tokens` / `Today {0} tokens`）。
- 需显式调整语序/标点的：
  - `输入 {0} · 输出 {1}` → `Input: {0} · Output: {1}`（英文加冒号）。
  - `调用 {0} 次` → `{0} calls`（英文把数字提前，去掉“次”）。
  - `共 {0} 次 · 主{1} 子{2} 失败{3}` → `{0} calls · main {1} · sub {2} · failed {3}`（英文数字提前，补 `main/sub/failed` 单词）。
  - `历史消息 {0} 条` → `History messages: {0}`（英文加冒号）。
  - `删除配置 {0}` → `Delete profile {0}`（英文动词在前，与中文“删除配置 名”语序天然一致）。
- 动态嵌套值（L1876 的 `fee`、L4038 的 `phaseLabel()`）作为占位符插入，英文语序由整句决定，不破坏。

### 3.5 嵌套 t()
- L594 原为 `t("未返回回答") + '（' + (… ? t("已完成 {0} 次工具调用", n) : t("模型未生成正文")) + '）'`。
- 重构后拍平为两条整句 key（§1.4 #5），**消除运行时嵌套 t()**。
- L372 `ph.toolName || t('工具')`、L1685 effort 映射里的 `t('自动')` 等是“把已翻译结果作为占位符参数”，不是嵌套 t() 计算，安全保留。

### 3.6 条件片段（计价/未计价、读写/只读、auto/手动）
- **拆为两条整句 key**（推荐）：
  - L1761：priced 走 `今日 {0} tokens` + 中性 `' · ¥' + cost`；unpriced 走 `' · ' + t("未计价")`。条件 `todayStats.priced !== false` 原样。
  - L1870/L1922：`pricedDay ? t("费用 ¥…{0}…") : t("费用未知…")`，条件原样。
  - L2375：`src.rw ? t("{0}{1} · 读写") : t("{0}{1} · 只读")`，条件原样。
  - L569/L1632/L1692：auto/manual 各一条整句。
- **不引入**一个 key 内塞条件变量的写法（t() 无分支能力）。

### 3.7 功能逻辑零改动（逐条确认）
- 所有重构**只替换字符串构造**，不改：条件判断（`=== 'auto'`、`priced !== false`、`src.rw`、`overLimit`、`estimated`）、变量取值（`|| 0`、`|| 65536`、`?? '?'`）、DOM 结构（`el(...)` 父子关系、`append`、`setAttribute`、`textContent`、`title`、`aria-label`）。
- L1685 把 effort 映射先存局部变量 `eff`，仅为可读性，不改变取值。
- L2375 把 `sp` 提为局部变量，表达式与原三元链逐字等价。
- 不触碰 Go 后端、settings-schema.json、profiles.json、index.html 结构。
- ⚠️ **L3707 特例**：`const name = input.value.trim() || '小秘'` 是把默认名**写回** `config.voiceAssistantName`。包成 `t('小秘')` 后，英文空白提交会持久化 `"Assistant"`（中文仍 `"小秘"`）。这是把“默认名”本地化，符合调研组结论；但若产品希望默认名始终是中文「小秘」、仅显示时翻译，则应**只包显示侧**（L3188/3701/3702/4061），**L3707 保持 `'小秘'` 不包 t()**。二选一，推荐按调研组“5 处都包”执行，上线前目认一次。

### 3.9 覆盖率脚本 bug（记录，不修脚本）
`/tmp/check_i18n_coverage.py` 有两处已知 bug，导致 52 个假阳性：
1. **strip 不对称**：`add()` 对 needed（app.js/index.html 提取值）做 `.strip()`，但 `have`（en.js key）正则提取不 strip。带首尾空格的片段 key（`"输入估算 "`、`" 个"`、`" · 输出 "`）因此误报缺失。
2. **HTML 实体未解码**：index.html `data-i18n-placeholder="…&amp;&amp;…"` 按原始文本采集 needed，得到 `…&amp;&amp;…`，而 en.js key 是解码后的 `…&&…`，误报（即 #47）。
3. **看不见变量 t()**：`t(statuses[run.status])`、`t(control.label || ...)` 这类变量传参，脚本正则 `t("字面量")` 抓不到——`等待澄清` 就是这么漏出来的（真缺口）。
- **处置**：不修脚本（验收要求脚本缺失=0）。整句 key 无首尾空格后 bug #1 自然消失；#47 用 en.js 补 `&amp;&amp;` 词条消化；#3 靠人工补 `等待澄清` 与包 5 处 `小秘`。
- settings-schema.json 33 个中文 key 全部在 en.js、index.html 加载链正确，均无需改动。

### 3.8 旧片段词条残留（en.js 清理风险）
重构后以下旧片段 key 不再被 app.js 引用（仅举例，非全部；完整清单见 §2.1）：
` · API 已配置`、`策略: `、`自动 → `、`手动 · `、`正在调用 `、`⚒ 文件查看 · `、` 次`、`⚒ 工具调用 · `、`参考 · `、`移除附件 `、`输入估算 `、` tokens + 输出预留 `、` / 窗口 `、`历史消息 `、` 条`、`附件 `、` 个`、`工具定义 `、` 字符 ≈ `、`删除配置 `、`策略 · `、`K 上下文`、`已切换模型：`、`已切换推理强度：`、`已切换为手动策略 · `、` 次调用`、`今日 `、` · 未计价`、`调用 `、`未计价历史 `、`余额 `、`查询失败: `、`费用 ¥`、`输入 `、` · 输出 `、` 次 · `、`（用量为估算）`、`所在周合计 `、` tokens，查看当日明细`、`（按调用时刻计价快照）`、` tokens · 输出 `、` 次 · 合计 `、`费率保存失败: `、` 上下文`、`已获取 `、` 个可用模型，在输入框中选择即可`、`最近 `、` 条消息`、` · 本地`、` · 读写`、` · 只读`、`删除来源 `、`已添加来源：`、`共 `、` 次 · 主`、` 子`、` 失败`、`打不开文件: `、`已折叠 `、`已压缩 `、` 条历史消息`、`运行中 · `。

- **建议：本次先不清理**。原因：(1) 删除有手抖误伤风险；(2) 这些残留 key 在 en.js 里无害（运行时查不到就不影响）；(3) 覆盖率脚本只检查“缺”，不检查“多”，留着不影响绿灯。
- **清理时机**：待重构上线、coverage 全绿且 UI 双语冒烟通过后，单独开一个 PR 做“死词条清理”，并在清理后**重跑 coverage + 人工双语回归**。
- 特别注意：`费用 ¥{0}`（新）与 `费用 ¥`（旧）、`策略 · {0}`（新）与 `策略 · `（旧）、`删除配置 {0}`（新）与 `删除配置 `（旧）是**不同 key**，新 key 必须新增，不能复用旧的带空格版本。

---

## 4. 需开发组特别小心的点（汇总）

1. **首尾空格铁律**：新增 53 条 key 逐条核对，绝不能带前导/尾随空格；否则 coverage 报 MISS 且英文回退中文。
2. **L1870 vs L1922**、**L1874 vs L1925**：是两对**不同 key**（一个带 tokens/计价快照后缀、一个不带），别图省事合并。
3. **L594 拍平**：把嵌套 t() 改成两条整句，别保留 `t("未返回回答") + '（' + …` 的拼接壳。
4. **L1033/L1761 前导空格外置**：` · ` 要放在 t() 外面，不要烤进 key。
5. **L1650 不 round**：保留 `(m.contextWindow || 65536) / 1024` 裸除法，与原显示一致。
6. **#47 index.html**：运行时已由 en.js L320 覆盖；新增的 `&amp;&amp;` 词条是**为让 coverage 脚本绿灯的“原始属性串”**，运行时不会被查找。若希望零冗余，可改为把 HTML 里 `data-i18n-placeholder="…&amp;&amp;…"` 的 `&amp;` 规范化为 `&&`（与同元素可见 `placeholder` 一致），即可不加这条——二选一，推荐先按“补 en.js 词条”执行（零 HTML 改动）。
7. **旧片段暂不删**，留待独立清理 PR。
8. **验收**：改完后跑 `python3 /tmp/check_i18n_coverage.py` 应输出“缺失 0”；再手动切到 English，逐处目检 §1.4/§1.10/§1.12 这些动态拼接点（策略标签、费用 tooltip、来源 chip title、日历 aria-label）。
9. **真缺口 `等待澄清`**（§1.15 #48）：en.js 必须加 `"等待澄清": "Awaiting clarification"`，否则 English 下 `awaiting_clarification` 状态回退中文。它经 `t(statuses[run.status])` 变量调用，脚本查不出，别漏。
10. **5 处裸 `小秘`**（§1.15 #49）：L3188/3701/3702/3707/4061 把 `'小秘'` 包成 `t('小秘')`；en.js 已有词条、无需新增。其中 L3707 会持久化默认名，见 §3.7 特例备注。

## 5. 不在范围（确认）
- 不改 Go 后端；不改 settings-schema.json；不改 profiles.json。
- index.html 仅 #47 可选补词条，不改结构、不加 data-i18n 属性。
- 不改变任何功能逻辑、条件判断、DOM 结构。
