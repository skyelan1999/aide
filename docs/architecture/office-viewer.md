# Office 文档内联查看方案（docx / xlsx / pptx）

> 状态：**本期不实施**。本文档作为后续任务输入，给出可离线 vendor 方案与许可取舍。
> 本期已落地：PDF 内联预览（见 `internal/server/web/vendor/pdfjs/README.md`，PDF.js 4.4.168，Apache-2.0）。

## 目标约束（与 PDF 查看器一致）

- **离线 / 空气间隙可用**：全部资源 vendor 进 `internal/server/web/vendor/`，经 `//go:embed web/*` 打包，不访问 CDN。
- **固定版本 + 记录 SHA-256**：从官方 / jsDelivr 取，写入 `vendor/<lib>/CHECKSUMS.txt`。
- **复用现有取流**：经 `/api/file/raw?path=...&access_token=...` 取 ArrayBuffer（与图片/STL/PDF 相同）。
- **深浅主题**：沿用 CSS 变量；只读查看、禁用保存（与 PDF 一致）。
- **不回归**：图片 / STL / drawio / md / PDF 已有分支互不影响。

## 方案对比

### 1. docx（Word）

| 方案 | 说明 | 许可 | 离线 | 取舍 |
|---|---|---|---|---|
| **mammoth.js** | 把 .docx 转为语义化 HTML（忽略样式，重排版） | BSD-2-Clause | ✅ 纯 JS | 推荐：只读预览足够；**不保留**复杂排版/字体 |
| **docx-preview** | 按原排版近似渲染（表格/分页/字号） | MIT | ✅ 纯 JS | 推荐用于「所见即所得」需求；体积较大 |
| LibreOffice 转 PDF | 服务端 `soffice --headless --convert-to pdf` 后复用 PDF.js | LibreOffice MPL-2.0 | ✅ 需装 LibreOffice 运行时 | 保真最高；镜像体积 +~400MB，启动慢；见下「通用方案」 |

**建议**：先 vendor `docx-preview`（MIT）做内联预览；对保真要求高的文档走 LibreOffice→PDF 通道。

### 2. xlsx（Excel）

| 方案 | 说明 | 许可 | 离线 | 取舍 |
|---|---|---|---|---|
| **SheetJS（xlsx.js）** | 解析工作簿为 JSON / 渲染表格 | Apache-2.0（社区版） | ✅ 纯 JS | 推荐：用 `XLSX.utils.sheet_to_html` 或自渲染 `<table>`；注意 **CDN 版与 npm 社区版** 的功能/补丁差异，固定 npm 版本 |
| Luckysheet / Univer | 在线表格组件，功能重 | 见各自许可 | ✅ 体积大 | 仅当需要编辑/公式时再评估；本期只读预览不必 |

**建议**：vendor `xlsx`（SheetJS 社区版，Apache-2.0），只读渲染表格网格。

### 3. pptx（PowerPoint）

纯 JS 保真渲染 pptx 非常困难（版式/母版/动画）。可选：

| 方案 | 说明 | 许可 | 离线 | 取舍 |
|---|---|---|---|---|
| **LibreOffice 转 PDF** | `soffice --headless --convert-to pdf` → 复用已落地的 PDF.js 查看器 | LibreOffice MPL-2.0 | ✅ 需运行时 | **推荐**：一套 PDF 渲染管线通吃 pptx/复杂 docx/xlsx |
| pptxjs 等纯 JS 库 | 多为实验性、保真差 | 各异 | ✅ | 不建议作为正式方案 |

## 推荐落地方案（后续任务）

**双通道**：

1. **轻量纯 JS 通道**（默认，零运行时依赖）：
   - `.docx` → `docx-preview`（MIT）
   - `.xlsx` → `SheetJS`（Apache-2.0）
   - vendor 到 `vendor/docx-preview/`、`vendor/xlsx/`，固定版本 + CHECKSUMS。
2. **高保真通道**（按需，可选构建标签）：
   - 服务端容器内装 LibreOffice（`soffice`），新增 `GET /api/file/convert?path=...&to=pdf`，
     把 pptx / 复杂 docx / xlsx 转为临时 PDF 字节流，前端复用 `setupPdfPreview`。
   - 镜像膨胀（+~400MB），建议作为可选镜像 flavor，不进默认镜像。

## 前端集成点（与 PDF 同一模式）

