# vendor/pdfjs — PDF.js 内联预览（离线 vendor）

本目录为 **Mozilla PDF.js** 的离线 vendor 副本，由 `internal/server/server.go`
经 `//go:embed web/*` 打包进二进制，**不访问任何 CDN**，保证离线 / 空气间隙可用。

## 版本与哈希（pin）

- 上游：`pdfjs-dist`（npm）/ Mozilla PDF.js
- 许可：Apache-2.0（见各文件头 `@licstart` 声明）
- 版本：**4.4.168**
- 来源：`https://cdn.jsdelivr.net/npm/pdfjs-dist@4.4.168/build/`

| 文件 | SHA-256 |
|---|---|
| `pdf.min.mjs` | `fc1cc3bae4b1b7ca11ec614a5816c5ce68307c3a9a19eab7c7da436c9d05d558` |
| `pdf.worker.min.mjs` | `2730926f0f82be02b5212501491aa42185a16ce5f29a131a864867bf8a965a66` |

校验：

```sh
cd internal/server/web/vendor/pdfjs
shasum -a 256 -c CHECKSUMS.txt
```

## 升级

1. 在 jsDelivr 选定新的固定 `4.x.y` 版本；
2. 重新下载 `build/pdf.min.mjs` 与 `build/pdf.worker.min.mjs` 覆盖本目录；
3. 更新本文件与 `CHECKSUMS.txt` 的版本号和 SHA-256；
4. 重新评审并保留下面的本地资源补丁，运行定向回归、重建镜像、真机点 PDF 验收。

## 运行时加载

- 主 bundle：`app.js` 内动态 `import('/vendor/pdfjs/pdf.min.mjs')`（懒加载，仅打开 PDF 时）。
- Worker：`GlobalWorkerOptions.workerSrc = '/vendor/pdfjs/pdf.worker.min.mjs'`。
- 两者均同源、由 `go:embed` 提供，**断网可用**。

## 本地资源限制（#432、#433）

Worker 的 Apache-2.0 许可头保持。原始上游 worker SHA-256 为
`a7cf858ab0bbe21a00ef438c24a4ef5833c3d05935a294382c8d0773a8958f50`；表格和
`CHECKSUMS.txt` 记录加入以下补丁后的实际摘要：

- `PredictorStream` 验证整数 predictor、1–32 个颜色分量、1/2/4/8/16 位深和正整数列数；采用安全整数计算，避免位运算溢出。每行解码最多 8 MiB，在向底层流请求数据之前拒绝超限参数。
- TIFF/PNG 行先读取并检查长度，再分配输出。正常 EOF 不分配新行；短行明确失败，不补出并遍历缺失数据。
- Aide 调用 `getDocument` 时设置单张嵌入图片最多 16 Mi 像素、画布面积预算 32 MiB。图片尺寸检查在解码前执行；文件名提示说明超限图片会略过，完整内容需查看原文件。这些边界不代表 PDF 的全部累计内存或运行时间均已受限。

定向检查：`node scripts/pdf-predictor-budget-review.cjs`；
`AIDE_COMPONENT_EVIDENCE_DIR=<证据目录> node scripts/browser-pdf-resource-budgets.cjs`。
脚本从仓库根目录运行；浏览器脚本使用实际本地模块、worker、预览函数和合成 PDF，不连接外部来源。

### 对象结构与交叉引用（#442、#443）

`Parser.getObj` 最多同时进入 128 层，退出（包括异常）时恢复计数。普通平坦数组的元素数不计为嵌套深度；密文转换参数保持传递。过深结构返回格式错误，避免消耗调用栈。

每个 XRef 实例最多处理 1,000,000 条声明记录，记录地址范围也必须在该边界内。流的 `/Index` 最多 2,048 个范围，`/W` 必须有三个 0–8 字节的字段；先校验全部范围并预留处理预算，再读取或构造记录。断点重入同一流不重复预留。经典 XRef 表与损坏文件的对象索引恢复同样使用该预算，不能通过恢复路径绕过。已预留或恢复尝试消耗的预算不回退；某些异常/超大文件可能被拒绝，完整查看请使用原文件。

