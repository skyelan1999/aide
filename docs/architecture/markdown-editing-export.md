# Markdown 编辑布局、导出与文件历史

Markdown 编辑工具栏使用单行紧凑布局。图片、文件、链接、draw.io 和每种常用 Markdown 模板都以同一层级的图标按钮呈现，不再放进下拉菜单；窄屏时可在整行工具栏横向滚动。模板包括段落、H1–H6、粗体、斜体、删除线、行内代码、代码块、Mermaid 流程图、表格、Markdown 链接与图片语法、引用、有序/无序/任务列表、分隔线、脚注、提示块、折叠块、数学公式和 YAML 文档元数据。悬停或键盘聚焦时，工具提示给出名称与用途；按钮也提供完整的屏幕阅读器标签。插入后选中示例占位文字，直接键入即可替换。脚注、GitHub 风格提示块、HTML 折叠块、数学公式与 YAML 前置信息属于扩展或发布工具语法，是否渲染取决于 Markdown 渲染器。

左右对比编辑时，文件弹窗和独立文件页的标题栏都可切换**锁定滚动**。编辑器滚动按 Markdown 源行映射到对应预览块，并按块内阅读进度细化位置；预览滚动则反向映射到同一源行，因此长段落和不同高度的渲染块不会只按整页比例粗略跳转。开启时以最近手动滚动的一侧对齐，预览因编辑重排后重新对齐；关闭后两侧恢复独立滚动。纯文本或单栏预览不显示此按钮。滚动锁是当前编辑视图的交互状态，不改动 Markdown 内容。

## 编辑模式

设置 → 文件查看与渲染 → **Markdown 编辑模式**提供“左右对比”和“纯文本”。默认左右对比；桌面左侧编辑原文、右侧实时预览，窄屏上下排列。纯文本只显示原文，仍可点“预览”阅读。选择自动保存到当前浏览器的 `aide-ui` 偏好，同源页面同步；不是服务端账户配置，不跨设备同步。切换不替换草稿。实时预览延迟 250ms，中文组合输入结束后刷新。

## 导出菜单

文件弹窗及独立文件页的 Markdown 工具栏提供 **导出**。点击显示四种格式；选择后直接生成浏览器下载，不调用 `print()`、不打开输出窗口，也不调用模型或第三方转换服务。

| 格式 | 下载内容 |
| --- | --- |
| PDF | 本机渲染的 A4 分页文档，保留中文、表格、代码和图片；页面为图像，文字不可选中复制 |
| Markdown | 当前编辑器 UTF-8 原文，包含未保存修改 |
| Markdown + 资源 | ZIP 中含当前原文、直接引用的本地图片／附件／draw.io SVG 和资源清单，重写为包内相对路径 |
| PNG 图片 | 本机渲染的一张长图 |

下载路径遵循浏览器设置；若浏览器设置为每次询问位置，仍会出现浏览器自身的保存提示。导出不保存原文、不新增历史版本、不在工作空间生成中间文件。原先的浏览器打印／另存为 PDF 流程已替换。

## 资源、性能与权限

- PDF／PNG 复用已有 Markdown 渲染、相对图片解析及严格 Mermaid 渲染；资源请求固定当前文件的工作区／引用来源身份。跨域图片需允许 CORS；无法读取时取消导出并提示，不能静默漏图。
- PNG/PDF支持显式`image/*`类型的内嵌data图片：按剩余32MiB总预算本地解码base64/百分号数据，元数据限制512字符，编码输入限制为剩余预算四倍加512字符。保留原有CSP，不把data图片交给受connect-src约束的fetch；正常HTTP图片继续使用原读取路径。读取Blob期间取消会中止FileReader。
- ZIP 识别行内链接、引用式定义和 HTML 的 `src`／`href`，忽略代码围栏与行内代码。最多 64 项资源、总计 32 MiB；外部 URL 保留在原文并在清单注明未打包，不递归收集被链接文档内部资源。特殊 Markdown 扩展语法不保证识别。
- ZIP 包内使用安全资源名和相对路径；不会将查看器生成的访问令牌 URL 写入资源清单。资源无权限或丢失时整个 ZIP 导出失败。
- PDF 按页面边界分片：从已渲染文档中只克隆当前页覆盖的块，逐页栅格化并释放页面临时节点，避免超长文档一次生成整页高画布。导出状态会显示当前页进度。PDF 不再因文档总 CSS 高度而拒绝；超时按页数动态增加，最多 15 分钟。PNG 仍是单张长图，文档高度上限 10000 CSS 像素，超出请改选 PDF。图片总字节上限 32 MiB，单张图片上限 2400 万像素。
- 锁屏、切换文件、关闭编辑弹窗或离开页面取消未完成下载并清理临时框架；PNG/PDF将同一取消信号传入本地渲染器，阻止取消后的异步恢复并释放自有克隆帧、等待资源及新建画布。调用方画布和无关框架保留；已经运行的同步JavaScript与已提交的浏览器原生编码不能即时抢占。已经下载的文件由浏览器／用户管理。导出菜单支持 Escape、方向键、点击外部关闭，并能显示在模态编辑器上层。
- html2canvas 1.4.1、jsPDF 4.2.1 按需从本机加载；固定版本源码、许可证与校验值位于 `web/vendor/markdown-export/README.md`。ZIP 复用已有 JSZip。

