# 按需执行环境验收（2026-11-01）

已在功能分支实现并验证。当前运行的 Aide 服务未重启，未部署、未提交或推送本轮改动。

## 实现结果

- X 包含主环境。主调度可按需提交、收集、取消任务及声明依赖；队列只在依赖成功并取得槽位后建立临时运行态，10X 最多九个工作者。
- 临时 App 具有私有源码目录、进程、数据、插件副本和 daemon 实例。写入只落到临时副本；主项目保持普通审批、哈希校验、工具策略、钩子与审计。
- 工作区/插件私有资源声明完整时允许各环境插件并行；未知声明保守协调。浏览器、电脑、设备使用主协调器原工具和审批。
- 任务交换区带来源、版本和未验证标记；已完成环境结果也发布到交换区。取消/结束释放运行态和交换区，保留结果、候选文件与工具证据。恢复时保留结果，不自动重放中断任务。
- 普通 1X 不额外披露调度工具，不增加该部分 schema 开销。上下文预览和执行使用同一份多环境声明。切换项目不会把旧任务重定向到新项目；无法在捕获的旧根上安全派工时继续单环境执行。
- 保留既有上下文恢复、并行读取、严格文件草稿、版本化 apply 和旧并行检查点兼容路径。

## Luna 实测

通过实际 Aide HTTP 会话/运行接口调用 `gpt-6-luna`，low 推理强度，precise profile。仅从现有凭据信封读取所选模型配置，真实项目、会话、引用和文件未进入测试；所有源文件、插件和应用操作都在一次性项目中。

同一任务修复 sum.js、slug.js、clamp.js。工作者可自行分工；要求至少两个环境以验收多环境链路，不固定每个环境的职责，不要求填满九个槽位。私有插件真实加载并执行独立断言；主文件在审批前逐字节保持原样，随后经真实版本化 apply，再用主插件独立验证全部三个模块。

| 容量 | 模型与调度阶段 | 上游任务 Token | 实际临时环境 | 峰值临时环境 | 应用后模块断言 |
| --- | ---: | ---: | ---: | ---: | --- |
| 1X | 24.664 s | 27,239 | 0 | 0 | 3/3 |
| 10X | 26.521 s | 64,802 | 2 | 2 | 3/3 |

最终 10X 自行创建两个环境完成三个模块；各环境均有真实插件验证、收集、提案汇入和释放回执。首次成功样本创建三个环境：1X 16.143 s / 24,829 Token，10X 25.564 s / 89,091 Token；同样通过最终断言。

短任务的调度、复制与汇总成本超过并行收益，本验收没有证明速度优势，也没有达到或验证“十分钟复杂工程压到一分钟”的目标。当前结果证明的是实际运行态、工具/插件、交换、待审批合并与释放链路。不得将它描述为已获得十倍速度。

原始合成证据：`.agent-state/environment-luna-final-20261101.json`、`.agent-state/environment-luna-schema-fixed-20261101.json`。结果含候选源码、真实工具回执、每个环境状态与上游任务用量，不含 API 密钥。一次性项目本身在测试后删除；这些 JSON 是保留的验收证据。

## 自动化验证

- 兼容回归（race）通过：`go test -race -mod=vendor ./internal/server -run 'Test(ExecutionEnvironment|ConversationParallel|ParallelEdit|ParallelRead|WorkflowParallel|ContextRecovery|ActiveContext|PluginResource|PluginPlan|Daemon)' -count=1 -timeout=5m`，70.392 s。
- 最终环境/上下文预览/任务根快照增量回归（race）通过：`go test -race -mod=vendor ./internal/server -run 'Test(ExecutionEnvironment|ContextPreview|AvatarContextPreview|TaskAgentRootSnapshot)' -count=1 -timeout=2m`，13.171 s。包括私有插件拒绝写入另一项目、独立 daemon PID、依赖失败、队列取消、审批串行且保留原 CWD、取消清除询问、旧结果恢复不重放、1X 无新增 schema、哈希/提案/工作区冲突。
- Luna 实测：`TestExecutionEnvironmentsLiveLuna`，52.791 s，通过 1X 与 10X 的正常 API、候选应用及独立断言。
- `node scripts/parallel-chat.test.cjs`、`node scripts/context-meter.test.cjs`、`node scripts/auto-model-router-ui.test.cjs`、前端语法和 `git diff --check` 通过。
- Go 使用已有 golang:1.26-bookworm，本地已有运行镜像中的 Node 提供插件宿主；未拉取新基础镜像或重启生产服务。

## 界面验收清单与证据

使用仓库 Playwright 技能，在独立 Chromium 组件页加载实际脚本、主题和 conversation.css，并呈现 Luna 的真实结果 JSON；这是组件验收，未把当前 Safari 生产页称为已升级。

- 桌面 1280×900 与窄屏 390×844：容量、实际活跃、完成数、释放及提案回执可见，窄屏无横向溢出。
- 展开/折叠、10X 选择生效；1X 不出现环境进度板。
- 探索场景：执行中与失败同屏、版本冲突错误；含 HTML 的工作说明保持文本，未注入图片/事件。
- 桌面与窄屏截图：`.agent-state/environment-desktop-20261101.png`、`.agent-state/environment-mobile-20261101.png`。验收浏览器已关闭。

## 实际边界

这些是目录和进程隔离的运行态，不是整机 VM；使用 Linux Landlock，能力不可用时私有进程拒绝执行，不静默降级。网络未被宣称隔离。SSH 使用原主执行链，多环境文件副本不冒充远程隔离。

源码快照跳过链接、特殊文件、Git 内部信息和已说明的运行/依赖缓存目录；需要的依赖仍按普通流程准备。二进制产物和删除保留为证据，由主执行器明确处理；不自动覆盖或拼接。物理浏览器/硬件和所有第三方插件并未在本次合成验收中逐一接入测试。