`node scripts/pdf-structure-budget-review.cjs` 检查深度、流/经典表、恢复索引、精确边界、非法字段、累计预算和重入；不分配百万条测试记录。浏览器资源回归另包含正常 XRef 流及超限文档的可恢复错误/受限恢复路径。以上不是对其他 PDF 解析器、字体或原生编解码路径的全面资源保证。

## 页面布局与画布预算（#500）

`setupPdfPreview` 在布局与原生画布分配之前校验页面宽、高和变换矩阵均为有限有效值。单页 CSS 最长边与画布最长边均最多 8192 像素，画布面积最多 16 Mi 像素；超出时降低页面显示比例或输出分辨率，工具栏显示“预览尺寸已受限”。适宽允许低于 0.4 倍，超宽页面不再被固定最小比例放大；缩小仍可达到适宽比例的 1/16。页码和缩放操作继续使用同一有界布局。

重设尺寸时先清空两个画布维度，避免新宽度与旧高度短暂形成超大分配；关闭预览也清空画布。无效尺寸和同步/异步渲染错误显示可恢复错误并销毁文档。该限制针对单页布局与画布，不代表多页总缓存、PDF 操作符或所有编解码路径均有累计资源上限。完整内容可下载原文件查看。

`node scripts/pdf-page-canvas-budget-review.cjs` 验证 2240 组几何组合、常规 DPI 和非法尺寸；`AIDE_COMPONENT_EVIDENCE_DIR=<目录> node scripts/browser-pdf-page-canvas-budget.cjs` 使用实际本地 PDF.js 模块/worker、超大页面、20 倍 DPI 和蓝色矢量像素，验证缩放/适宽、分配前拦截、受控错误与关闭释放。

## 解码器与图片缓存修正（#501–#505）

- LZW 单个解码输出缓冲区最多 64 MiB。构造提示和后续增长均在分配前校验；这是按块增长的容量上限，块预留可能使某些略低于上限的输出也被拒绝。该预算不等于整份文档累计解码量上限；其他解码器并未因此自动获得相同限制。PDF.js 原有容错路径可能略过超限流，完整内容需下载原文件。
- JBIG2 按提取后的高三位计数识别扩展引用形式，保留标志占 `ceil((引用数+1)/8)` 字节。段头、引用、载荷范围和游标进度均须有效；未知长度区域的终止位置换算为相对载荷长度。每段最多 65536 个引用、每次解析最多 65536 个段，超出拒绝，避免在数组构造前接受无界声明。
- 全局图片缓存、被跟踪引用和独立解码失败集合各最多 2048 条。引用仅记住达到跨页缓存门槛所需的两个不同页码；缓存满后新图片仍可走原有非全局缓存解码。原有 50 MB 像素字节预算保留；局部清理与完整清理分别保持原语义。
- Indexed 色表在分配前要求条目数 1–256（PDF `/HighVal` 为 0–255）、基色分量数 1–32，避免非法色表分配。
- 内联图片拼图的边缘填充写入目标 atlas，保留源图片。每张图片仅填充自己的顶/底及两侧，换行后仍预留左边缘；普通、不同高度和跨行图片都检查实际像素。

`node scripts/pdf-decoder-followup-review.cjs` 验证实际 worker 导出的解码器、缓存与优化器；浏览器脚本 `scripts/browser-pdf-decoder-followups.cjs` 使用实际离线模块/worker和合成 PDF。测试专用钩子仅记录 LZW 限制异常及 display 拼图优化是否运行，不改变其返回值或异常。公开 `getOperatorList()` 路径自身关闭优化，不能用其未优化列表冒充 display 拼图验收。

## JPX 分量与子采样（#506）

