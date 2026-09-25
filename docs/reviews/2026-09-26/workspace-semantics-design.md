# #61 工作目录语义不一致 · 只读调研与设计方案（待用户确认，未改任何代码）

分支 `feature/permission-panel`。本文件仅为调研结论与设计建议，**未改动任何 Go / 前端 / 配置**，未 commit、未 push。

---

## 一、调研发现（附证据）

### 1. 三层“工作目录”语义是混乱根源

容器内同时存在三个被叫做“工作区”的东西，模型只清楚其中一个：

| 概念 | 容器内路径 | 宿主机路径 | 谁在用 |
| --- | --- | --- | --- |
| **(A) 产品安装目录** | `/workspace` | `…/aide`（本仓库根） | Go 服务器自身：`workspace-config.json`、`profiles.json`、插件、`.cache` |
| **(B) 用户所选工程目录** | `/local/Users/skyelan/debug` | `/Users/skyelan/debug` | UI 文件列表、`a.workspace`、write_file 批准后落点 |
| **(C) 宿主 FS 直通** | `/local` | 挂载基 `…/aide/.agent-state/host-root`，再叠加 `/Users` `/Volumes` `/private` | 工作区选择器浏览、run_shell 自由读写 |

### 2. 挂载实测证据（`docker inspect aide-aide-1`）

```
…/Harness                              -> /context   (RO)
…/aide/.agent-state/host-root          -> /local      (RW, 基目录)
…/aide                                 -> /workspace  (RW)   ← 产品目录 (A)
/Users                                 -> /local/Users     (RW)
/Volumes                               -> /local/Volumes   (RW)
/private                               -> /local/private   (RW)
env AIDE_HOST_LOCAL=/
```
- 来源：`compose.yaml:34-46`（`/workspace`=${AIDE_WORKSPACE:-.}、`/local`=${AIDE_LOCAL_ROOT:-$HOME}）；`compose.macos-root.yaml:5-19` 额外把 `/Users /Volumes /private` 挂到 `/local/...`，并设 `AIDE_HOST_LOCAL=/`。
- `scripts/configure-local-root.py`：当 `AIDE_LOCAL_ROOT='/'` 且 macOS 时启用 `compose.yaml:compose.macos-root.yaml`，并明确提示“Recreate the aide container to apply the mount”——即**改挂载要重建容器**。

### 3. workspace_config 如何解析 / 生效

- 配置文件：`a.wsConfigPath = filepath.Join(a.workPath, "workspace-config.json")`（`internal/server/workspace_config.go:80`），实测落在 `/workspace/workspace-config.json`（产品目录 A 内）。
- 加载时机：启动时 `loadWorkspaceConfig()`（`server.go:677`）→ `applyWorkspaceConfig()`（`workspace_config.go:205`）。
- 用户所选路径如何传到容器：界面填宿主绝对路径 `/Users/skyelan/debug`，`resolveHostPath`（`workspace_config.go:141-192`）因 `AIDE_HOST_LOCAL=/`，把它映射成容器路径 `/local/Users/skyelan/debug`，再 `os.OpenRoot` 赋给 `a.workspace`（`workspace_config.go:225-230`）。
- 关键映射分支（`workspace_config.go:161-191`）：
  - `/workspace...` → 拼到 `a.workPath`（**产品目录 A**）；
  - `/local...` → 拼到 `a.localRoot`；
  - 相对路径 → 拼到 `a.workPath`（**产品目录 A**）；
  - 宿主绝对路径 → 必须在 `a.hostLocal(=/)` 内，映射为 `/local/<rel>`。
- 缓存默认落点：`cacheContainer = filepath.Join(a.workPath, ".cache")`（`workspace_config.go:251`）——即使用户选了别的工程目录，**默认缓存仍写在产品目录 A 的 `.cache`**。
- **改工作区路径不需要重启容器**：`updateWorkspaceConfig`（`workspace_config.go:297`）在 HTTP 请求期直接 `applyWorkspaceConfig()` 重开 root。只有改挂载（`AIDE_LOCAL_ROOT`/macos-root overlay）才需重建容器。

### 4. 工具的相对路径根与 CWD

App 字段初始化（`server.go:553-676`）：`workPath=/workspace`（产品目录 A 的字符串）；`workspace` 启动时指向 A，随后被 `applyWorkspaceConfig` 改到 B；`localRoot=/local`；`reference=/context`；`hostLocal=/`。

