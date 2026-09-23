# 文档核对与目录整理

日期：2026-09-23。范围：仓库自有 Markdown、设计图、文档引用及开发入口。基线 `c2dd38a`，当前分支 `feat/macos-ui`；不是发布报告。

## 核对方法与结论

对照当前 Compose、Dockerfile、HTTP 路由、工作流/工具循环、插件宿主、设置存储与 schema 检查现行行为；逐文件区分现行规范、历史设计和历史验收。旧文档中的代码草案不重新执行，也不冒充当前程序。第三方许可证、私人助手记忆、运行缓存不作为可清理文档处理。

| 发现 | 处理 | 对照依据 |
| --- | --- | --- |
| README 错写不挂 HOME | 明确 /local 默认可写 HOME，安装示例指导缩小范围 | compose.yaml / .env.example |
| 附件被说成唯一模型上下文 | 明确读工具可额外获取工作区内容，写仍需附件快照 | workflow.go |
| PRD 头部和状态混用早期/后期 | 重整 v2.0，保留 93 个 FR，补遗漏 FR-94，登记 FR-95~98 | PRD 旧快照 / context.go / 当前 UI |
| 主题已存在却标未实现 | 当前组合、浏览器验证和未发布边界分别说明 | settings-init.js / schema / UI 记录 |
| 文档仍称无 remote、模型未验证 | 记录当前 remote，真实模型只引用历史记录 | git remote / verification.md |
| 插件轮数写 6、全功能暗示 | 修正为每阶段 10；明确预设、helper 与沙箱边界 | workflow.go / plugin_host.js |
| 运行版本写成读取工作区文件 | 改为构建身份，补正确 build args | Dockerfile / server.go |
| 零第三方库、完整 GFM/脚注承诺 | 改为本地 marked，无 CDN/npm 构建要求，不承诺所有扩展 | vendor / app.js |
| doc 与 docs 并存 | 13 个文件迁至 docs，修正入口、注释与链接 | 全库引用检查 |
| 历史审查引用本机绝对路径 | 转成对应 6a4e441 提交的 GitHub 链接 | 历史审查明确基线 |
| 首页缺操作与真实效果 | 重做 README、增加使用指南、2 张真实预览截图和流程 SVG | 隔离 Mock 预览，未使用生产数据 |

## 清理记录

删除 6 个未受 Git 管理、可再生的 .DS_Store：仓库根、cmd、plugins、internal、internal/server、旧 doc。运行数据、`.cache`、`.workbuddy/memory`、IDE 配置、已有测试证据和 Docker 镜像归档均保留。此前 UI 改动没有回滚，也没有为“干净”自动提交。

## 验证边界

- 路由 quick：diff / 两个 JS 语法 / 版本格式 / 路由回归 / 文档检查。
- 路由回归覆盖任务路径越界、过期或非 full 收据不能放行、未验证阶段不能发布、未知/受保护/已跟踪文件及符号链接不能被清理。
- 文档检查验证相对链接、图片格式和 FR 编号；不证明外部 URL 的权限、可访问性或旧历史测试仍适用。
- README 的实际渲染检查使用本地 marked；GitHub 自身排版可能略有差别。
- 本轮未跑全部 Go/API/真实模型验收，未合并、升版、推送、重建生产。发布门禁应保持 BLOCKED。

## 逐文件处置索引

下列文件均纳入本次有效性和引用检查。历史数据保留，不追认历史结论；原始 JSON/截图证据不改写测试结果。

