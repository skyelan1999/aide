# Office 文档内联查看方案（docx / xlsx / pptx）

> 更新：结构化 Office 生成与 XLSX 工作目录/引用编辑已按
> [Office 工具集](../plugins/office.md) 实现。下文早期「本期不实施」
> 是历史方案记录，不代表当前 XLSX 状态；PPTX 仍仅支持生成和文本提取，
> 不提供内联排版预览。

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

旧侧车批注的正文和每条回复最多4000个Unicode字符，作者最多100个字符，每条批注最多50个回复。回复正文/作者在写入前一起检查，失败时同一请求中的其他修改也不落盘。历史超长侧车记录不自动删除或改写；本限制约束后续写入，不替代未来列表分页或历史迁移。

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

---

## #63 前端集成（RC3 已落地）

> 后端契约见上 §1–§5。本节记录前端侧已落地的 vendor 与集成点，与 PDF 查看器同模式。

### 1. vendor 清单（离线，go:embed）

| 库 | 版本 | 许可 | 位置 |
| --- | --- | --- | --- |
| docx-preview | 0.3.2 | Apache-2.0 | `internal/server/web/vendor/docx-preview/` |
| JSZip | 3.10.1 | MIT | `internal/server/web/vendor/jszip/`（docx-preview 解包 .docx 所需） |
| dxf-parser | 1.1.2 | MIT | `internal/server/web/vendor/dxf-parser/`（#57，见下） |

均经 `//go:embed web/*` 打包，不访问 CDN；固定版本 + CHECKSUMS。

### 2. 三处 isDocx 分支

- `isDocxPath(path) = /\.docx$/i.test(path)`（`isDxfPath` 同模式，`/\.dxf$/i`）。
- `openFile()` 与 file-view（`#file=…` 从标签页）两处分支把 `.docx`/`.dxf` 归入"走 `/api/file/raw` 取 ArrayBuffer、跳过 `/api/file` 文本端点"的集合。
- 编辑器标题栏：`isImg || isStl || isPdf || isDxf || isDocx` 时隐藏保存按钮、显示"只读 · 可视化查看器"badge（#59 收口：只读文件不显示保存）。

### 3. docx-preview 渲染行为

- 经 `/api/file/raw?path=…&access_token=…` 有界读取，再完成 ZIP 预检后交给 `docx.renderAsync(bytes, container)`；
- 保留原排版：标题/段落/表格/列表/图片；内部滚动不撑乱布局（复用 PDF 查看器 flex 滚动容器）；
- 只读：不回写 `.docx`，工具栏不显示保存；
- 历史实现为批注侧车：前端拿到渲染 DOM 后，按 `anchorQuote` 在文本中查找、`anchorIndex` 消歧高亮，调 §2 的 CRUD 端点增/删/回复/解决；`stale=true` 时给"锚点可能失效"提示，不删除批注。

浏览器预览压缩输入最多16MiB、512条目、单条目解压8MiB、总解压32MiB。先检查普通单卷ZIP中央目录的计数、大小和范围，再使用固定JSZip3.10.1的公开流接口逐块计数实际输出并丢弃，不用`async()`累积预检结果；伪造声明大小同样会被实际计数拒绝。预检总时间上限10秒，超限暂停流并拒绝渲染；当前有界压缩块可能继续完成解码，后续输出不再计入或保留。正常通过后才进入既有DOCX渲染器。加密/多卷/ZIP64等不支持的包及库版本变化均拒绝安全预检，展示下载查看提示。该门禁检查输入及ZIP解压资源，不替代OOXML语义、图片像素或排版检查；不修改原文件或通用上传/下载大小。

### 3.1 原生 DOCX 批注升级（office-native-comments）

