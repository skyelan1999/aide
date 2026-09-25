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
| `pdf.worker.min.mjs` | `a7cf858ab0bbe21a00ef438c24a4ef5833c3d05935a294382c8d0773a8958f50` |

校验：

```sh
cd internal/server/web/vendor/pdfjs
shasum -a 256 -c CHECKSUMS.txt
```

## 升级

1. 在 jsDelivr 选定新的固定 `4.x.y` 版本；
2. 重新下载 `build/pdf.min.mjs` 与 `build/pdf.worker.min.mjs` 覆盖本目录；
3. 更新本文件与 `CHECKSUMS.txt` 的版本号和 SHA-256；
4. `node --check ../../app.js`、重建镜像、真机点 PDF 验收。

## 运行时加载

- 主 bundle：`app.js` 内动态 `import('/vendor/pdfjs/pdf.min.mjs')`（懒加载，仅打开 PDF 时）。
- Worker：`GlobalWorkerOptions.workerSrc = '/vendor/pdfjs/pdf.worker.min.mjs'`。
- 两者均同源、由 `go:embed` 提供，**断网可用**。
