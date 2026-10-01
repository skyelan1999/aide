# aide 验证记录

> **历史证据**：以下日期、提交、测试数量、镜像和运行状态只适用于各节记录时点。本轮文档核对不重新认定这些结果，现行操作请读仓库根 HANDOVER.md；原审查报告中的“缺陷已复现”不代表当前代码仍存在该缺陷。

## 现行验证入口（2026-09-30）

当前以统一开发工作流脚本为准，不以下文历史测试数量为准：

- 快速门禁：`python3 scripts/agent-route.py verify quick`（git diff --check、`node --check` 前端 JS、`scripts/version.sh check`、`test_agent_route.py`、`check_docs.py`、`bash -n` 脚本、i18n/stream/local-root 回归等，清单见 `docs/agent/router.json` 的 `checks.quick`）。
- 完整门禁：`python3 scripts/agent-route.py verify full` = quick + `bash scripts/aide.sh test`，后者在 `aide:local` 容器内跑 `go test -race -count=1 ./... && go vet ./...`。
- 发布门禁：`python3 scripts/agent-route.py release-check <id>`（只检查不发布）。
- 后端测试已从历史记录时点的 9 个函数持续扩展；具体覆盖以当前 `go test` 结果和 `internal/server/*_test.go` 为准。

## 0.1.14.0 RC1 源码版本门禁（2026-10-01）

本节记录虚拟形象与 macOS 两侧栏拖动修复随源码版本升版前的验证。版本编号与代码身份以 `version.md` 和 Git tag 为准；本次不构建镜像、不创建 GitHub Release、不部署生产。

- `python3 scripts/agent-route.py verify quick`：PASS；文档检查覆盖 114 个 Markdown 文件及 261 个本地链接。
- `python3 scripts/agent-route.py verify full`：PASS；包含 `go test -race -count=1 ./...`（`internal/server` 319.328 秒、`internal/server/tts` 3.498 秒）及 `go vet ./...`，命令整体退出码 0。原始门禁收据保存在本机忽略目录 `.agent-state/full.json`，其任务账本记录 fingerprint。
- 虚拟形象专项 `test_avatar_player/settings/feedback.cjs` 与 `test_i18n.cjs`：PASS；播放器素材、设置迁移、反馈路径及本地化回归通过。
- 系统 Safari 手工拖动两侧分隔条：左侧 `300 → 340 → 300 px`，右侧 `366 → 408 → 366 px`；分隔线和对应侧栏同步变化，测试后恢复原偏好。
- 完整38种动作的逐场景主观验收、锁屏动态现场验收、量化帧率仍未全部完成；详见[虚拟形象任务记录](tasks/virtual-avatar-alpha.json)。

## 集成分支收尾验收（2026-09-30）

本节只记录本次 `consolidate-push-followup-20260930` AP 补验结果。版本 tag `v0.1.13.0-RC1` 仍是最近发布基线，分支整合本身不升版、不发布、不部署。

- `python3 scripts/agent-route.py verify quick`：首轮通过；完整门禁冻结工作树后重跑的结果记录在任务 [`consolidate-closeout-20260930`](tasks/consolidate-closeout-20260930.json) 和 `.agent-state/full.json`。
- Go 全量测试：容器内执行 `go test -race -count=1 ./... && go vet ./...`，`internal/server` 与 `internal/server/tts` 通过。第一次官方 full 收据因并行整理文档改变 fingerprint 而判 fail；后续稳定工作树复跑状态见本任务收据。
- 前端专项、时间/农历/Office/SQLite 插件、文档链接检查、SFTP/FTP/FTPS/SMB 本地服务与 MCP 引用调用、真实临时 sshd cancel/timeout 进程组清理结果见同一任务账本。
- Safari 隔离实例确认主工作台可渲染；系统日志级别选择 `ERROR` 后显示空结果，并能触发日志下载成功提示。系统随后自动锁屏，提醒 CRUD、窄屏侧栏拖动、会话长历史、IME、小秘会话交互未完成手工验收，不记为 PASS。