## 实现与验收

入口：`web/app.js` 的模式切换与两处导出绑定；`web/settings-init.js` 保存 `fileMarkdownEditing`；`web/settings-schema.json` 声明选项；`web/markdown-export.js` 管理菜单、资源收集、分页渲染和下载。没有新增后端接口或配置迁移。

2026-10-09 直接下载组件验收使用当前导出模块、样式和实际浏览器下载事件：MD、ZIP、PDF、PNG 四种文件均下载到浏览器下载目录。已解压 ZIP 核对 SVG 图片、TXT 附件和原文相对路径；已查看 1 页中文／表格／SVG PDF 和 8 页长文 PDF；确认失效图片返回 404 提示且不生成 PDF。模态编辑器菜单、Escape 关闭和 390px 窄屏菜单均检查。脚本覆盖资源匹配位置、代码排除、源身份绑定、文件关闭守卫及加载库期间取消。

完整已鉴权工作台、独立文件页端到端、远程 SSH 资源、Windows 与跨域外部图片仍未验收；这些不能以组件测试替代。生产实例／镜像未替换，当前为源码候选。历史打印测试记录仅适用于上一实现，不能证明本次直接下载能力。

## 超长 Markdown PDF 分页修复（2026-10-14）

修复长文导出时将整个 Markdown 正文连同嵌入页面一次复制到渲染画布的问题。PDF 现在逐页挑选可见块并渲染，先按实际分页规则规划页界，再显示准确页数进度并按页数延展超时（最多 15 分钟）；PNG 仍保留 10000 CSS 像素长图限制。每页渲染也纳入取消竞速：锁屏、切换文件、关编辑器或总超时发生时，即使 `html2canvas` 卡在当前页，导出状态也会结束并清理临时框架。逐页切片对超高列表／表格容器使用递进子节点游标，避免每页重扫此前所有行；分页终点取最后一个实际内容块，并为末页保留标准画布高度，避免空白页及内容裁切。

组件回归覆盖短段落避切、准确总页数、挂起渲染取消和直接下载路径。2026-10-14 使用系统 Safari 加载当前导出脚本和仓库实际 html2canvas/jsPDF 库，对 1000 节长文夹具完成 154 页 PDF 下载；`pdfinfo` 确认 154 页 A4、120194279 字节，Safari 下载列表显示 120.2 MB，首屏版式已目视检查。同日在已鉴权 Aide 文件页通过实际“导出 → PDF”生成项目 Markdown 的 PDF；Safari 下载列表显示 3 MB，`pdfinfo` 确认 29 页 A4、2966702 字节。随后使用系统 Safari 对单个 700 项有序列表完成 65 页 PDF 下载，`pdfinfo` 确认 65 页 A4、23667765 字节；抽查首页和末页，确认顺序到第 700 项、无空白末页且内容未裁切。峰值内存测量仍未完成，因此不扩大为所有长文规模都通过验收。

## 长混排文档的坐标保留（2026-10-15）

长列表／表格等大容器被按页裁剪时，所有顶层块及保留子块统一使用源文档测得的坐标、宽高和零外边距，避免大容器脱离普通流后，后续标题和正文又从页首排版而重叠。小型格式子树保持原结构，超高段落／代码的文字按源行拆分，保留行内字体与原位置，不再把多个行内片段各自重排为整段宽度；表格保留测得的单元格宽度，列表复用原生标记。

