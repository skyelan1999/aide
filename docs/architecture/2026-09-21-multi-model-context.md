# 多模型管理与上下文统计 · 系统设计（增量）

> **历史设计（2026-09-21）**：模型选择已移入策略弹层；预算预览已增加服务端构造器，旧侧栏公式不是完整请求预算。 当前状态见 [现行 PRD](../PRD.md) 与 [架构](../architecture.md)。下文“待实施/已验证”仅代表原设计时间点，不能用于今天的发布判断。

| 项 | 值 |
| --- | --- |
| 文档类型 | 系统设计（System Design） |
| 日期 | 2026-09-21 |
| 上游依据 | `docs/prd/2026-09-21-multi-model-context.md`（增量 PRD）+ `docs/PRD.md` v1.5 |
| 对应分支 | `feat/multi-model-context` |
| 功能基线 | `03386db` |
| 作者 | 编码助手 |
| 状态 | 待实施 |

## 1. 架构总览

```text
① 后端
   Settings{BaseURL, APIKey, Model, Models []ModelRef, ActiveModel}
     ModelRef{ID, Name, ContextWindow}
   迁移：旧 settings.json（单模型）→ Models=[{ID:Model,Name:Model,ContextWindow:65536}], ActiveModel=Model
   校验（LIM-25）：≤20 个、id 1–64、名称 ≤32（缺省=id）、窗口 1024–1,048,576（缺省 65,536）、active ∈ 列表
   GET /api/config → + models + activeModel（model 字段保留 = activeModel，兼容旧前端语义）
   GET /api/models → 代理 {baseURL}/models（Authorization 转发，30s 超时，≤2 MiB）→ 解析 OpenAI {data:[{id}]} → {models:[id…]}
   startTask → task.Model = a.settings.Model（快照记录）

② 前端（参考 DSH：DeepSeekModelsEditor + token-meter）
   模型设置弹窗：baseURL + API Key 不变；模型列表（每行：名称输入 + id + 上下文窗口输入 + 「当前」单选 + 删除）
   新增行：「自动获取」→ GET /api/models → datalist 候选 → 「＋ 添加」
   侧栏「模型」选择器：当前模型按钮 + 弹出列表（单选切换 → PUT /api/settings 保留密钥）+「⚙ 管理模型…」
   侧栏「上下文」统计卡：估算已用/窗口 tokens + 进度条（DSH token-meter 启发式：4 字符/词 + 每消息 4 开销，
   只统计 60,000 字符回放预算内的最近消息；分母 = 当前模型 contextWindow）

③ 数据流
   refreshConfig → 渲染模型选择器 + 统计卡分母
   renderSession/renderAttachments → 重算统计卡
   切换模型 → PUT settings → refreshConfig → 后续 startTask 用新模型
```

## 2. API 契约

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/models` | 代理拉取上游可用模型 id 列表；上游非 200/解析失败 → 400 错误信息 |
| PUT | `/api/settings` | 增加 `models`、`activeModel` 字段；省略时保留旧值（兼容） |
| GET | `/api/config` | 增加 `models`、`activeModel`；`configured = activeModel != ""` |

## 3. 前端结构

- `index.html`：侧栏 `#model-picker`（按钮 + `#model-picker-menu` 弹层）+ `#context-card`（`#context-stat`、`#context-fill`、`#context-detail`）；设置弹窗改造 `#model-list`（`#new-model-id` + datalist `#model-datalist` + `#fetch-models` 按钮）。
- `app.js`：
  - `renderModelPicker()`：按钮显示当前模型名；弹层每模型一项（radio）+ 管理入口；点击切换 → `PUT /api/settings`（models/activeModel，无 apiKey 字段 → 后端保留密钥）→ refreshConfig。
  - `renderModelList()`（弹窗内）：渲染模型行；`fetchModels()` 拉取候选填 datalist；`addModel()`/`removeModel()`/`setActiveModel()` 操作本地 draft；保存时整体提交。
  - `estimateContext()`：`used = Σ(ceil(len/4)+4)`（从最近消息回溯，预算 60,000 字符）；`limit = activeModel.contextWindow || 65536`；渲染 `已用 X / Y` + 进度条（>90% 变色警示）。
- `style.css`：沿用 79 令牌；`.model-picker`/`.model-picker-menu`、`.context-card`/`.context-bar`、`.model-row` 等新组件样式。

## 4. 文件变更清单

| 文件 | 变更 |
| --- | --- |
| `internal/server/server.go` | Settings 扩展、迁移、校验、config/models 端点、updateSettings 改造 |
| `internal/server/workflow.go` | Task 增 `Model` 字段并在 startTask 快照 |
| `internal/server/models_test.go` | 新建：多模型校验/迁移/任务模型记录/代理端点（成功/失败/密钥转发） |
| `internal/server/web/index.html` | 侧栏模型选择器 + 上下文统计卡；设置弹窗模型列表区 |
| `internal/server/web/app.js` | 上述三个交互模块 + 估算函数 |
| `internal/server/web/style.css` | 新组件样式（令牌化） |
| `docs/PRD.md` + 增量文档 | v1.5，FR-67~70、LIM-25 |

## 5. 约束自查

- 「api 还是之前的 api」：不新增密钥/多 Base URL；`complete()` 与既有 Provider 逻辑零改动。
- 旧设置迁移不丢密钥；`clearKey` 语义不变。
- CSP 不放松、无内联脚本/样式；颜色只引用既有令牌。
- 上下文统计标注「估算」（4 字符/词启发式），不做虚假精确。