| 文件 | 处置 |
| --- | --- |
| [AGENTS.md](../../../AGENTS.md) | 新增统一入口/工作流，标明自动加载与手工适配边界 |
| [CLAUDE.md](../../../CLAUDE.md) | 新增统一入口/工作流，标明自动加载与手工适配边界 |
| [CODEBUDDY.md](../../../CODEBUDDY.md) | 新增统一入口/工作流，标明自动加载与手工适配边界 |
| [HANDOVER.md](../../../HANDOVER.md) | 核对并更新现行内容/路径/预览与发布边界 |
| [README.md](../../../README.md) | 核对并更新现行内容/路径/预览与发布边界 |
| [docker-images/README.md](../../../docker-images/README.md) | 核对镜像导入/导出与卷分离说明；保留 |
| [docs/PRD.md](../../PRD.md) | 核对并更新现行内容/路径/预览与发布边界 |
| [docs/README.md](../../README.md) | 核对并更新现行内容/路径/预览与发布边界 |
| [docs/agent/ADAPTERS.md](../../agent/ADAPTERS.md) | 新增统一入口/工作流，标明自动加载与手工适配边界 |
| [docs/agent/WORKFLOW.md](../../agent/WORKFLOW.md) | 新增统一入口/工作流，标明自动加载与手工适配边界 |
| [docs/architecture/2026-09-21-model-profiles.md](../../architecture/2026-09-21-model-profiles.md) | 保留当时设计；逐类标记已被替代的存储/UI/版本行为 |
| [docs/architecture/2026-09-21-multi-model-context.md](../../architecture/2026-09-21-multi-model-context.md) | 保留当时设计；逐类标记已被替代的存储/UI/版本行为 |
| [docs/architecture/2026-09-21-settings-panel.md](../../architecture/2026-09-21-settings-panel.md) | 保留当时设计；逐类标记已被替代的存储/UI/版本行为 |
| [docs/architecture/2026-09-21-theme-switching.md](../../architecture/2026-09-21-theme-switching.md) | 保留当时设计；逐类标记已被替代的存储/UI/版本行为 |
| [docs/architecture/2026-09-21-version-management.md](../../architecture/2026-09-21-version-management.md) | 保留当时设计；逐类标记已被替代的存储/UI/版本行为 |
| [docs/architecture.md](../../architecture.md) | 核对并更新现行内容/路径/预览与发布边界 |
| [docs/archive/2026-09-23/PRD-before-review.md](../../archive/2026-09-23/PRD-before-review.md) | 保留旧需求快照；醒目标记历史，修正迁移链接 |
| [docs/design/macos-ui.md](../../design/macos-ui.md) | 核对并更新现行内容/路径/预览与发布边界 |
| [docs/plugin-protocol.md](../../plugin-protocol.md) | 核对并更新现行内容/路径/预览与发布边界 |
| [docs/prd/2026-09-21-model-profiles.md](../../prd/2026-09-21-model-profiles.md) | 保留当时设计；逐类标记已被替代的存储/UI/版本行为 |
| [docs/prd/2026-09-21-multi-model-context.md](../../prd/2026-09-21-multi-model-context.md) | 保留当时设计；逐类标记已被替代的存储/UI/版本行为 |
| [docs/prd/2026-09-21-settings-panel.md](../../prd/2026-09-21-settings-panel.md) | 保留当时设计；逐类标记已被替代的存储/UI/版本行为 |
| [docs/prd/2026-09-21-theme-switching.md](../../prd/2026-09-21-theme-switching.md) | 保留当时设计；逐类标记已被替代的存储/UI/版本行为 |
| [docs/prd/2026-09-21-version-management.md](../../prd/2026-09-21-version-management.md) | 保留当时设计；逐类标记已被替代的存储/UI/版本行为 |
| [docs/reviews/2026-09-23/remediation-status.md](remediation-status.md) | 历史证据加适用时点说明，不重认定测试结果 |
| [docs/reviews/2026-09-23/系统功能分析与整改建议.md](系统功能分析与整改建议.md) | 历史证据加适用时点说明，不重认定测试结果 |
| [docs/user-guide.md](../../user-guide.md) | 核对并更新现行内容/路径/预览与发布边界 |
| [docs/verification.md](../../verification.md) | 历史证据加适用时点说明，不重认定测试结果 |
| [version.md](../../../version.md) | 发布历史保留；本次不升版 |

补充：`docs/architecture/theme-sequence.mermaid` 保留为历史时序图，已添加适用时点注释。
