# aide 开发与运维交接

> **启动配置以 `.env` 为准**：`start.command` → `scripts/aide.sh` → Docker Compose，统一读取 `AIDE_PORT`（默认 8097）和 `COMPOSE_FILE`。临时验收端口不是用户启动入口。目录范围和 macOS 共享根模式见 [工作目录配置](docs/workspace-paths.md)。

更新：2026-09-23。本文是现行操作入口。历史测试结果保留在 [验证记录](docs/verification.md)，不能据此推定今天的运行服务状态。

## 接手时先确认什么

```bash
git status --short --branch
git log -3 --oneline
git remote -v
bash scripts/version.sh show
bash scripts/aide.sh status
```

本次交付目标 `v0.1.9.0-RC1`（2026-09-24 已发布：tag + GitHub 预发布 + arm64 镜像附件）：运行中排队与插话、Codex 风格队列面板、发送/停止同键、一键回底。此前 v0.1.8.0-RC1 为逐 token SSE 流式输出与启动修复。上一轮 v0.1.7.0-RC1 为新版 UI、主题、关于、Agent 开发路由与统一 docs。aide 定位为 AI + IDE 专业工作台；开发 Agent 路由用于维护与场景扩展，见 [定制指南](docs/customization.md)。

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