- `isDocxPath / isXlsxPath / isPptxPath()` 辅助函数。
- `openFile()` 与 file-view 两处分支，把对应扩展名加入「走 raw 端点、跳过 /api/file 文本」的集合。
- 新增 `setupDocxPreview / setupXlsxPreview`，签名与 `setupPdfPreview(container, path, root, source)` 一致。
- 工具栏复用 PDF 查看器主题；内部滚动不撑乱布局（`.pdf-scroll` 同款 flex 滚动容器）。

## 许可清单（备查）

- PDF.js — Apache-2.0（Mozilla Foundation）— 已 vendor。
- docx-preview — MIT。
- mammoth.js — BSD-2-Clause。
- SheetJS（xlsx 社区版）— Apache-2.0。
- LibreOffice — MPL-2.0（仅服务端转换，不向浏览器分发其代码）。

---

## #63 已落地：侧车批注后端 + aide Office 工具

> 本节是与前端串行任务对接的**唯一契约**。批注层不写死 docx，后续 xlsx/pptx/pdf 复用。

### 1. 批注数据结构（JSON）

```json
{
  "id": "16字节hex",
  "docPath": "report.docx",
  "docHash": "创建时文档内容 SHA-256(hex)",
  "anchorQuote": "用户选中的原文片段",
  "anchorIndex": 0,
  "text": "批注正文",
  "author": "me",
  "createdAt": "RFC3339",
  "updatedAt": "RFC3339",
  "status": "open | resolved",
  "replies": [{ "text": "...", "author": "...", "createdAt": "..." }],
  "stale": false
}
```

- `stale` 由后端读取时实算：当前文件内容哈希 ≠ `docHash`（或文件已删除）即为 `true`。
  前端应据此给该批注加"锚点可能失效"提示，**不得**删除。
- `docHash` 算法：文件原始字节 SHA-256 hex，与 `/api/file` 返回的 `hash` 同一算法；
  `.docx` 取 `/api/file/raw` 字节流前端自行计算，或新建批注时直接传当前已知哈希。
- 高亮定位：前端拿到 `anchorQuote` 后在渲染文本中查找，`anchorIndex` 指定它是第几次出现
  （0 起始），用于重复片段消歧。

### 2. REST 端点（统一 Bearer 鉴权）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/comments?path=<docPath>` | 返回 `{"comments": [...]}`，按创建时间升序 |
| POST | `/api/comments` | body: `{path, hash, anchorQuote, anchorIndex, text, author?}` → 201 返回完整批注 |
| PUT | `/api/comments/{id}` | body: `{text?, status?, reply?{text, author?}}`；`status` ∈ open/resolved；`reply` 追加一条回复 |
| DELETE | `/api/comments/{id}` | 删除批注 → `{"ok": true}` |

错误统一为 `{"error": "..."}`（400/404/500）。审计写 `/data/audit/comments-audit.jsonl`。

### 3. 存储布局（`/data/comments/`，目录 0700 / 文件 0600）

```
comments/
  index.json               id → docPath 索引（原子写）
  <sha256(docPath)>/
    <commentID>.json       单条批注（含 replies）
```

每批注一个文件：增/删/回复均为局部写，无整文件读-改-写竞争。

### 4. aide 可调用的 Office 工具（python-docx 1.2.0，实测）

| 工具 | 参数 | 说明 |
|---|---|---|
| `docx_structure` | path | 标题层级/段落前50字/表格行列数/原生批注数 |
| `docx_list_comments` | path | 读 .docx 原生 comments.xml |
| `docx_add_comment` | path, quote, text, author?, anchorIndex? | 按 quote 定位段落锚定原生批注 |
| `docx_resolve_comment` | path, id | 写 commentsExtended.xml（w15:done=1）标记解决 |

实测结论（python-docx 1.2.0）：原生批注**可读可写**（`Document.comments` / `add_comment(runs, text, author, initials)`）；
"已解决"状态库内无 API，脚本直接改包内 XML（Word 2016+ 格式），改后 python-docx 可正常重开。
脚本位置 `scripts/office/`，Go 经 `os/exec` 调用；查找顺序 `$AIDE_OFFICE_SCRIPTS` → `/workspace/scripts/office` → `/opt/aide/office-scripts`。

### 5. .doc（旧二进制）局限

- `GET /api/file` 与 `/api/file/raw` 对 `.doc`（非 `.docx`）直接返回 400：
  "旧版 .doc 二进制格式，暂不支持在线查看/批注。请另存为 .docx 后打开。"
- 容器内无 LibreOffice/antiword（已探测）。若后续要高保真转换，可选：
  镜像加装 `soffice`（+~400MB，MPL-2.0），新增 `/api/file/convert?to=pdf`，前端复用 PDF.js。
  **是否加装待用户决定**，本期不实施。