新增批注不再写入侧车，而通过 `/api/office/docx/comments` 写入 DOCX 自身的 `comments.xml` 与正文批注锚点。读取时先展示原生批注，再保留旧侧车批注并标记“未写入文件”；两者不静默合并。用户可逐条点“写入 DOCX”复制旧批注；成功后旧侧车记录仍保留但标记已写入，锚点无法匹配则不写入。GET/POST/PUT 的哈希、工作区身份和读写权限约束见 `docs/plugins/office.md`。按批注自动改写时必须校验批注 ID 及原文，若锚点跨段、含复杂对象或已经变化则拒绝写入。DOCX 下载使用现有 `/api/file/raw`，因此带上文件内批注；WPS 客户端验收单独记录。

### 4. DXF 渲染器（#57，RC2）

- `ensureVendorScript('/vendor/dxf-parser/dxf-parser.js')` 懒加载；
- SVG 渲染：Y 翻转、包围盒自适应窗口、支持 LINE/CIRCLE/ARC/ELLIPSE/LWPOLYLINE/POLYLINE/SPLINE/TEXT/MTEXT/INSERT 递归/DIMENSION；
- ACI 颜色、图层、线宽映射；工具栏缩放 ± / 适应窗口；只读禁用保存。

DXF输入最多4MiB，流式读取到界即取消；解析前最多100,000行，避免解析器先创建过大的行数组。定义与展开各最多50,000实体，SVG最多50,000节点，几何处理最多200,000点，文本单实体最多16,384字符、总计1Mi字符，避免少量节点重复大文本绕过内存预算；INSERT和DIMENSION共用32层块深度及当前递归路径循环检查，允许同一块在不同分支重复使用。任何预算或循环失败都会清除部分SVG并显示预览错误，不被单实体格式容错吞掉。这些资源边界只限制预览，可下载完整文件后使用CAD工具处理。

## 附件文本提取与图片多模态（#63 扩展，后端）

原 `attachmentContext` 只接受 UTF-8 文本，docx/pdf/xlsx/pptx 一律报"不支持二进制文件"。本次后端补齐：

### 1. 可解析文档 → 后端提取文本（不依赖模型视觉）

- 扩展名 `.docx/.pdf/.xlsx/.pptx` 在附加时由后端调 `scripts/office/extract_text.py` 提取纯文本，
  包进 `<untrusted-file>` 文本上下文随首条 user 消息发给模型。
- 库：docx=python-docx、xlsx=openpyxl（只读+data_only）、pptx=python-pptx、pdf=pypdf（Dockerfile 已加 pypdf==6.19.0）。
- 提取脚本与现有 office 工具同一查找链：`$AIDE_OFFICE_SCRIPTS → /workspace/scripts/office → /opt/aide/office-scripts`。
- 边界：单文档提取上限 `maxDocExtractChars=60000` rune，超出截断并标注「…已截断，共 N 字」；
  全部附件上下文总上限 `maxAttachmentChars=200000`；提取失败明确报错（不静默丢内容）；
  SSH 工作区 / 参考来源库暂不支持（脚本跑在容器内），返回明确提示。
- 确实无法解析的二进制（非上述类型）改文案："…aide 暂不能解析的二进制格式；可让 aide 直接在工作区打开该文件"。

### 2. 图片 → 按模型视觉能力多模态

- 扩展名 `.png/.jpg/.jpeg/.gif/.webp`：后端读字节编为 `data:<mime>;base64,…`。
- 视觉判定 `modelSupportsVision(modelID, models)`：先看模型列表显式 `vision` 字段，
  再按已知视觉模型清单兜底（gpt-4o/4.1、qwen-vl、glm-4v、deepseek-vl、gemini、claude-3/4 等）。
- 支持 → `Message.Images` 在 outgoing 时由 `outgoingMessages` 把该 user 消息 content
  从字符串改为 `[{type:text},{type:image_url,image_url:{url:dataURL}}]` 多模态数组；
  `Images` 带 `json:"-"`，不持久化、不进 UI 历史（每次 run 重新附加）。
- 不支持 → 在发送前由 `visionGateLocked` 返回可操作错误：
  "当前模型 X 不支持图片输入，请切换到支持视觉的模型（如 gpt-4o、qwen-vl-max…）"。

