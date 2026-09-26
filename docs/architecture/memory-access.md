# 记忆单向可见与流式输出桥接

> 版本：0.1.11.0-RC1 · 分支：feature/permission-panel · 关联：#29 加密 / #30 跨会话工具 / #31 目录分层 / #33 身份核心
> 代码：`internal/server/memory_access.go`、`internal/server/stream_broker.go`、`voice_agent.go`、`workflow.go`

aide（AI 工作台）与小秘（语音秘书）共用一个进程，但两者的记忆与上下文必须按**单向可见**原则划分。本文固化三条核心规则、显式权限层与流式桥接架构。

## 一、三条单向规则（用户明确，核心验收）

1. **小秘的记忆/历史不与 aide 共享**：aide 及其工具、沙箱命令一律不得读取小秘私有记忆与对话历史（隐私隔离）。
2. **aide 的记忆可以被小秘了解（只读）**：小秘能读到 aide 长期记忆，但**不能写、不能污染**——编译期保证小秘侧无任何写 aide 记忆的方法。
3. **小秘能拿到 aide 主会话的流式输出**：含**进行中**的内容，以及**被打断后已产出的那部分**。

## 二、权限矩阵

| 数据区 | 路径（相对 `/data`） | aide | 小秘 |
| --- | --- | --- | --- |
| aide 长期记忆 | `memory/core/`（`memory.md`） | 读 √ 写 √ | 读 √（只读）写 × |
| 小秘长期记忆 | `assistant/voice-memory.json` | 读 × 写 × | 读 √ 写 √ |
| 小秘对话历史信封 | `assistant/voice-history.json` | 读 × 写 × | 读 √ 写 √ |
| aide 主会话流式输出 | 内存（`StreamBroker`） | 产生（SSE 发布） | 读 √（按会话拉取） |
| 已落库会话消息 | `sessions/{active,archived,assistant}/` | 本会话 | 跨会话读（#30 `get_session`） |

## 三、显式权限层（非文件名巧合）

判断依据是**路径前缀与目录归属**，不是文件名。即便文件改名/移动，只要落在正确的目录归属里，策略依然成立。权威函数在 `memory_access.go`：

- `isAssistantMemoryPath(data, path)`：目标是否在 `<data>/assistant/` 整棵子树。
- `isAideMemoryPath(data, path)`：目标是否在 `<data>/memory/core/` 整棵子树。
- `canAccessMemory(data, caller, path, op)`：唯一权威判定，`caller ∈ {aide, assistant}`、`op ∈ {read, write}`。
- `withinDataBase(base, target)`：用 `filepath.Rel` 做归一化，显式拦截 `..` 逃逸（跨盘符/异常挂载给出 `..` 前缀的情况也拒）。

纵深防御点：

- **记忆工具入口**：`read_memory` / `write_memory` 不接收路径参数（路径固定指向 `memory/core/memory.md`），入口仍调 `canAccessMemory(aide, …)` 做断言式越权检查。
- **工作区文件工具**：`read_file/write_file/list_files` 早已被 `safePath()` 限制在工作区内（拒绝绝对路径与 `..`），物理上碰不到 `/data`。
- **run_shell 沙箱**：run_shell 是同容器 bash（`danger-full-access` 模式下本不拦截），新增 `shellTouchesAssistantZone()`：命中 `/data/assistant` 路径前缀或 `voice-memory.json` / `voice-history.json` 文件名即拦截，且**不受沙箱模式影响**。

> aide 记忆文件从旧版 `.cache/memory.md` 归位到 `memory/core/memory.md`（#31 布局）；`migrateLegacyAideMemory()` 在首次读写时一次性复制旧记忆，不丢数据。

## 四、小秘只读 aide 记忆

`VoiceAgent.readAideMemory()`（`voice_agent.go`）读取 `memory/core/memory.md` 返回摘要（截断 4000 字）。本类型**刻意不提供任何 `writeAideMemory` 方法**——小秘能看不能写，写操作在编译期就不存在。

注入点（与小秘私有记忆分两个上下文块，明确区分归属）：

- **analyze（语音听写/意图判断）**：`【aide 的长期记忆（只读参考，绝不修改；不是你自己的记忆）】` 独立块，与 `【你自己的长期记忆（小秘私有）】` 并列。
- **narrate（边演示边讲解）**：附加同一只读块。
- **小秘对话（`baseSystemPrompt`，persona.go）**：小秘人格 system prompt 末尾附加同一只读块。

## 五、流式桥接架构

```mermaid
flowchart LR
    subgraph aide主会话
      L[用户发消息] --> E[execute / toolLoop]
      E -->|onDelta 正文增量| SSE[SSE publishStream]
      E -->|Publish sid,chunk,\"\"| B[(StreamBroker 内存)]
      E -->|结束: done/interrupted/failed| BF[finishLiveRun]
      BF --> B
    end
    subgraph 小秘
      V[VoiceAgent] -->|getSessionLiveOutput sid| B
      V --> R[口头总结/讲解]
    end
    B -. 重启清空 .-> G[已落库消息走 #30 get_session]
```

`StreamBroker`（`stream_broker.go`）：

- 按 **sessionID** 隔离；`Publish(sessionID, chunk, status)`：`status=running` 开始/重置一轮；`""` 追加增量；`done/interrupted/failed` 标终态。
- 每个会话保留最近一次运行的完整输出缓冲，上限 **100KB**（超出截断）。
- `GetSessionLiveOutput(sessionID) → (text, status, found)`：被打断（`interrupted`）/失败（`failed`）**仍保留已产出部分**。
- task→session 映射：execute 开始 `beginLiveRun` 注册 `taskID→sessionID`，`onDelta` 据此把正文增量桥到会话维度；结束 `finishLiveRun` 清理映射并标终态（`cancelled→interrupted`）。
- 纯内存态：重启清空是预期——进行中输出本就不跨重启；已完成内容早已落库，小秘用 #30 `get_session` 读取。

**与 #30 `get_session` 互补**：`get_session` 读已落库消息；本 broker 覆盖进行中/未落库内容及打断残片。小秘侧通过 `VoiceAgent.getSessionLiveOutput(sessionID)` 拉取。

## 六、隐私设计

- 小秘记忆与历史加密保存（#29），aide 进程内任何工具/命令都无法触达。
- 权限在代码层显式判定，不依赖前端/提示词约束；提示词里的“只读”只是给模型的行为指引，真正的强制在 `canAccessMemory` 与 `shellTouchesAssistantZone`。
