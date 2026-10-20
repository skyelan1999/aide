# 命令底栏 SSH 限制与交互修复

用户报告底栏命令显示工作区受限。根因不是缺少 SSH 传输：服务端 `shellSandboxPolicy` 目前在受限模式拒绝远程 shell；另外权限页的通用 segmented 控件原先只调用 `aideUI.set()`，把沙箱模式写入浏览器偏好，没有更新服务端，造成界面选择和真实权限不一致。

本次增加沙箱专用控件，读取 `/api/config` 中真实模式，显式选择时经 `/api/settings` 保存并刷新，完全访问需确认；取消不写入，旧 localStorage 不覆盖服务端。底栏提前提示限制和权限入口，显示真实工作区位置，拒绝前保留草稿；服务端拒绝策略未修改，也没有替用户自动切换模式。完全访问同时作用于 AI 与命令面板，不是手动命令专有权限。

上一轮通用 action 的表单锁会覆盖整条命令输出流，阻止后续 SSH PTY 输入。命令面板改为使用已有的 commandAbort / commandSessionID 状态控制，不套用通用长请求表单锁；会话建立后可点击“发送”或使用 Enter，Tab/Ctrl+C 保持原始字节；停止后恢复按钮。HTTP 拒绝保留输入，并在终端记录错误。

## 实际验证

- 浏览器：真正嵌入的 Aide 页面；1440 / 390 截图已查看；10 项检查通过，包括本地旧偏好不覆盖真实模式、取消不写入、显式保存并刷新保留、原 NDJSON 流未关闭时持续输入、发送按钮/Enter/Tab/Ctrl+C、停止恢复、403 留草稿。
- 夹具边界：工作区 SSH 元数据为模拟；持续输出端使用自建 io.Pipe，输入经过生产 `commandInput` API。没有执行真实 SSH 远端进程，没有接触用户设备或配置。
- Go：受限 SSH 命令端点在连接前对两种受限模式 × 两种交互标记返回 403；现有精确字节转发、Tab、关闭会话、登录 shell、沙箱远端边界测试通过。
- Node：翻译、通用异步反馈、发送/切换延迟、设置即时打开、身份认证路由等相关脚本通过；全仓 quick 及源码指纹见 verification.json。

本机内核未提供 Landlock（ABI=0 / function not implemented），扩展执行 `TestSandboxCommandPanelEnforcement/workspace-write` 返回预期的拒绝状态，导致该已有正向测试失败；read-only 拒绝与完全访问本地执行分支通过。未为测试绕过限制，不能声称本机支持受限本地 shell 的正向执行。既有启动器 quick 失败沿用上一任务的基线结论。

本轮交付源码修复；未执行完整 Docker 发布门禁、镜像 Release 或生产部署。原有十份未跟踪插件任务文件保留。