| 工具 | 代码位置 | 相对路径根 / CWD | 是否正确指向 B |
| --- | --- | --- | --- |
| list_files（模型） | `workflow.go:2590→2536` `listLocalDir(wsRoot)` | `wsRoot=a.wsRoots[task.WorkspaceID] ?? a.workspace`（B） | ✅ |
| read_file（模型） | `workflow.go:2615→2553` `readText(wsRoot)` | B | ✅ |
| write_file（提案） | `workflow.go:2632→recordToolProposal:2770`，批准后 `applyTask:660→writeWorkspaceText→putText(a.workspace)` | B（相对路径） | ✅ |
| run_shell（模型） | `workflow.go:2369-2379` `cmd.Dir=EvalSymlinks(a.workspace.Name())` | CWD=B，**但命令体无路径边界** | ⚠️ CWD 对，可被绝对路径逃逸 |
| search_text | `workflow.go:1903-1905` 硬编码 `cd /workspace && grep` | **产品目录 A** | ❌ |
| office 解析 | `workflow.go:1881-1882` `cmd.Dir=/workspace` | 产品目录 A | ❌ |
| 失败反馈话术 | `workflow.go:2459` 直接教模型 `saveas("/workspace/户型.dxf")` | 把模型推向 A | ❌ |
| 手动命令（UI 按钮） | `command.go:106-123` | CWD 锁在 `a.workspace` 内且有 `dir` 越界校验 | ✅（有边界） |

- `safePath`（`files.go:27-37`）：拒绝绝对路径（`HasPrefix("/")`）、`..`、`.git/.data/.env/docker-images`。它作用于“传入某个 root 的相对路径”，本身**不约束 /local**；模型工具也没有 `root` 参数（`executeToolCall` 只解析 `path`/`source`），所以模型的结构化文件工具**只能落在 B，够不到 /local**。
- Web 编辑器文件 API（`files.go:226/353/393`）接受 `?root=workspace|context|local`（`root()` 在 `files.go:214`）——这是**已登录 UI**，不是模型工具。

### 5. 子 agent 继承

- `spawnSubagent`（`workflow.go:1355-1362`）：子任务 **继承** `WorkspaceID / WorkspaceRev / WorkspaceMode / WorkspaceRemotePath`。
- 执行时 `wsRoot := a.wsRoots[task.WorkspaceID]`（`workflow.go:2504`）能解析回 B。
- 因此子 agent 的 list/read/write 结构化工具**正确落在 B**。
- **但**：子 agent 拿到的系统提示与父会话相同（见下），且 `run_shell` 的 CWD 取的是**全局** `a.workspace.Name()`（`workflow.go:2369`），不是绑定到 task 的 wsRoot——运行中切工作区会有错位风险。

### 6. 系统提示把模型指向了哪里

- `context.go:144`：系统提示 = `baseSystemPrompt() + "\n当前工作目录: " + a.workspaceDisplay + "\n可用工具: " + toolListHint()`。
- `workspaceDisplay` 在 `applyWorkspaceConfig` 被设为**宿主路径** `/Users/skyelan/debug`（`resolveHostPath` 第三返回值，`workspace_config.go:191`）。
- **问题**：这个宿主路径在容器内根本不存在（容器里没有 `/Users`）。模型被告知“当前目录是 /Users/skyelan/debug”，但它在容器里能用的“真实容器绝对路径”只有 `/workspace`（产品目录 A）——于是模型一旦想用绝对路径，就回落到 `/workspace`。
- 状态 API 还给前端硬编码 `"workspace": "/workspace"`（`server.go:1075`）。

### 7. 现状问题确认（实锤）

- **证据 1**：仓库根（=容器 `/workspace`=产品目录 A）下有一个未跟踪文件 `subagent_test.txt`，内容 `subagent ok`，时间 `Sep 26 01:33`。
- **证据 2**：持久化会话显示——父会话（`session-f5d2…`）下发任务“**在 /workspace 目录**创建 subagent_test.txt”，子会话（`session-92d2…`，`workspaceId=local|/Users/skyelan/debug|`）照做，并自述：
  > “文件 `/workspace/subagent_test.txt`… `/workspace` 是沙箱容器内路径，与我默认操作的工程目录（`/Users/skyelan/debug`，即文件列表那侧）不是同一位置…**不会出现在工作目录文件列表里，Finder 也看不到**。”
