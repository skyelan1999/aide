# 本机 8097 更新回执 · 2026-10-14

本次仅更新 `aideMacMini/aide` 的 AIDE Compose 服务。旧容器此前返回退出事件故障，本轮重新检查时已是 exited/137，8097拒绝连接，因此不再需要重启Docker Desktop。执行 `docker compose up -d --no-deps aide` 后，AIDE容器运行且健康；原 `/data`、`/home/aide` 命名卷与工作目录挂载保持一致。没有对Docmost、PostgreSQL、Redis发出重启或修改操作。

- 来源：规范仓 `feat/conversation-parallel` 的后端 `22fb899`、前端 `0ab2d34`，连同此前已镜像到本机的复制/高亮、上下文、星际日志、插话、Git比较、Linux属主、会话输出框和品牌悬停修复。
- 实际构建来自本机开发仓，包含既有未提交工作；不将该镜像宣称为上述规范仓commit的干净可重现发行构建。两份源码的当前quick及并行调用定向race通过，本机Compose构建成功。
- 运行镜像：`sha256:1657ad320d9648ac9b3e249d09a9f5ac7090e1dbffebb02ebe057041bc9f917e`。
- 启动前备份：本机忽略目录 `.agent-state/conversation-parallel/data-before-update.tar.gz`，只读挂载原数据卷生成。14304字节、39个归档条目，tar可读且非空；SHA256 `9d4e9c769ec3dc0b7a4faad0e8c8ca985e687d05778a3db92966bc135eb06027`。备份包含服务数据，不提交Git、不公开其中内容。
- 运行只读冒烟：`/healthz`、鉴权后的 `/api/sessions`、`/api/config` 均HTTP200并返回JSON。新首页包含 `chat-parallelism`；`parallel-chat.js`、`conversation.css`、`macos.css` 服务字节与本机源码逐字节一致。
- 浏览器边界：Safari打开实际 `https://127.0.0.1:8097/` 停在本机证书警告，需要用户手动处理；没有自动绕过。离线组件验收已做，但实际工作台交互、真实模型效果和真实提速比例仍未验收。没有向真实模型发送测试请求。
- 回滚边界：旧容器引用的 `c0a441...` 镜像已经无法通过Docker image inspect找到，不存在已验证的一键旧镜像回滚。需要按任务提交revert源码并重建；数据备份保留供人工审阅恢复，不自动覆盖当前卷。

详细运行日志、静态资源哈希和只读API回执留在本机 `.agent-state/conversation-parallel/`，仓库只记录脱敏结论。此回执更新此前“8097部署受阻”的状态，不追认此前未做的浏览器或模型验收，也不代表GitHub Release或main合并。
