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
