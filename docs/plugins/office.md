# Office 工具集：生成、DOCX 批注与 XLSX 编辑

Office 插件在“插件”面板中登记 `office_create`；实际执行由 aide 原生工作区后端负责，以便在 SSH/SFTP 工作区沿用已认证会话。插件关闭后，模型不再获得生成工具；XLSX 文件查看器仍作为文件面板基础能力可用。离线使用容器中固定版本的
`python-docx`、`openpyxl`、`python-pptx`，不依赖外部 Office 服务。

## 生成

模型工具 `office_create` 直接写当前工作区（本地或 SSH/SFTP）：

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

## DOCX 原生批注

在工作目录或引用的 DOCX 预览器中选中文字并输入批注，会直接修改该 DOCX：批注正文保存在 `word/comments.xml`，选中原文在 `word/document.xml` 中有 `commentRangeStart` / `commentRangeEnd` / `commentReference`。再次从文件面板下载的是同一份带批注的文件。只读引用不能添加批注；保存时比较文件 SHA-256 与工作区身份，冲突返回 409，需重新打开。旧版侧车批注仍显示为“旧批注：未写入文件”，用户可逐条点“写入 DOCX”；复制成功后原侧车记录保留并标为“已写入文档”，避免破坏历史数据。锚点找不到时不会猜测写入，须重新选中原文添加。

Office 插件提供 `office_comments(path)`，返回批注 ID、作者、正文、`anchorQuote` 与 `anchorValid`；`office_comment_edit(path,id,expectedText,newText)` 依据批注指示修改**该批注锚定的原文**。aide 应先读取批注并理解指示，再提供替换文本。只有锚点仍有效、`expectedText` 与当前原文逐字一致且没有嵌套/复杂对象时才写入；修改后批注保留，供用户复核。两个工具支持本地和 SSH 工作区。原有 `docx_list_comments`/`docx_add_comment` 也改用相同通道。

API：`GET /api/office/docx/comments?path=...&source=...` 返回 `comments,hash,workspaceId,readOnly`；`POST` 以 `{path,source?,hash,workspaceId,quote,anchorIndex?,text,author?}` 新增；`PUT` 以 `{path,source?,hash,workspaceId,id,expectedText,newText}` 精确替换。预览器仅支持同一段落内的普通文本选区。修改使用 `python-docx` 保存原文件，复杂 Word 扩展、修订、宏不承诺完整往返；重要文件建议先备份。OOXML 结构已通过自动测试，WPS 桌面实际打开仍需单独验收。
