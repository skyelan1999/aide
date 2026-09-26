# aide 开发与运维交接

---

# 【GPT 接手交接 · 2026-09-26】（本区块为最新权威入口，以下历史章节仅供溯源）

用户已停止全部豆包子智能体（统筹 organizer 及其下属开发/测试 agent 均已 kill）。剩余工作由 GPT 接手。**请先读本区块，再按需翻下方历史。**

## 0. 项目一句话

aide = AI + IDE，本地优先、离线/air-gap 可用的 AI 专业开发工作台。后端 **Go（标准库为主）**，前端 **原生无框架 HTML/CSS/JS**，**Docker Compose** 部署；开源依赖全部 vendor 到仓库（MIT 等），**不用任何 CDN**。

- 仓库根：`/Users/skyelan/Library/Mobile Documents/com~apple~CloudDocs/WorkStation/AI WorkStation/aide`
- 相邻参考项目：`../Harness`（只读挂载为内置上下文 `/context`）
- 当前 Agent 工作目录（用户实际工作区）：宿主 `~/debug` ↔ 容器 `/local/Users/skyelan/debug`

## 1. 当前精确快照（已实测）

| 项 | 值 |
|---|---|
| 工作分支 | **`feature/permission-panel`**，working tree **clean**，已与 `origin/feature/permission-panel` 同步 |
| HEAD commit | **`c978fd5`** i18n: wrap WebAuthn credential name with t() (#26) |
| 版本（/api/config） | **0.1.11.0-RC3**，buildCommit=c978fd5，integrity=**ok** |
| 访问入口 | **https://localhost:8097**（容器内 HTTPS 8080；http 单端口 308 跳 https；自签证书） |
| 主容器 | `aide-aide-1` healthy（镜像 `aide:local`） |
| 模型 | deepseek-v4-flash（非视觉），另有 deepseek-v4-pro；BaseURL https://api.deepseek.com |
| 关键配置 | toolMaxRounds=60、shellTimeout=60、sandboxMode=workspace-write、reasoningEffort=auto |
| 账户 | 已设密码、已注册 Touch ID 凭证（hasPlatformCredential=true）、用户名 SkyeLan |
| TTS | edge 可用（edgeAvailable=true），sherpa 未安装；currentTTSEngine=unavailable |
| 性格 | aide（work）/ 小秘 xiaomi（life）两套；voiceReplyEnabled=true |
| Git tag | **v0.1.11.0-RC3 指向更早 commit 2d5e368（不是当前 HEAD）**；是否升 RC4 待用户拍板 |
| main 分支 | 落后，**未获明确许可前严禁合入 main** |

> 本地测试凭据不要写入仓库；需要自测时由用户在本机输入。

## 2. 【最高优先 · 修复到一半被打断】小秘语音框回归

**真机现象**：主会话（新建会话空白页）点麦克风，麦克风变红（语音在跑），但小秘的语音“框”不出现，转写/状态无处可看。用户原话：“小蜜的框没了啊”。

**根因**：`internal/server/web/app.js` 的 `$('voice-btn').onclick`（约 **4978-4985**）按会话 kind 分流，把主会话降级成纯听写：
```js
if (state.session?.kind === 'assistant') { voice.listening ? voiceStopAndFlush() : voiceStart(); }
else { dictation.listening ? dictationStop() : dictationStart(); }   // ← 主会话走 dictation
```
`dictationStart()`（约 4988）只把文字填进 `#prompt` 让人手动发：**不显示 `#voice-panel`、不做 AI 噪声甄别、不自动断句发送**——偏离用户原始诉求。

**用户的明确诉求（验收口径）**：
1. 点麦克风即唤起【小秘语音管线 voiceStart】，**主会话与小秘会话都要**显示小秘语音框；若保留“纯听写填框”，做成设置里的语音模式开关（默认=小秘助理，不要默认听写）；
2. `#voice-panel` 显示要**提前/独立于麦克风成功**：点击先弹框显示“正在请求麦克风…”，再 `await voiceOpenMicStream()`；失败/拒绝给明确提示与重试，避免麦克风慢/失败时整个框不出现；
3. 框 **内嵌在主要聊天框区域、不挡下面内容**，做成贴输入区/会话区上方的**薄条**（关联 #47），用主题 CSS 变量、不硬编码颜色；展示：小秘名字、聆听/停止状态、当前断句 interim、每条甄别结果（已发送/已忽略）、排队状态；停止即收起；
4. 恢复**自动动态断句 + AI 甄别**（要传达的意图 vs 背景噪声/刷视频/与他人闲聊，闲聊自动退下）+ **直接发送**（不放进输入框手动发），发送沿用 #41（紧急/中止插队、新任务排队，小秘自判）；
5. 现有可复用链路：`voiceExtractSentences / analyze / voiceDrainQueue / voiceArmPauseFlush`（voiceStart 内，约 4890-4927）。

**验证**：无真实麦克风时用 mock（stub `getUserMedia` / `webkitSpeechRecognition`）做桌面 E2E——点击后面板立即显示、状态/断句/甄别渲染、发送被触发（#41 排队/插队）、停止后面板隐藏；附截图。**真实麦克风的实时甄别与发送属 NOT_RUN，需用户真机验收。** 约束：只改前端（app.js + 必要 CSS），`node --check` 通过。

## 3. 任务 #29–#64 真实状态（很多已在代码落地，仅任务状态未关闭）

实质已落地、真机或单测验证（GPT 可在回归后关闭任务）：
- **#29** 安全/欧盟合规：TLS、Argon2id（kdf.go）、docs/security/eu-compliance.md
- **#30** 会话编号 + 小秘置顶（assistant-entry）+ 跨会话 dispatch 卡片
- **#31** 数据目录分层（docs/architecture/data-layout.md）+ 完整性基线 `/data/.integrity`（integrity=ok，可自愈）
- **#32** 配置导入导出 + 跨版本默认回填/迁移
- **#33** 小秘自我意识/统一身份核心提示词（assistant_agent.go）
- **#35** 记忆单向可见（小秘可读 aide、aide 禁读小秘）+ 流式输出桥接（6 单测，docs/architecture/memory-access.md）
- **#37** 通讯插件 comm-tcp/udp/serial/ssh（默认禁用、仅回环；docs/plugins/）
- **#38** SSH 私钥路径/粘贴双输入 + 凭据加密（secret-vault）
- **#39** SQLite 插件（node:sqlite，零依赖；docs/plugins/sqlite.md）
- **#40** 恢复出厂设置（Factory reset 分区）
- **#41** 小秘自判插队/排队（app.js 约 4673）
- **#43** TLS 收口（https 单端口、WebAuthn https origins）
- **#45** 硬停止 voiceHardStop + 概括设置
- **#46** 启动提速（默认不跑全量 test、no-build 直起）
- **#48** 外部 AI 调试接口（无障碍开关、表格化、日志下载；当前 debugAccessEnabled=true）
- **#49** 设置 number 渲染器（toolMaxRounds 可见可配）
- **#50** 模型 token 占比饼图（labelLine 引导线）
- **#52** max_tokens 截断自动续写（DXF 已能生成）
- **#55** 刷新不再误锁屏（锁定态后端持久化）
- **#56** 集成测试收口（两层测试已大量执行；最终 tag/push 待真机终审）
- **#58** 取消文本 256KiB（maxFile=64MiB，files.go）
- **#59** 编辑器只读/保存归并顶部栏
- **#60** 会话列表 SSE 实时刷新
- **#61** 工作目录语义（AgentRoot 抽象 P0-P2 已实施，a93980a）
- **#62** 小秘会话闭环（锁定门三态、历史、单例）——已完成
- **#63** Word 查看 + 批注（docx-preview + /data/comments，test.docx 已验证）
- **#64** 代码语法高亮（highlight.js 离线 vendor，93f4a47）——**实际已完成**

部分落地、效果待真机/用户仍有反馈（勿轻易关闭）：
- **#34** 性格“自主微调演化”：框架与文档在（personality-evolution.md / persona.go），长期演化效果未验证
- **#36 / #42** 音色克隆与“音色没差”：克隆 Key 未配（hasCloneKey=false），edge 音色区分度用户仍不满意
- **#44** 多引擎 TTS：edge 在、sherpa 未装，currentTTSEngine=unavailable
- **#47** 小秘薄条 UI：左侧条目在，**语音内嵌薄条随第 2 项一起做**
- **#51** 思考转圈/思考滚动：节点复用改过，用户后续仍有反馈，需回归
- **#53** 辅助资料多来源：/context 常驻在，“新增来源按钮”用户反馈时有时无，需回归
- **#54 / #57** PDF.js、DXF 渲染：内联在，用户多次反馈“看不了/无渲染”，**高优先回归**（注意可能是容器没用新镜像 force-recreate 造成的“假未修复”）

## 4. NOT_RUN（需用户真机/真实环境，禁止写成 PASS）

- Touch ID 按压解锁（凭证已注册，按压流程待真机）
- 真实麦克风：实时聆听、断句、AI 甄别、直接发送
- sherpa-onnx 离线 TTS（模型未下载，用 `scripts/tts-setup`）
- xlsx/pptx 附件桌面 E2E（代码层单测已覆盖）
- 64MiB 大文件降级桌面实测（代码层单测已覆盖）

## 5. 测试遗留容器与游离分支（先确认再清理，勿擅自删）

- 容器：`elegant_jepsen`(aide:local，随机名疑似构建测试遗留)、`nifty_ellis`(aide-serial-test)、`infallible_bhaskara`(node:24 插件测试)；`ai-jupyterlab` 可能是用户自己的，先保留确认。
- 大量历史分支：`feat/*`、`fix/*`、`feature/*`、`experiment/*` 等（成果已并入 permission-panel）；用户曾要求“把游离子分支合起来”，GPT 核对后再决定清理，**不要 `git clean -fdx`、不要 `down -v`**。

## 6. 铁律 / 关键约束

1. **未获用户明确许可，`feature/permission-panel` 不合 main、不正式 release**；可 push 到远程 feature 分支（用户已授权“推上去不合 main”）。
2. 离线优先：开源依赖 vendor 入仓（highlight.js / docx-preview+JSZip / dxf-parser / PDF.js / mermaid / marked / three.js 等），**禁止 CDN**；`start.command` 导入 Docker 后不得再拉外网。
3. 原生无框架：Go 标准库 + 原生 JS，不引 React/Vue/构建链。
4. 静态资源统一 `Cache-Control: no-store`（浏览器不缓存）。**判断新代码是否生效以 `/api/config` 的 buildCommit 为准。**
5. 改前端必须重建并用新镜像重创建容器：
   ```bash
   export AIDE_VERSION="$(bash scripts/version.sh show)"
   export AIDE_COMMIT="$(git rev-parse --short HEAD)"
   docker compose build aide && docker compose up -d --force-recreate aide
   ```
   不能用 `docker start`（会复用旧容器/旧镜像）；注意历史上 `/data/settings.json` 新旧位置迁移曾导致容器 Exited，重建后留意日志。
6. 配色用主题 CSS 变量，避免大量紫/靛蓝、避免高饱和整段着色；不泄露 token/密码/内部 ID。

## 7. 命令速查

- 启动：macOS `./start.command`（或 `bash scripts/aide.sh start`）；Linux `./start.sh`；Windows `start.ps1` / `start.bat`（**Win/Linux 未实际验证，局限已在 docs 标注**）。
- 状态/日志：`bash scripts/aide.sh status|logs`。
- 代码测试（容器内，整仓无 skip）：`go test -race -count=1 ./...`（server 约 228s + tts）、`go vet ./...`、`go build ./...`。
- 路由门禁：`python3 scripts/agent-route.py verify quick|full`。
- 版本：`bash scripts/version.sh show`；正式升版只在 main：`version.sh bump <档位> -m "..."`。
- 前端 i18n 检查：`scripts/test_i18n.cjs`；UI 流式/队列无头检查：`scripts/ui_stream_check.cjs`、`scripts/ui_queue_check.cjs`（缺 playwright 自动 SKIP）。

## 8. 文档地图（含中英）

- 架构总览：`docs/architecture.md`、`docs/architecture-overview.html`；专题 `docs/architecture/`（data-layout / memory-access / office-viewer / personality-evolution / touchid…）
- PRD：`docs/PRD.md`、`docs/prd/`
- 安全/合规：`docs/security/`（eu-compliance、secret-vault、tls、password-hashing、voice-cloning、tts-local…）
- 插件：`docs/plugin-protocol.md`、`docs/plugins/`、权威清单 `plugins/registry.json`
- 工作目录：`docs/workspace-paths.md`；外部 AI 调试：`docs/debug-api.md`；验证记录：`docs/verification.md`；版本：`version.md`
- **英文文档：`docs/en/`**（README、installation、user-guide、architecture、security、agent、plugins 配对）
- drawio 示例：仓库根 **`demo.drawio`**；工作区 `~/debug` 下另有 `用户管理流程.drawio / 用户登录流程.drawio / 登录流程图.drawio`

## 9. 建议接手顺序

1. 读 `AGENTS.md` → `docs/agent/WORKFLOW.md`；`git status` + `/api/config` 确认与本快照一致；
2. 先修 **第 2 项（小秘语音框）**，mock E2E 闭环；
3. 高优先回归用户反复反馈的 **#54 PDF / #57 DXF / #53 来源按钮 / #51 思考**，每次以 buildCommit + force-recreate 确认，排除“假未修复”；
4. 推进 #34/#36/#42/#44 等待真机项，约用户做第 4 节 NOT_RUN 终审；
5. 全部闭环后，请用户决定 **push / 升 RC4 重新 tag / 合 main**，不得自行合 main。

---

> **启动配置以 `.env` 为准**：`start.command` → `scripts/aide.sh` → Docker Compose，统一读取 `AIDE_PORT`（默认 8097）和 `COMPOSE_FILE`。临时验收端口不是用户启动入口。目录范围和 macOS 共享根模式见 [工作目录配置](docs/workspace-paths.md)。

更新：2026-09-25。本文是现行操作入口。历史测试结果保留在 [验证记录](docs/verification.md)，不能据此推定今天的运行服务状态。

## 本轮实验性改进（2026-09-24，分支 feature/permission-panel，未合并 main）

在 main (0.1.10.0) 之上连续交付 6 项改进，已合并为一条线：
1. run_shell 从"只生成提案"改为容器沙箱内实际执行（execShellCommand，bash --norc，60s 超时，128KB 截断）
2. 设置面板新增「权限管理」栏，展示各工具权限
3. run_shell 入口加危险命令黑名单 shellBlocked()（递归删除/提权/推送远端/pipe-to-shell 等）
4. 归档当前会话后自动回新会话页
5. 运行中插话/排队消息加 wrapSteer() 上下文包装，避免模型误判新话题
6. 运行中发送默认排队模式（state.queueMode 默认 true，按钮默认高亮）

验证：docker compose build 通过，go test ./... 全绿（含 TestShellBlocked、更新后的 TestToolLoopWriteProposalAndShellExec）；/healthz、/api/config、/api/command(echo) 冒烟通过。

后续在 0.1.10.1 / 0.1.10.2 继续交付：run_shell 容器内实际执行、设置面板「权限管理」栏与 per-tool 开关持久化、md/Mermaid 渲染修复与相对路径链接内部打开、在线网页搜索（web_search 走 DDG HTML 抓取、SearXNG 兜底；semantic_search 本地 TF-IDF）、draw.io 插件、持久记忆、模型上下文预设按钮、工具轮次默认 60、轨迹导出与调用分析、上下文预览堆叠图、子 Agent（spawn_subagent）、失败反馈循环、三级沙箱、AI 工作流四阶段与自动模式、推理强度五档、语音小秘、配置备份、锁屏与性格持久化。

未完成 / 已知边界：
- 远程 SSH 工作区的 run_shell 自动执行未打通
- 架构总览图见 docs/architecture-overview.html

## 接手时先确认什么

```bash
git status --short --branch
git log -3 --oneline
git remote -v
bash scripts/version.sh show
bash scripts/aide.sh status
```

当前版本 `v0.1.10.2-RC1`（tag 已打在对应提交）：在会话管理（置顶/归档/删除/导出）、会话状态灯（运行绿闪/审批黄/失败红/完成蓝点加粗）、运行中排队/插话、SSE 流式输出之上，本轮新增持久记忆、模型上下文预设按钮、工具轮次默认 60、md/Mermaid 渲染修复、轨迹导出与调用分析、上下文预览堆叠图、子 Agent（spawn_subagent）、失败反馈循环、三级沙箱、提前停止保留输出、AI 工作流四阶段与自动模式、推理强度五档、语音小秘、per-tool 权限开关、配置备份、锁屏与性格持久化。aide 定位为 AI + IDE 专业工作台；开发 Agent 路由用于维护与场景扩展，见 [定制指南](docs/customization.md)。

发布只交付 GitHub 源码/tag/Release/镜像；不自动替换本机 8097 的生产服务。镜像导入启动见 [镜像说明](docker-images/README.md)。

开发从 [AGENTS.md](AGENTS.md) 开始，恢复 [任务记录](docs/tasks/agent-workflow-docs.json)。日常操作见 [使用指南](docs/user-guide.md)。

## 配置与运行

- Dockerfile：Go 1.26、Python 3.12、Node.js 24 工具链，Git/curl/bash，非 root 用户；精确版本由构建镜像决定。
- 基础镜像与离线构建：node/golang 按 digest 固定；python 基础镜像用 `ARG PYTHON_BASE`（默认本地 tag `python:3.12-slim-bookworm`）。本机该 tag 从既有构建产物提取（与固定 digest `392307d2...` 内容一致），避免受限网络下按 digest 拉取 metadata 卡死 `start.command`。重新提取：`docker run --rm --entrypoint tar aide:local -C / --exclude usr/local/go --exclude usr/local/include/node --exclude usr/local/lib/node_modules --exclude usr/local/bin/node --exclude usr/local/bin/npm --exclude usr/local/bin/npx --exclude usr/local/bin/corepack --exclude usr/local/bin/yarn -cf - usr/local | docker import - python:3.12-slim-bookworm`。发布构建可传 `--build-arg PYTHON_BASE=python:3.12-slim-bookworm@sha256:...` 恢复固定。
- Compose：本机端口、cap_drop、no-new-privileges、2 CPU / 2 GiB / 256 PID。
- `/workspace` 可写；`/context` 只读；`/local` 默认为可写 HOME。`.env` 可以缩小本地根目录范围。
- `/data` 和 `/home/aide` 为命名卷；切换挂载目录不是迁移数据卷。
- API Key、访问令牌与会话可能包含私密数据，不进 Git，不贴到公开日志。
- Go embed 前端，修改源码要重建；只刷新页面不能换掉旧镜像。

`start.command` / `scripts/aide.sh start` 会构建并启动，不适合当作只读检查命令。状态与日志使用 status/logs。

## 检查代码与文档

```bash
python3 scripts/agent-route.py check
python3 scripts/agent-route.py verify quick
python3 scripts/agent-route.py verify full
python3 scripts/agent-route.py audit
```

full 在一次性 Docker 容器运行 race/vet，需要本地 `aide:local` 镜像与 Docker 引擎。浏览器检查与变化相关 API 验收另行执行并记录。脚本自身的回归通过不意味着所有客户端已自动加载路由。

## 发布

先完成文档工作流中的前置阶段、测试证据和发布授权记录。生产重建前结束任务并保存恢复点。不要自动混入其他人的未提交文件。

1. 审阅本次任务、diff、测试与未验证项；按授权合并到 main。
2. main 工作树干净后运行 full 与 `release-check <任务号>`。
3. 使用 `scripts/version.sh bump <档位> -m "说明"` 或 `patch`。它会提交版本文件并打 tag；只允许 main。
4. 构建时明确注入身份，避免默认 dev/unknown：

```bash
export AIDE_VERSION="$(bash scripts/version.sh show)"
export AIDE_COMMIT="$(git rev-parse HEAD)"
docker compose build aide
```

5. 保留旧镜像回滚标签和数据备份后，在授权维护窗口执行 `docker compose up -d --no-build`。
6. 检查 `/healthz`、认证后的 `/api/config` version/revision、登录、文件、任务与需要的界面。对应用于这一数据集的 `scripts/verify_runtime.py --check` 才能据其结果判定持久化；默认脚本会创建测试会话/文件，不能随意对业务目录运行。
7. 需要迁移时再 export；推送和 GitHub Release 各自记录实际状态。备份/构建/tag/健康检查都不能单独代表完整发布成功。

## 备份与恢复

源码 Git 历史、未提交改动、项目文件、镜像归档、配置及两个数据卷需要分别备份。Git bundle 不包含未提交文件；镜像不含卷。不要运行 `docker compose down -v` 来做普通升级。

下面是维护窗口参考命令，**不是本次已执行的动作**。执行前检查实际卷名称；不要覆盖已有备份。

```bash
umask 077
AIDE_BACKUP_DIR="$HOME/aide-backups/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$AIDE_BACKUP_DIR"
git bundle create "$AIDE_BACKUP_DIR/aide-source.bundle" --all
docker image tag aide:local aide:rollback
docker compose stop aide
docker run --rm --user 0 --entrypoint tar -v aide_aide-data:/source:ro aide:local -C /source -czf - . > "$AIDE_BACKUP_DIR/aide-data.tgz"
docker run --rm --user 0 --entrypoint tar -v aide_aide-home:/source:ro aide:local -C /source -czf - . > "$AIDE_BACKUP_DIR/aide-home.tgz"
docker compose start aide
shasum -a 256 "$AIDE_BACKUP_DIR"/*.tgz "$AIDE_BACKUP_DIR/aide-source.bundle"
```

确认 tar 可读取、校验和已保存；任一步失败都不能将半成品当恢复点，并及时恢复服务。业务文件与 `.env` 另做私有备份。

恢复时使用独立项目名、端口、空数据卷和工作区副本，保留 UID/GID 1000 可访问权限。检查会话、模型配置、费率、来源、文件与只读约束后再切正式入口。

回滚优先用旧镜像和配套备份，在隔离环境验证数据兼容；若恢复卷，不覆盖仅有的数据副本。镜像回滚不会撤销已经写入宿主工作区的文件。导入旧归档后应 `--no-build` 启动，不能紧接着重新构建新源码。

## 排障

| 现象 | 首要核对 |
| --- | --- |
| Docker 不可达 | Docker Desktop / docker info / CLI 路径 |
| 401 或重新要求登录 | 当前实例的本地令牌、浏览器存储、端口；不要使用 API Key 代替 |
| 模型已配置但失败 | Base URL、模型 ID、提供商权限/网络与实际错误 |
| 409 文件冲突 | 工作区身份、文件哈希、外部编辑；重新读取而非绕过保护 |
| 页面没有更新 | 实际镜像构建身份和资源内容，再排查缓存 |
| cd 不保留或 vim 卡住 | 非交互独立 shell；单条命令内组织工作目录 |
| 统计费用不同于账单 | 本地费率快照、估算/未计价、缓存折扣与上游结算 |
| 压缩后缺细节 | 回查原文件、历史任务和摘要；不把压缩当无损归档 |

## 交接输出

报告任务 ID、修改范围、代码/镜像身份、测试命令与证据、未验证范围、数据迁移、当前服务、发布和回滚状态。不要写“全部完成”掩盖未发布或未实测的步骤。

## 2026-09-24 启动与目录选择交接

- 日常入口统一为 `start.command`，默认端口 8097；`.env` 保存端口与 Compose 组合。
- macOS 共享根、范围限制与切换方式见 [工作目录配置](docs/workspace-paths.md)。
- 目录弹窗采用与父面板一致的紧凑布局；地址栏与前往按钮等高；保存配置后刷新目录。
- 已在实际 8097 Safari 页面验证；临时预览容器已移除，数据保留。
- 本轮发布目标为 v0.1.7.0-RC1，包含 main 合并、tag、GitHub 预发布及 arm64 镜像附件。

## 2026-09-24 SSE 流式输出修复与验收交接

- 任务 `sse-streaming`（[任务账本](docs/tasks/sse-streaming.json)）：模型响应改为逐 token SSE 推送，`GET /api/sessions/{id}/runs/{run}/events`（step/delta/tool/status/done），`?access_token=` 仅对该路由生效；最终状态仍由会话接口兜底。
- 修复初版三处运行时缺陷：事件 hub 发送/关闭竞态（统一 eventMu 内操作，close 为终态信号）、晚订阅者永久挂起（runEvents 先订阅后复查状态）、前端 chat 答案重复渲染（运行中只渲染 live 文本，delta 带 round 按轮重置）。
- 前端致命缺陷与显示升级：EventSource URL 曾漏写 `/events` 段导致流式请求 401 失效（界面只剩“正在思考”）；已修复并加 `scripts/test_stream_url.cjs` 回归检查（进 quick 门禁）。显示按 DSH/Codex 风格：首 token 前呼吸思考点、流式文本+闪烁光标、live 工具行（⚒ 实时显示）、按轮次重置；index.html 资源版本升 `?v=32`。
- 生成速度优化：delta 渲染改为 requestAnimationFrame 批量（每帧最多一次全量解析，原为逐 token 全量）；live 渲染跳过代码高亮（2.85KB 基准 12.2ms→0.7ms，完成态补全高亮）；轮询/刷新在会话 JSON 未变时跳过整页重渲染；summarizeTopic 改并发执行，不再阻塞首 token（对应测试改为轮询等待标题）。
- 无头浏览器验收资产：`scripts/ui_stream_check.cjs`（需 playwright-core + CHROME_PATH，缺依赖自动 SKIP）+ `scripts/fixtures/tool_stream_mock.py`（带工具调用的流式 QA mock）；本机已用 Chromium headless 实测 PASS（chat/tool 两种模式）。
- 其他加固：stream_options 被网关 400 拒绝时自动去字段重试一次；前端流错误 5s 退避后由轮询兜底重开；`scripts/mock_provider.py` 支持流式（QA fixture，QA_BIND/QA_DELAY/QA_START_DELAY 可控）。
- 验证：`go test -race -count=1 ./... && go vet`（aide:local 官方路径）全过，新增 10 个流式/事件测试；`agent-route.py verify quick` 全过；容器端到端冒烟（chat 流式 14 个 delta、workflow 三阶段 step 事件、取消晚订阅立即关闭、鉴权收窄 401）通过；无头 Chromium UI 检查 PASS。
- start.command 卡死修复：本机无 `python:3.12-slim-bookworm` 镜像且 Docker Hub 不可达，按 digest 拉 metadata 永久挂起；已从 aide:local 提取等价本地 tag 镜像，Dockerfile 改为 `ARG PYTHON_BASE`（默认 tag，发布可恢复 digest），`docker compose build aide` 2.8s 离线完成；8097 服务已用新镜像拉起并验证 healthz/config/events 鉴权。
- 发布状态：已发布 `v0.1.8.0-RC1`（2026-09-24）：提交 47bec0e 与 tag 已推送 origin；GitHub 预发布 https://github.com/skyelan1999/aide/releases/tag/v0.1.8.0-RC1 含 arm64 镜像附件 + SHA-256；候选镜像 aide:0.1.8.0-RC1 冒烟通过（healthz/config 身份、chat SSE 14 事件）；`verify full` 与 `release-check` PASS。8097 生产服务未自动替换（同代码以开发身份运行）。剩余：8097 用真实模型确认观感（首 token 延迟、工具实时状态、取消、断网降级）。

## 2026-09-24 排队与插话（queue/steer）交接

- 任务 `queue-steer`（[任务账本](docs/tasks/queue-steer.json)）：任务运行中可继续发送——`queued=true` 进 FIFO 队列、`queued=false` 插话（通道容量 4，满 429）；toolLoop 每轮模型返回后先消费插话、再取队首；排队项支持修改/删除/升级插话（`POST /api/sessions/{id}/runs/{run}/queue/{index}`）。
- 界面定型：队列条参照 Codex pending_input_preview 风格悬于输入框上方（分区标题 + ↳ 弱化条目 + 提示行）；插话消息带标签入时间线；发送按钮空闲 ↑、运行中原地切 ■ 停止（插话/排队经 Enter 或「排队」+Enter）；一键回底 sticky 粘会话区底部并水平居中，滚离约一屏出现、点击平滑回底（内部滚动恢复全部瞬时，防 smooth 拉锯）。
- 修复豆包初版缺陷：429 路径先记录后拒收（先探通道再记录）；selectSession 清空 live 打断流式（同会话刷新保留）；运行中发送隐藏；队列条编辑双提交；CSS 未定义变量与死代码；回底按钮 absolute 随内容滚出屏幕（改 sticky）；插话未入时间线；后端零测试。
- 验证：3 个 Go 测试 race×3 全过；全量 race + vet 过；`scripts/ui_queue_check.cjs` 无头浏览器全断言 PASS（模式切换/停止取消/队列条/插话/回底）。资源版本 ?v=40。
- 状态：已获用户授权提交与推送（「修一下文档，合并推送吧」）；升版与 GitHub Release 待授权。8097 以 dev 身份运行。已知边界：steer 中间回答折叠进最终答案；任务结束瞬间到达的排队消息可能不获回答；每轮限 10 次模型调用，多轮插话/排队消耗轮次。

## 2026-09-24 会话管理（置顶/归档/删除）交接

- 任务 `session-manage`（[任务账本](docs/tasks/session-manage.json)）：每个会话项右侧 ⋯ 菜单（置顶/归档/删除，删除带确认）；置顶排最前；归档默认隐藏、侧栏「归档」按钮切换视图并支持取消归档与删除；设置页「归档」区块提供全部导出（GET /api/export，不含令牌与密钥）。
- 会话状态灯：运行中荧光绿闪烁、等待审批黄常亮、失败红常亮、完成蓝点+标题加粗；点击查看后蓝点与加粗消失（`Checked` 持久化，不改排序）；运行/审批/失败灯在选中时保持显示。排序：置顶最前，其余按活动时间倒序，点击不再触发置顶排序（仅提问/完成更新活动时间）。
- 交互修复：完成会话的标题在主题总结落库后自动刷新（完成事件与轮询兜底两条路径的延时补同步）；点击会话轻量化（查看标记请求不阻塞加载、同会话数据未变时跳过时间线重渲染）；点击会话后焦点进输入框；n 快捷键仅当焦点在页面主体时生效；发送流程不再以空壳会话抢占用户刚点击选中的会话，任务启动失败时空壳会话自动清理。
- 验证：2 个 Go 测试 race×3 全过；全量 race+vet 过；`scripts/ui_queue_check.cjs` 全断言 PASS（模式切换/停止取消/队列条/插话/回底/状态灯/会话管理/设置/菜单遮挡）；新增 `scripts/verify_race`（临时资产，不入库）覆盖 429 空壳清理与点击聚焦场景 PASS。资源版本 ?v=63。
- 状态：已获用户授权提交与推送（「先和入吧，修改文档并推送」）；升版与 GitHub Release 待授权。8097 以 dev 身份运行。
