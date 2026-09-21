# 模型参数 Profile 与策略路由 · 系统设计（增量）

| 项 | 值 |
| --- | --- |
| 文档类型 | 系统设计（System Design） |
| 日期 | 2026-09-21 |
| 上游依据 | `doc/prd/2026-09-21-model-profiles.md`（增量 PRD）+ `doc/PRD.md` v1.3 |
| 对应分支 | `feat/model-profiles` |
| 功能基线 | `9a47983` |
| 作者 | 编码助手 |
| 状态 | 待实施 |

## 1. 分层架构

```text
① 存储层（工程目录 = 工作区根，经 /workspace 挂载同步主机）
   profiles.json        {"version":1,"strategy":"manual","activeProfile":"default","profiles":[用户配置...]}
   routing-policy.json  策略路由规则（json 优先）；routing-policy.md 内 ```json 块兜底
   系统配置（default/precise/creative）硬编码于 Go —— 文件如何被改也无法篡改

② 后端层
   profiles.go   loadProfiles/saveProfiles（atomicJSON）、GET/PUT /api/profiles、参数校验、
                 resolveProfile(strategy, profile, prompt, mode) → (profileID, ProfileParams)
                 readRoutingPolicy()：json → md 兜底 → 空策略
   provider.go   complete(ctx, cfg, messages, params)：合并参数进请求体；reasoner 剔除不支持参数
   workflow.go   startTask 接收 strategy/profile → 解析并记录 Task.Strategy/Task.Profile → execute 透传

③ 前端层
   设置面板「模型参数」：profiles-manager 渲染器（app.js）
     ＋ 新建（复制 default 参数）／－ 删除（仅用户配置）／名称编辑（仅用户配置）／参数编辑
     系统配置卡片显示 🔒 锁定态；参数变更防抖 700ms 自动 PUT /api/profiles
   聊天栏策略按钮（composer-toolbar 左侧）：弹层单选 auto／手动 profile；选择即 PUT 保存
   发送任务 payload 增加 strategy/profile；run-meta 展示「策略：自动→precise」等
```

## 2. 后端数据模型

```go
type ProfileParams struct {
    Temperature      *float64 `json:"temperature,omitempty"`       // 0–2
    TopP             *float64 `json:"top_p,omitempty"`             // 0–1
    MaxTokens        int      `json:"max_tokens,omitempty"`        // 1–8192
    FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"` // −2–2
    PresencePenalty  *float64 `json:"presence_penalty,omitempty"`  // −2–2
    ResponseFormat   string   `json:"response_format,omitempty"`   // text | json_object
    Stop             []string `json:"stop,omitempty"`              // ≤16 项
}
type Profile struct { ID, Name string; System bool; Params ProfileParams }
```

- `systemProfiles` 为包级常量切片（3 项，含完整参数）。
- 内存态 `a.profiles`（用户配置）在 `New()` 时加载自 `profiles.json`（不存在则空 + 缺省 strategy manual/activeProfile default）；PUT 校验后 atomicJSON 写回。
- `resolveProfile`：manual → 校验 activeProfile 存在；auto → 读策略文件规则（mode/promptContains，首个命中），命中或回落 profile 必须存在，否则 `default`。
- 请求参数 `temperature/top_p/penalty` 用指针区分「未设置」与 0；`complete()` 只序列化非 nil 字段（`omitempty`）。

## 3. API 契约

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/profiles` | 返回合并列表（系统 3 个 + 用户配置）+ strategy + activeProfile |
| PUT | `/api/profiles` | `{strategy, activeProfile, profiles:[用户配置]}`；系统 id 出现即 400；参数越界 / 非法枚举 / 未知 id 均 400 |

`startTask` 请求体新增可选字段：`{"strategy":"auto"|"manual","profile":"<id>"}`（缺省 = manual + default）。

## 4. 前端结构

- `settings-schema.json` 新增分组：`{"id":"model-profiles","type":"profiles-manager","label":"模型参数"}`。
- `controlRenderers.profilesManager`：加载 GET /api/profiles → 渲染卡片列表；编辑态本地缓存 + 防抖 PUT；错误 toast 并回滚到最近成功快照。
- 策略按钮：`#strategy-button` + 弹层 `#strategy-menu`（列表：自动路由 auto + 各 profile）；选中 ✓；点击后 PUT（保留 profiles 数组原样）。
- 发送 payload：`{prompt, mode, attachments, strategy, profile}`（strategy=auto 时 profile 为空，由后端解析）。
- 任务展示：run-meta 追加 `策略：自动→precise`（auto）或 `策略：手动 · 精确`（manual）。

## 5. 文件变更清单

| 文件 | 变更 |
| --- | --- |
| `internal/server/profiles.go` | 新建：类型、系统配置常量、加载/保存、校验、路由解析、HTTP handlers |
| `internal/server/server.go` | App 增 profiles 字段与路径；注册 GET/PUT `/api/profiles`；New() 加载 profiles.json |
| `internal/server/provider.go` | `complete()` 增加 params；reasoner 参数剔除 |
| `internal/server/workflow.go` | startTask 解析 strategy/profile；Task 增 Strategy/Profile 字段；execute 透传 params |
| `internal/server/profiles_test.go` | 新建：不可变/校验/持久化/路由/透传/reasoner 测试 |
| `internal/server/server_test.go` | complete 调用点适配新签名 |
| `web/settings-schema.json` | 新增模型参数分组 |
| `web/app.js` | profilesManager 渲染器 + 策略按钮/弹层 + 发送携带 + run-meta 展示 |
| `web/index.html` | composer-toolbar 增策略按钮与弹层标记 |
| `web/style.css` | 配置卡片、锁定态、策略按钮与弹层样式（只引用既有令牌） |
| `profiles.json` | 种子文件（strategy=manual, activeProfile=default, 空用户配置），提交进仓库 |
| `routing-policy.json` | 种子文件（示例规则 + default 回落），提交进仓库 |
| `doc/PRD.md` + 增量文档 | v1.3，FR-61~64、LIM-22/23 |

## 6. 约束自查

- 系统配置不可修改由「代码定义 + PUT 拒绝系统 id」双重保证。
- `profiles.json` 写入沿用 atomicJSON（临时文件 + rename），与既有会话/设置持久化一致。
- 不放松 CSP、不新增颜色字面量（沿用 79 令牌）。
- deepseek-reasoner 兼容：透传前剔除 temperature/top_p/frequency_penalty/presence_penalty。
- 向后兼容：startTask 不带 strategy/profile 时行为与改造前完全一致。
