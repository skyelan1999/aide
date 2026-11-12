# 当前功能说明与使用入口

维护日期：2026-11-12；当前源码基线 `05e0b8a7`，覆盖 0.1.19.0 RC1 发行准备。此前批次修正见[余项核查](reviews/2026-11-12-remaining-issues/README.md)，当前迁移与验收边界见[0.1.19 指南](release-guide-0.1.19.md)。实际公开安装包以 [Releases](https://github.com/skyelan1999/aide/releases) 为准，不把主干能力视为旧 RC3 已有功能。操作见[用户指南](user-guide.md)，原理见[技术白皮书](architecture/aide-technical-whitepaper.md)，需求编号见 [PRD](PRD.md)。

## 项目与工程任务

| 能力 | 使用方式 | 实现与约束 |
| --- | --- | --- |
| 对话和 AI 工作流 | 输入框选择模式、模型、策略和审批；可排队、插话、暂停与继续 | 任务保存输入、工具交换、提案和检查点；阶段输出不等于执行成功 |
| 多提供商模型 | 设置中配置提供商、命名密钥、模型和参数 | Chat Completions 与适用模型的 Responses 适配；配额与支持能力取决于上游 |
| 并行协作 | 普通对话提供 1X–5X、10X 执行容量 | 新聊天默认主环境与最多 X−1 个私有环境协同；可用工具和测试，主项目候选仍经审批；不保证等比例提速 |
| 审批与实际应用 | 手动或辅助审批；待处理动作集中在任务区域 | 写入前重查路径、来源、身份和版本；模型答复与实际回执分开 |
| 调用轨迹 | 输入、模型请求、工具、审批和结果按过程查看，支持搜索和折叠 | 时间跨度、记录耗时和缺失事件分别表达，不补造执行历史 |
| 用量与上下文 | 累计消耗、最近请求窗口、并行明细及摘要进度分别查看 | 估算与上游 usage 区分；多请求累计用量不是共享窗口 |

## 文件、资料与知识

| 能力 | 使用方式 | 实现与约束 |
| --- | --- | --- |
| 本地与远程工作区 | 工作区配置选择目录、SSH/SFTP 与文档绑定 | 工作区身份固定到任务；改配后不静默重定向旧提案 |
| 引用与缓存 | 按来源设置启用、AI 可见、只读/可写；缓存选择父目录 | 实际使用父目录下 `.cache/`；保留旧数据，不自动搬迁或删除 |
| 编辑与预览 | Markdown 编辑/预览、历史、Git 比较、图片和专用查看器 | 草稿、磁盘和 Git 版本分开；格式导出和落盘需相应回执 |
| 大 Markdown 附件 | 超内联预算后附文件引用，按需读取相关段落 | 16 KiB 是请求内共享内联预算，不是上传上限；不移除模型窗口保护 |
| 其他文档与图片 | 按格式提取文本或使用图片输入，无法内联的文件保留路径指引 | 各格式资源限制和模型能力不同，不保证完整 OCR 或全文进入单次请求 |
| 知识星图 | 浏览文件、会话、代码与文档，检索后回填证据 | 有界扫描、身份与指纹复核；本地 TF-IDF，语义/embedding 能力另行配置 |
| 远程资料中继 | 下载目录/SSH/SFTP 中继脚本后配置来源 | 来源权限与远端账户权限同时生效，实际连通另验 |

[工作目录](workspace-paths.md) · [远程中继](reference-relay.md) · [知识架构](architecture/knowledge-map.md) · [Markdown 编辑导出](architecture/markdown-editing-export.md) · [大附件实测](reviews/2026-11-09-large-markdown-attachments.md)。

## 工具、浏览器和电脑控制

插件可提供工具、专用工作台、查看器、媒体与文档操作；宿主按协议登记工具和处理提案。插件管理与引用管理有独立授权，不与日常调用混用。Node 插件是可信可执行代码，包检查不是恶意代码隔离。[插件协议](plugin-protocol.md)。

电脑控制已内置到 Aide 设置，支持目标应用检查、窗口截图、视觉证据、点击、拖拽、双轴滚动与复杂组合键。平台支持时可定向读取或操作后台应用；依赖系统前台的动作仍需激活。会话虚拟焦点不等于多个独立桌面，也不保证共享实体键鼠能同时操作不同软件。

辅助审批覆盖符合条件的单次网页和电脑操作，基本浏览、聚焦、点击不再一律要求人工确认。它不会扩充应用/站点永久允许名单或授予操作系统权限；目标变化、证据不足及不适合自动审核的操作转人工。[控制说明](plugins/computer-browser-control.md)与[动作验收](reviews/2026-11-09-computer-actions.md)。

## 助理、语音与消息

页面助理可跟进交办任务、提醒与授权消息，创建并启动或排队工作任务。助理代审和消息管理有各自授权；锁屏或页面生命周期影响页面统筹，不能当作无条件常驻后台服务。

朗读、麦克风转写和双向语音通话分别配置提供商与设备权限；云端目录、配额和本机音频可用性各自影响结果。工作台插件还可声明本机录音/转写能力，由宿主提供显式开始入口；iframe 不直接获得麦克风。详见[语音架构](architecture/assistant-openai-speech.md)和[插件协议](plugin-protocol.md)。

消息中心提供联系人、群组、通话邀请、文件传输与提醒；短码用于查找，完整公钥指纹用于身份核验。跨网络发现、中继、加密和邀请规则详见[消息架构](architecture/decentralized-messages.md)，不承诺所有网络下都能连接。

## 外观与原生快捷键

外观支持现代、专业、舒适和经典配色、明暗主题及多种工作台背景，动态效果遵循系统减少动态效果设置。旧鲸鱼娘背景已移除。[外观说明](builtin-background.md)。

设置中可调整快捷键。macOS 桌面可选双 Shift/双 Command 截取前台应用窗口，同时附应用名、窗口和时间等上下文；仅加入草稿，不自动发送，需相应系统权限。浏览器版没有同等原生全局手势能力。

## 验证范围

本页描述源码提供的能力，不把代码存在、设置可见、某次操作成功和公开安装包交付混为一项。已有[测试与审查](reviews/)按原日期和环境保留；[2026-10-17 截图](ui-evidence.md)仍是历史图片。本轮实际执行了隔离 Go/插件进程与浏览器回归；各项范围见余项核查。没有替换正在运行的生产实例，也未执行付费模型、真实音频或新的跨平台硬件验收。

## PDF / JPX preview support boundary

The PDF.js worker uses a reproducibly generated OpenJPEG fallback with pinned C sources, source patches and compiler image; see `scripts/openjpeg/README.md`. Baseline JPEG 2000 and supported HT-only inputs are accepted within the preview budgets; T.814 MIXED HT signaling is unsupported and returns a decoder error. Subsampled e-sYCC is explicitly rejected rather than silently converted as sYCC; full-resolution unsigned e-sYCC uses the PIMA 7667 Annex B conversion to standard sRGB display values. Native ownership failures, DC shifts, MCT record/offset handling, HT layer segments and terminal row bounds are checked by focused regressions and a native sanitizer harness. These are source preview capabilities, not a universal JPEG 2000 conformance claim.

Per-JPX limits: decompressed input 64 MiB; FTYP 64 KiB; JP2H 8 MiB; ICC 4 MiB; 4096 tiles / 16384 tile-components / 256 components; 96 MiB estimated component planes; 64 MiB RGBA output; 4096 quality layers; 8 Mi estimated packet-iterator elements; 4096 markers; 256 CDEF entries; packed packet headers 4 MiB; aggregate MCT tile copies 16 MiB; cumulative native allocation requests 256 MiB. PCLR precision above 31 bits and coding overrides after tile-part zero are rejected. Limits can reject valid but unsupported large/profile-specific images. They do not bound an entire PDF/browser's peak memory or resolve every remaining decoder report. See the [individual audit](reviews/2026-11-12-remaining-issues/README.md).


[0.1.19 使用与迁移](release-guide-0.1.19.md)。
