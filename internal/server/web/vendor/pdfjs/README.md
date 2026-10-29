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
| `pdf.min.mjs` | `895c72c332dbd608a07ae54003e5e42741c566ade52ddbd2a00c8e57ffa35d9d` |
| `pdf.worker.min.mjs` | `8594047b1180bf302374a56a0af1484c3629b4539ee0a7fb84fea20e9c70edc8` |

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
