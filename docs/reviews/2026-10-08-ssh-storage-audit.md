# SSH 目录访问与存储配置检查

日期：2026-10-08。任务：`ssh-storage-audit-20261008`。起始源码：`912cc06f49167f6789f1007547fc0b128f7dbfa0`。

用户报告“访问目录时显示链接失败”。未取得用户服务器的完整报错、连接参数或访问目录，因此本轮不能把隔离环境中定位的问题直接认定为该服务器的唯一根因。检查与修复对象为当前候选源码；现行运行实例未替换。

## 配置结论

| 内容 | 当前支持 | 独立服务器 |
| --- | --- | --- |
| 工作空间 | 本机目录或一套 SSH/SFTP 连接 | 可以选择 A |
| 自动系统文档来源 | 本机目录或工作区服务器上的另一个目录 | 远端共用 A，无 B 的连接字段 |
| 项目缓存 | 本机目录或工作区服务器上的目录，附本地工作副本 | 本机或 A，无 C 的连接字段 |
| 辅助参考文档 | 多个独立 SFTP 来源 | 可以 B、C 等 |
| 会话、应用配置、凭据与提取缓存 | 应用数据卷或进程内 | 不受项目 docs/cache 设置迁移 |

因此可配置“工作空间 A、辅助文档 B、缓存 A 或本机”，不能声称原生支持“工作空间 A、自动系统文档 B、项目缓存 C”。宿主网络挂载与远端星图/RAG 索引未在本轮实现。操作说明见 [SSH 与存储配置](../architecture/ssh-storage.md)。

## 源码发现与修复

| 发现 | 修复 | 文件 |
| --- | --- | --- |
| 多个首次请求可同时建立同一 master，竞争 socket/临时凭据 | 每个 socket 的建立与关闭使用可取消门闩；已建立通道仍可并行 | `internal/server/ssh_session.go`、`server.go` |
| 连接切换时旧的建连可能在旧连接清理之后完成 | 配置变化推进连接代次；过期建连拒绝复用/发布旧 master | `ssh_session.go` |
| 来源建连完成后未复检，工作区 askpass 未及时清除 | 来源复检，连接时限和保活，成功/失败均清理临时认证文件 | `ssh_session.go` |
| master 突然失效时目录读取直接失败 | 仅对明确传输断开的只读目录查询重连读取一次，不重放写批次 | `ssh_session.go` |
| 空远端工作区 path 被用于 `cd ""` | 留空对应远端账户主目录 `.` | `workspace_config.go` |
| 文档/缓存的远端绝对路径被错误拼接到工作区根下 | 单独解析配置目录；保持文件请求必须相对且通过路径校验 | `workspace_config.go`、`sources.go`、`ssh_session.go` |
| 自动系统文档的创建目录与文件传输仍使用旧的绝对路径拼接 | 创建、读写与传输身份统一使用配置目录解析 | `files.go`、`file_transfer.go` |
| 同主机/用户仅换来源密码或密钥仍复用旧认证连接 | 凭据更新/清除也使来源 master 失效 | `sources.go` |
| 加密来源凭据的部分更新仅从明文回退读取，可能丢失未提交的私钥或密码 | 从旧 vault 合并字段；锁定或损坏的部分更新整批先拒绝，空更新不清除 | `sources.go` |
| SFTP 列目录输出 `Permission denied`，但进程退出 0 | 按诊断行识别错误，不把权限拒绝显示为成功的空目录；不以普通文件名中的错误词判失败 | `ssh_session.go` |
| OpenSSH 对新文件返回 `File "..." not found.`，被误报保存冲突；`Couldn't stat` 也可能是权限错误 | 新增明确不存在格式，拒绝以单独 `Couldn't stat` 放行新建 | `files.go` |
| 远程命令包装用尾部 `rm` 覆盖退出状态；真实 `setsid` fork 后父进程提前返回 0 | EXIT trap 清理、`setsid -w` 等待子会话，保留普通/PTY 命令真正退出码 | `ssh_session.go` |
| 取消后进程组清理脚本拼接成 `thenkill`，语法失败被忽略 | 修复拼接，补实际 shell 执行与进程状态验证，取消错误附清理回执 | `ssh_session.go` |
| 加入连接快照锁后，SSH 提案应用原来全程持有状态锁会自锁；文件锁顺序也需统一 | SSH 提案仅网络 I/O 释放状态锁，固定连接代次，写入回执及前后状态复核 | `workflow.go` |

最后一项为本轮修复过程中发现并处理的锁兼容问题，不称为已发布版本原有的自锁故障。提案应用仍是逐文件原子操作，多个文件之间不提供整体事务。

