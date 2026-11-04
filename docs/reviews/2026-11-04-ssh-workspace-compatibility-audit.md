# Aide 插件与 SSH 权限兼容性审计（2026-11-04）

## 范围、版本与身份

- Aide 审计基线：`origin/main` `572f5dfd32bd8587efb99030409b9859fef36690`。
- 市场目录：`skyelan1999/aide-pkg-shop` revision `582effee8fc2530b6c552ff3fad24ef9d519dbae`。
- 市场目录有 **30 个 public 条目**；其中 `computer-control` 已迁入 Aide 内置能力且从 UI 隐藏/拒绝单独下载，另 **29 个是现行可安装 public 包**。Aide 原冻结 QA lock 漏掉了 `computer-control` 条目，导致目录盘点在开跑测试前失败。本分支补入精确版本/提交/ZIP 大小/SHA，并将其 QA 验收转为测试内置宿主能力，而不是调用已退役的插件执行器。
- 主仓内置/迁移能力与包市场元数据分别检查；私有 `pjplan` 仅登记，无商业实现包被读取或修改。
- 未连接用户 SSH 服务器、真实串口/ROS 设备、物理桌面或付费服务。本报告区分静态代码检查、合成宿主功能测试与真实设备验收。

连接身份必须独立：

1. **选定的 SSH 工作区**：Aide 当前工作区连接和目录；允许的文件能力只操作该工作区配置的相对路径。
2. **已授权 SSH 引用**：当前工作区可见且启用的 `sftp` 或 `workspace-sftp` 来源；读取按来源 ID 固定。写入要求该来源为 `RW` 并经过适用的提案/审批。`workspace-sftp` 复用工作区通道，但仍以引用来源身份授权。
3. **其他 SSH 连接**：如 Terminal/RViz 自行建立的设备连接；需独立连接配置/凭据/用户操作，不继承工作区或引用凭据和目录权限。允许读取一份 SSH 文件，不表示允许连接同一机器的 shell、ROS 或桌面。

## 当前公开包逐项矩阵

所有 29 个现行安装包来自上面的不可变市场 revision 与 `scripts/ci/public-plugins.lock.json`。下列“无工作区文件访问”不表示应用不能发起相应工具调用，而是该包逻辑不直接选择或读写工作区/引用文件。

| 插件/版本 | 文件/连接能力 | SSH 身份边界与结论 |
| --- | --- | --- |
| `ali-bailian-supporter` 0.1.2 | 离线计价目录 | 无工作区文件访问；不读取账户凭据。 |
| `almanac` 1.1.0 | 宿主本地日历计算 | 无文件访问；README 声明支持 SSH 工作区，因为只用宿主时钟/包内数据，不打开远端连接。 |
| `archive-tools` 1.0.0 | Aide 原生归档执行器 | 通过 Aide 文件来源/工作区执行，不自行构造 SSH 通道；宿主已有 workspace SFTP、SFTP 引用分支。 |
| `auto-ai-model-router` 0.1.2 | 离线模型能力目录 | 无工作区文件访问。 |
| `browser-control` 1.1.0 | Aide 宿主浏览器桥接 | 控制当前宿主浏览器；不控制 SSH 主机浏览器，也不继承 SSH 文件授权。 |
| `current-time` 1.1.0 | 日期计算 | 无文件访问。 |
| `deepseek-supporter` 0.1.0 | 离线计价目录 | 无工作区文件访问；不读取账户凭据。 |
| `environment-guide` 1.0.0 | Aide 原生环境清单 | 仅注入边界明确的清单和来源元数据，不连接 SSH 或读取远端文件正文。 |
| `feedback` 1.0.0 | 能力预设 | 不提供独立文件操作 handler。 |
| `goal` 1.0.0 | 能力预设 | 不提供独立文件操作 handler。 |
| `lunar-calendar` 1.1.0 | 包内离线历法 | 无工作区文件访问。 |
| `markdown-history` 1.0.0 | Aide 原生 Markdown 历史 | 以来源 ID/工作区 ID 调用宿主历史执行器；远端能力取决于宿主对应来源读写权限。 |
| `office` 1.1.0 | Aide 原生文档生成/批注 | DOCX 结构读取与 resolve-comment 仍被核心 `docxTool` 的 SSH guard 拒绝；其他 Office 操作部分已有 SFTP 路径。 |
| `openai-supporter` 0.1.0 | 离线计价目录 | 无工作区文件访问；不读取账户凭据。 |
| `plan` 1.0.0 | 能力预设 | 不提供独立文件操作 handler。 |
| `plugin-authoring` 1.0.0 | 插件/引用提案 | 只创建 Aide 原生待确认提案；SFTP 引用配置不等于取得 SSH 文件或 shell 权限。 |
| `rviz` 0.1.1 | ROS/项目文件工作台 | URDF/mesh 输入经 `api.sources` 选择并暂存，可使用当前授权来源；显示配置经 Aide 提案。 ROS/SSH 隧道是单独连接，不继承工作区凭据。 |
| `skill` 1.1.0 | Skill 引用管理 | 通过宿主读取已登记引用；可引用 SSH 来源时仍受来源可见性/读权限限制，不自行启动远端 shell。 |
| `sqlite` 1.0.1 | SQLite 文件读写 | 直接绑定本机 `/workspace`、`/context`、`/local` 和 Node `fs`，没有 SSH/SFTP 来源身份输入；当前所选 SSH 工作区/引用不能作为数据库路径，可能错误操作宿主容器挂载文件。明确不兼容，需来源 API 与受控写回设计。 |
| `starlink` 0.1.4（候选修复 0.1.5） | Aide 知识星图只读 API | 原包只接受 `local` 工作区；已提交修复让其在 SSH 工作区按知识图身份读取 Aide HTTPS API，严格要求 `workspaceRoot` 为空。包测试通过，源仓库 PR #1 等待代码所有者审查；尚未发布/进入市场。 |
| `step-viewer` 0.1.1 | 绑定文档/几何预览 | 预览输入通过 Aide 文档绑定桥；不使用包自身路径或其他 SSH 凭据。实际 SSH 文件读取由宿主绑定输入能力决定，未做真实远端设备验收。 |
| `subagent` 1.0.0 | 能力预设 | 不提供独立文件操作 handler。 |
| `terminal` 1.6.0 | Aide 进程、串口/网络工具、独立设备 SSH | 独立 SSH 连接有自己的 SSH API/config，未发现从工作区/引用继承凭据；但 SFTP 的本地文件端点明确只支持本地工作区，SSH 工作区与 SFTP 引用无法用作传输源/目标。远端设备 SSH 和工作区 SSH 不是同一身份。 |
| `todo` 1.0.0 | 能力预设 | 不提供独立文件操作 handler。 |
| `tts-tools` 0.1.0 | 合成语音服务 | 无工作区读取；输出由 Aide 原生写文件/审批通道处理。 |
| `video-preview` 0.1.0 | 绑定媒体预览 | 文件内容经 Aide 绑定文档/媒体桥，不自行解析任意工作区路径；真实 SSH 路径验收未覆盖。 |
| `workflow` 1.0.0 | 能力预设 | 实际文件读写走 Aide 原生工具，能力边界由来源/审批执行器决定。 |
| `file-preview` 0.1.0 | 只读文件预览 | `api.sources` 选择授权来源并经有界暂存；可走宿主 SSH 来源读取，不接收凭据或直接路径。 |

