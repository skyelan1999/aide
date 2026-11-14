# DWG 二维预览

## 使用说明

在项目文件或引用来源中点击 `.dwg`，稍候即可看到二维图纸。滚轮和 ± 缩放、拖动平移、“适应”恢复全图；触屏可拖动。工具栏始终保留“下载原文件”；出现错误时可重试，或下载到 CAD 检查。图纸只用于预览，不会因打开而发送给模型或云端转换服务，也不会写回原文件。

只读引用可以预览，不需要打开引用写权限或 AI 可见性。来源停用、目录或工作区切换后，旧请求会被拒绝，需要从当前目录重新打开。

## 范围和限制

复用现有 DXF 的 SVG 渲染：LINE、POINT、CIRCLE、ARC、ELLIPSE、多段线、SPLINE、TEXT/MTEXT、INSERT、DIMENSION、SOLID 等。LibreDWG 的版本与实体支持、浏览器渲染器能力都会影响显示。三维实体、代理对象、外部参照、字体和复杂图形可能缺失；无自动加载外部参照、DWG 编辑或工程校核能力。即使转换成功也须在 CAD 中核对关键尺寸。

- 输入 16 MiB，DXF 输出 4 MiB；沿用前端 50,000 实体、50,000 节点、200,000 点、32 层块预算。
- 最多两个并发请求；忙时提示稍后重试。每次转换 20 秒，包含读来源的请求 30 秒。进程限制 512 MiB 地址空间、15 秒 CPU、4 MiB 文件、64 个文件句柄，禁止 core dump。
- 取消请求会终止整个转换进程组；临时目录仅含图纸副本，结束后删除，不持久化转换文件。
- 路径沿用项目与来源身份校验；拒绝穿越、符号链接、FIFO、设备和超限文件，读取和转换后复查来源身份。身份校验不承诺远端文件系统的原子快照。

## 构建和运维

`Dockerfile` 与 `Dockerfile.local` 单独构建 GNU LibreDWG 0.14 的 `dwg2dxf`。源码版本、URL 和摘要固定于 `docker/libredwg/source.json`，构建脚本 `scripts/build-dwg-converter.sh` 先核对 SHA-256，再生成独立工具，不将 GPL 库链接进 Go 程序。首轮需要下载已固定的开源源码，后续复用缓存；下载或校验失败会中止构建。

镜像 `/opt/aide/dwg/` 包含 CLI、COPYING、AUTHORS、版本元数据、精确对应源码归档、摘要和构建脚本。第三方许可及来源另外记录于 `legal/third_party/libredwg/`。

独立 Linux/macOS 二进制部署需 Python 3 与可执行的 `dwg2dxf`，管理员通过 `AIDE_DWG_CONVERTER=/absolute/path/dwg2dxf` 指定（默认 `/opt/aide/dwg/bin/dwg2dxf`）。普通用户无需设置；缺少组件会给出更新标准构建和下载原文件的提示。独立 Windows 原生进程的资源限制适配未在本次验证，标准 Windows Docker 路径运行 Linux 组件。

排查时先看界面错误，确认图纸大小、组件是否存在及来源连接。接口不把转换器标准错误、宿主路径或图纸内容返回日志/浏览器。单独的 CLI 和资源限制不等于恶意原生代码的完整隔离沙箱。

## 接口和验证

`GET /api/file/dwg-preview?path=...&root=...` 或 `source=...`，接受现有查看器的 `knowledgeOrigin` / `knowledgeWorkspace` 身份，需现有访问认证和解锁状态；成功返回不缓存的 `application/dxf`。原始下载继续使用原有 `/api/file/raw` 接口。

自动化单测：`go test ./internal/server -run TestDWGPreview -count=1`。真实转换测试需设置 `AIDE_DWG_TEST_FILE` 为 GNU 示例 DWG，并在包含组件的镜像运行；未提供时明确跳过，不能算解析验收。浏览器及生产验收证据见本次 [验收记录](../reviews/2026-11-14-dwg-preview.md)。
