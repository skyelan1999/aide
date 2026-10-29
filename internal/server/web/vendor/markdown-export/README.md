# Markdown export browser dependencies

Vendored from official npm registry tarballs; loaded locally on demand. Both MIT licensed; license files included.

- Upstream html2canvas 1.4.1 original SHA-256 `e87e550794322e574a1fda0c1549a3c70dae5a93d9113417a429016838eab8cb`
- `jspdf.umd.min.js` SHA-256 `e6551fcdc32f09d6853b2c5126d18d01d9447e0da618a41a11ebeee0f6c20d54`

- html2canvas 1.4.1: https://registry.npmjs.org/html2canvas/-/html2canvas-1.4.1.tgz
- jsPDF 4.2.1: https://registry.npmjs.org/jspdf/-/jspdf-4.2.1.tgz

Aide local backports (#411/#427): cursor-based CSS tokenization, including string slices, and explicit `li[value="0"]` ordinals. No numeric line-break rule change (#428 disproved against this version). MIT license retained. Patched `html2canvas.min.js` SHA-256 `31eb2428a97584bafd3edc53f50e605554790a4a570cac358db174366685cac6`. Regression oracle and actual Chromium canvas checks are in `scripts/test_html2canvas_scans.cjs` and `scripts/browser-html2canvas-scans.cjs`.
