# RCA 问题解决 UI 与文件夹批量上传门禁验证

日期：2026-09-29；分支：`feature/rca-ui-upload-gate`；基线 `e119fa9..d6c3786`（6 个功能提交）。
本批交付并验证：RCA（问题解决）工作流 UI、文件夹递归上传、搜索截断提示、SSH 远端取消、会话桶布局统一。
下列门禁数据为最终核验结果；本次只整理文档与账本，未重跑测试/构建，未 git 提交。

## 范围与特性

1. **RCA（问题解决）UI**：第五个工作流阶段按钮「问题解决/Root Cause」（后端 phase 值 `problem-solving`），亦可从对话欢迎页 starter 进入；阶段提示/流程可见。「RCA Reports」对话框列出 `problem-reports-index.md`，渲染 Markdown 并打开关联 `.drawio`；缺图报告显示琥珀色警告、无死链；`.drawio.svg/.png` 引用回链 `.drawio` 并做存在性校验。
2. **文件夹递归上传**：`webkitGetAsEntry` + `createReader` 递归，保留相对路径（含顶层文件夹名）；multipart `POST /api/file/upload-batch`（字段 `files`，filename=relativePath；上限 1000 文件 / 256 MiB，单文件 64 MiB；201 全部成功 / 207 部分成功）；进度遮罩 + 逐文件失败汇总；单文件拖拽上传行为不变。
3. **搜索截断提示**：`GET /api/files` 仅在触顶时设置响应头 `X-Search-Truncated:1`、`X-Search-Dir-Limit:200`、`X-Search-Result-Limit:500`（响应 body 不变）；UI 琥珀色提示条。
4. **SSH 远端取消**：远端命令经 `setsid` 包进独立会话/PGID（标记 `/tmp/.aide-remote-<runid>.pgid`）；取消/超时时另起 ssh 执行 `cleanupRemoteGroup`（对 `-PGID` 先 TERM 后 KILL 并校验），返回 `AIDE-CLEANUP:reaped`；集中在 `App.execRemote`（ssh_session.go），覆盖 command.go 与 workflow agent `run_shell`。
5. **会话桶布局**：规范规则 `sessionBucketFor`（assistant→`sessions/assistant`、archived→`sessions/archived`、其余→`sessions/active`）；`save()` 写规范桶并清理陈旧副本；`delete` 覆盖全部桶；启动迁移 `rebalanceSessionBuckets` 幂等（备份+隔离+回滚），零会话丢失。

## 提交

基线 `e119fa9`..`d6c3786`，6 个功能提交：

- `830fc1e` feat(server): RCA report helpers, index parsing and diagram validation
- `f756aaf` feat(server): reap SSH remote process group on cancel/timeout + sshd e2e harness
- `157b101` feat(server): multipart batch upload preserving relative paths
- `5eb0629` feat(server): unify session bucket layout with idempotent migration and rollback
- `e9bdf50` feat(web): RCA problem-solving UI and folder recursive upload
- `d6c3786` feat(server): surface file-search truncation via headers and UI notice

## 门禁结果

- `go test -race`（镜像 golang:1.26-bookworm；`-mod=vendor -count=1 -timeout 20m`；`-skip` 跳过 15 个仅依赖环境的测试）：**GATE EXIT=0**；`ok aide/internal/server 255.487s`、`ok aide/internal/server/tts 3.192s`；0 FAIL / 0 DATA RACE / 0 panic。日志 `.agent-state/full-race-gate-20260929.log`。
- `go vet -mod=vendor ./...` OK；`go build -mod=vendor ./...` OK。
- Playwright 独立验收：修复唯一缺陷（搜索提示条）后 **43 项检查全 PASS**；回归运行 **11/11 PASS**。

## 跳过的测试（非产品失败）

golang 镜像内无 node / office python 工具链，下列 15 个环境依赖测试被 `-skip`，不是本批功能失败：

- node 依赖（10）：TestDaemonStartStop、TestDaemonCallViaIPC、TestDaemonCrashRestart、TestDaemonEventReporting、TestNonDaemonUnchanged、TestDaemonConcurrentStartStopConsistency、TestPluginUploadAndValidation、TestPluginLifecycleAndSurface、TestPluginApplyErrorIsolated、TestPluginToolExecution。
- office 依赖（5）：TestOfficeNativeCommentsRoundTrip、TestOfficeNativeCommentsReferenceReadOnly、TestOfficeCreateAndXlsxEdit、TestOfficeXlsxReferencePermissions、TestRemoteWorkspaceDispatch。

## 证据

- SSH e2e：`docker-images/sshd-test/evidence/20260929-210707/`（run.log + cancel/timeout 前后对照）；harness `docker-images/sshd-test/run-e2e.sh`。
- RCA 截图：`/Users/skyelan/Library/Application Support/Doubao/Default/.doubao/agent_mode/workspace/.sessions/38443764965179650/agents/s_000cQhYGzOk/artifacts/screenshots`（01-workflow-mode … 05-missing-diagram-warning）。
- 文件夹上传截图：`/Users/skyelan/Library/Application Support/Doubao/Default/.doubao/agent_mode/workspace/.sessions/38443764965179650/agents/s_000c20T6OVW/artifacts/pw/screenshots`（01-folder-upload-success、02-upload-in-progress、03-failure-summary、04-search-truncated-notice、05-search-no-truncation）。
- 验收截图：`/Users/skyelan/Library/Application Support/Doubao/Default/.doubao/agent_mode/workspace/.sessions/38443764965179650/agents/s_000cQylnHmF/artifacts/pw/screenshots`（含 01e-search-cap 与 regress-*）。

## 待用户确认（未验证）

- 真实公网 SSH 远端主机稳定性：未提供主机/凭据；仅证明 localhost 容器内 sshd e2e。取消/超时在真实跨网、长尾会话下的远端进程回收稳定性待真机验证。

## 既有、已记录、不在本批范围（未修）

1. CSP `style-src 'self'` 全局阻断 mermaid 内联 SVG 样式（mermaid 节点已创建，降级渲染）。
2. workflow.go `rewriteDocIndex` 多字节切片在索引条目名留下孤立 0xB7 字节（前端 `ParseRCAIndex`/`cleanRCAName` 已中和）。
3. `assignSessionNumber`/`repairSessionNumbers` 把 `NextSessionSeq` 持久化到 flat-root `data/settings.json`，而读取方用 `config/settings.json`（settings 路径问题，同时影响布局检测）。
