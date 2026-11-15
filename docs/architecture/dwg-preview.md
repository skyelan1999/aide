# DWG 二维预览

## 使用说明

在项目文件或引用来源中点击 `.dwg` 即可查看二维图纸。滚轮和 ± 缩放、拖动平移、“适应”恢复全图；触屏可拖动。工具栏保留“下载原文件”；错误可重试。图纸不会因打开而发送给模型或云端转换服务，不写回原文件。

只读引用不需要打开引用写权限或 AI 可见性。来源停用、目录或工作区切换后，旧请求会被拒绝，需要从当前目录重新打开。

## 绘制与限制

标准 DWG 查看器先用固定 GNU LibreDWG 0.14（带摘要固定的 R2013+ 样条场景修复补丁） 生成保留源版本的 DXF，再用已内置的 ezdxf 1.4.4 绘制 SVG。浏览器把它作为隔离的矢量图像显示，复用缩放、平移和适应窗口；不将图纸内容插入页面 DOM。直接打开 DXF 仍使用既有浏览器解析器。

详细 DWG 绘制支持常见线、弧、椭圆、多段线、按控制点/节点/权重计算的样条、填充、引线、尺寸、块及属性、文字格式，以及图纸内已保存且绘图库可识别的代理图形。嵌套块、基点、旋转和缩放由 CAD 绘图库处理。中文以标准构建提供的文泉驿正黑字形生成矢量轮廓，减少浏览器字体依赖；替代字体的外观可能与原 SHX/TTF 不同。

预览显示源模型实体数量；不能恢复的内容按“未恢复 N 处内容”提示，悬停可看实体类型计数。转换后的样条缺少定义或存在非有限/绝对值超过 10¹² 的坐标时，跳过整条曲线，避免错误几何影响整图。有效控制点和节点数据用于求值曲线，不把控制多边形当成曲线。缺失节点参数标志时保留原始控制点场景，避免将其错当拟合点读取。

无效填充比例仅恢复原有边界，不猜测图案比例或改成实心，界面提示“仅显示轮廓”。非法二进制代理记录可在临时 DXF 中整条跳过并计入未恢复数；不修补代理二进制，也不修改相邻几何。损坏的绘制顺序链接仅在临时文档中忽略并计入绘制提示；其它结构错误继续明确报错。完全透明的绘制记录不参与自动适应边界，避免不可见文字底框将整图压缩成空白。SVG 固有尺寸限制在 1000 像素内，几何仍保留规范化矢量坐标。

三维、缺少代理图形的自定义对象、外部参照和转换器未读出的数据仍可能缺失。没有自动加载外部参照或外部栅格文件、DWG 编辑和工程校核能力。即使预览显示成功，关键尺寸和制造图纸仍需 CAD 核对。屏幕绘制采用相对线宽并自动适应图形边界避免大型模型的绝对线宽缩成几乎不可见，不代表原图的打印比例。

## 测量和主题

工具栏直接显示平移及各测量按钮，当前工具高亮，可用左右方向键切换（Home/End 到首尾），无需展开工具下拉菜单。工具包括两点距离（含 ΔX/ΔY）、连续长度、三点夹角（第二点为顶点）、多边形面积/周长、三点圆半径/直径和坐标。连续长度和面积通过“完成”或 Enter 结束；Escape 取消当前点，撤销回退当前点或上个结果，也可逐条删除/清空。最多保留 50 个结果，每次最多 256 个点，结果仅存在于当前预览，不写入 DWG。

服务端返回实际 CAD 模型到 SVG 的仿射矩阵、`$INSUNITS` 及至多 25,000 个可见实体的世界坐标端点/圆心。浏览器通过屏幕 CTM 和逆矩阵计算二维投影坐标，不按屏幕像素测量；捕捉半径固定为 10 CSS 像素。有效单位可换算为 mm/cm/m/in/ft；原图未指定单位时明确标注，选择单位按 1:1 解释，不推断毫米。直接 DXF 复用同一测量 UI 和其 Y 轴翻转矩阵。

