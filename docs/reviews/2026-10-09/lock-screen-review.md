# 锁屏复核 — 2026-10-09

## 范围

用户报告解锁后再次锁住、刷新状态不一致、知识星图未同步锁屏。本轮修正源代码，未更新运行中的 9999 服务，也未推送或发布。

## 修正

- 身份验证与持久状态解锁在同一后端验证路径提交；前端等待成功保存后才揭开遮罩。
- 解锁必须携带当前代际，旧请求不能清除新锁定；配置密码时禁止直接 PUT 未锁状态。
- 文件页跟随后续锁定；星图与其他可见页每 5 秒复核服务端，恢复前台立即复核。
- 工作台优先接管先打开的星图；主页面退位停止心跳，新标签页不会重置从页的本地解锁。
- 空闲计时跟随角色交接启动；读取配置不延长计时，解锁后重新计时，避免旧计时器立即重锁。
- 状态保存失败显示错误；有密码且状态损坏时保持遮罩；无密码不被历史锁定困住。

## 实际执行证据

| 验收 | 结果 | 范围 |
| --- | --- | --- |
| `node scripts/lock-cluster-state.test.js` | PASS | 实际集群源码在多页面 VM 和虚拟时钟中运行；主从接管、星图同步、局部解锁、新页面加入、刷新、写失败、旧请求、服务端恢复、乱序 GET |
| `node scripts/lock-cluster-election.test.js unlocked/locked/down` | PASS | 三种服务端可达性与锁定状态 |
| `go test -mod=vendor -race -count=1 ./internal/server -run "TestLockState|TestMasterAuth"` | PASS | Docker aide:local；持久文件、请求校验、错误密码、旧代际、密码验证成功后保存解锁 |
| `CGO_ENABLED=0 go build -buildvcs=false -mod=vendor -o /tmp/aide-lock-review ./cmd/aide` | PASS | Docker 内编译当前源码；产物未安装到运行服务 |
| Node 语法检查 app.js / lock-cluster.js / starmap.js | PASS | 语法检查 |
| `git diff --check` | PASS | 工作树格式检查 |
| Safari 实际页面与 Touch ID 硬件 | NOT_RUN | 本轮未启动新候选服务，未执行真实浏览器锁屏验收 |
| 全量回归 | NOT_RUN | 本轮仅执行锁屏与关联身份验证测试 |

## 边界

锁屏是视觉保护，不撤销 bearer API 权限、不停止后台任务。VM 回归不等于真实浏览器、操作系统后台节流或 Touch ID 验收。生产生效、浏览器验收和发布均未完成。

## 后续用户反馈

同轮追加 SSH 引用目录无法选择、Markdown 图片失效：已新增请求级目录浏览与来源一致的图片请求。详见 `docs/workspace-paths.md`。新增 `source_browse_test.go`、`scripts/markdown-image-path.test.js`；真实 SSH/浏览器验收未执行。
