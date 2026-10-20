# 大文件传输验收 · 2026-10-20

源码提交 `dfc772336984ada3b905cd0ad19a2598abc34fc9` 已推送 origin/main。基线 `bb25bdf3fbe34ac4482e45b769d3c05c7ff1a7d6`。客户端为云环境中的 Codex；Go1.26、Linux amd64、系统 Chromium。使用 backend/frontend/security skill 顺序自审，没有启动子 Agent。

## 实际结果

- 浏览器 `AIDE_LARGE_TRANSFER_BROWSER=1 AIDE_EVIDENCE_DIR=… go test ./internal/server -run '^TestLargeTransferBrowser$' -count=1 -v -timeout=5m`：PASS，26.464s。实际嵌入式 Aide 页面和后端，67MiB+39文件通过上传、复制到、移动到、下载；上传和下载最高同时4个真实请求。暂停并真实刷新后，编号0分块各只请求一次；下载每块SHA256与原文件相同，Go再次核对上传和移动目标的完整摘要。
- 保存选择器替换成浏览器OPFS文件句柄；实际HTTP、随机写盘、IndexedDB句柄/分块摘要、页面刷新均真实执行。没有验证系统原生保存弹窗或跨浏览器文件API。不能把这个测试称为生产启动器部署。
- `source /workspace/.aide-cloud/env.sh; python3 scripts/test-large-transfer-ssh.py`：PASS，7.190s。一次性仅监听回环的OpenSSH服务器，临时Ed25519密钥与独立master socket；65MiB+37断点上传、SSH到本地复制、本地到SSH移动、远端Range下载校验。未使用生产SSH目标、账号秘密或现有控制连接。该认证测试只测公钥，密码认证沿用现有实现。
- SFTP子进程协议测试额外覆盖不支持fsync的服务端、随机顺序写入、重启恢复、静态父路径符号链接拒绝。5GiB稀疏文件Range覆盖64位长度和偏移，不代表传输了5GiB有效载荷。
- 桌面1440与窄屏390、blue/comfort各明暗主题截图通过；标题/关闭按钮保持可见，Escape关闭传输中心。检查运行中控件使用稳定DOM，不在轮询时整体替换按钮。

服务端竞争检测、兼容与静态检查的最终命令及结果记录在本目录 `verification.json`，原始日志在 `.agent-state`。

## 证据

[result.json](result.json) 是浏览器机器结果；[暂停上传](upload-paused-1440.png)、[桌面完成](transfers-complete-1440.png)、[窄屏完成](transfers-complete-390.png)、[窄屏浅色](appearance-blue-light-390.png)、[经典深色](appearance-comfort-dark-1440.png)。均为一次性测试数据，没有真实用户文件或令牌。

## 发布边界

当前仅交付源码。start.command仍被此前环境20GiB可用空间门槛阻止；没有绕过启动器门槛宣称部署成功。真实页面由隔离testApp+httptest启动，测试结束后关闭浏览器和服务；临时SSH进程/密钥/socket由专属runner清理。没有重启已有用户服务，没有新建Docker镜像、tag或GitHub Release。

快速检查保留两项已知基线失败（launcher数据所有权断言、offline浏览器opener断言）；未将它们写为通过。完整Docker发行验证与公开镜像门禁未通过，不进入版本或镜像发行。

旧批量上传移除请求总预算曾被自动审批拒绝，原因为单个请求可能无限消耗磁盘资源。最终方案保留旧请求保护，文件管理改用固定4MiB分块、8槽上限及目标磁盘空间检查；不是绕过该拒绝。
