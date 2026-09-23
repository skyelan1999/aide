# 客户端接入

打开项目时以 aide 为工作目录。先读对应入口，再使用同一个任务 JSON。
首次必须回报 workflow v1、任务号、当前阶段及能力限制，不能只说“已遵守”。

| 客户端 | 入口与接入方式 | 本次验证边界 |
| --- | --- | --- |
| Codex | 根 AGENTS.md，项目规则入口 | 本地文件已配置；下次会话核对加载 |
| Claude Code | 根 CLAUDE.md 引用 AGENTS.md | 文件已配置；未启动 Claude 实测 |
| DeepSeek DSH | 根 AGENTS.md；工作目录必须指向 aide | DSH 源码有工作目录指令加载；未给当前长会话注入 |
| 豆包 | `python3 scripts/agent-route.py prompt doubao` 输出作为开场指令 | 通用手工适配；不假定支持自动读取磁盘或执行命令 |
| WorkBuddy | 根 CODEBUDDY.md；不能确认自动加载时用 `prompt workbuddy` | CodeBuddy 官方支持此文件，WorkBuddy 具体客户端必须握手确认 |

普通聊天版 Claude/DeepSeek 同样使用 prompt 命令。没有文件工具的客户端，将入口、工作流和任务记录作为附件/文本提供；必须明确不能执行的动作，将产出交还可操作的客户端执行，不伪造本地测试。
`prompt <client>` 只输出公共流程文本，不自动发送到外部服务，不读取本地密钥或生产会话。
不要用五份复制的规则维护五套逻辑，也不要修改各客户端私人全局记忆。

参考（核对日期 2026-09-23）：
- Claude 官方：[CLAUDE.md 项目上下文](https://support.claude.com/en/articles/14553240-give-claude-context-claude-md-and-better-prompts)
- CodeBuddy 官方：[Rules](https://www.codebuddy.ai/docs/ide/User-guide/Rules)
- WorkBuddy Enterprise：[记忆入口](https://cloud.tencent.cn/document/product/1831/137011)
- DeepSeek 官方：[Harness](https://github.com/deepseek-ai/deepseek-harness)；本机源码 apps/cli/tests/profiles/headless/tests/workspace-context-resume.expected.e2e.ts。
