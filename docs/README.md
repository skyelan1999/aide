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
| 运维、备份、恢复与发布 | [交接手册](../HANDOVER.md) |
| 看需求范围与实际状态 | [现行 PRD](PRD.md) |
| 看实现、数据和接口 | [架构与 API](architecture.md) |
| 写插件 | [插件协议 v1.1](plugin-protocol.md) |
| 让 AI 参与开发 | [统一 Agent 工作流](agent/WORKFLOW.md) · [客户端接入](agent/ADAPTERS.md) |
| 看当前设计与验证边界 | [macOS UI](design/macos-ui.md) |
| 看本次整理过程 | [文档核对报告](reviews/2026-09-23/documentation-review.md) |

## 目录职责

```text
docs/
  PRD.md                 现行需求基线与编号
  architecture.md        现行实现和 API 索引
  user-guide.md          用户操作指南
  plugin-protocol.md     插件协议
  agent/                 统一开发工作流与客户端入口说明
  tasks/                 可交接的任务记录
  design/                当前界面设计与验证范围
  images/                示例数据下的实际 UI 截图
  prd/                   按日期保存的历史增量需求
  architecture/          按日期保存的历史增量设计
  reviews/               审查、验收证据与核对报告
  archive/               历史文档快照
  verification.md        分日期的历史运行验证记录
```

## 如何判断一份文档还适不适用

1. 当前行为优先查 PRD、用户指南、架构和实际源码。
2. 带日期的历史设计保留当时的约束与结论，开头写明被什么实现替代。
3. 验收记录只证明记录中的提交、环境和操作，不证明当前 HEAD 全部通过。
4. 发布以 version/tag/构建身份/部署记录共同确认，不能只看截图角落的版本。
5. 文档改动后运行 `python3 scripts/check_docs.py`；有截图更新时，使用示例数据并说明是预览还是发布版本。

本次交付版本 0.1.6.0 RC4；截图使用此前隔离预览。此次没有把历史证据重新包装为当前测试结果。
