# Markdown export browser dependencies

Vendored from official npm registry tarballs; loaded locally on demand. Both MIT licensed; license files included.

- Upstream html2canvas 1.4.1 original SHA-256 `e87e550794322e574a1fda0c1549a3c70dae5a93d9113417a429016838eab8cb`
- `jspdf.umd.min.js` SHA-256 `e6551fcdc32f09d6853b2c5126d18d01d9447e0da618a41a11ebeee0f6c20d54`

- html2canvas 1.4.1: https://registry.npmjs.org/html2canvas/-/html2canvas-1.4.1.tgz
- jsPDF 4.2.1: https://registry.npmjs.org/jspdf/-/jspdf-4.2.1.tgz

Aide local backports (#411/#427): cursor-based CSS tokenization, including string slices, and explicit `li[value="0"]` ordinals. No numeric line-break rule change (#428 disproved against this version). MIT license retained. Patched `html2canvas.min.js` SHA-256 `25891ab3355762cd1b904c3a5bf3090f358615ce25656267c3c9024a33a33754`. Regression oracle and actual Chromium canvas checks are in `scripts/test_html2canvas_scans.cjs` and `scripts/browser-html2canvas-scans.cjs`.

Aide local backport (#426): optional `AbortSignal` per render. Aborting rejects waiting async generators and prevents their later resumption; drops generator references; removes the render-owned clone iframe and polling; cancels owned image waits and clears newly allocated renderer canvases. Caller-provided canvases and unrelated frames are retained. Calls without a signal preserve normal rendering. This cannot preempt synchronous JavaScript already running or native canvas encoding already submitted. Real Chromium cancellation, image pixels, canvas ownership and normal PNG/PDF checks: `scripts/browser-markdown-export-cancel.cjs`.
