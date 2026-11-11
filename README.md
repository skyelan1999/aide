<div align="center">

[English](README.en.md) · 简体中文

# aide

### AI + IDE：把项目、模型、工具与执行过程放进同一个工作台。

连接本地或 SSH 项目，用自选模型理解资料、编写与修改文件、调用工具、检查结果。文件提案、审批、执行回执和运行轨迹围绕同一个任务保存。

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE) [![GitHub release](https://img.shields.io/github/v/release/skyelan1999/aide)](https://github.com/skyelan1999/aide/releases/latest)

[安装](docs/installation.md) · [使用指南](docs/user-guide.md) · [功能说明](docs/features.md) · [技术白皮书](docs/architecture/aide-technical-whitepaper.md) · [文档中心](docs/README.md)

</div>

## 从真实项目到可核对的结果

aide 是可自托管的 AI 工程工作台，面向可信单用户环境。项目文件、辅助资料、对话、工具、插件和命令共享工作区身份；模型的回答与真正的文件写入、命令退出状态分别记录。

```mermaid
flowchart LR
    Project[项目与授权资料] --> Task[对话 / AI 工作流]
    Task --> Tools[模型与工具执行]
    Tools --> Review[提案与审批]
    Review --> Result[文件 / 命令 / 实际回执]
    Result --> Trace[轨迹与后续核对]
    Trace --> Task
```

| 能力 | 当前源码提供什么 |
| --- | --- |
| 对话与工程任务 | 多轮工具执行、规划与审查、运行中插话/排队、暂停与继续 |
| 多模型与上下文 | 提供商及命名密钥、参数策略、兼容 Chat Completions / Responses 的适配、上下文预算与压缩 |
| 并行协作 | 1X～5X、10X 容量；主环境与私有执行环境异步协同，工具与测试在各自范围执行，候选汇入主项目仍经版本校验与审批 |
| 文件与资料 | 本地与 SSH/SFTP 工作区、引用的可见/只读/可写控制、Markdown 编辑与历史、可解析文档和图片附件 |
| 大 Markdown | 超出内联预算后使用路径、摘要和有限节选，按需分段读取；模型窗口预算继续生效 |
| 知识与检索 | 文件/会话/代码星图、来源身份与差量同步、原文检索和本地 TF-IDF 片段排序；可选语义能力单独配置 |
| 插件与工作台 | 独立插件市场、工具注册、专用工作台、图片证据与受控文档保存接口 |
| 审批与电脑控制 | 手动/辅助审批；受授权约束的浏览器访问、应用检查、视觉定位、点击、拖拽、滚动和组合键 |
| 助理与语音 | 工作统筹、任务交办、朗读和语音通话；提供商、设备权限和本机能力分别配置 |
| 桌面体验 | 快捷键绑定、明暗配色、工作台背景；macOS 可配置双 Shift / 双 Command 应用截屏并附应用上下文 |
| 过程与运维 | 会话调用轨迹、请求与累计用量、诊断、备份、源码构建及发行包校验 |

并发倍率不是等比例提速承诺；模型审查不是测试通过。详细接口、约束与已有验收见[功能说明](docs/features.md)和[技术白皮书](docs/architecture/aide-technical-whitepaper.md)。

## 快速开始

### 使用发行版

从 [GitHub Releases](https://github.com/skyelan1999/aide/releases/latest) 下载适合平台的包，按该版本说明校验摘要、解压并使用包内 `start` 入口。源码状态与发行包范围分别核对，不把后续 `main` 功能视为旧安装包已有功能。

### 从源码构建

标准源码路径需要 Git、Docker Engine、Docker Compose 和 Buildx；macOS 可使用 Docker Desktop。

```bash
git clone https://github.com/skyelan1999/aide.git
cd aide
bash scripts/install.sh --check
bash scripts/install.sh --source
```

构建后配置模型提供商、API Key 和模型，选择工作区开始任务。默认地址为 `https://localhost:8097`，端口与挂载以 `.env` 为准。目录设置、首次登录与平台差异见[安装说明](docs/installation.md)。

| 操作 | 标准入口 |
| --- | --- |
| 日常启动 / 复用已有健康实例 | `start.command`、`start.sh`、`start.bat` 或 `bash scripts/aide.sh start` |
| 首次源码构建 / 拉取源码后应用变更 | `build.command`、`build.sh`、`build.bat` 或 `bash scripts/aide.sh build` |
| 查看状态 / 日志 | `bash scripts/aide.sh status` / `bash scripts/aide.sh logs` |

`start` 不编译源码；改变源码后仅刷新页面不会更新已运行的二进制。原生桌面窗口和浏览器可以使用同一个后台服务，不必为每个窗口启动容器。清理、停止与回退见[标准启动接口](docs/architecture/launcher-contract.md)和[运维交接](HANDOVER.md)。

## 使用与扩展

1. 确认工作区和辅助资料的范围，再打开或附加文件。
2. 选择模型、执行模式与审批方式，说明目标和验收条件。
3. 根据工具事实、文件差异、应用回执和命令结果判断完成状态。
4. 使用插件扩展专业能力；开发者从 `AGENTS.md` 和[开发流程](docs/agent/WORKFLOW.md)进入。

插件独立建库和发行，从市场或 ZIP 安装。主程序更新不会自动替换本地已安装插件。Node 插件运行可信代码，插件协议和工具开关不构成恶意代码隔离沙箱。

项目状态与文件由本地或用户配置的服务保存；使用云端模型时，发送的任务上下文、图片和工具结果会交给相应提供商。`AIDE_LOCAL_ROOT`、工作区挂载和引用写锁决定可访问范围，配置前应阅读[目录与存储边界](docs/workspace-paths.md)。

## 文档与版本

本页按 **2026-11-11 源码 `921a95a5`** 核对。查询到的最新公开程序发行版为 **v0.1.18.0-RC3**（2026-11-02）；两者不是同一交付身份。后续版本以 Release 页面为准。

| 文档 | 内容 |
| --- | --- |
| [文档中心](docs/README.md) | 当前使用、开发、运维入口与历史资料分类 |
| [技术白皮书](docs/architecture/aide-technical-whitepaper.md) | Skye Lan 署名；架构、任务状态、数据边界、协议、验证和披露证据 |
| [架构与 API](docs/architecture.md) | 模块、端点与专题入口 |
| [插件协议](docs/plugin-protocol.md) | 工具、工作台、文档与图片通道 |
| [使用指南](docs/user-guide.md) | 模型、文件、会话、资料和日常操作 |
| [产品需求](docs/PRD.md) | 需求范围、状态口径和历史编号 |
| [运维交接](HANDOVER.md) | 启动、构建、数据保护、验证与恢复 |
| [版本记录](version.md) | 源码版本；程序附件以对应 Release 为准 |

带日期的任务、测试与截图是历史证据。白皮书中的源码推导、既有测试和未执行验证分别标注；文档修改本身不证明新程序已部署或论文已公开发布。

## 许可与致谢

[MIT License](LICENSE)。第三方组件保留各自许可。aide 是独立项目，借鉴 [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) 的设计思路，不是 DeepSeek 官方产品，也不承诺完整 DSH/Cordis 运行时兼容。
