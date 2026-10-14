# 暂停任务会话隔离 · 2026-10-14

用户报告点击回复右上角“暂停任务”后所有会话崩溃。本轮检查时 AIDE 进程运行、restart count 为0，没有看到对应 panic/restart 日志；没有复现真实模型运行中的全部会话崩溃。确认的代码风险是暂停在共享会话互斥锁内取消任务，取消过程可能阻塞其他会话接口；前端回执也没有校验原会话归属。本次修复这些风险，不将其表述为已确认的唯一根因。

## 行为与提交

- `6067595`：先在锁内验证目标会话/任务并保存暂停意图，再释放共享锁后取消该任务。重复的待暂停请求返回202，不重复调用取消。其他任务的上下文和状态保持。
- `1fba0d5`：移除回复头部的暂停按钮，统一使用输入区停止按钮旁的暂停入口；尚无检查点、请求中或已请求暂停时禁用。请求捕获原会话、任务、工作区，切走后的晚回执或错误不改当前会话。历史已暂停任务保留对应恢复入口。
- 两批均已推送 `fix/pause-session-isolation`；没有创建 Release 或合并 main。本机旧仓缺少规范仓已有的输入区暂停按钮，本轮仅补入该按钮、相应样式和监听，不覆盖其其他开发工作。

## 实际验证

两仓 quick 均通过。隔离 Linux Docker、模拟 HTTP 模型、Go race 下运行：

```sh
go test -race -mod=vendor ./internal/server -run '^(TestPause|TestResume|TestToolLoopCheckpoint|TestInterruptedTask|TestConversationParallel|TestSessionModelSelection|TestGlobalApprovalModeAssistedDoesNotMarkPaused)' -count=1 -timeout=4m
```

规范仓通过18.095s，本机兼容仓通过18.784s。测试覆盖：A真正进入模型请求后暂停并保存检查点，B仍运行并独立完成；取消函数故意阻塞时B的GET仍200；跨会话任务ID404、重复暂停不再次取消、暂停意图在取消前落盘，以及既有恢复/并行/配置回归。初始新夹具的清理函数重复关闭通道及模型请求标记匹配过窄分别修正后重跑；它们不是运行服务崩溃证据。

前端VM覆盖重复点击、晚成功/晚失败、切换会话/工作区、失败重试、检查点和小秘状态。原生Safari打开 `python3 scripts/fixtures/render-pause-controls.py` 生成的离线夹具，观察实际生产输入区按钮与处理函数：等待回执禁用、切到B不被A回执改动、暂停B后A仍可用，以及深色/浅色、390px容器布局。夹具只模拟请求，不连接模型。实际8097浏览器仍受本机证书提示限制，需要用户手动处理；没有绕过提示或进行真实模型调用。

## 本机8097

更新前只读鉴权检查：1个既有小秘会话，0个运行/待澄清任务。只停止并重建Compose `aide` 服务，其他容器未重启；原数据卷、home卷和bind挂载保持。先停AIDE再只读挂载数据卷备份，备份验证后启动候选镜像。

- 运行镜像 `sha256:2cf54f033f8a92292faf057b05ac53c060f6af6ba3f6844203527637217dae35`，healthy。
- 上一版保留为 `aide:pause-isolation-previous-20261014`，核验ID `sha256:10e35d72c80bbf89d0dda8c1fdafbce656a60aa27dfface106471c3f6e740647`。回滚时重新标记为 `aide:local` 后仅重建AIDE，不自动覆盖数据卷。
- 备份在本机忽略目录 `.agent-state/pause-session-isolation/data-before-update.tar.gz`，14305字节、39条，tar可读且非空，SHA256 `71fe74a9d4e30420f00abc80193a7de6b0a1bb9148e24994eb07531c190d8867`。包含服务数据，不提交内容。
- `/healthz`、鉴权后的 `/api/sessions`、`/api/config` 均200。不存在的暂停目标404。首页含输入区暂停按钮；实际提供的app.js、index.html、style.css、en.js逐字节匹配本机构建源码，app.js已无回复暂停动作。

镜像来自保留既有未提交工作的本机兼容仓，不宣称来自规范仓干净提交。日志和脱敏smoke收据保留在 `.agent-state/pause-session-isolation/`，任务见[账本](../tasks/pause-session-isolation-20261014.json)。线上普通会话/真实模型复现仍未验收。
