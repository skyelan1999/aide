# docx-preview vendor

- **docx-preview**: v0.3.2, Apache-2.0
  - File: `docx-preview.min.js` (71389 bytes; locally patched)
  - SHA256: `8a221472fbb5b3c2291766ab643e3b38eeb9322ba0df122ae4f0fa25868f3aa9`
  - UMD bundle, exposes `window.docx.renderAsync`

- **JSZip**: v3.10.1, MIT (dependency of docx-preview)
  - File: `../jszip.min.js` (97630 bytes)
  - SHA256: `acc7e41455a80765b5fd9c7ee1b8078a6d160bbbca455aeae854de65c947d59e`
  - Loaded as classic script before docx-preview

Loaded via `<script src="/vendor/jszip.min.js">` and `<script src="/vendor/docx-preview/docx-preview.min.js">` in index.html.

Local patches (#234, #243): allow HTTP/HTTPS/mailto and relative/fragment hyperlinks; render validated Unicode symbol values with textContent. Original upstream v0.3.2 SHA256: `95c458b73a718ca96b511b7e712f0b743fe4284502b9ffaea5afdde96ba3036a`.

Aide supplies a detached style container and installs generated rules using scoped CSSOM stylesheets (`docx-style-bridge.js`), retaining the default CSP (#244). Fonts receive preview-local names; unsupported non-renderer at-rules are omitted. Default CSP still governs font/image URLs.
