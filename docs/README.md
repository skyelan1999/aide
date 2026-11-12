# Aide 文档中心

维护日期：2026-11-12。当前说明对照源码 `5bec2477`，覆盖 `0.1.19.0 RC1` 发行准备；实际公开状态与附件见 [Releases](https://github.com/skyelan1999/aide/releases)。本轮新增能力不视为旧 RC3 安装包已有功能。

## 使用 Aide

| 目标 | 入口 |
| --- | --- |
| 了解产品与快速开始 | [项目首页](../README.md) · [English](../README.en.md) |
| 安装、启动与源码构建 | [安装说明](installation.md) · [启动器接口](architecture/launcher-contract.md) |
| 配置项目、文件、模型与任务 | [用户指南](user-guide.md) · [功能说明](features.md) |
| 设置工作目录、引用与缓存 | [工作目录配置](workspace-paths.md) · [SSH 与存储](architecture/ssh-storage.md) · [远程目录中继](reference-relay.md) |
| 管理文件与大附件 | [Markdown 编辑与导出](architecture/markdown-editing-export.md) · [文件传输](architecture/resumable-file-transfers.md) · [大 Markdown 附件机制与验收](reviews/2026-11-09-large-markdown-attachments.md) |
| 理解模型窗口、摘要与记忆 | [上下文恢复](architecture/context-recovery.md) · [长会话支持](industrial-long-conversation-support.md) |
| 浏览知识、代码和文档 | [知识星图](architecture/knowledge-map.md) |
| 使用浏览器与电脑工具 | [控制架构、审批和平台边界](plugins/computer-browser-control.md) |
| 配置语音 | [语音架构](architecture/assistant-openai-speech.md) |
| 更新、备份与排障 | [开发运维交接](../HANDOVER.md) · [软件更新](architecture/software-updates.md) · [诊断](architecture/diagnostics.md) |

## 理解和开发 Aide

| 目标 | 入口 |
| --- | --- |
| 阅读技术原理、设计取舍和证据 | [Aide 技术白皮书](architecture/aide-technical-whitepaper.md)，作者 Skye Lan |
| 查架构、模块与 API | [架构索引](architecture.md) · [软件模块](architecture/modules.md) · [代码目录](architecture/code-layout.md) |
| 查需求和历史编号 | [PRD](PRD.md) |
| 理解执行和并发 | [执行策略](architecture/execution-policy.md) · [自适应并行](architecture/adaptive-parallel.md) |
| 开发工具、插件和工作台 | [插件协议](plugin-protocol.md) · [插件市场架构](architecture/plugin-market.md) |
| 使用 AI 客户端参与开发 | [统一 Agent 工作流](agent/WORKFLOW.md) · [客户端接入](agent/ADAPTERS.md) |
| 选择测试与交付检查 | [Release 验收](testing/release-validation.md) · [测试目录](testing/test-cases.md) |
| 维护文档或独立产品站点 | [文档分层和维护规则](documentation-map.md) · [网站说明](website.md) |

全部项目文档统一在 `docs/`，不再创建 `doc/`。英文操作入口见 [English documentation](en/README.md)。

## 证据与历史

[任务账本](tasks/)、[审查与验收](reviews/)、[发行记录](releases/)和[历史档案](archive/)保留各自日期、提交和失败信息。它们用于追溯，不能把某次测试通过扩展为当前源码、所有平台或新版安装包均已验收。

[2026-10-17 截图](ui-evidence.md)仍是当时的界面记录，不作为当前首页效果图。[连续工作交接](continuity-handoff.md)等旧交接保留原任务范围；当前接手先读根目录 [HANDOVER](../HANDOVER.md)。本次清理删除当前入口中的失效说明，不删除历史原始收据。

白皮书的源码、公开发行身份和校验清单见[证据快照](architecture/evidence/aide-public-baseline-20261111.json)。白皮书在提交、推送或正式发布之前仍是本地文档，不能由已有程序 Release 的日期推定其已经公开。


[0.1.19 使用与迁移](release-guide-0.1.19.md)。
