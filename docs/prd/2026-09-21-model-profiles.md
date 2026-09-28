# 模型参数 Profile 与策略路由 · 增量 PRD

> **历史设计（2026-09-21）**：Profile 与规则仍存在；工具循环和模型选择入口以后续实现为准。 当前状态见 [现行 PRD](../PRD.md) 与 [架构](../architecture.md)。下文“待实施/已验证”仅代表原设计时间点，不能用于今天的发布判断。

> **现状核对（2026-09-25，版本 0.1.10.2 RC1）**：FR-61~FR-64 全部落地（`internal/server/profiles.go`）。偏差：① 策略弹层现已扩为三列——除本文所述「策略（auto／手动 profile）」外，另增「模型」列（多模型列表/`activeModel`，见多模型文档）与「推理强度」列（`reasoningEffort` 五档，走 `PUT /api/settings`）；② 模型列表管理（ModelRef id/name/contextWindow，≤20 个，窗口 1024–2097152、缺省 65536，预设 32K/64K/128K/200K/256K/1M）在模型设置弹窗内，与采样参数 Profile 是两套独立配置，不要混淆。

| 项 | 值 |
| --- | --- |
| 文档类型 | 增量 PRD（Incremental PRD） |
| 日期 | 2026-09-21 |
| 上游依据 | `docs/PRD.md` v1.3 |
| 对应分支 | `feat/model-profiles` |
| 功能基线 | `9a47983`（设置中心合并后 main HEAD） |
| 作者 | 编码助手 |
| 状态 | 已实现·已验证（采样参数部分） |

## 1. 需求来源（用户原话）

> 把这些参数（DeepSeek API 的 temperature、top_p、max_tokens 等）添加到方块 a 设置里面，每个参数都可以配置；可以加载不同的配置 profile，就是 + 和 -；每个配置名可以自定义；默认三个系统配置；用户配置可以自行添加，系统配置不可修改；只能去改对应的 json；json 一定要放在工程目录，要做到可以保存的；聊天栏左边放一个小按钮用来选择策略，可选 auto 或手动（Manual）的某一个 profile；auto 的路由规则根据一个策略的 md 或者 json 来定；当前默认直接对接 default 参数。

## 2. 需求分解

| 编号 | 需求 | 验收标准 | 状态 |
| --- | --- | --- | --- |
| FR-61 | 模型参数配置 | 设置面板「模型参数」分组可配置 temperature / top_p / max_tokens / frequency_penalty / presence_penalty / response_format / stop；参数随模型调用生效 | 已实现·已验证 |
| FR-62 | 配置 Profile 管理 | 内置 3 个系统配置 default／precise／creative，不可修改、不可删除；用户配置 ＋ 添加、－ 删除、自定义命名；全部配置持久化于工程目录 `profiles.json`（运行时保存写回主机，原子写入）；参数越界被拒绝 | 已实现·已验证 |
| FR-63 | 聊天栏策略按钮 | 聊天输入栏左侧小按钮弹出策略选择：`auto` 或手动指定某一 profile；默认手动选择 `default`；任务记录展示本次实际使用的配置 | 已实现·已验证 |
| FR-64 | auto 路由策略 | auto 模式下按工程目录策略文件路由：`routing-policy.json` 优先，`routing-policy.md` 内 ```json 代码块兜底；规则支持按 `mode` 与 `promptContains` 匹配；无命中／文件缺失回落 `default` | 已实现·已验证 |

## 3. 关键决策

| 决策 | 结论 | 理由 |
| --- | --- | --- |
| D1 系统配置定义位置 | 3 个系统配置**硬编码在 Go 代码**中，`profiles.json` 只存用户配置 + strategy + activeProfile | 用户可通过文件编辑器改工作区文件，只有代码定义才能保证系统配置不可修改 |
| D2 持久化位置 | 工程目录（工作区根）`profiles.json`，通过 `GET/PUT /api/profiles` 原子读写 | 用户明确要求 JSON 放工程目录且可保存；专用 API 保证校验与原子性 |
| D3 策略存储 | `strategy ∈ {manual, auto}` 与 `activeProfile` 存于 `profiles.json`；任务提交时随 payload 上送，后端落库到任务记录 | 策略是用户持久偏好；任务记录展示实际生效配置 |
| D4 auto 路由 | 后端在 `startTask` 解析策略文件并决定生效 profile；客户端不参与规则判断 | 规则文件在容器工作区内，后端读取最可靠；也支持 md 兜底 |
| D5 参数透传 | `complete()` 增加 params 参数，合并进请求体；模型名含 `reasoner` 时剔除 temperature/top_p/两个 penalty | 兼容 deepseek-reasoner 不支持的参数，避免调用 400 |
| D6 默认行为 | 策略缺省 = manual + `default`；`default` 系统配置参数 = 改造前行为（temperature 1 / top_p 1 / max_tokens 4096 / penalty 0 / text） | 用户要求"当前默认直接对接 default 参数"；默认行为与现状等价 |

## 4. 参数契约（LIM-22）

| 参数 | 类型 | 范围 / 取值 | 默认（default profile） |
| --- | --- | --- | --- |
| temperature | number | 0 – 2 | 1 |
| top_p | number | 0 – 1 | 1 |
| max_tokens | integer | 1 – 8192 | 4096 |
| frequency_penalty | number | −2 – 2 | 0 |
| presence_penalty | number | −2 – 2 | 0 |
| response_format | string | `text` / `json_object` | text |
| stop | string[] | ≤16 项，每项 ≤64 字符 | 空 |

系统配置：
- `default` 默认：上表默认值（与改造前行为一致）
- `precise` 精确：temperature 0.2，top_p 0.9，其余同 default
- `creative` 创意：temperature 1.5，top_p 0.95，其余同 default

## 5. 策略文件契约（FR-64 / LIM-23）

```json
{
  "version": 1,
  "rules": [
    { "when": { "mode": "workflow" }, "use": "precise" },
    { "when": { "promptContains": ["审查", "review", "缺陷", "bug"] }, "use": "precise" },
    { "when": { "promptContains": ["创意", "文案", "命名"] }, "use": "creative" }
  ],
  "default": "default"
}
```

- 匹配顺序：规则从上到下，首个命中生效；`mode` 精确匹配，`promptContains` 大小写不敏感子串（任一命中）。
- `use` / `default` 必须是已知 profile id；无效则回落 `default`。
- 文件优先级：`routing-policy.json` → `routing-policy.md`（提取第一个 ```json 代码块）→ 无策略（全部走 `default`）。

## 6. 非目标（本期不做）

- 不做 SSE 流式输出（FR-24）与 tools 参数接入（FR-33）。
- 不做 profile 的导入/导出/云端同步。
- 不做按会话/按步骤的自动 profile 切换（只有 auto 路由与手动整体策略）。

## 7. 验证方式

| 项 | 方法 |
| --- | --- |
| 后端门禁 | `bash scripts/aide.sh test`（go test -race + go vet） |
| 新增测试 | 系统配置不可变；参数越界拒绝；用户配置持久化（重建 App 后仍在）；auto 路由（mode/关键词/回落）；参数透传到 mock provider 请求体；reasoner 剔除不支持参数 |
| 浏览器实测 | 设置面板增删改配置并保存 → 主机侧 `profiles.json` 同步；聊天栏策略按钮切换与发送；任务记录显示配置；auto 模式按策略文件路由 |
