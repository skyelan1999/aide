# Office 1.1.0 文档检索

保留 Office 创建和批注工具，新增只读 `office_document_search`。

参数：`query`（1–200字符）、`mode`（`original` 原文逐字搜索或 `rag` 本地TF-IDF片段排序）、可选 `source`（已启用来源ID：本地、Skill、SFTP、HTTP、FTP/FTPS、SMB；MCP仅检索已发现工具说明）、`path`（相对文件路径）。返回最多8个真实片段，含文件ID、片段ID、SHA-256、页／段落／表格行／工作表行／幻灯片定位和范围诊断。工具不调用生成模型、不运行宏、无OCR；主Agent可依据结果继续回答并引用。

插件须在支持本工具的新版Aide中更新并启用；旧版本原生执行器无法执行新工具。星图与内置 `document_search` 共用提取及检索服务。详细范围见 `docs/architecture/knowledge-map.md`。本次没有自动更新已安装的用户插件。