- 结论：**模型/子 agent 一旦用绝对路径（或被话术诱导）就写进产品目录 A，UI 文件列表（根=B）和本机 Finder 都看不到**。结构化 write_file 走批准流时落点是 B；只有 run_shell 自由命令会逃逸到 A。
- 子 agent 产出落点：结构化工具=B；run_shell 自由命令=取决于模型用的绝对路径，实测落到 A。

### 8. /local 权限与安全边界（实测，容器 uid=1000 aide）

- run_shell 权限策略 `shellBlocked`（`workflow.go:1234-1281`）只拦**破坏性命令模式**（`rm -rf`、`sudo`、`mkfs`、`curl|bash` 等），**不按路径限制**；`readOnlyAllowed`（`workflow.go:2308`）按命令白名单前缀放行 `cat <任意绝对路径>`，同样不查路径。
- 实测容器可**读**：`/local/Users/skyelan/.docker/config.json`(353B)、`.gitconfig`、`.kube/config`、`.zsh_history`(1848B)、`.ssh/known_hosts`；`.ssh/id_rsa` 因 600 属主不同被拒。
- 实测容器可**写**到 `/local/Users/skyelan/`（宿主主目录根，**在所选工程目录 B 之外**）。
- `/local/private/etc` 可达。
- 即：在 `workspace-write`（默认）或 `danger-full-access` 下，模型的 run_shell 能越出所选工程目录 B，读写宿主主目录其它位置、读敏感 dotfile。沙箱三档（`read-only/workspace-write/danger-full-access`，`workflow.go:2336-2360`）都不做路径隔离。

---

## 二、设计方案（目标：用户所选工程目录 B 成为模型默认工作目录；子 agent 继承；/workspace 回归纯产品目录语义、对模型不可写）

### 1. 路径解析层改造：统一 `WorkspaceRoot` 抽象

新增单一结构，替代当前散落的 `workPath / workspace / localRoot / hostLocal / workspaceDisplay` 五件套对模型的暴露：

```
type AgentRoot struct {
    DisplayHost string   // 给人看：/Users/skyelan/debug（仅 UI/提示标签）
    ContainerAbs string  // 给模型/run_shell：EvalSymlinks(a.workspace.Name())，如 /local/Users/skyelan/debug
    GoRoot *os.Root      // 结构化文件工具用的 chroot 根（=现 a.workspace）
    ID string            // = wsID()，提案/子agent绑定
}
```
- 任务创建时快照进 `Task.AgentRoot`（替代现在只存 `WorkspaceID` 字符串再回查 `wsRoots`）。
- 所有“模型可见的 CWD”一律取 `ContainerAbs`，禁止再把宿主路径当容器 CWD 报给模型。
- `workspaceDisplay` 改为人读标签，不再与 CWD 混用。

### 2. 工具 CWD / 根目录切换

- **系统提示**（`context.go:144`）改为：
  `当前工作目录(容器内绝对路径)=<ContainerAbs>。所有文件操作使用相对路径；禁止使用 /workspace 绝对路径，/workspace 是产品安装目录，不得写入。`
- **run_shell**（`workflow.go:execShellCommand`）：
  1. `cmd.Dir` 改为取 **task 快照的 `ContainerAbs`**，而不是全局 `a.workspace.Name()`（消除运行中切工作区的错位）。
  2. 命令体前加 `cd '<ContainerAbs>' 2>/dev/null || cd <ContainerAbs> &&`。
