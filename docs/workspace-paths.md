# macOS 工作目录选择 / Workspace selection

工作目录与 Docker 可访问范围是两项不同配置。浏览器可以选择已挂载范围内的任意目录；保存工作目录不会自动添加新的 Docker 挂载。

## 从根目录导航

Docker Desktop 的 Linux 虚拟机根目录不是 macOS 根目录。不要直接用 `/:/local`。
本项目提供组合配置，按真实路径展示已共享的 `/Users`、`/Volumes`、`/private`：

```sh
python3 scripts/configure-local-root.py --path /
bash scripts/aide.sh start
```

配置脚本将组合配置写入 `.env` 的 `COMPOSE_FILE`。以后双击 `start.command`、运行 `scripts/aide.sh` 或 `docker compose` 均读取同一设置，无需另记启动命令。端口由 `.env` 的 `AIDE_PORT` 决定，默认 8097。

这会重建 aide 容器并保留命名数据卷，不重启 Docker 或其他项目。此模式使 aide 的命令、可信插件可访问上述共享目录，请按实际用途启用。若 Docker 拒绝某一目录，先在 Docker Desktop 的文件共享设置中开放该目录。

打开工作空间配置 → 浏览，可输入绝对路径、点击“前往”或逐级点击“上级”。只有到达 `/` 才禁用“上级”。选择目录后点击“保存配置”生效。当前工作目录保持不变，直到用户保存。

这不是完整 macOS 文件系统映射：未配置的 `/Applications`、`/System` 等目录不会出现在列表中。需要其他目录时，在组合配置中添加同名宿主机路径，例如将 `/Applications` 挂载至 `/local/Applications`，并确保 Docker 允许共享。系统与文件权限仍然适用。

回退：`python3 scripts/configure-local-root.py --path "/你的项目目录"` 后再次启动，恢复单目录挂载；如当前工作目录超出该范围，先在界面将工作目录改回可访问位置。

## English

The working directory is separate from Docker's accessible mount boundary. The configuration helper stores the selected Compose files in `.env`; `start.command`, `scripts/aide.sh`, and `docker compose` use this same configuration and port (8097 by default). Use the commands above to browse shared macOS `/Users`, `/Volumes`, and `/private` from a common `/` view. The Linux VM root is never presented as the macOS root. Other system directories require explicit additional mounts and Docker file-sharing permission. Select a folder, then save the workspace configuration. Data volumes are preserved; only aide is recreated.

## 自动生成文档跟随绑定路径（源码候选，2026-10-09）

工作空间设置中的“自动系统文档”路径是自动建档的目标根目录。绑定非空路径后，需求、设计、实现记录、验证记录及问题报告分别写入该根下的 `requirements/`、`designs/`、`implementations/`、`verifications/`、`problem-reports/`，索引与文档存放在同一子目录。Office 插件创建 DOCX/XLSX/PPTX 时，`path` 也相对该绑定根。读取产物可用 `read_file(source="system-docs", path="相对路径")`。

本地绑定使用本地来源根；SSH 工作空间选择“工作空间 (SFTP)”时沿用该工作空间连接写入远端文档路径。绑定不可访问时返回错误，不静默写入其他位置。绑定为空时维持既有行为：阶段文档使用项目缓存，Office 文件使用工作目录。问题报告列表根据绑定选择来源。

通用文件提案 `write_file`、手动文件操作和 draw.io 图仍使用既有指定工作目录；此改动不按扩展名重定向代码或用户指定的文件。旧缓存文档不自动迁移。

当前完成源码修订、语法与构建检查；未运行本地／SSH创建、工作空间切换及浏览器端到端验收，未部署生产。详见[任务记录](tasks/generated-docs-binding-20261009.json)。

### 自动系统文档来源权限

`system-docs` 是固定的可读写来源，加载来源和保存工作空间配置均恢复 `enabled=true`、`rw=true`、`builtin=true`，引用面板标注“固定 · 读写”。文件保存接口继续校验来源权限与底层文件系统权限；读写标记不表示自动绕过服务器目录权限。AI 的来源列表同时返回 `rw`，绑定目录的文档写入由工作流建档与 Office 创建工具执行，通用引用写入工具仍保持原权限限制。

此权限校正为源码候选，浏览器显示与实际保存尚未验收，未部署生产。

### 文件面板“＋”创建目标（源码候选）

新建菜单记录当前工作目录或引用来源及正在浏览的子目录。新文件编辑器保留该 `root/source`，保存时携带来源 ID；新文件夹请求携带同一来源及 `parentPath`。只读来源禁止新建；菜单打开后切换来源或目录时，提交要求重新打开新建菜单，避免写入另一位置。新文件路径输入框默认填入当前子目录前缀，仍允许输入该来源根内的其他相对路径。