画布使用主题背景，SVG 背景透明。有效主题/配色变化时重建隔离的 SVG Blob，将黑白自动色替换为当前文字色，保留其它 ACI 颜色；不重置缩放或测量。关闭预览时清理 Blob URL、主题/视图观察器和测量事件。

## 资源与身份边界

- 输入 DWG 不超过 16 MiB；转换后的 DXF 小于 16 MiB。详细绘制输入不超过 2,000,000 行；详细 JSON/SVG 输出小于 16 MiB。直接 DXF 保留 4 MiB / 100,000 行限制。
- 最多两个并发请求；忙时提示重试。转换和绘制各最多 20 秒，含来源读取的整个请求最多 30 秒。每个子进程限制 512 MiB 地址空间、15 秒 CPU、16 MiB 文件、64 个文件句柄，禁止 core dump。
- 详细绘制限制 100,000 个源数据库对象（含字典及未使用的块）、50,000 次展开实体访问和绘制记录、32 层调用、2,000,000 个路径/字形控制顶点、单段 16,384 字符和累计 1 MiB 字符。填充生成另有单次 1 秒预算；既有直接 DXF 的实体/节点/几何/块预算保持不变。
- 取消请求终止进程组；临时目录仅含私有图纸副本和中间结果，结束删除。达到转换文件容量上限时，即使 CLI 返回成功也按容量超限拒绝。
- 项目和来源身份校验拒绝路径穿越、符号链接、FIFO、设备和超限文件；读取、转换及绘制后复查身份。身份校验不承诺远端文件系统的原子快照。

## 构建和运维

`Dockerfile` 与 `Dockerfile.local` 构建独立 GNU LibreDWG CLI；源码版本、URL 和摘要固定于 `docker/libredwg/source.json`，脚本先核对 SHA-256，不将 GPL 库链接进 Go。镜像 `/opt/aide/dwg/` 包含 CLI、COPYING、AUTHORS、元数据、精确源码归档、修复补丁及其 SHA-256、摘要和构建脚本，来源见 `legal/third_party/libredwg/`。

详细绘制脚本 `internal/server/dwg_preview_svg.py` 嵌入 Go 程序；使用固定的运行时 ezdxf 包。标准镜像提供 `fonts-wqy-zenhei=0.9.45-8`；字体原许可/版权声明保留在 Debian 包及 `/opt/aide/licenses/platform/wqy-zenhei-copyright`，源文本和来源指纹见 `legal/third_party/runtime/wqy-zenhei/` 与 `legal/third_party/sources.json`。

独立 Linux/macOS 部署需要 Python 3、ezdxf 1.4.4、中文字体及可执行的 `dwg2dxf`。`AIDE_DWG_CONVERTER=/absolute/path/dwg2dxf` 可覆盖默认 `/opt/aide/dwg/bin/dwg2dxf`。缺少组件时更新标准构建。Windows 标准 Docker 路径运行 Linux 组件；Windows 原生资源限制适配未在本轮验证。

接口不把转换器 stderr、宿主路径或图纸内容返回诊断。独立 CLI 和资源限制不等于原生代码的完整隔离沙箱。

## 接口和验证

`GET /api/file/dwg-preview?path=...&root=...` 或 `source=...` 默认仍返回不缓存的 `application/dxf`。附加 `format=svg` 返回 `application/json`，包含 SVG、规范化宽高、模型实体数、未恢复/降级计数和类型统计、`modelToSvg` 六参数仿射矩阵、`units` 和有界 `snapPoints`；新 DWG 查看器使用此格式。两种格式均要求原访问认证、解锁和来源身份，接受 `knowledgeOrigin` / `knowledgeWorkspace`；下载继续使用 `/api/file/raw`。

