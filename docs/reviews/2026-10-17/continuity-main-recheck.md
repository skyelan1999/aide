# main 同步后的连续性复核

本轮基线为 `ceb824814034860d1881ded6d27394ebd12fef9d`。用户要求撤销本地额外合并、只同步 main；已执行并核对 HEAD 与 origin/main 一致。未把其他功能分支重新合入。后续复核在 `test/continuity-main-recheck-20261017` 进行。

## 当前源码

以下专项通过，使用 Codex 安装的 Node 运行时；系统 PATH 未提供 Node，初次命令未能启动测试，随后使用明确路径完成测试。

- `scripts/reasoning-settings.test.cjs`：模型设置不重复提供推理强度控件，保存模型时不覆盖已有推理设置。
- `scripts/session-model-selection.test.cjs`：会话选择隔离、同会话保存串行化、旧响应不覆盖当前会话、较新远端版本优先、409 后不继续提交过时队列。
- `scripts/context-meter.test.cjs`：会话／工作区切换、失败重试、缓存与防抖。
- `scripts/session-run-controls.test.cjs`：队列、暂停／停止和终态切换。

这是源码组件回归，不能替代新版完整工作台中的保存、刷新与磁盘核对。先前推理设置真实 Safari 验收仍是其当时版本的证据，见 [原验收记录](../2026-10-16/workbench-live-reasoning.md)。

当前源码 `verify quick` 全部通过，指纹 `b8f1c24b69d9a53904ebff54548d26d23a5c2361efb2331d7ecbc24e639e1883`，日志 `.agent-state/verify-20261017T055310854198Z.log`。本轮没有执行完整 Go race/vet；本机旧 full 收据指纹不同，不能复用于当前源码。

## 既有专用成果记录夹具

对上一轮专用测试会话完成后续检查：已导入记录的来源为 `operator_import`；使用新快照重复导入返回 `duplicate: true`，记录数保持 1；使用导入前旧快照返回 409；提交伪造 `source` 字段返回 400。所导入的是已有 RC2 的历史发布记录，不代表测试任务产生了该资产，也不代表当前部署已独立核验。

随后移除该专用会话、临时提供商与模型，终止仅绑定容器回环地址 19056 的自有监听进程。再次独立检查会话返回 404、临时模型和提供商不存在、端口不再监听。原设置除会话顺序号 52→53 外一致，加密保险库原有条目一致。

实际 Safari 关闭该专用轨迹后，恢复原有 #39 会话，任务输入为空，引用仍为自动系统文档；没有新增或关闭用户标签页。

原始私有证据保留在 `.agent-state/outcome-release-native-20261017/negative-receipt-proof.json`、`fixture-cleanup-proof.json` 和 `provider-stop-proof.json`；无密钥或用户会话内容入库。

## 运行与交付边界

本机现有 `aide-aide-1` 与 `aide-cad-1` 健康运行，镜像为 `sha256:6a8a2dcb290be897de5a27ce8a10436d92dd601e9fedef9153a4621b4f4c0107`，镜像源码指纹为 `40fda58e51eed211b13ea23e6638181694d06235f901ec33679d49c0f68def24`。本轮未替换或重启服务。该镜像不是刚同步的 main；其 MCP／成果夹具运行证据不能升级为当前 main 的运行验收。

原始十二项工作流仍进行中。新版完整浏览器验收、其余连续性与知识路径缺口、精确源码完整验证和发布门禁继续推进；Windows 由用户验收，真实向量测试暂缓。本轮不推送 main、不发布 Release、不更新默认镜像清单。