下文为历史记录，保留原始证据。

验证日期：2026-09-21，Apple Silicon Mac / Docker Desktop，linux/arm64。

## 实际部署

- 容器：`aide-aide-1`，镜像 `aide:local`，状态 `healthy`。
- 本机入口：`http://127.0.0.1:8097`，映射容器 8080。
- 运行身份：`uid=1000(aide) gid=1000(aide)`。
- Go：`go1.26.8 linux/arm64`。
- Python：`3.12.14`；ssl、sqlite3、bz2、lzma、ctypes 导入正常。
- Python venv 创建成功，venv 内 `pip 25.0.1` 可运行。
- Node.js：`v24.21.0`，npm `11.19.0`。
- Git：`2.39.5`。
- 容器 `/workspace` 绑定本地 aide 目录，实际写文件后在主机读取到相同内容。
- 容器 `/context` 绑定本地 `../Harness`；Mounts 显示 `RW=false`，实际写入探针返回 `Read-only file system`。
- 无 Docker socket 挂载。现有 `ai-jupyterlab` 保持 healthy。

## 后端验证

在 Go 开发容器及最终非 root aide 容器中运行：

```bash
go test -race -count=1 ./...
go vet ./...
```

均通过；共 9 个测试函数：

| 测试 | 验证内容 |
| --- | --- |
| TestAuthenticationAndOrigin | 未认证拒绝、跨站 Origin 拒绝、静态资源与健康接口 |
| TestFileBoundariesAndConflicts | `..`、越界符号链接、敏感路径、哈希冲突、主机写入 |
| TestSettingsNeverReturnKey | API 不返回密钥，配置文件权限 0600 |
| TestWorkflowApprovalConflictAndPersistence | 三步骤模型适配、等待应用、外部编辑冲突、应用与状态持久化 |
| TestRejectUnattachedExistingFile | 未附加的现有文件不可被模型提案覆盖 |
| TestProviderErrorsAndCancellation | 上游错误处理、取消传播 |
| TestCommandExecutionAndExitCode | 真实 shell 输出、退出码、模型密钥不继承到 shell 环境、cwd 边界 |
| TestRestartMarksInterrupted | 运行中任务在服务重启后标记中断 |
| TestChatHistoryAndCancelEndpoint | 对话保存、取消 API 和状态 |

前端 `node --check internal/server/web/app.js`、启动脚本 `bash -n` 通过。

## 浏览器验证

临时预览容器中使用 `scripts/mock_provider.py` 作为受控的本地测试 API。界面和输出明确标注 QA 模拟；没有调用云端模型。

已实际操作验证：

1. 令牌登录；模型设置保存；工作目录文件浏览与打开。
2. 切换 AI 工作流，提交任务，依次观察规划、提案、审查完成，状态进入等待应用。
3. 展开修改前后内容，点击应用，主机出现 `test-results/workflow-smoke.txt`。
4. 点击“填入命令面板”，单独运行 `cat`，输出与文件一致，退出码为 0。
5. 浏览器编辑并保存该文件，主机读到新增的 `browser edit saved` 行。
6. 将该文件附加到下一轮对话，取得明确标注的 QA 回答，会话保留两次任务。
7. 切换辅助资料，看到已挂载的 Harness 目录，新建文件按钮禁用。
8. 正式页面自动登录，模型显示未配置；服务重启后刷新页面仍能访问，部署验证会话可见。
9. 窄窗口下打开、关闭文件面板；首页、输入框、模型设置可使用。
10. 1280×720 布局检查：首页卡片底部 406.86 px，小于会话区底部 484.30 px，页面无横向溢出。修复初版小窗口卡片裁切。

浏览器检查未发现应用 JavaScript error/warn。临时预览容器和模拟 API 已清理；正式实例没有保存测试模型配置。

## 重启与镜像

运行 `scripts/verify_runtime.py`：认证 API、会话保存、挂载文件写入通过。仅重启新建 aide 容器后，`--check` 确认原令牌、会话及文件仍可读。