- **search_text**（`workflow.go:1903`）、**office 解析**（`workflow.go:1882`）：`cd /workspace` / `cmd.Dir=/workspace` 全部改为 `ContainerAbs`。
- **失败话术**（`workflow.go:2459`）：删掉“saveas(\"/workspace/…\")”，改为相对路径示例。
- **缓存默认**（`workspace_config.go:251`）：用户工程目录非默认时，`.cache` 放到 `ContainerAbs/.cache/`（或按 wsID 隔离），不再默认落产品目录 A。

### 3. 子 agent 继承机制

- 现状已继承 `WorkspaceID`（`workflow.go:1360`），结构化工具正确。补两点：
  1. `spawnSubagent` 把父任务的 `AgentRoot`（含 `ContainerAbs`）整体快照复制给子任务，而不是只传 ID 再回查全局 map。
  2. 子 agent 系统提示复用同一 `ContainerAbs`，与父会话一致。
- 验收：子 agent 用相对路径写文件 → 落在 B；用 `pwd` → 打印 `ContainerAbs`。

### 4. /local 安全边界收紧

run_shell 增加**路径白名单**（在 `shellBlocked` 之后、执行前做一次粗筛，深度隔离留给第 8 点命名空间方案）：

- 允许读/写前缀：`ContainerAbs`（B）、`/tmp`、`/home/aide/.cache`（构建缓存）。
- 明确**拒绝**（命令中出现即拦截，报“超出工程目录”）：
  - 写/改 `/workspace`（产品目录 A）、`/data`（配置/vault/会话）；
  - 读 `/local/Users/<home>/.ssh`、`.docker`、`.kube`、`.aws`、`.gnupg`、`.zsh_history`、`.zsh_sessions`、`Library/Keychains` 等敏感 dotfile；
  - 写 B 之外的 `/local/...`。
- 说明：字符串/命令串过滤天生可绕过（`cd` 后用相对、`$(cat)` 等），所以它是“防呆+提示”，**不是强隔离**；强隔离靠第 8 点。

### 5. 向后兼容策略

- `workspace-config.json` 格式不变；空 `workspace.path` 仍回退产品目录 A（老用户无感）。
- 模型历史里已出现的 `/workspace/xxx` 旧引用：不改历史，仅在新提示里约束；旧提案仍按 R02 身份校验（`workflow.go:623-631`）原样生效。
- 已误写进产品目录 A 的文件（如 `subagent_test.txt`）**不自动迁移/删除**，由用户决定；文档说明其位置。
- 所有行为改动加 settings 开关（如 `agentCWDMode`，默认新行为），出问题可一键回退旧提示。

### 6. 迁移步骤（实施顺序，待批准后执行）

1. 抽 `AgentRoot` 抽象并在任务创建/子 agent 处快照（不改行为，先铺结构）。
2. 改系统提示 + 修三处硬编码 `/workspace`（search/office/失败话术）+ 缓存落点。
3. run_shell 改 task 绑定 CWD。
4. 加 run_shell 路径白名单。
5. 补回归测试（见下）。
6. 灰度：先对新会话生效，观察一轮真实任务。

### 7. 安全约束（必须限制访问的目录）

- 产品目录 A `/workspace`：对模型只读/禁写（它是 aide 自身代码与配置）。
- `/data`：vault、会话、settings——模型完全不可达。
- 宿主敏感 dotfile（`.ssh/.docker/.kube/.aws/.gnupg/.zsh_history/Library/Keychains` 等）：模型 run_shell 不可读。
- 写：仅允许 B（所选工程目录）+ `/tmp`。

### 8. 强隔离（可选，Phase 2）：挂载命名空间 jail

字符串白名单可绕过。终极方案：run_shell 用 `unshare --mount --pid --fork` 起新命名空间，把用户工程目录 B **bind-mount 到命名空间内的 `/workspace`**，并隐藏真实产品目录 A。这样模型心智里的 `/workspace` 就等于用户工程 B，相对/绝对路径都正确，且碰不到宿主其它位置。
- 风险：Docker Desktop for macOS 下 `unshare`/mount 命名空间可能需要额外特权（`--privileged` 或 seccomp 调整），需在目标环境验证；不可用则退回第 4 点白名单 + 明确风险提示。

---

## 三、建议实施优先级

| 优先级 | 项 | 价值 | 风险 |
| --- | --- | --- | --- |
| P0 | 改系统提示（报真实 ContainerAbs + 禁止写 /workspace） | 立刻消除模型误写 A 的主因 | 极低 |
| P0 | 修三处硬编码 `/workspace`（search_text / office / 失败话术 2459） | 停止主动把模型推向 A | 低 |
| P1 | run_shell CWD 绑定 task 的 AgentRoot | 子 agent/运行中切换不再错位 | 低 |
| P1 | 缓存默认落点改到工程目录 B | 产物/缓存不再混进产品仓库 | 低 |
| P2 | run_shell 路径白名单（防呆） | 初步收敛 /local 越权 | 中（可绕过，需提示） |
| P2 | AgentRoot 抽象落库 + 子 agent 快照 | 可维护性、R02 身份更稳 | 中 |
| P3 | 挂载命名空间 jail | 真正强隔离 | 高（需特权，待验证） |

## 四、NOT_RUN 与未验证项

- 未改任何代码、未跑测试、未重启容器（只读调研）。
- 未验证挂载命名空间 jail 在本机 Docker Desktop 是否可用（需单独 spike）。
- 未枚举全部敏感 dotfile 清单；第 7 点为初版，实施时应补全。
- 前端文件列表根、设置面板文案是否也需从 `/workspace` 改为工程目录标签，本次仅后端只读，未动前端。
