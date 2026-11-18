# Aide 内置 Office 文档

在文件面板直接打开 DOC、DOCX、ODT、XLS、XLSX、ODS、PPT、PPTX 或 ODP。Aide 使用内置 LibreOffice Writer、Calc 和 Impress 生成版式预览，首次打开时无需安装插件或填写服务地址。表格预览按工作表切换，保留可见空白表和合并单元格。演示文稿预览按页显示，文档字体使用本机可用字体。

版式预览将原件分块读取到临时文件。生成的 PDF 按浏览器需要的范围传输，原件和预览 PDF 都没有固定字节上限。LibreOffice 处理的是临时副本，宏和外部网络访问关闭。特别复杂的文件仍可能受磁盘空间、可用内存、转换时长和单页渲染资源影响。

DOCX 的「正文与批注」提供原文选区和批注操作。XLSX 的「单元格」支持保存指定单元格，检查文件版本和来源写权限。版式预览支持静态页面，浏览器内完整排版编辑随独立 Office 插件退役。AI 的结构化生成、读取、精确修改、检索和批注由 Aide 内置工具提供，可在工具权限中逐项管理。使用云端 AI 仍依赖所配置的模型服务。

现有独立 Office 插件设置和文件保留在工作区历史数据中。新版宿主直接提供文档能力并拒绝安装、启动或升级旧插件。之前的 Collabora 集成见[历史验收记录](../reviews/2026-11-16-office-offline.md)。

## 生成

模型工具 `office_create` 优先写当前工作空间绑定的自动系统文档目录（本地或工作空间 SFTP），`path` 相对该目录；未绑定非空路径时维持写当前工作区。配置失败时返回错误，不静默回落。参见[路径说明](../workspace-paths.md)。

| format | path 后缀 | content 结构 |
| --- | --- | --- |
| `docx` | `.docx` | `{ "title": "标题", "blocks": [{"type":"heading","text":"小节","level":1},{"type":"paragraph","text":"正文"},{"type":"table","rows":[["A","B"]]}] }` |
| `xlsx` | `.xlsx` | `{ "sheets": [{"name":"数据","rows":[["名称","数量"],["甲",12]]}] }` |
| `pptx` | `.pptx` | `{ "slides": [{"title":"标题","body":"正文"}] }` |

路径相对当前工作区；同名文件不会自动覆盖。工具报告字节数，并要求从文件面板复核成品。生成的是标准 OOXML ZIP 文档；不承诺复杂 Word 样式、图表、宏或 PowerPoint 动画。

## XLSX 查看和修改

在工作目录或引用中点开 `.xlsx`，表格按 100 行 × 26 列分页，支持工作表和行列切换。
输入单元格内容后点查看器内“保存文件”。`=` 开头保留为公式；纯数字转为数值；清空输入框清除单元格。
只读引用仅查看，读写引用才可保存。保存对比打开时的文件哈希与工作区身份；文件被其他操作改动后返回 409，须重新打开。

编辑使用 `openpyxl` 修改指定单元格并保存原工作簿，未触碰的工作表、普通单元格和样式尽量保留。复杂扩展（如部分外部链接、嵌入对象、特殊图表）可能不被 `openpyxl` 完整往返保留；编辑此类文件前建议备份。单次保存最多 500 个单元格。旧版 `.xls` 暂不支持。

API：`GET /api/office/xlsx?path=...&source=...&sheet=...&row=...&col=...` 返回窗口、hash、workspaceId、readOnly；`PUT /api/office/xlsx` 提交 `{path,source?,hash,workspaceId,changes:[{sheet,ref,value}]}`。

Office 文件在 aide 的 `read_file` 工具中可按文本提取，适用于本地/SSH 工作区及文件型引用来源。二进制文件的生成与修改仍通过专用 Office 工具/API，而非文本 `write_file`。

## 解析资源边界

AI 工具 `office_document_read` 可分页读取 DOCX、XLSX 和 PPTX。它按流将原文件复制到私有临时目录并计算完整文件的 SHA-256，原文件没有固定字节上限。解析只展开本次分页需要的 XML 部件，检查 CRC，合计最多 128 MiB。每页最多返回 200 个结构块和 60,000 个正文字符，可使用 `nextOffset` 继续读取。暂存耗时和可用磁盘有各自的限制。大于 64 MiB 的文件在该工具中显示为只读。

旧DOCX工具和本地附件也在启动解析前检查输入：原文件最多64MiB，OOXML最多10000个ZIP条目、合计128MiB展开内容。校验逐流读取实际展开字节并验证ZIP完整性，不只信任目录中声明的大小。PDF保留原文件大小/格式检查，解析内存由进程限制兜底。

Office Python解析最多同时运行两个；Linux在导入解析库前设置512MiB地址空间上限，继承的更严格限制保留，等待和执行共用60秒超时。超限明确失败，不保证所有复杂文档都可解析。DOCX/XLSX/PPTX/PDF文本按行/段落迭代，最多保留240000个字符及4096个文本块并标记截断；调用入口仍应用各自原有的上下文预算。本轮验证覆盖Linux，不代表macOS/Windows解析进程限额或桌面Office验收。

## DOCX 原生批注

在工作目录或引用的 DOCX 预览器中选中文字并输入批注，会直接修改该 DOCX：批注正文保存在 `word/comments.xml`，选中原文在 `word/document.xml` 中有 `commentRangeStart` / `commentRangeEnd` / `commentReference`。再次从文件面板下载的是同一份带批注的文件。只读引用不能添加批注；保存时比较文件 SHA-256 与工作区身份，冲突返回 409，需重新打开。旧版侧车批注仍显示为“旧批注：未写入文件”，用户可逐条点“写入 DOCX”；复制成功后原侧车记录保留并标为“已写入文档”，避免破坏历史数据。锚点找不到时不会猜测写入，须重新选中原文添加。

Aide 内置工具提供 `office_comments(path)`，返回批注 ID、作者、正文、`anchorQuote` 与 `anchorValid`；`office_comment_edit(path,id,expectedText,newText)` 依据批注指示修改**该批注锚定的原文**。aide 应先读取批注并理解指示，再提供替换文本。只有锚点仍有效、`expectedText` 与当前原文逐字一致且没有嵌套/复杂对象时才写入；修改后批注保留，供用户复核。两个工具支持本地和 SSH 工作区。原有 `docx_list_comments`/`docx_add_comment` 也改用相同通道。

API：`GET /api/office/docx/comments?path=...&source=...` 返回 `comments,hash,workspaceId,readOnly`；`POST` 以 `{path,source?,hash,workspaceId,quote,anchorIndex?,text,author?}` 新增；`PUT` 以 `{path,source?,hash,workspaceId,id,expectedText,newText}` 精确替换。预览器仅支持同一段落内的普通文本选区。修改使用 `python-docx` 保存原文件，复杂 Word 扩展、修订、宏不承诺完整往返；重要文件建议先备份。OOXML 结构已通过自动测试，WPS 桌面实际打开仍需单独验收。

## Office 输出目录配置截图（2026-10-17 源码基线 0fef6411）

2026-10-17：生成的正式文档跟随工作空间自动系统文档绑定。用户指定普通引用时，以来源 ID 和相对路径表达目标，仍校验启用、可见和可写。截图仅记录绑定配置，不证明 DOCX/XLSX/PPTX 的实际生成或版式验收。

![Office 输出目录的实际配置](../images/current-20261017/02-workspace.jpg)

[完整原图、环境与边界](../ui-evidence.md)。
