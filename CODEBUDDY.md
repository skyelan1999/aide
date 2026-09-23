# CodeBuddy / WorkBuddy 兼容入口

首先完整读取 `AGENTS.md` 和 `docs/agent/WORKFLOW.md`，使用共享任务记录。
当前 WorkBuddy 客户端是否自动加载本文件，必须在首次接入时确认；不要推定与 CodeBuddy 相同。
未自动加载时，将 `python3 scripts/agent-route.py prompt workbuddy` 的输出作为任务开场指令。
保留 `.workbuddy/memory/` 的本地历史，不将其上传、删除或覆盖为项目规则。