此修订未部署；新建文件与文件夹的浏览器及实际落盘验收尚未运行。


### SSH 引用目录选择与 Markdown 图片（2026-10-09）

SSH/SFTP 引用配置的“浏览”使用当前表单主机、端口、用户名及认证方式，调用 `POST /api/sources/browse`。浏览连接使用独立临时 socket，完成后关闭，不改写来源注册表、不复用 SSH 工作空间连接。编辑已保存来源且凭据留空时，仅在连接身份一致的情况下使用已有凭据；更换主机后需填写对应凭据。选择器只列出目录，浏览过程不提供新建和重命名。

Markdown 预览中的本地图片按文档所属 root/source 请求 `/api/file/raw`，相对路径以文档目录解析。侧栏与独立文件页均传递文件来源；不再将引用目录的图片强制请求到工作目录。支持 `../`、来源内根路径和 URL 编码文件名，图片路径仍受后端来源边界检查。

本轮已执行路径解析回归；SSH 目录选择定向测试使用一次性 OpenSSH/SFTP 客户端替身。真实服务器、Safari 渲染和当前运行实例生效状态单独验收，不能由替身测试推定。


### CSV / TSV 表格视图（2026-10-09）

CSV / TSV 在文件侧栏和独立文件页默认打开为可编辑表格，采用 A/B/C 列标和数字行号。支持逗号、分号、Tab 分隔，引号包裹字段、双引号转义和单元格内换行；值保持字符串，前导零不会被转成数字。可切换到原文模式编辑完整文件，再使用现有保存按钮提交。只读引用与知识来源只显示数据，不开放写入。

单元格修改同步到原文；序列化保留 BOM、换行风格和末尾换行。保存仍使用现有 `/api/file` 的来源权限、工作空间身份和 hash 冲突检查。公式作为文本显示，不执行计算。表格最多展示 500 行、100 列，未显示数据仍保留；超过 2 MiB 时提示使用原文模式，避免阻塞主线程。当前文本接口要求 UTF-8，不自动转换 GBK/UTF-16。

新增组件已在本机隔离网页中实际打开，确认列标、前导零、含逗号字段、换行字段与修改后原文同步；该预览复用产品组件源码，不代表运行中的 Aide 已更新或完整 API 保存链路验收通过。


### Markdown 标题导航（2026-10-09）

文件编辑器的预览模式与独立文件标签页共享标题目录，索引渲染后的 H1–H6 标题，包含中文与同名标题；不索引代码块。宽窗口目录位于正文左侧，窄窗口折叠在顶部。点击目录滚动到对应章节，当前章节随正文滚动高亮；支持键盘访问，并遵守系统减少动态效果设置。无标题文档不显示空目录。修改 Markdown 后重新切换到预览即可重建目录；导航不修改文件内容。

实现：`web/markdown-outline.js` 与 `app.js` 的 `setupMarkdownPreview`，布局在 `macos.css`。本次查看了隔离的组件页面，尚未进行运行中 Aide 的完整文件打开/保存联调。


### Markdown 文件、图片与 draw.io 插入（2026-10-09）

Markdown 编辑器和独立文件页提供“插入图片 / 插入文件 / 插入链接 / 插入 draw.io 图”工具栏，可在正文光标位置插入，也支持在文本编辑区粘贴图片、拖入文件。图片使用 `![名称](相对路径)`，附件使用 `[名称](相对路径)`，链接可指向现有相对路径或 HTTPS 资源。预览中的相对附件链接在独立 Aide 文件页打开。

上传附件保存在当前 Markdown 同来源、同目录的 `<文档名>.assets/`，以随机前缀避免重名；通过现有 `/api/file/upload` 通道写入，保留来源读写限制。工作区上传携带读取文档时的 `workspaceId`，工作区已切换则拒绝。附件先保存，正文需点击“保存文件”持久化引用；若插入期间正文或来源变化，不覆盖新的正文，提示重新插入引用。正文不保存访问令牌，也不嵌入附件的 base64。取消保存正文可能留下已经上传的附件，不自动删除用户文件。

内置本地 draw.io 编辑器通过 iframe 的 init/load/save/export 协议导出 `xmlsvg`，保存为 `.drawio.svg`，保留图像及可编辑图表数据。上传 `.drawio` XML 时先在编辑器打开，保存后转换并插入 SVG。预览中的 `.drawio.svg` 可双击，或点击“编辑 draw.io 图”再次编辑，保存带原文件 hash 校验。普通 PNG/JPEG 图片只作为图片插入。只读文档隐藏插入与图表写入入口。

