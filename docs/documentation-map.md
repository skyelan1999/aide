# 文档覆盖、截图和历史记录

本轮日期 2026-10-17，源码基线 `0fef6411`。所有新 UI 插图均使用[真实运行原图](ui-evidence.md)，使用场景与环境逐图登记；不提供设计效果图代替使用截图。

## 当前文档入口

| 文档类别 | 当前文档 | 本次更新内容与图片依据 |
| --- | --- | --- |
| 产品与需求 | [首页](../README.md)、[英文首页](../README.en.md)、[PRD](PRD.md) | 校正旧版本基线、模型/MCP/消息/助理/管理权限；工作台与管理权限实图 |
| 功能 | [功能说明](features.md)、[中英用户指南](user-guide.md)、[English guide](en/user-guide.md) | 操作入口、授权与可用边界；工作台、提醒、引用、百炼实图 |
| 演示 | [演示手册](demo-guide.md) | 按真实操作编排；未执行的模型步骤明确标出 |
| 交互设计 | [当前工作台设计](design/current-workbench.md) | 真实三栏布局、控件层级、权限和消息入口；旧设计保留为历史 |
| 软件架构 | [系统总览](system-overview.md)、[架构接口](architecture.md)、[HTML 总览](architecture-overview.html) | 更新关键模块与权限链；工程关系图标为示意，UI 用真实截图 |
| 模块 | [模块职责](architecture/modules.md)、[代码导航](architecture/code-layout.md)、[服务目录索引](../internal/server/README.md) | 源码文件、依赖、数据流与模块边界；实际工作台入口 |
| 助理、语音与通信 | [助理架构](architecture/assistant-live.md)、[消息架构](architecture/decentralized-messages.md) | 页面统筹、百炼模式与目录、账户名和消息同意边界；实际设置 |
| 插件和管理 | [插件协议](plugin-protocol.md)、[定制指南](customization.md)、[执行策略](architecture/execution-policy.md) | 四项管理权限、工坊、人工管理提案；真实工坊与权限 |
| 文档、缓存与来源 | [目录配置](workspace-paths.md)、[数据布局](architecture/data-layout.md)、[记忆访问](architecture/memory-access.md)、[知识星图](architecture/knowledge-map.md) | 文档绑定、缓存旧回退、引用可见与可写；星图本轮停在锁定门，未宣称索引通过 |
| 文档插件 | [Office](plugins/office.md)、[Markdown 历史](plugins/markdown-history.md)、[编辑与导出](architecture/markdown-editing-export.md) | 实际目录配置与 Markdown 预览；不把预览图视为导出测试 |
| 安装与运维 | [中英安装](installation.md)、[English installation](en/installation.md)、[交接](../HANDOVER.md) | 实际启动后界面，区分源码/运行/发行及本轮未重启生产 |
| 导航与证据 | [文档中心](README.md)、[English index](en/README.md)、[截图证据](ui-evidence.md) | 原图、SHA-256、实际操作与未覆盖能力 |

## 哪些文档不改成截图报告

带日期的 PRD 增量、设计、reviews、tasks、archive、releases 和 verification.md 是历史证据，保留原始环境、失败和未验证结果，不重写成当前通过。旧设计入口注明现行替代文档；历史截图不冒充当前 UI。

协议、API 参数、加密结构、CLI 命令、法律/安全模板及许可证依赖原始源码、命令结果或外部依据，单靠截图不能验证。因此保留其专业内容，通过当前导航和相应真实入口关联，不机械插入无关图片或伪造三平台界面。

## 本轮验证口径

真实 Go App 与原仓库前端启动后，直接操作工作台、目录配置、引用、文档预览、插件搜索和设置；示例提醒有实际保存记录。没有执行真实模型、云端 TTS、麦克风、双端通信、三平台安装、生产更新和发行。这些范围保持未验，不能由当前截图或旧测试反推通过。

历史源码工作与发布记录保持分离；此文档同步本身不授权 push、tag、Release 或生产重建。具体任务记录见[文档同步任务](tasks/documentation-sync-20261017.json)。
