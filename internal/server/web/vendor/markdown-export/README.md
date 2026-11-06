# Markdown export browser dependencies

Vendored browser dependencies loaded locally on demand. PDF output uses pdfmake 0.3.13, html-to-pdfmake 2.5.29, pdf-lib 1.17.1, and Noto Sans SC Variable. Their upstream licenses are included beside each asset.

- `pdfmake.min.js` 0.3.13 SHA-256 `cd2b315d9453352486f47a7ecb91f23de53d4c701275f59c31b6c0e7e971979d`
- `html-to-pdfmake.js` 2.5.29 SHA-256 `cc684d09112f99741173b8942e0eff26ced6ee8c22f4cfbb8770dd2c390502e0`
- `pdf-lib.min.js` 1.17.1 SHA-256 `278627f3a5e3efa95e77ab565f10831d3cdb9cbb617ef8388d6ff4bf962d4887`
- `NotoSansSC-VF.ttf` SHA-256 `a3041811a78c361b1de50f953c805e0244951c21c5bd412f7232ef0d899af0da`
- Noto Sans SC Variable font source: https://github.com/google/fonts/tree/main/ofl/notosanssc (SIL Open Font License 1.1).
- pdfmake source: https://registry.npmjs.org/pdfmake/-/pdfmake-0.3.13.tgz
- html-to-pdfmake source: https://registry.npmjs.org/html-to-pdfmake/-/html-to-pdfmake-2.5.29.tgz
- pdf-lib source: https://registry.npmjs.org/pdf-lib/-/pdf-lib-1.17.1.tgz

The PDF pipeline converts rendered HTML to PDFMake vector/text elements, embeds source images and clickable PDF file attachments, and merges bounded Markdown chunks with pdf-lib. `html2canvas.min.js` remains for the PNG export only; it must not be used to render PDF pages.

- Upstream html2canvas 1.4.1 original SHA-256 `e87e550794322e574a1fda0c1549a3c70dae5a93d9113417a429016838eab8cb`
- `jspdf.umd.min.js` SHA-256 `e6551fcdc32f09d6853b2c5126d18d01d9447e0da618a41a11ebeee0f6c20d54`

- html2canvas 1.4.1: https://registry.npmjs.org/html2canvas/-/html2canvas-1.4.1.tgz
- jsPDF 4.2.1: https://registry.npmjs.org/jspdf/-/jspdf-4.2.1.tgz

Aide local backports (#411/#427): cursor-based CSS tokenization, including string slices, and explicit `li[value="0"]` ordinals. No numeric line-break rule change (#428 disproved against this version). MIT license retained. Patched `html2canvas.min.js` SHA-256 `25891ab3355762cd1b904c3a5bf3090f358615ce25656267c3c9024a33a33754`. Regression oracle and actual Chromium canvas checks are in `scripts/test_html2canvas_scans.cjs` and `scripts/browser-html2canvas-scans.cjs`.

Aide local backport (#426): optional `AbortSignal` per render. Aborting rejects waiting async generators and prevents their later resumption; drops generator references; removes the render-owned clone iframe and polling; cancels owned image waits and clears newly allocated renderer canvases. Caller-provided canvases and unrelated frames are retained. Calls without a signal preserve normal rendering. This cannot preempt synchronous JavaScript already running or native canvas encoding already submitted. Real Chromium cancellation, image pixels, canvas ownership and normal PNG/PDF checks: `scripts/browser-markdown-export-cancel.cjs`.