参考：[Docmost 编辑器](https://docmost.com/docs/user-guide/editor)、[Docmost 图表存储](https://docmost.com/docs/user-guide/diagrams)、[draw.io 嵌入协议](https://www.drawio.com/docs/reference/embed-mode/)。Aide 保持 Markdown 文件格式与本地资源存储，不将该实现描述为完整 Docmost 富文本编辑器。

本次 JS 语法、diff 和 Go 编译检查通过。隔离组件浏览器预览实际插入中文相对图像链接，打开内置 draw.io、添加文字节点、导出 SVG 并插入 Markdown；预览资源只在页面内存中。生产文件上传、引用/SSH 权限、再次编辑后的持久化和真实文件保存联调尚未执行，当前运行服务未更新。

### 文件查看与渲染偏好

设置面板新增“文件查看与渲染”，六项均采用复选框：显示行号、代码语法高亮、Markdown 默认渲染预览、Markdown 标题目录、Markdown 插入工具栏、CSV / TSV 默认表格预览。默认全部开启。

偏好通过 `window.aideUI` 自动写入当前浏览器的 `localStorage['aide.ui']`，同源标签页通过 `storage` 事件同步；不是服务器、账号或工作区配置，也不会自动保存正在编辑的文件内容。行号和语法颜色即时应用于已打开编辑器；目录与插入工具栏即时更新；默认预览选项在下次打开文件时生效，手动切换预览仍可用。

工作台文件编辑器及独立文件页共用 `text-editor.js`。所有作为文本打开的文件（含 TXT、未识别语言与只读来源）均支持逻辑行号；原文采用横向滚动，避免软换行造成行号错位。空行与末尾空行计入行号。编号栏只绘制可见行，滚动时同步位置。语法高亮仅用于已识别语言；超过 500,000 字符时保留可编辑原文与行号，避免大文件高亮阻塞。二进制和专用可视化查看器不显示源文行号。

2026-10-09：JavaScript 语法检查、Go 编译通过；在独立组件预览中观察了 TXT/JavaScript 行号、滚动到第 120 行的对齐、取消勾选后刷新保留及窄屏布局。此记录不代表生产实例已升级或完整 Aide 设置面板验收通过。

### 独立文件定位与自动保存

独立文件页保留 `#file` 定位，并将文件元数据绑定到当前历史记录（`file-route.js`）。文内锚点不覆盖文件定位；处理 URL 中的临时 token 只删除 token 字段，不再清空整个 fragment。重新认证已打开的文件页保留编辑内容，不重复装载原文。定位值支持含百分号的路径；这是定位恢复修正，不代表所有浏览器长时间闲置异常已复现和排除。

工作台编辑器及独立文件页在“保存文件”右侧新增自动保存开关，默认关闭，开关偏好保存在当前浏览器 `aide.ui.editorAutoSave`，同源标签页同步。切换使用滑轨动画，减少动效设置下取消过渡。开启后输入停顿 1.2 秒发起保存，中文组合输入结束后才计时。保存使用当时的内容快照、文件路径、来源、工作区与 hash；自动/手动保存串行执行。文件切换后旧响应不覆盖新文件状态，尚未加载完成的上下文不参与写入。只读和专用画布/二进制查看器不自动写入；draw.io 仍使用自身保存操作。

HTTP 冲突、认证或其他保存错误会暂停当前文件的自动保存并提示，禁止无条件重试/覆盖。修复错误后手动保存成功，或重新切换自动保存开关可继续。锁屏和登录等待期间不写入；解锁后再排队。存在未保存编辑时，离开页面使用浏览器离开提示；浏览器进程被强制关闭时不能保证最后一次修改写入。

### 文本编辑光标与高亮对齐

代码、Markdown 源文与 TXT 共用原生文本输入框。语法高亮仅改变颜色，不使用会改变字宽的粗体、斜体或额外内边距。高亮绘制区域跟随输入框的 clientWidth/clientHeight，排除原生滚动条与边框；横纵滚动以输入框为唯一基准。字体加载、缩放及尺寸变化后重新测量。中文输入法组合输入期间显示原生文字，结束后恢复高亮，避免组合文本与高亮层不同步。

2026-10-09 已在 macOS 浏览器组件预览中检查常驻 17px 滚动条、长行、中文、制表符与横纵滚动；Windows 原生 Edge/Chrome 与中文输入法仍待实机验收。源码修复不代表已更新运行实例。