## Aide 随包市场快照与私有条目

Aide 原随包目录快照来源提交 `4dc9a7b0b01fef6ac3550b0b0fcf97db42d45f94`，含 22 个 public 元数据条目和 `pjplan`、`compare`、`professional-project` 三个 `privateListings` 展示条目。原快照不是当前可安装清单，版本号和代码来源已历史化；其中 `propeller-aero`、`comm-serial`、`comm-ssh`、`comm-tcp`、`comm-udp` 当前市场没有安装 ZIP。此次分支已将 Aide 离线 fallback 更新到锁定市场 revision `582effee8fc2530b6c552ff3fad24ef9d519dbae`，含 30 条 public 目录元数据和 3 个 private 展示条目。私有条目不携带公开包路径/摘要，不属于 public 安装/发行测试；私有实现和包仓库均未读取或改动。

### 市场里已迁移的电脑控制条目

`computer-control` 1.2.0（市场仍保留 ZIP/目录记录）现已迁移为 Aide 内置能力；Aide UI 隐藏市场卡片，下载/安装端点返回 Gone，工具通过只允许本机 loopback 的专用桥接令牌调用。它只能操作 Aide 所在桌面的允许应用，不会控制 SSH 主机；截图由内置 `computer_snapshot` 返回图像协议。冻结 QA 漏项修复后，ARM64 合成宿主验收覆盖 token、截图协议及未确认点击拒绝。真实 macOS/Windows 桌面硬件仍需对应原生验收。

## Aide 宿主侧与结论

- `pluginWorkspaceLocked()` 在 SSH 工作区传入空 `workspaceRoot`，防止插件把本机 `/workspace` 冒充远端工作区；`api.sources` 每次按来源 ID、配置代际、启用状态和当前工作区可见性复核。SFTP/workspace-SFTP 的暂存读取由宿主限额处理。
- SSH 工作区插件工具采用串行独占 footprint，避免本地路径锁误用于远端资源；这是吞吐限制，不是权限继承。
- 已发现的高优先级兼容项汇总于 [Aide Issue #790](https://github.com/skyelan1999/aide/issues/790)：StarLink、SQLite、Terminal 远端文件端点及 Office DOCX SSH guard。用户授权了修复/发布，不意味着可以去掉不同连接的权限边界。
- `computer-control` 不适用 SSH 工作区权限；`rviz` 的 ROS transport、Terminal 的设备 SSH 也须独立连接授权。
- 远端图片/Office 附件读取、工作流文件绑定/托管 Git、交付基线、无符号链接远端提案和 SSH 安全沙箱属于宿主额外能力，不能通过删除本地 guard 达成。当前附件 SSH 限制按主仓代码仍存在，应在后续宿主 issue 中另行实现；不将它误报为插件包支持。

## 验证与尚未完成

- 30 条市场目录（29 可安装包 + 1 迁移条目）已按 exact bytes、SHA256、ZIP 结构和 manifest 核验。
- Aide `public_plugins.py run` 在 Linux ARM64 隔离工具镜像执行：**30/30 inspection、installation、function PASS**。含已发布包测试夹具、synthetic loopback、无付费模型调用、无真实外设操作。证据位于 `.agent-state/plugin-audit-arm64-20261104/`。
- StarLink 0.1.5：`npm test` 18/18 PASS，`npm run build` 生成 ZIP `64e9f19e23c7b1fe5bd2751e7b66a35119b912f857ec64d5e1b047a4c9d96fde`；源仓库 PR #1 已开但待 owner review，市场/Aide 锁未更新到候选包。
- Linux AMD64 插件矩阵、完整 Aide Delivery、原生 Windows/macOS bridge、真实 SSH 服务器及各包 SSH 读写 fixture 尚未验收，因此不能宣称全平台发行门禁通过或发布完成。
- 生产实例未替换/重启。
