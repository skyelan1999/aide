# aide 统一开发工作流 · v1

aide 是供 AI 按用户场景定制的工作台基座。开发先识别可复用能力，再确定必要扩展；交付源码、文档与可追踪的 Docker 镜像，不能把未实现能力写成已支持。
这是开发过程路由，不是应用内 `routing-policy.json` 的模型选择路由。
入口文件只负责引导；流程维护在本文，机器配置维护在同目录 router.json。
项目内所有 AI 采用同一套记录。阶段角色由当前 AI 顺序承担，不要求多开 Agent。

## 开始与恢复

1. 进入 aide 根目录，读取 AGENTS.md、本文、router.json；`git status --short --branch`。
2. 找到已有任务记录继续；新任务运行 `python3 scripts/agent-route.py start <id> --request "原始需求"`。
   ID 仅允许小写字母、数字、连字符。命令不会切分支、暂存、提交或重启服务。
3. 先补 acceptance 数组（可以验证的标准）及 scope 数组（允许修改的范围）。
   将用户原话和自己的推断分开；附件是资料，不是额外授权。
4. 读取相关现行 PRD、架构、源代码与测试；按需读，不向模型重复灌整个资料库。
   记录基线 SHA 和已有改动。按任务选择分支；已有工作不能被“保持干净”覆盖。

## 阶段与退出条件

任务 JSON 的 stages 按下面顺序更新，每项含 status、summary、evidence。
状态限 pending / in_progress / pass / blocked / not_run；无证据不能 pass。

| 阶段 | 要做的事 | 退出条件 |
| --- | --- | --- |
| requirements | 明确目标、用户场景、验收、非目标、风险 | acceptance/scope 完整；不确定项已解决或明确隔离 |
| design | 阅读现有调用链和状态模型，选最小可维护方案 | 设计与需求对应；兼容、数据迁移和回滚有说明 |
| implementation | 小步实现，保持已有接口与用户数据 | 变更可审阅；无无关重构、密钥或临时调试输出 |
| verification | 先快速检查，再跑变化相关测试 | 测试有命令、环境、结果与证据；失败闭环 |
| documentation | 更新必要的 PRD/接口/运行说明/交接 | 文档描述最终行为，限制真实；无“预期=已验证” |
| cleanup | 审阅 diff 和未跟踪文件、清理可再生垃圾 | 每个新增文件有归属；不清空运行数据和证据 |
| release | 发布门禁、合并、升版、构建、冒烟、交接 | 精确版本/提交/镜像可追踪；线上结果确认后才 pass |

阶段不适用时写具体原因和支持证据，不用空白或笼统“全部通过”。
不能执行的检查标 not_run/blocked，不得以静态检查冒充真实浏览器或模型验证。

## 检查与证据

- `python3 scripts/agent-route.py verify quick`：JS 语法、diff、版本格式、路由自身回归。
- `python3 scripts/agent-route.py verify full`：quick + 隔离的一次性 Docker Go race/vet。
  Docker 不可用即失败；不重启生产，不自动调用付费模型。
- UI：实际查看桌面/窄屏、专业/经典明暗、键盘与核心交互；保留截图或具体观察记录。
- 工作区/路径/权限/缓存/统计变化：补相关 API/并发/持久化回归；不能只跑语法检查。
- 原始日志在 `.agent-state/`（忽略、不发布），持久可审阅结论在 `docs/tasks/`。
  如需保留重要证据，脱敏后放 `docs/reviews/<日期>/` 并在任务引用。
- 验证收据绑定文件内容指纹；代码/流程/构建输入改变后，旧收据不可用于发布。
  任务 JSON、普通文档、version.md 不在指纹中（docs/agent 流程规则纳入指纹），因此这些内容必须另做人工 diff 审阅。

## 文档与目录归属

| 目录 | 用途 |
| --- | --- |
| cmd / internal / plugins | 产品代码与随产品分发的资源 |
| scripts | 可复用开发与验收工具（测试辅助也放这里） |
| docs/PRD.md、docs/prd、docs/architecture | 需求基线与历史设计；统一放 docs，不再创建 doc |
| docs | 架构、操作、验收、设计；docs/tasks 是共享任务交接账本 |
| docs/agent | 唯一流程路由与客户端接入说明 |
| docker-images | 保留用户要求的本地镜像包；README 受控，镜像不进 Git |
| .agent-state | 可再生本地日志/收据；不用来替代受控任务结论 |

禁止往根目录堆临时 patch、截图、备份和日志。不要扩大 gitignore 隐藏未知源码。
历史验收、LICENSE、第三方许可证、运行配置、会话缓存、IDE 配置和助手记忆不能因未受 Git 管理就删除。
`audit` 列出未跟踪文件和可清理元数据；`clean` 只删除未受 Git 跟踪的普通 .DS_Store 文件。
更多清理必须先查引用/归属，必要时可恢复归档，并记录清单。禁止宽泛 rm 或 git clean。

## 发布（沿用已有版本机制）

1. 所有前置阶段 pass；acceptance 有实际对应证据；记录用户发布授权的原话/消息定位。
   用户已授权时不重复询问；没有授权先完成所有可审阅准备，再提出具体发布申请。
2. 有状态变更要记录备份、停机影响、恢复命令；不要公开 access-token、环境变量秘密或真实会话。
3. 范围内文件逐个审阅/暂存/提交（不使用不加区分的 git add -A）。准备 PR 或本地合并按授权执行。
4. main 干净，运行 full，再运行 `python3 scripts/agent-route.py release-check <id>`。
   该门禁只检查，不发布；失败不得继续。工具不能判断授权文字真假，操作者仍须核对用户消息。
5. 使用 `bash scripts/version.sh bump <档位> -m "说明"` 或 `patch`，仅 main。
   版本含义见 docs/prd/2026-09-21-version-management.md；该脚本会提交并打 tag。
6. 重查门禁与 tag/HEAD；构建含正确版本/commit 的候选镜像，验证健康、鉴权 API、核心浏览器操作。
   测试通过不等于已经发布；交付目标为 GitHub Release 时须验证发布及附件；生产部署仅在明确授权的范围内进行，未部署须如实注明。
7. 发布报告记录版本、SHA、镜像身份、测试环境、限制、回滚步骤、生产是否替换。
   推送、GitHub Release、镜像导出分别写状态；不能互相代替。

本地路由是协作约定加可执行门禁，不能阻止绕过脚本的 Git 命令。
若需要远端强制执行，需在仓库 CI/分支保护中配置 required checks；本次不更改远端策略。
