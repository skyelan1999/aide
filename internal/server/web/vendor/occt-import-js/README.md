# STEP preview parser

- Upstream: [kovacsv/occt-import-js](https://github.com/kovacsv/occt-import-js), npm release `0.0.23`.
- `occt-import-js.js` and `occt-import-js.wasm` are distributed without modification. Both upstream LGPL-2.1 notices are retained here.
- `occt-import-js-0.0.23-source.tgz` is the original npm source package. Its `occt-import-js/` directory includes the parser interface source; the OpenCascade source is the upstream `occt` git submodule documented in that package's `.gitmodules`.
- SHA-256: source package `eaab9ca7bf02799360d8fc70468dc8bf78eb2073c3fafb1ff982006e558fc1f3`; JavaScript `3fb44ce11d00611f9b3f3c5775d520ebab48930c1f08279b7b1316f05f0d3379`; WebAssembly `33391fc9d94ea5c869a6718488bf0a9a464222bac9bdc764dfe1690cef281952`.
- The preview loads these same-origin assets only when a STEP/STP file is opened. No model bytes are sent to a remote service.

This viewer uses only the STEP importer from the package. Keep the upstream licenses and source archive with the distributed assets. Re-check upstream license/source terms before changing versions.
