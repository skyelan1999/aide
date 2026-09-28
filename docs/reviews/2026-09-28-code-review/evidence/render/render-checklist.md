# 渲染/查看器/代码高亮 验证清单（2026-09-28，v65 · 最终）

应用 https://127.0.0.1:8097 ｜ 工作区 /workspace ｜ 样例目录 render_samples/

## 一、文件查看器（经 UI 文件浏览器点开）

| 类型 | 文件 | 结果 | 说明 |
|------|------|------|------|
| PDF | sample.pdf | PASS | pdf.js 渲染 2 页，2 canvas |
| DOCX | sample.docx | PASS | 标题/段落/表格/列表渲染 |
| DXF | sample.dxf | PASS | SVG，3 实体 |
| STL (three.js) | sample.stl | PASS | canvas 1892×740，meta『1 三角面 · 10.00×8.66×0.00』 |
| PNG | sample.png | PASS | natural 640×400 |
| drawio | sample.drawio | PASS | iframe 内 SVG 渲染 |
| Markdown | sample.md | PASS | GFM 全要素 |
| TXT | sample.txt | PASS | 多行/空行保留 |
| ZIP | sample.zip | PASS | 3 文件 2 目录 |
| SQLite | demo.db | PASS | 3 表 + schema + 行 |

## 二、独立代码文件高亮（hljs overlay）—— 7/7 PASS
worker.go(47) · point.py(46) · word_count.rs(61) · stack.cpp(72) · list_c.c(51) · fetch_json.ts(73) · orders.sql(82)。

## 三、Markdown fenced 代码块高亮（highlight-all.md，42 块）—— 最终复核
- **有高亮 token：42 / 42** ✅
- clojurescript：上游 highlight.js 11.9.0 无此语法，已移除无效 <script>、删除损坏的 404 占位文件，并回退到 clojure 语法，现产生 8 个 hljs token。
- 其余 41 种语言全部着色。
- 截图 evidence/render/highlight-all-md-FINAL.png

## 四、console / 语言包 复核
- 无 highlight 语言包 404；ruby/php/matlab 正常。
- 无 'Unexpected token <'（损坏的 clojurescript 包已删除）。
- 残留：index.html:63 一条 CSP style-src 内联样式提示（P3，不影响任何已测功能）。

## 最终统计
- 查看器：10/10 PASS
- 独立代码高亮：7/7 PASS
- MD 代码块高亮：42/42 PASS
- 缺陷状态：P1(STL) fixed、P2(MD 多语言) fixed、P3(clojurescript 包) fixed；仅 P3(CSP 内联样式提示) open（不影响功能）