- 镜像平台：`arm64`。
- 镜像 ID：`sha256:6736e4bd765a1c653fa85daba29eae8d6b8b0c56f72167f3f9b220043fcfadc3`。
- 镜像归档：`docker-images/aide-local.tar.gz`，约 334 MiB。
- SHA-256：`46d226a838243e6a7627442dee784e54b6971e43d7463f13d881909f72ac0707`。
- 校验文件：`docker-images/aide-local.tar.gz.sha256`，`shasum -a 256 -c` 通过。
- 归档 manifest 中包含 `aide:local`，config 为 `fd84b65de3536cf648fa80ccf3443918dc4cf4f5cd7f5ac9d003b2ae30bdb0be`。
- 三个官方基础镜像已在 Dockerfile 中按 digest 固定。

归档不包含本地挂载资料、会话卷或 API 密钥。未在另一台机器上执行恢复测试。

## 未验证项与当前限制

~~没有用户的 API 密钥，因此没有验证真实 DeepSeek 或其他云端模型的推理质量、账户权限、模型 ID、费用或上下文上限。上述模型测试证明应用协议与工作流链路，不能等同于真实模型验收。~~（已于 2026-09-22 完成真实模型验收，见下节）

命令面板为非交互 shell；没有 PTY、长期任务守护、自动工具循环、DSH 插件兼容或多用户隔离。文件提案需用户应用，建议命令需用户运行。适用范围及架构扩展点见 README 与 architecture.md。

## 真实模型端到端验收（2026-09-22，FR-23）

- 账户：用户自配 DeepSeek 账户（`deepseek-v4-pro`，密钥不记录）；余额接口 `/api/balance` 的历史实测返回成功（不保留账户余额数值）。
- 短对话：chat 任务「1+1等于几」→ 回答正确；`run.usage` 记录真实用量（977 tokens，上游返回）；会话标题自动总结（FR-88/91 同时验证）。
- 三阶段工作流：任务「新建 test-aide-proposal.txt」→ plan/propose/review 全部 completed，提案为有效 JSON，进入 awaiting_approval。
- 应用与人工验证：POST apply → completed、applied=true；宿主机读回文件内容 `hello aide` 一致。
- 此前实测发现的真实模型偶发失败「模型方案不是有效 JSON」（自由文本输出所致）已通过 propose 步骤强制 `response_format=json_object` 修复，并有测试断言（TestWorkflowApprovalConflictAndPersistence 校验第 3 次调用 response_format）。
- 历史「工具调用轮次超过 10 轮」失败为 LIM-29 设计上限触顶，非缺陷。
- `go test -race -count=1 ./...` 与 `go vet ./...` 通过（容器 go1.26.8）。

## 整改批次发布（2026-09-23，0.1.5.0 RC5）

- 发布链：`remediate/r01-r07` 合入 `main`（合并提交 `8724afc`）→ 发布基建提交 `2b2ee01`（Dockerfile 构建身份 ARG）→ `scripts/version.sh patch` 升版 `0.1.5.0 RC5`（提交 `905d261`，tag `v0.1.5.0-RC5`）。
- 镜像重建：`AIDE_VERSION=0.1.5.0-RC5 AIDE_COMMIT=905d261… docker compose up -d --build`；镜像 `aide:local` ID `sha256:8f4455dfe822…`，已导出 `docker-images/aide-local.tar.gz` 并更新 `.sha256`。
- 生产验证：`aide-aide-1` healthy；`/api/config` `version=0.1.5.0-RC5`、`revision=buildCommit=905d261…`；`/healthz` 200；容器内 `verify_runtime.py --check` PASS（令牌/会话/工作区文件跨重启完好）。
- 卷备份：`~/aide-backups/20260923-195334/{aide-data.tgz,aide-home.tgz}`（sha256 已记录于备份目录内输出）。
- 整改验收证据：`docs/reviews/2026-09-23/`（11 套独立验收全 PASS + 真实 Chromium 浏览器 12/12 + race/vet/gofmt 干净）。