- Go 专项：`go test ./internal/server -run TestDWGPreview -count=1`。
- 在真实 CAD 运行环境设置 `AIDE_DWG_TEST_FILE` 与 `AIDE_DWG_TEST_SVG=1`，实际执行转换/详细绘制/API 集成测试；未设置时明确跳过，不算解析验收。
- 标准 CAD 运行环境：`python3 scripts/test_dwg_svg.py`，检查填充、引线、属性、中文文字、尺寸、曲线、异常数据、线宽和资源边界。
- 转换器上游六代 SPLINE 样本：`python3 scripts/test_dwg_converter.py`（标准运行镜像）。
- 测量几何：`node scripts/test_cad_measure.cjs`。
- 前端：`node scripts/test_dxf_preview_bounds.cjs`，检查既有预算及隔离图像、数量、缩放/适应和 URL 清理。

原实现验收见 [2026-11-14 记录](../reviews/2026-11-14-dwg-preview.md)，容量修复见 [首次故障修复记录](../reviews/2026-11-15-cad-all-entities.md)，实体完整性改进见 [详细预览验收](../reviews/2026-11-15-cad-all-entities.md)。

图纸的 Pointer/Wheel、键盘及捕获阶段内层滚动会刷新账户空闲计时，并通过锁屏集群转发真实末次操作时间；后台状态轮询不算用户操作。真正空闲仍按账户设置锁屏。后续按钮及空闲计时验收见 [直接按钮与空闲计时修复](../reviews/2026-11-15-cad-all-entities.md)。

## PDF compatibility and offline resources

PDF.js retains its 16 Mi-pixel embedded image and canvas limits, 8192-pixel page edge, and hardened OpenJPEG component/allocation guards. Matching 4.4.168 CMaps and standard fonts are bundled locally with file hashes and license notices in `web/vendor/pdfjs/resources-source.json`; system font probing is disabled. Font substitution does not recover an unembedded original font.

A page with drawing operations that renders blank or fails decoding may use protected `GET /api/file/pdf-preview?path=...&page=1&size=2048`, with the existing root/source/knowledge identity. Ordinary PDF input is bounded to 16 MiB, page to 1-2000, and size to 256-4096. The standard image pins Debian Poppler 22.12.0-2+deb12u3 as a separate process, with package copyright in `/opt/aide/licenses/platform/poppler-copyright`. It receives a private copy and returns a size/dimension-checked PNG; CAD process limits, shared concurrency slots, cancellation and source revocation checks apply. The browser bounds streaming output, cancels on close/zoom and revokes object URLs. Compatibility preview is raster and limited to a 4096-pixel edge. The endpoint does not receive PDF passwords.

Empty dictionary-name/null-handle placeholder pairs are normalized only in the temporary DXF, preserving named references and geometry, and counted as drawing notices. Full archive results and limits: [CAD/PDF follow-up](../reviews/2026-11-15-cad-all-entities.md).

## Complete planar geometry follow-up

The pinned converter now retains all HATCH paths and pattern definitions after zero fit-point spline boundaries, exports exact saved proxy graphics independently from opaque custom data, and preserves bounded handle-indexed AcDs SAB/ASM records in native R2013+ ACDSDATA. The renderer evaluates planar REGION loops with straight/elliptic edges and body/block transforms. Unsupported topology is rejected explicitly; no chord substitution is used. ACIS linked chains are capped at 4096 nodes and a region at 50000 edges.

Tiny positive TEXT heights are kept verbatim. Fixed single-column MTEXT that cannot fit substituted font metrics retries with automatic column height, retaining insertion/width/formatting/font height and recording recovery notices. Zero-width formatting-only labels contain no visible glyphs or rectangle. See the [integration review](../reviews/2026-11-15-cad-all-entities.md).

旧亚洲 MIF 转义按固定一位代码页与四位十六进制解析，未知映射保留原转义；DXF 文字输出在实际写入位置终止。转换器构建会直接测试补丁后的原函数、真实代码页表和填充非零缓冲区的一致性，防止尾部未初始化字节变成文字。