### 3. 与前端接口约定（前端需配合）

- `GET /api/config` 新增：`vision`(bool，当前模型是否支持图片)、`visionRecommend`(string[]，推荐视觉模型)；
  `models[]` 每项新增 `vision`(bool)。前端据此在选图附件时：模型不支持则置灰/提示切换。
- 附件上传/附加仍走既有 `attachments:[{root,path,source}]`，无需新端点；后端自动按扩展名分流。
- 附件 chip 的保留/×删除仍由前端负责；后端只提供提取与发送能力。
- 「不支持」的二进制 chip：前端据 400 错误文案标记不可发送即可。


### draw.io 浏览器预览资源边界

本地 draw.io 在打包版 `app.min.js` 加载后、`App.main` 之前安装 `drawio-preview-safety.js`，开发模式也必须先安装检查。输入 XML/base64 最多8MiB，逐块解压输出最多8MiB，mxfile 最多64页、所有压缩页面累计展开最多16MiB；非激活页同样预检。超过限制会终止预览并显示下载查看提示，原文件不被修改。

XML 在 DOMParser 之前检查128层深度和50,000个节点，正确跳过引号、注释和CDATA中的标记；预览不接受DTD/实体声明。解析后的DOM和首次 mxCodec 索引前再次迭代检查，避免递归索引先触发栈溢出。每次DOM检查最多10,000个mxCell，按模型核对父ID关系，包括带外层ID的自定义对象；128层父链和循环关系在懒加载引用解码之前拒绝。实际对象解码也限制128层调用深度，覆盖跨模型及source/target引用链。注册实际模型根前再次检查子树的节点、深度及循环/共享节点，防止平铺XML绕过嵌套限制。

父页面只接收该iframe发出的同源消息；资源限制事件会清除iframe和计时器，展示固定本地化提示。正常load、编辑和save协议保留。这是浏览器预览资源门禁，不代表对所有draw.io形状、布局或其他导入格式作出完整内存/时间保证，也不限制通用文件传输。

`drawio-layout-fixes.js`在相同加载门禁安装布局修正，直接作用于打包版和开发模式的运行时：异步布局状态表使用无原型对象，支持`constructor`、`__proto__`等合法单元格ID；表格中缺少geometry的单元格保留零宽列边界，不压缩列索引或补造geometry。连线跳点按每次validation分配1,000,000个保守工作单位，包含预检扫描、线段配对及交点插入排序上界；单条当前连线超过128个点或预算不足时，清除该线的旧跳点，绘制普通连线，保留模型样式。预算在下次validation重置，正常小图的跳点保持可用；复杂图可能提前退化，这是避免同步跳点计算卡住页面的保守选择，并非限制整个布局引擎的运行时间。

draw.io源码及本地打包文件同步维护以下交互修正：复制自定义动作链接时，克隆ID映射使用无原型对象和自身属性判断，包括外部传入的普通映射也以安全属性写入保留`__proto__`等ID；链接改写只读取映射的自身属性，未映射ID及通配符保留，未插入的空ID仍移除。选中缺少geometry的表格时，仅跳过该列的控制句柄，其他列/行句柄保留。连接约束处理器在鼠标离开画布时清除焦点图标及高亮，销毁时移除同一监听函数。

几何核心修正在源码、生产`app.min.js`及独立`mxClient.js`保持一致：旋转吸附间隔为处理器局部变量，保留15/5/1度吸附且不写入页面全局`raster`；正交自环路由在克隆端点之前的原状态上判断身份，使用两端较大的jetty间距；零半径弧由画布显式画为直线并推进终点，同起终点保持空操作。公开`arcToCurves`仍返回曲线数组，对零半径返回沿直线的退化三次曲线控制点，非零半径路径保留。

