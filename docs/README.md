# aide 文档中心

[English](en/README.md) · 简体中文

全部项目文档统一在 `docs/`。根目录仅保留 README、HANDOVER、version、许可证及客户端入口。不再创建 `doc/`。

## 从这里开始

| 目标 | 文档 |
| --- | --- |
| 了解定位、安装与主要功能 | [项目首页](../README.md) |
| 让 AI 定制专属工作台 | [场景定制指南](customization.md) |
| 安装环境与首次启动 | [安装说明](installation.md) |
| 工作目录、共享范围与上级导航 | [工作目录配置](workspace-paths.md) |
| 学会使用项目、模型、文件、轨迹、统计 | [使用指南](user-guide.md) |
| 检查与安装软件更新 | [软件更新流程](architecture/software-updates.md) |
| 运维、备份、恢复与发布 | [交接手册](../HANDOVER.md) |
| 看需求范围与实际状态 | [现行 PRD](PRD.md) |
| 看实现、数据和接口 | [架构与 API](architecture.md) |
| 看系统整体方案、状态机和运行机制 | [系统方案总览](system-overview.md) |
| 写插件 | [插件协议 v1.1](plugin-protocol.md) |
| 生成 Office 文件与编辑 XLSX | [Office 工具集](plugins/office.md) |
| 让 AI 参与开发 | [统一 Agent 工作流](agent/WORKFLOW.md) · [客户端接入与 skill 路由](agent/ADAPTERS.md) · `python3 scripts/agent-route.py route --request "需求原文"` |
| 看当前设计与验证边界 | [macOS UI](design/macos-ui.md) |
| 看当前版本发布记录与验收边界 | [0.1.14.0 RC5 跨平台资产发布](reviews/release-assets-2026-10-01.md) · [发布任务验收](tasks/release-assets-2026-10-01.json) |
| 查看 RC6 后端口切换、虚拟形象与助理名称更新 | [RC7 发布记录](reviews/release-rc7-2026-10-01.md) · [发布任务账本](tasks/release-2026-10-01-rc7.json) |
| 查看 RC8 启动器自动下载镜像的修复与发布状态 | [RC8 发布记录](reviews/release-0.1.14.0-RC8-2026-10-01.md) · [发布任务账本](tasks/release-2026-10-01-rc8.json) · [安装说明](installation.md) |
| 查看上一版本实现记录 | [0.1.14.0 RC2 源码记录](reviews/2026-10-01-v0.1.14.0-RC2.md) · [任务验收](tasks/release-2026-10-01.json) |
| 查看历史审查与发布证据 | [审查归档](reviews/) · [版本记录](../version.md) |

## 目录职责

```text
docs/
  README.md              本索引（中英文入口）
  installation.md        安装与首次启动（英文版 en/installation.md）
  user-guide.md          用户操作指南
  customization.md       场景定制指南
  workspace-paths.md     工作目录、共享范围与挂载模式
  architecture.md        现行实现和 API 索引
  architecture-overview.html  架构总览图
  PRD.md                 现行需求基线与编号
  plugin-protocol.md     插件协议
  agent/                 统一开发工作流与客户端入口说明
  tasks/                 可交接的任务记录
  design/                当前界面设计与验证范围
  images/                示例数据下的实际 UI 截图
  en/                    英文文档（README/user-guide/installation/localization）
  prd/                   按日期保存的历史增量需求
  architecture/          按日期保存的历史增量设计
  reviews/               审查、验收证据与核对报告
  releases/              发布附件与归档说明
  archive/               历史文档快照
  verification.md        分日期的历史运行验证记录
```

## 如何判断一份文档还适不适用

1. 当前行为优先查 PRD、用户指南、架构和实际源码。
2. 带日期的历史设计保留当时的约束与结论，开头写明被什么实现替代。
3. 验收记录只证明记录中的提交、环境和操作，不证明当前 HEAD 全部通过。
4. 发布以 version/tag/构建身份/部署记录共同确认，不能只看截图角落的版本。
5. 文档改动后运行 `python3 scripts/check_docs.py`；有截图更新时，使用示例数据并说明是预览还是发布版本。

当前版本以根目录 [version.md](../version.md) 及对应 Git tag 为准；截图使用此前隔离预览，不把历史证据重新包装为当前测试结果。