## 验证方法

真实 SSH/SFTP 验收入口：

```sh
bash scripts/test-ssh-workspace.sh
```

使用缓存测试镜像启动两个不同的 OpenSSH 服务器与一次性客户端，内部 Docker 网络、不暴露端口、不挂用户数据/凭据，源码只读。默认 `-race -count=2`。测试为 opt-in；直接运行普通 Go 测试而未提供 fixture 环境变量时会跳过真实服务器部分。

覆盖远端登录主目录、中文与空格路径、8 路并发首次列目录、master 关闭后恢复、密码和加密 Ed25519 私钥（导入/引用）、调用方超时、目录不存在/列目录与写权限拒绝、服务器 B 的独立来源读写、A 的相对/绝对系统文档、绝对缓存的建档/拉取/字节一致性、`false`/`exit 7` 的退出状态及取消后的远端 shell/子进程清理。

另有本地替身回归检查目录读取断线后重试、写批次不重放、取消等待、临时凭据清理、过期连接及提案状态边界。源码前后 SHA 必须一致；编辑中运行不作为最终验收。

## 验证结果

最终真实 SSH/SFTP 验收已通过。初始环境运行分别因 helper 编译未完成和 `/tmp` 无执行权限停止，不属于 SSH 行为验证；临时客户端 `/tmp` 已明确允许执行 Go 测试程序。

稳定的真实诊断运行先揭示 SFTP 退出 0 的权限错误、新文件不存在格式未识别、命令退出码为 0，以及取消后 shell/子进程仍可运行。修复前三项后的两轮运行仍在取消清理失败，源码 SHA 前后一致；不能把 11/12 子用例通过表述为整轮通过。完整原始日志保留在 `.agent-state/ssh-storage-audit/live/`。

最终增补文档目录创建/复制/移动、来源私钥部分更新后重连用例时，第一轮因自有 fixture 账户缺少 `.ssh` 目录失败。脚本补齐目录与账户权限后，整个用例重新重复两轮；不把该准备失败当作产品通过。

| 检查 | 最终结果 | 证据 |
| --- | --- | --- |
| 两台真实 OpenSSH/SFTP，`-race -count=2` | 12 个子用例 × 2，24 项全部 PASS，14.263 秒 | `live/aide-ssh-test-20261008122411-9386.log`，`ssh-live-final.json` |
| 取消后远端进程 | 两轮均 `AIDE-CLEANUP:reaped`；shell 与 sleep PID 均 `absent`，约 2.54 秒 | 上述真实日志的 `cancellation_stops_remote_process_group` |
| SSH 生命周期/退出码/清理 focused `-race` | PASS，40.385 秒；含实际 Linux 受控进程组和 `/proc` 检查 | `ssh-reliability-focused-cleanup.json` |
| 来源保险库回归 `-race` | 10 项主测试、2 项子测试 PASS，7.605 秒 | `source-vault-patch-race.json` |
| SSH 审批应用 `-race` | 6 项新增主测试、5 个状态子项 PASS，已有审批冲突/持久化用例也 PASS | `final-server-race.jsonl` 中 `TestSSHApply*` 与 `TestWorkflowApprovalConflictAndPersistence` |
| 后端其余测试 `-race` | 分批通过 503 项主测试、128 个子测试；首批 498 项后达到 300 秒时限，剩余 5 项补跑 PASS，4.958 秒；1 项基线超时排除，2 项 opt-in 未启用 | `final-server-receipt.json`、`final-server-race.jsonl`、`final-server-tail-race.jsonl` |
| `go vet` 与 `cmd/aide` 编译 | PASS；命令入口包无测试，不算运行入口行为验收 | `final-vet-command.log` |
| 开发 quick 检查 | PASS | `final-quick.log` |

证据路径均相对本机 `.agent-state/ssh-storage-audit/`，原始日志不发布。最终真实运行日志 SHA-256 为 `ab863f48a1af62ca8d2df9f0379b2796af9817b2829661d505fa547dfebd9541`；运行前后全部 Go、测试脚本、go.mod/go.sum 指纹相同，输入清单 SHA-256 为 `6aec4c383e21aa531149e02bf66739268377d1f696082c7d867bbb7edd9c6a59`。

全量 `internal/server -race` 诊断在 `TestChatHistoryAndCancelEndpoint` 的 `httptest.Server.Close()` 超时，该轮未通过。使用起始 HEAD 的 `git archive` 独立只读快照，在同一禁网 Docker 环境单独运行该用例，仍于 60.089 秒超时。堆栈为后台 `summarizeTopic` 请求未退出，测试 mock 等请求取消，而 `p.Close()` 先于应用的测试 cleanup 执行；无本次 SSH 修改也会复现。本轮不改此聊天测试。