销毁图时同时销毁自动平移管理器，在画布和视图释放前停止活动平移，清除定时器并移除图及document鼠标监听；管理器直接销毁同样清理残留定时器。菜单销毁迭代释放所有层级子菜单（包括未打开的子菜单），移除挂到body的DOM、清理监听和活动引用，可重复调用。模板`fillalpha`、`strokealpha`分别设置填充和描边透明度，`alpha`继续设置整体透明度；三份核心代码保持同步。


### draw.io 自定义 AI 请求模板

可选 `aiConfigs` 请求模板按 JSON 复制：保留 `null`、布尔值、数字、空字符串、数组及自身字段，仅替换字符串占位符。不会调用模板提供的 `constructor`，`constructor` / `__proto__` 等字面字段也能保留，输入模板不被修改。源码 `Dialogs.js` 与实际分发的 `app.min.js` 同步维护。

模板复制与对话历史拼接在现有请求重试函数内准备；准备失败显示当前回复的错误及手动重试入口，尚未发送模型请求。修正配置中的请求模板后，重试会重新复制模板，沿用该次发送的历史快照，避免重复追加历史。错误以既有本地化标题显示，并以文本写入页面。

回归覆盖源码/打包版复制语义，以及真实打包 ChatWindow 的自定义配置、空值请求、可见准备错误与手动恢复，桌面明色和窄屏暗色均验证。浏览器使用隔离的模拟模型响应，不读取真实密钥，不调用真实模型；这不是服务端模型能力或完整工作台的验收。


### draw.io 动画步骤校验与修复

`Editor.parseAnimationData` 校验步骤须为对象，已知对象动作的载荷须为非空对象；`wait` 接受非负有限数字或数字字符串，`open` 保留字符串。嵌套 `animation.steps` 同样检查，校验遍历最多100,000个步骤、128层嵌套。未知扩展字段保留；这是已知动作的结构检查，不是全部自定义动作的完整语义规则或大型动画渲染性能保证。

步骤错误以带有 `animationData` 的校验错误返回，保留解析得到的数据。动画编辑器把坏步骤显示成带编号的错误行，可删除整条步骤或用「编辑文本」修正；不会自动过滤掉坏步骤。页面动画和链接动作模式均支持修复，调用方初始对象不被改写。数据无效时禁用保存和预览，关闭自动保存也会被阻止并保留窗口及草稿；修正后恢复正常保存。`AnimationPlayer` 在构造和开始播放前验证，避免先执行效果再发现错误。现有存储属性中语法无效JSON的空白回退行为未在此轮改动，任意直接自定义动作执行器也未全面改写。

动画源码与分发包的校验、播放器和窗口函数通过 `scripts/sync_drawio_animation.cjs` 同步。维护时在独立工具目录安装Terser **5.39.0**，设置 `AIDE_TERSER_MODULE=/path/to/node_modules/terser`，运行 `node scripts/sync_drawio_animation.cjs`；追加 `--check` 只读确认一致。脚本不安装依赖、不联网，也不改变产品运行时依赖。

