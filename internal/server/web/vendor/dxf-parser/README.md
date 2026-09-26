# vendor/dxf-parser — DXF 矢量图内联预览（离线 vendor）

本目录为 **dxf-parser** 的离线 vendor 副本，由 `internal/server/server.go`
经 `//go:embed web/*` 打包进二进制，**不访问任何 CDN**，保证离线 / 空气间隙可用。

## 版本与哈希（pin）

- 上游：`dxf-parser`（npm）/ gdsestimating
- 许可：MIT（见 `LICENSE`）
- 版本：**1.1.2**
- 来源：`https://cdn.jsdelivr.net/npm/dxf-parser@1.1.2/dist/dxf-parser.js`
- 产物：webpack 打包的 UMD bundle，挂载为全局 `window.DxfParser`（内含 loglevel 与全部实体解析器）

| 文件 | SHA-256 |
|---|---|
| `dxf-parser.js` | `445dd62529369a4ef32520d8b6232031ea8af01f1029b08b2d876ff6b7807b7b` |
| `LICENSE` | `56eb37102e6e49ce363e8021f017fe0a6de61d3705273bed307110a3386cb42f` |

校验：

```sh
cd internal/server/web/vendor/dxf-parser
shasum -a 256 -c CHECKSUMS.txt
```

## 运行时加载

- 由 `app.js` 内 `ensureVendorScript('/vendor/dxf-parser/dxf-parser.js', () => typeof DxfParser !== 'undefined')`
  动态加载（懒加载，仅打开 .dxf 文件时）。
- 同源、由 `go:embed` 提供，**断网可用**。
- 解析结果（实体 JSON）由 `setupDxfPreview` 转换为 SVG 渲染（Y 翻转、viewBox 自适应、
  ACI 颜色映射、缩放/平移/适应窗口）。

## 升级

1. 在 jsDelivr 选定新的固定 `1.x.y` 版本；
2. 重新下载 `dist/dxf-parser.js` 与 `LICENSE` 覆盖本目录；
3. 更新本文件与 `CHECKSUMS.txt` 的版本号和 SHA-256；
4. `node --check dxf-parser.js`、重建镜像、真机点 .dxf 验收。