分页前一次测量文字行及短列表项、表格行和图像的边界，优先在这些内容之前换页；仍逐页克隆可见子节点并释放画布，保留原有资源上限、取消和超时处理。超高表格行遍历当前页相交的所有单元格，下一页从最早未完成的子节点继续，避免因第一列跨页就漏掉右侧列。超出整页的单个对象仍需要跨页，PDF 仍是图像页面。回归与真实浏览器导出的混排夹具、Poppler 页面检查及实际运行镜像的验收范围见 [本批记录](../reviews/2026-10-15/markdown-pdf-mixed-layout.md)，不由合成文档推断用户所有文档均已通过。

## SSH 历史恢复验收（2026-10-09）

真实双服务器 SSH/SFTP 回归两轮（race）通过工作区和绑定自动系统文档（workspace-sftp）两种位置的 Markdown 正文与嵌入图片恢复。验证无变更不重复建版本、外部资源变更生成新版本、预览后再次修改资源返回 409 且正文不写入、重新预览后恢复正文及图片、无关文件保留、恢复日志每项 applied/verified。同时核实生成设计文档跟随绑定目录，未绑定时使用原缓存落点。见 [真实 SSH 验收记录](../tasks/ssh-continuity-integration-20261009.json)。

这是鉴权 Handler 经真实 SSH 的 API 验收；浏览器 SSH 编辑与导出、跨主机故障恢复、Windows 和生产部署未由本次证明。

补充同日验收：独立 SFTP 引用（另一台服务器）也完成相同的整版恢复、409 冲突拒绝与逐项读回，且确认工作区服务器没有同路径副本。自动系统文档及独立 SFTP 来源均通过文件夹属性、重命名、压缩、重复压缩 409、解压内容读回和递归删除。最终双服务器 race 两轮耗时 19.962 秒。以上为 API 验收，浏览器操作和跨主机中断恢复仍待完成。

## 2026-10-10 系统 Safari 自动保存冲突验收

在隔离实例 `https://localhost:18211` 打开测试 Markdown，外部修改服务器正文后编辑本地草稿并开启自动保存。服务器拒绝旧哈希写入；Safari 显示服务器当前正文和本地恢复草稿的对比。采用包含两方内容的合并文本后，自动保存成功，磁盘正文与界面“服务器已保存”一致。验收后自动保存恢复为原来的关闭状态。

证据位于 `.agent-state/continuity-runtime/autosave-conflict-receipt-20261010.json` 与同目录截图。该验收仅证明本地工作区的系统 Safari 路径；SSH 浏览器冲突与 Windows 输入仍待分别验收。

## 历史恢复通知生命周期修复（2026-10-10）

系统 Safari 联调发现：冲突提示将全局通知元素移入临时对话框，关闭对话框删除该元素，后续历史恢复虽已落盘，但成功通知报错，恢复对话框滞留并出现 null。通知现在在对话框关闭时回到页面，元素缺失时自动重建，计时器绑定实际元素。

Node VM 验证关闭、复用、重建与计时器；候选构建通过。隔离实例 Safari 恢复测试版本 000002 后，对话框正常关闭，显示成功通知，正文与磁盘一致。当前测试文档不含嵌入资源，不作为嵌入资源浏览器恢复证据。该 UI 修复之后需要重跑发布全量门禁。

## 嵌入资源版本的系统浏览器验收（2026-10-10）

隔离实例 Safari 对测试 Markdown 和 SVG 完成：初始版本捕获；仅外部修改 SVG 生成第二版本，正文保持相同；预览恢复范围显示只改变 SVG；恢复后 SVG 内容及 SHA-256 与第一版本一致；再次无变更保存及查看历史仍保持三个版本（初始、资源变更、恢复），未重复建版本。

证据：`.agent-state/continuity-runtime/history-assets-receipt-20261010.json` 和同目录截图。此项验证本地工作区 SVG 嵌入资源；不扩展为所有嵌入格式或 SSH 浏览器路径均通过。通知生命周期回归已纳入 quick/full 门禁。

### SFTP 浏览器保存与冲突验收（2026-10-10）

在隔离实例的系统 Safari 中，以内部 Docker SFTP 夹具验证未保存来源的目录浏览、子目录/父目录切换与选定目录；登记读写来源后可打开并保存远程 Markdown，远端读取内容一致。远端外部修改后再次保存会显示服务器正文和本地草稿对比；合并内容后手动保存，两部分均保留。验收后已移除临时来源、夹具容器和内部网络。此证据覆盖本地隔离 SFTP 的浏览器手动保存与冲突，不代表公网 SSH、远程自动保存或全部嵌入资源恢复。任务记录见 `docs/tasks/aide-continuity-knowledge-20261009.json`。