PDF.js API/其余 worker 仍为 4.4.168 加上述本地补丁。仅替换私有同步 OpenJPEG 解码模块为 Mozilla PDF.js 固定提交 `a72eb1599fb7712ffd31c96656fc79464aaf9ba9`（上游 PR22169）的官方生成 wasm2js 版本，原生部分支持缺失分量补零及子采样上采样；这不是将整套 PDF.js 升为新版本，也不是宣称采用新的稳定版发布。

[来源与摘要](openjpeg/provenance.json)、原始生成文件和两份 BSD-2-Clause 许可保存在 `openjpeg/`。本地 `scripts/sync_pdf_openjpeg.cjs` 将仅接受 warn 回调的私有工厂改为同步初始化，保留原生生成函数；分配输入后无论成功或失败均释放输入，及时清除图片/错误字段。没有额外网络、动态 eval 或 WebAssembly 编译依赖，现有离线预览与 CSP 保持。嵌入于 worker 的代码通过 `--check` 与原始生成文件及本地适配器逐字校验。未在本地重新编译 C 解码器。

`PDFImage` 识别过滤器列表中的最后一项，修正 `numComp` 拼写并传递实际颜色分量；DeviceRGBA 使用自动 RGBA 模式，避免把缺失 alpha 补零为透明。正常指定颜色空间时依上游行为丢弃多余分量或为缺失分量补零。已有图片尺寸、行、解析结构、缓存和 LZW 预算保留；不声称此更新解决所有原生编解码报告。

`node scripts/pdf-jpx-components-review.cjs` 验证 18 种原生分量/填充及 24 种 PDFImage 参数/过滤器组合、短流失败和清理后再次解码。合成 4×4 图像由 Pillow 生成，保存在 `scripts/fixtures/pdf-jpx-components.json`。真实浏览器脚本 `scripts/browser-pdf-jpx-components.cjs` 验证六种颜色/透明度/多过滤器场景的像素；可设置 `AIDE_PDF_UPSTREAM_JPX_DIR` 加跑固定提交的两份上游回归 PDF（从上游下载到临时目录，不随产品分发），检查全部十张图片均有解码结果且无解码错误。

## 颜色别名与 PDF 函数（#507–#509、#611）

颜色空间解析为每次独立调用维护活动集合与深度，按解析后的 Name 或对象身份识别循环，最多 128 层；异常退出也恢复状态，异步入口之间不共享计数。常规命名别名、Indexed 和重复调用可继续使用。

Type0 采样函数先校验 Domain/Range 与 Size/Encode/Decode 的维度，再构建小数组。最多 8 个输入维度、32 个输出分量和总计 1048576 个样本值（所有 Size 维度与输出分量的乘积）；支持 1/2/4/8/12/16/24/32 位样本，按字节向上取整读取并检查短流，超限在分配/样本循环之前拒绝。合法 32 位值使用无符号移位，`0xffffffff` 正确归一化为 1，避免渐变被当成负数钳为黑色。预算针对单个函数；不是整页所有函数的累计计算时限。

PostScript 条件块最多同时进入 128 层，解析错误后恢复计数。原有 if/ifelse 跳转输出保留；该限制不等于全部 PostScript 指令数或求值工作量预算。PDF.js 原有容错可能略过非法颜色图片或替换异常渐变，不保证超限文档完整显示。

`node scripts/pdf-function-depth-review.cjs` 验证实际 worker 中的循环/边界/清理、分配前拦截、采样精度、普通渐变函数及条件控制；`scripts/browser-pdf-function-depth.cjs` 验证七个实际 PDF/worker 场景及灰色图片、常规/32 位采样渐变、PostScript 常量渐变的像素。

## Form 与平铺图案展开（#510、#511）

每个 PartialEvaluator 按 WorkerTask 分别跟踪正在展开的 Form/Pattern 字典（优先采用 objId），进入时拒绝循环，二者合计最多128层。Promise 完成/失败时通过 finally 释放记录；平铺图案必须等待内部展开结束后再释放，不能在返回 Promise 时提前释放。相同资源在已完成的兄弟分支可继续复用，不同任务不会互相判成循环。保留原有容错路径，异常资源可能被略过。