源码/打包版回归与实际窗口验证覆盖空步骤/空载荷、嵌套错误、遍历边界、保留原值、删除与文本修正、自动保存保护及播放前拒绝。桌面明色和390px暗色功能验收通过；窄屏页脚保存按钮裁切另登记为[#441](https://github.com/skyelan1999/aide/issues/441)，未在此轮混改布局。浏览器同时回归上轮AI模板及已有预览门禁，全程为合成数据和模拟响应。


### Lucidchart EI 消息文字样式

Lucidchart 的 `EIMessageBlock` 和 `EIMessageHistoryBlock` 导入时，把每个 `message_N` 的标签样式追加到对应子单元格，保留各自字号、颜色与粗斜体；已有历史头部 `message_0` 保持原处理。源码 `Extensions.js` 与生产动态加载的 `extensions.min.js`、集成版本 `integrate.min.js` 同步修正，主 `app.min.js` 不包含这份转换器。

浏览器回归使用实际 `convertLucidChart` / `LucidImporter.importState`、图模型与文本渲染器，分别预载三份文件中的完整 LucidImporter 代码段，验证四条不同格式消息及已有正确头部。模拟输入在桌面明色和窄屏暗色下验证；这不替代真实Lucidchart文件/账号、完整文件选择交互或离线自动加载的验收。


### Lucidchart 符号身份与内置图形缓存

导入类型与渲染目标保持一致，源码及两份分发转换器同步维护：

| Lucidchart 类型 | 渲染目标 |
| --- | --- |
| `AndroidIconRefresh` | `mxgraph.ios7.icons.repeat` |
| `EE_BJT_NPN1` / `EE_BJT_PNP1` | 各自的 NPN / PNP 晶体管图形 |
| `PEPlugValveBlock` | `pid2valves.valve`，`valveType=plug` |
| `AmazonRDSMySQLDBinstance2017` | `aws3.mysql_db_instance` |
| `AWSAppSyncAWS19` | `aws4.resourceIcon`，`resIcon=mxgraph.aws4.appsync` |
| `IoT_AlexaVoiceServiceAWS19` | `aws4.alexa_voice_service_2`；Alexa Skill 仍为 `aws4.alexa_skill` |

真实导入回归同时核对消息样式、图模型的类型字段、实际注册的渲染器/Stencil及正确的相邻类型对照，覆盖源码、动态扩展包和集成包。

回归还发现原文档XML预算误拦了内置AWS4缓存，已作为#128的回归补修。`stencils.min.js` 的闭包私有表只允许自身的字符串条目使用独立XML解析器；该表来自随产品分发的常量，解析器不导出，损坏XML报错，继承属性不被当成内置缓存。动态回退和用户文档仍走既有路径，不扩大文档节点、深度或解压预算。此边界信任的是分发代码本身，不提供对被修改代码或恶意同源脚本的隔离。

回归对同一实际缓存文本分别验证：内置同步/异步加载成功，而作为用户输入的完整XML仍被原有预算拒绝；也覆盖损坏缓存和继承条目。实际缓存文本为5,088,259字节，与独立XML文件的空白格式不同，负例使用缓存本身的文本，避免混淆。运行 `AIDE_DRAWIO_LUCID_ACCEPTANCE=1 AIDE_COMPONENT_EVIDENCE_DIR=/tmp/aide-lucid-evidence node scripts/browser-drawio-preview-bounds.cjs` 可复跑这些合成组件用例；它不是完整工作台、真实Lucidchart账号或文件选择交互的验收。


### 本地 Visio 导入的 ZIP 展开预算

VSDX/VSSX 共用 `mxVsdxCodec.loadArchive`：压缩输入最多8MiB，JSZip目录最多1,024个保留条目（包含目录与未使用条目）；需要的XML/rels及媒体成员各最多8MiB，累计实际展开最多16MiB。头部声明仅作提前拒绝，流式读取继续累计真实字节；一个压缩成员完成后才开始下一个，出错停止后续成员。检查通过的原始字节交给原有XML、UTF-16及媒体转换流程，不修改原文件。VSSX把错误回调传入共用路径，超限错误可进入既有错误窗口，之后仍可导入有效文件。

源码和两份分发转换器同步维护。`scripts/sync_drawio_visio_archive.cjs` 使用外部工具目录中的Terser **5.39.0**，通过 `AIDE_TERSER_MODULE=/path/to/node_modules/terser node scripts/sync_drawio_visio_archive.cjs` 更新对应代码段；加 `--check` 只读验证，不安装依赖或联网。构造器工厂内的赋值使用其局部构造器，避免初始化时写到旧的全局类。

单测使用分发JSZip验证输入/成员/累计/数量边界、伪造或缺失声明、实际展开的串行性、原始字节保留、损坏ZIP及VSSX错误转交。浏览器用完整的源码/分发Visio转换器代码段调用真实 `doImportVisio`，检查两种格式的超限错误、实际错误窗口与同运行时恢复，正常夹具是单个空白页面和空图库。运行 `AIDE_DRAWIO_VISIO_ACCEPTANCE=1 AIDE_COMPONENT_EVIDENCE_DIR=/tmp/aide-visio-evidence node scripts/browser-drawio-preview-bounds.cjs` 可复跑。这不替代完整工作台、真实微软文件/复杂主控形状、媒体解码像素预算、后续几何算法或整个集成启动的验收；解压流停止以库的块边界为粒度，不承诺整个导入的严格进程内存或耗时上限。


### Visio PolylineTo 公式解析

PolylineTo按顺序索引读取坐标，避免每一对坐标都移动数组剩余内容；不再复制两遍字段数组。公式原文最多262,144个UTF-16代码单元，规范化后最多4,096对坐标（不含独立的末尾终点），超限以 `AIDE_VISIO_LIMIT` 拒绝，发生在写入该行的形状位置之前。正常坐标换算、终点与闭合处理保持既有语义；本轮不改其他几何算法或坐标模式解释。

源码及两份分发包用 `scripts/sync_drawio_visio_polyline.cjs` 同步，工具依赖与Visio压缩包同步脚本一样为外部Terser **5.39.0**；`AIDE_TERSER_MODULE=/path/to/node_modules/terser node scripts/sync_drawio_visio_polyline.cjs --check` 只读核对。单测检查实际前端方法不再反复从数组头部删除，并验证普通路径、限制与精确点数边界。实际浏览器将Visio XML交给行工厂/几何转换，检查超限拒绝、恢复及转换路径的真实Stencil渲染。设置 `AIDE_DRAWIO_VISIO_ACCEPTANCE=1 AIDE_DRAWIO_POLYLINE_ACCEPTANCE=1` 运行上述浏览器脚本可复跑；这不是包含PolylineTo的真实微软压缩包、复杂坐标语义或所有几何运算的验收。


### Visio NURBS 边曲线的计算预算

边转换的NURBSTo公式原文最多65,536个UTF-16代码单元，有效度数最多8，包含当前点和末尾终点在内最多256个控制点；无效公式原有的直线回退保持。完成结构/大小检查后、构建该行控制点及进入De Boor计算前，按最大采样次数、度数、二分查找与遍历开销保守计费。每次 `getControlPoints` 调用共250,000个工作单位，跨同一条边内的NURBS行共享，超过剩余额度以 `AIDE_VISIO_LIMIT` 拒绝；新的边转换重新计费，不把复杂曲线静默截断或降阶。

源码与两份分发包用外部Terser **5.39.0** 和 `scripts/sync_drawio_visio_nurbs.cjs` 同步；`AIDE_TERSER_MODULE=/path/to/node_modules/terser node scripts/sync_drawio_visio_nurbs.cjs --check` 只读验证。生成时捕获分发包的命名空间，避免方法内部同名局部变量干扰。单测检查真实转换方法的普通三次曲线、度数/控制点/公式边界、共享工作预算和独立重试。浏览器结合实际Visio行工厂及 `VsdxShape` 方法转换合成XML行，再以真实图编辑器渲染曲线；设置 `AIDE_DRAWIO_VISIO_ACCEPTANCE=1 AIDE_DRAWIO_NURBS_ACCEPTANCE=1` 可复跑。

这是单条边的NURBS评估工作预算，不是文件所有几何算法或整个页面的严格耗时/内存上限。高于上述限制的复杂合法曲线也会拒绝；完整工作台、真实微软压缩包、复杂权重/坐标语义及跨平台未在此组件回归中验收。


### Visio 媒体转换错误与导入终态

XML/rels及媒体的异步读取和转换统一接入错误处理。BMP解码、画布写入或导出失败时，当前基础导入器进入失败终态并调用错误回调，不再等待失去的完成计数；终态后忽略晚到的处理回调，避免其他条目再次触发完成或错误。没有错误回调的旧调用保留一次空字符串结果。VSSX继续把错误转交调用方，现有EMF容错跳过行为保留。正常导入完成也经过同一终态保护。

源码与两份分发转换器通过Visio压缩包同步脚本维护。回归使用实际源码/分发BMP解码器和有限合成文件，覆盖坏BMP、多个失败条目、画布异常、同步/异步读取错误、旧空结果回退，以及真实1×1红色BMP的像素与导出。浏览器以真实VSDX/VSSX入口检查错误窗口、关闭、同运行时正常重试及故障注入的画布导出错误；开启 `AIDE_DRAWIO_VISIO_ACCEPTANCE=1 AIDE_DRAWIO_MEDIA_ACCEPTANCE=1` 可复跑。

这不改变BMP解码器本身的结构校验或新增像素预算，也不代表所有BMP/EMF格式、真实复杂微软文件或后续全部异步页面回调均已验收。验证的正常BMP为1×1、24位；完整工作台及跨平台未验收。


### 可选 Trello 附件的字节上限

Trello客户端在上传前按实际附件Blob的 `size` 检查，字符串先按原上传方式构建Blob并复用同一对象提交，Blob/File保留原对象。中文、emoji等UTF-8字节和PNG导出的Blob都经过相同检查；错误提示使用附件本体字节数。现有边界不变：达到10,000,000字节即拒绝，不包含multipart表单的其他字段。该上限属于可选Trello附件，不改变通用文件传输限制。

源码 `TrelloClient.js` 与 `app.min.js` 的writeFile用外部Terser **5.39.0** 和 `scripts/sync_drawio_trello_limits.cjs` 同步，`--check` 只读核对。VM回归用较小的16字节配置检查Blob、File、中文、emoji、ASCII及精确边界；浏览器检查原10,000,000字节配置、真实Blob/FormData、PNG插入/保存的超限路径和错误窗口。

AIDE默认嵌入页会清理并禁用Trello客户端，默认工作台入口未启用该集成。浏览器回归在App初始化清理前捕获真实分发构造器，与源码方法一起进行隔离测试，并断言全局客户端仍为空；授权与XHR使用mock，未读取真实账号凭据或上传至Trello。设置 `AIDE_DRAWIO_TRELLO_ACCEPTANCE=1` 可复跑；它不是完整工作台、真实Trello账户、PNG真实生成或云端上传验收。


### 动画 GIF 导出的帧数预算

`AnimatedGifExport` 每次导出最多300帧，保留至少2帧的原行为。自动/默认FPS和时长的既有falsy回退保持；解析后的有效FPS与时长必须为正有限数。计算得到的帧数须为安全整数且不超过上限，检查发生在移除SVG动画CSS、嵌入图片和创建编码器之前。公开 `renderFrames` 入口也在分配画布/编码器前复核帧数与FPS，避免绕过主导出入口。超限进入原错误回调，实际UI停止转圈并提示缩短动画时长或降低帧率，不静默截断动画。

自动时长识别支持CSS毫秒、秒及科学计数法，覆盖浏览器把长毫秒值规范化为秒的实际SVG。源码与 `app.min.js`、`integrate.min.js` 的导出器用外部Terser **5.39.0** 和 `scripts/sync_drawio_gif_frames.cjs` 同步，`--check` 只读核对；GIF编码器本身不改。

有限回归在计划阶段截获旧代码产生的15,000,000帧，未实际渲染大动画；还检查300帧精确边界、非法有效参数、直接渲染入口及默认值回退。浏览器在合成图中启用flow动画，通过实际导出UI验证超长SVG在编码器创建前报错和spinner恢复，然后缩短时长，经真实SVG栅格化、对应源码/分发编码器、FileReader和saveData调用生成两帧GIF，解析文件块并以浏览器原生解码检查尺寸。saveData被测试截获，不实际下载或写用户文件。设置 `AIDE_DRAWIO_GIF_ACCEPTANCE=1` 可复跑。

这是导出器的帧数预算，不是所有图像尺寸、编码器独立调用或进程总内存/耗时的保证；超出预算的合法长动画也会拒绝。完整工作台文件选择/保存对话框、300帧实际长导出、全部画质/透明度语义及跨平台未验收。