### 服务中断后的草稿恢复验收（2026-10-10）

系统 Safari 在 18211 隔离实例打开工作区 Markdown 后，短暂停止该实例；手动保存提示失败，本地输入和预览保留，服务器文件仍是原文。使用 `start.command` 重启后，重新打开同一路径提示未保存草稿；点击恢复后预览立即更新，手动重试保存显示服务器已保存，磁盘正文核对一致。本次覆盖服务中断和手动重试，不等同物理断网、SSH 自动保存或 Windows 实机验收。用户确认暂时无 Windows 环境，该项保留未验证。


## Markdown 输出复制与代码特征高亮（2026-10-14）

代码块快速复制使用文档事件委托，聊天、文件预览以及重新插入的 Markdown HTML 都可响应。复制取代码原始文本，保留缩进、换行、中文和特殊符号；整条回答的复制也使用同一个兼容函数。优先调用剪贴板 API，缺失或拒绝时回退到当前模态框内的临时文本选择，随后恢复焦点和选择。只有实际复制成功才显示成功；两种方式都失败时显示失败。

非流式代码块优先按围栏的有效语言渲染；没有语言或语言未注册时，使用本地 highlight.js 根据内容自动识别，结构合法的 JSON 可直接识别。自动识别仅使用常见代码语言，低相关性保持原样；`text`、`plaintext`、`none` 等明确纯文本标记不自动着色。超过 65,536 字符的块和流式活动块跳过高亮，保留原文及复制能力；完成态重新渲染时补齐有界高亮。识别结果是语法猜测，不保证所有短片段或混合语言都能准确识别。

定向回归覆盖动态按钮、原文复制、剪贴板拒绝及缺失、模态焦点选择恢复、失败反馈、真实语法库识别和内容转义。系统 Safari 离线验收页运行实际 `renderMarkdown` 与本地模块，完成 Markdown、重新渲染后 Python、模态 JSON 的真实复制粘贴；模拟剪贴板 API 拒绝后，模态 Markdown 回退复制也通过。完整已鉴权 Aide 工作台仍受本机候选证书警告阻挡，尚未验证运行中实例，也未替换 8097 服务。

Code-block copying uses document-level delegation so serialized and reinserted Markdown remains interactive. The shared helper preserves original text and falls back from rejected or unavailable Clipboard API calls to a selection inside the active modal, restoring focus and selection afterward. Successful feedback follows a successful operation only. Whole-answer copying uses the same helper.

Completed code blocks prefer a supported declared language, then use bounded local syntax inference for unlabeled or unsupported languages. Valid JSON is recognized structurally; explicit plaintext, low-confidence input, blocks over 65,536 characters and active streaming blocks retain plain text. Native Safari offline acceptance using the actual renderer passed real copy/paste, rerender and modal checks, including a deliberately rejected Clipboard API. This is component acceptance, not acceptance or deployment of the authenticated running workbench.


## Git 文件历史比较 / Git file history comparison

2026-10-14 第一部分：新增只读 `GET /api/file/git-history`。参数 `path` 为工作区相对路径；`workspaceId` 固定当前工作区，`source` 可指向已启用的本地目录／Skill 来源。`root` 省略或为 `workspace` 时使用主工作区。仅检测工作区／来源根目录内的最近 Git 仓库，支持嵌套仓库与 `.git` 文件形式的 linked worktree；不向根目录外的父仓库扩展。SSH 主工作区已扩展为读取远端 Git，详见下方补充；独立 SFTP、其他网络来源和非工作区根仍返回 `available:false` 及原因，均不使用本机缓存替代。未安装 Git／无仓库也返回明确不可用状态。

不带 `from`／`to` 时返回 HEAD、当前路径最近 50 次提交的完整编号、短编号、作者时间、标题以及 `truncated`；不追踪重命名前路径。新仓库没有 HEAD 时仍返回 `available:true`、空 HEAD 和提交列表。比较参数接受 `HEAD`、完整 40／64 位小写提交编号、`index`（暂存区）、`worktree`（磁盘正文）。省略一侧默认 HEAD → worktree；无 HEAD 时应显式选择 index。响应含 `before/after` 的 `ref`、`exists`、正文 SHA-256、`content` 和标准 unified `diff`。缺失条目按不存在的空侧比较，可查看未跟踪文件新增和磁盘删除；不等同于 Git 状态提交／恢复。