`node scripts/pdf-resource-depth-review.cjs` 覆盖自循环、互循环、混合资源循环、128/129层边界、取消后恢复、重复资源和独立并发任务。`AIDE_COMPONENT_EVIDENCE_DIR=<目录> node scripts/browser-pdf-resource-depth.cjs` 使用实际预览函数及本地PDF模块/worker检查循环、深层Form、普通Form/Pattern蓝色像素与关闭释放。该限制不保证宽而无循环的资源图、整页累计操作符或全部PDF工作量有总预算。

## 字体映射与标注预算（#512–#514）

- Composite 字体W/W2的CID必须在0–65535内，范围不得倒序，数组不得越界，垂直数组为完整三元组；每次extractWidths共享最多131072次声明宽度写入，包含水平/垂直及重叠声明。在展开前预留预算。完整16位水平与垂直范围各一次仍可用。
- Squiggly默认外观的所有Quad共享最多65536段波浪线，先校验有限坐标和预计段数，再输出路径。每步也检查确实前进；极大Float32坐标或超长范围会被略过，避免不终止或巨大路径。
- 每个CMap按包含端点的范围预留最多131072次映射工作；cidrange、bfrange、数组范围与mapOne共享预算，重复覆盖也计数，短数组按声明范围计数。要求合法无符号32位字符编码，继承通过mapOne同样计数。原有容错可略过无效声明/字体。

`node scripts/pdf-font-annotation-budget-review.cjs`验证写入前拒绝、精确预算、正常映射、波浪几何与累计Quad预算；浏览器脚本`browser-pdf-font-annotation-budgets.cjs`覆盖正常/超限CID、垂直CID、CMap及Squiggly实际预览。基线测试使用受控赋值/路径追加钩子，不真正运行大分配或不终止循环。这些是局部预览预算，不表示整份文档所有字体/标注累计内存有总上限；复杂内容请查看原文件。

## 图形状态与传真解码（#532、#533）

StateManager的save栈最多256层；EvalState/TextState/CanvasExtraState的clone祖先链同样最多256层，防止跨嵌套Form的新StateManager绕过限制。CanvasGraphics在保存原生上下文或改变栈前检查限制。超过上限通过原有格式错误路径中止/省略内容；优化器可将未完成的空操作压缩，不保证所有超限页都出现同一种错误界面。

CCITT在扫描线数组和底层数据读取前验证正整数Columns，默认1728，最大65536；Rows为非负整数（0保留未知行数语义），已知行数的声明像素最多16Mi。这不替代未知行数的整体解码输出预算。`pdf-graphics-fax-budget-review.cjs`验证深度边界/恢复/跨实例祖先、原生画布调用前拒绝和扫描线分配前拒绝。对应浏览器脚本验证正常蓝色绘制、32000对输入被限定为512操作、正常传真白色像素和无效流替代为空图像，关闭均释放文档。

## Flate图片解压（#534）

图片路径读够所需字节后取消DecompressionStream读取，不保留无用尾随解压块；完整读取模式累计最多64MiB，预算错误不回退再解压。读取器在成功/失败后取消，pipeThrough管理输入写入的关闭/拒绝，避免未处理的writer Promise。手动回退解码器的buffer增长及构造提示也限制在64MiB，防止单个同步块绕过上限。

只需前缀的预览不会为了检查尾随校验和而继续解码全部内容。原生解压不支持或普通解码错误时沿用手动回退；预算超限明确失败。这是单流输出/缓冲预算，完整模式在复制期间可同时持有块和目标，不代表整份PDF累计内存上限。`pdf-flate-image-budget-review.cjs`检查提前取消、预算拒绝且不回退、分配前拦截、EOF/零长度和手动回退；对应浏览器脚本验证真实压缩图片、1MiB无用尾随输出及禁用原生解压后三个红色像素场景。