跳过该用例后首批仍在 300 秒总时限到达时停止，最后 5 项另行补跑通过。两批没有失败的测试结果事件，全部未完成用例已由测试列表逐项比对补齐；这属于分批覆盖，不能称单次全量通过。两项 opt-in 为 `TestSourcesLiveProtocols`（未运行本轮范围外的全部来源协议）和 `TestSSHLiveWorkspaceAndIndependentSource`（已在真实双服务器中另行通过两轮）。两批 Go 输入指纹一致，输入清单 SHA-256 为 `37de8e9d2be10a98f9d4e38adff67d475b42ef127c152903ba9f14e8d6f60080`。基线临时快照、SSH 临时容器与网络已清理。

## 发布轮补充：重启后死socket恢复

以下是后续 `release-20261008` 轮的补充结论；上文24项、分批覆盖及原始超时保留为原实施轮历史，不用后续结果改写旧轮证据。完整发行门禁与最终候选状态见[发布记录](release-0.1.17.0-RC1-2026-10-08.md)，联调由[发布联调报告](2026-10-08-release-integration.md)保存实际回执。

重启联调确认凭据保险库和 `hasSecret` 正常，但控制socket残留且无监听进程，阻止独立SFTP master重建。这是隔离重启联调实际定位的问题，不能直接认定用户原始“链接失败”的唯一原因。

最小修复在 `internal/server/ssh_session.go` 的工作区与独立来源ensure共用 `prepareSSHReconnect`。控制检查失败且调用方未取消后，持有socket生命周期门闩并检查Unix socket类型；最多1秒探测仅在 `ECONNREFUSED` 时进入清理。清理前复核inode／修改时间未变化及调用方未取消，再移除socket和对应临时凭据文件后新建master。活跃监听、超时／取消、非socket路径、被替换的socket均保留，不因一次控制检查失败删除连接，也不向可能已替换的socket发送退出命令。

| 补充检查 | 当前结果 | 范围 |
| --- | --- | --- |
| 双真实OpenSSH/SFTP、race重复两次 | 14项×2=28／28 PASS，15.848秒 | 原12项加工作区与独立来源各1项真实死socket恢复；恢复后读取到对应服务器内容 |
| 活动socket及超时保留、race重复两次 | 4项×2=8／8 PASS | 工作区／来源的活动socket在控制检查失败或超时后保留；不启动或退出master |
| 新候选重启、来源API及身份持久化 | PENDING | 补充修复候选二进制SHA-256为 `b0ee07874a88ed119071d100b893c3735ff1af4a0538c9568b2a3bb488d59a1d`；重跑97项API及persist/origin正在进行 |
| 修复后新的完整门禁与发布 | PENDING | 修复前19项full通过不替代新源码门禁；版本、标签、镜像和附件仍由父release任务核对 |

后续多来源任务已经接入远端星图、原文／RAG及Office只读检索；原实施轮未做的这部分不再作为当前源码能力缺口，具体验收范围见上述联调与UI报告。用户服务器实际故障、Safari保存工作区配置/长空闲/公网波动以及原生三台服务器存储仍未被这些夹具结果覆盖。

## 原实施轮尚未覆盖的范围

以下是当次测试的历史范围；本轮后续补充结果见上一节及父发布记录。

- 用户服务器的具体目录、账号权限、防火墙、网络延迟、掉包和长时间空闲；需对应实际完整报错继续核对。
- 新源码在当前 Safari 界面中的保存、浏览和恢复操作；未完成浏览器验收。
- 发布、镜像构建、当前实例替换、提交或推送；本轮没有执行。
- 文档/缓存第三台服务器原生配置，以及远端文件参与知识星图/原文/RAG 索引。
- `workspace-config/test` 只验证工作区认证和根目录读取；不会证明 docs/cache 可写。缓存初始化部分 `MkdirAll` 错误仍可能不在配置保存时报告，使用时会返回具体失败。
- SSH 工作区身份仍采用 mode/path/host，端口及用户名未纳入历史 identity；不能把同主机不同端口/用户的缓存隔离视为已验证。
- 普通目录浏览和编辑器读写保持并行，配置检查与通道建立间仍有切换窗口；仅本轮 SSH 提案应用绑定连接代次，不能称所有远端 I/O 完成隔离。

隔离测试容器、网络和 fixture 数据退出后删除。测试日志与源码 SHA 保存在本机 `.agent-state/ssh-storage-audit/`，专用 Go 编译缓存保留供复跑；均不进入源码交付。