每侧正文限 2 MiB，仅接受 UTF-8 无 NUL 的普通文件；拒绝符号链接路径、子模块条目、冲突暂存区、非法路径与任意 revision 表达式。Git 请求有 12 秒总截止时间和有界输出，本地执行不经 shell；禁用 hooks、fsmonitor、external diff、textconv、继承的 Git 环境路由和网络协议。补丁使用临时正文快照生成并清除临时路径，不修改工作树、index、提交或远端。第一部分提交 `d1eb320` 交付接口；第二部分交付下述界面。后端验证不等同于浏览器或8097上线验收。

Stage 1 adds a read-only API scoped to the selected file within a local workspace/source repository, including nested and linked worktrees. It lists at most 50 commits affecting the current path and compares HEAD, full commit IDs, the index and disk content. It does not follow rename history. Missing sides support additions/deletions; untracked files are not silently omitted. Each text side is limited to 2 MiB, with bounded subprocess output and a 12-second deadline. For local reads no shell is used; hooks, external filters, fsmonitor, optional index writes and Git network protocols are disabled. Remote workspace caches are never presented as remote Git repositories. UI integration and running-instance acceptance are tracked separately.

验证：`go test -mod=vendor ./internal/server -run '^TestFileGitHistory' -count=1 -timeout=3m` 与对应 `-race` 均通过（隔离 golang:1.26-bookworm、本机网络关闭）。覆盖提交间、HEAD／index／磁盘、未跟踪、删除、嵌套／linked worktree、字面路径、越界、符号链接、非法 ref、二进制／超限与 SSH 缓存隔离；检查 index／磁盘正文未变化、恶意 fsmonitor／external diff 未执行。日志见 `.agent-state/file-history-git-compare/backend*.log`。


### 使用方式与比较口径 / UI and comparison semantics

1. 打开工作区文本文件。Markdown 保留原有“历史版本”入口；其他文本文件只有在所属目录检测到 Git 仓库后才显示此入口。检测响应绑定文件、来源、工作区与 root，切换后旧响应不再影响入口。
2. 在历史窗口切换“Aide 快照”和“Git 比较”。Markdown 快照的引用资源、恢复范围、哈希保护和备份管理保持原有语义；Git 比较不提供恢复、checkout、reset、add 或提交操作。
3. 选择基准及目标版本。支持 HEAD、提交列表中的完整提交、暂存区与磁盘正文；目标还可选择“编辑器正文（含未保存）”。窗口中的 HEAD 固定为读取列表时的提交编号，避免比较时分支移动而标题仍显示旧编号。没有提交的新仓库默认比较暂存区与编辑器正文。
4. 点击“比较版本”。双栏用红／绿／琥珀标记删除／新增／修改，正文滚动联动；窄屏上下排列。编辑器目标使用点击时的正文快照，明确标记可能未保存；不上传草稿，不把 Git 返回的磁盘补丁冒充草稿补丁。选择非草稿目标时可展开 unified diff。

每侧超过约 6000 行时显示完整正文而不展开逐行着色节点，避免超长文本造成 DOM 爆炸；正文仍受 2 MiB 限制。切换版本／来源标签会清除过期比较并中止请求，返回 Git 标签后可重新比较。关闭窗口、锁屏、切换文件或工作区会阻止旧响应展示。`probe=1` 只检测仓库，不读取提交列表。

The history entry remains available for Markdown snapshots and appears for other text files only after a scoped Git probe. The existing snapshot restore/backup workflow stays separate from Git's read-only comparison. The editor target uses the buffer captured when Compare was clicked, including unsaved edits, without uploading it. HEAD is pinned to the commit shown in the selector. Disk/index/commit comparisons can show a unified patch; draft comparisons never reuse a disk patch. Large line counts fall back to full plain text, and stale/closed/locked contexts cannot repaint the dialog.

### 分块交付与验收 / Incremental delivery and validation

