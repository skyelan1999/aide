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
| `pdf.worker.min.mjs` | `57f94b2bfde73e441bb6e496227926662182810979956ec335381719ad3e3e3e` |

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
