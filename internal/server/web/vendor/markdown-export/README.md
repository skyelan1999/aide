# Markdown export browser dependencies

Vendored browser dependencies loaded locally on demand. PDF output uses pdfmake 0.3.13, html-to-pdfmake 2.5.29, pdf-lib 1.17.1, and two static Noto Sans SC weights. Their upstream licenses are included beside each asset.

- Upstream `pdfmake.min.js` 0.3.13 SHA-256 `cd2b315d9453352486f47a7ecb91f23de53d4c701275f59c31b6c0e7e971979d`
- Aide `pdfmake.min.js` SHA-256 `27817155017babbe5c7e797d0fc505c3153772bc861d8afc75dfa3eef9625c97`
- `html-to-pdfmake.js` 2.5.29 SHA-256 `cc684d09112f99741173b8942e0eff26ced6ee8c22f4cfbb8770dd2c390502e0`
- `pdf-lib.min.js` 1.17.1 SHA-256 `278627f3a5e3efa95e77ab565f10831d3cdb9cbb617ef8388d6ff4bf962d4887`
- `NotoSansSC-Regular.ttf` SHA-256 `a45e7485c3df0b90cb0b753e5a81291c1d53aa0a55dfc4ef22a1337a0762cd55`
- `NotoSansSC-SemiBold.ttf` SHA-256 `085597b66a9c249102e2662609d9a80d2f9e8909aa896fca1497f6ebd1f5b267`
- Source Noto Sans SC variable TTF SHA-256 `a3041811a78c361b1de50f953c805e0244951c21c5bd412f7232ef0d899af0da`
- Noto Sans SC Variable font source: https://github.com/google/fonts/tree/main/ofl/notosanssc (SIL Open Font License 1.1).
- pdfmake source: https://registry.npmjs.org/pdfmake/-/pdfmake-0.3.13.tgz
- html-to-pdfmake source: https://registry.npmjs.org/html-to-pdfmake/-/html-to-pdfmake-2.5.29.tgz
- pdf-lib source: https://registry.npmjs.org/pdf-lib/-/pdf-lib-1.17.1.tgz

The PDF pipeline converts rendered HTML to PDFMake vector/text elements, embeds images and clickable PDF file attachments, and merges bounded Markdown chunks with pdf-lib. Images decoded by the browser that PDFMake cannot embed directly are converted to PNG. Inline SVG diagrams remain vector elements. `html2canvas.min.js` handles only PNG downloads.

Aide's PDFMake patch replaces the PNG decoder's `const co=unzlib;` alias with the callback adapter below. This uses the same bundled fflate decompressor in the workbench without starting a blob worker. The workbench retains `worker-src 'self'`. The adapter keeps errors from the decoder visible to the caller. Export checks input image bounds before calling PDFMake.

```javascript
const co = (bytes, done) => {
  let decoded;
  try {
    decoded = unzlibSync(bytes);
  } catch (error) {
    return done(error);
  }
  return done(null, decoded);
};
```

The static TrueType fonts retain the full source character map. They were generated
with fontTools 4.60.1 and Brotli 1.1.0. For each of Regular (400) and SemiBold (600),
load the source using `TTFont(source, recalcTimestamp=False)`, instantiate with
`instantiateVariableFont(font, {"wght": weight}, inplace=True, updateFontNames=True)`,
save an intermediate with `font.flavor = "woff2"`, then reopen it with
`recalcTimestamp=False` and save the final TTF with `font.flavor = None`.
The font license remains in
`NotoSansSC-OFL.txt`.

- Upstream html2canvas 1.4.1 original SHA-256 `e87e550794322e574a1fda0c1549a3c70dae5a93d9113417a429016838eab8cb`
- `jspdf.umd.min.js` SHA-256 `e6551fcdc32f09d6853b2c5126d18d01d9447e0da618a41a11ebeee0f6c20d54`

- html2canvas 1.4.1: https://registry.npmjs.org/html2canvas/-/html2canvas-1.4.1.tgz
- jsPDF 4.2.1: https://registry.npmjs.org/jspdf/-/jspdf-4.2.1.tgz

Aide local backports (#411/#427): cursor-based CSS tokenization, including string slices, and explicit `li[value="0"]` ordinals. No numeric line-break rule change (#428 disproved against this version). MIT license retained. Patched `html2canvas.min.js` SHA-256 `25891ab3355762cd1b904c3a5bf3090f358615ce25656267c3c9024a33a33754`. Regression oracle and actual Chromium canvas checks are in `scripts/test_html2canvas_scans.cjs` and `scripts/browser-html2canvas-scans.cjs`.

Aide local backport (#426): optional `AbortSignal` per render. Aborting rejects waiting async generators and prevents their later resumption; drops generator references; removes the render-owned clone iframe and polling; cancels owned image waits and clears newly allocated renderer canvases. Caller-provided canvases and unrelated frames are retained. Calls without a signal preserve normal rendering. This cannot preempt synchronous JavaScript already running or native canvas encoding already submitted. Real Chromium cancellation, image pixels, canvas ownership and normal PNG/PDF checks: `scripts/browser-markdown-export-cancel.cjs`.