- 第一块：`d1eb320` 后端及中英接口文档已 push 到 `feat/file-history-git-compare`。
- 第二块：`4cb9017` 前端组件、文本入口检测、双栏比较、中英词条、探测快路径和浏览器夹具已随文档一起 push。两部分均位于 `origin/feat/file-history-git-compare`；未合并 main、未发布 release、未替换8097。
- 后端定向 race：`go test -race -mod=vendor ./internal/server -run '^(TestFileGitHistory|TestMarkdownHistory)' -count=1 -timeout=3m`，最终日志 `.agent-state/file-history-git-compare/final-backend.log`。新增本地来源、新仓库无 HEAD、继承 GIT_DIR 与缺少 Git 的边界用例；不使用全仓测试冒充真实部署。
- 前端定向：`node scripts/git-history.test.cjs` 与 `node scripts/markdown-history-writable.test.cjs`；快速门禁包含新组件语法和回归。
- Safari 实接口：隔离的 `http://localhost:18618/fixture`，实际 Go handler + 实际历史组件 + 临时合成仓库。观察 HEAD 与未保存正文、HEAD 与真实暂存区、两个提交选项、Markdown 快照／Git 标签切换、非 Git 文本隐藏入口、深色桌面与浅色 390px iframe 布局。此夹具不接真实用户目录、8097或模型；未进行真实 SSH、所有平台、真实8097部署验收。

复现浏览器夹具：在隔离 Docker 中，仅将 `18618` 发布到 `127.0.0.1`，设置 `AIDE_GIT_HISTORY_BROWSER_FIXTURE=1` 并运行 `go test -mod=vendor ./internal/server -run '^TestFileGitHistoryBrowserFixture$' -v -count=1 -timeout=20m`。默认测试会跳过此人工夹具；`scripts/fixtures/git-history.html` 与 opt-in test 中的数据均为合成。浏览器夹具直接提供仅限测试的只读 handler，不能用于生产；生产路由仍通过原 Handler 的 Bearer/Origin 校验。

验收后已停止隔离18618夹具；无需保留测试服务。新本机镜像 `aide:local` 已构建，镜像身份和运行验收缺口见 [任务账本](../tasks/file-history-git-compare-20261014.json)。


### SSH 工作区 Git 比较（2026-10-14）

SSH 主工作区复用同一历史窗口，HEAD、完整提交编号、暂存区和磁盘正文均来自远端实际仓库；编辑器正文仍是点击比较时的本地缓冲。远端支持工作区根内的嵌套仓库、未跟踪与删除文件、新仓库无 HEAD；不追踪重命名前路径，也不搜索根目录外的父仓库。独立 SFTP 来源尚未扩展。

读取复用当前 SSH master，每条命令固定目标、工作区身份和连接代次，前后复核并与 master 关闭串行化。SSH 传输必须经远端 shell，使用固定脚本和单引号逐项转义参数；`env -i` 清理继承环境，禁用 Git 全局/系统配置、hooks、fsmonitor、lazy fetch、replace objects及网络协议。路径逐组件检查符号链接，远端只读 Git 对象和至多 2 MiB 正文，补丁在本机临时正文快照生成；不用本机缓存伪装远端 Git。依赖远端 POSIX shell、Git、`head -c` 及本机 Git；当前验证环境为 Linux/OpenSSH。

远端缺 Git／无仓库返回明确不可用原因，连接失败、权限、超限、二进制、非法 ref、工作区切换返回错误。请求总截止时间12秒，包含 SSH 建连；只读检查不替代远端服务器权限或完整 OS 沙箱。远端路径检查和打开不是原子的，其他进程同时替换目录/符号链接仍存在竞态，不宣称具有本地 `os.Root` 等价的内核路径隔离。

SSH workspaces use the same comparison dialog with commit, index and disk contents read from the actual remote repository. Drafts stay local. Fixed scripts and individually shell-quoted arguments run over the existing SSH master, scoped to the captured connection generation; results are checked before and after each command. Git configuration/external execution and network fetching are restricted. Comparison never writes the repository or index. Independent SFTP sources remain unsupported. Current live validation covers isolated Linux/OpenSSH fixtures, not a user's SSH host; remote path checks are not atomic against concurrent replacement.

## 实际文档预览（源码 0fef6411）

2026-10-17 使用配置绑定目录里的 example-notes.md 直接打开。此图可核对编辑/预览/导出入口和表格显示，不证明 PDF 导出成功、图片加载上限或全部分页排版已验收。

![实际文档预览](../images/current-20261017/12-markdown.jpg)

[完整原图、环境与边界](../ui-evidence.md)。
