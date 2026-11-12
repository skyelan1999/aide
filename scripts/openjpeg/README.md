# Aide OpenJPEG source build

The decoder is built from OpenJPEG `8314119b067c0fc77834731168daaebd379fdb12` and Mozilla's wrapper `c76ca8601f97bae7e14b3974a7d157c675f03b8c`, using the multi-platform Emscripten 4.0.19 image pinned in `pins.json`. Both source archive SHA-256 values and both reviewed patch SHA-256 values are checked before compilation; the compiler container has no network.

```sh
python3 scripts/openjpeg/build.py --work .agent-state/openjpeg-clean-rebuild --check
```

Use a new work directory. The builder never resets existing trees, invokes the Aide launcher, publishes an image, or updates an installed application. `--update` explicitly copies a rebuilt fallback into the vendored artifact. The decoder keeps the upstream BSD license notices under `internal/server/web/vendor/pdfjs/openjpeg/`.

`native-fixes.patch` includes Mozilla's BSD buffer-stream support followed by Aide's local changes: checked terminal HT row writes; a one-pass HT cleanup segment across quality layers; preserved nested ownership after failed precinct/code-block reallocations; signed MCT offset conversion, selected-MCC lookup and fractional offset state; widened DC shifts; cleanup segmentation-symbol errors; the tag-tree unknown sentinel; a checked Emscripten allocator boundary; and borrowed immutable-buffer PPM/PPT marker payloads. Borrowing is enabled only for the known buffer reader, with bounds checked against caller-owned input. File/custom streams keep owned copies; successful merging and error cleanup distinguish ownership. Codec destruction precedes release of the input buffer.

The wrapper patch normalizes signed grayscale before precision narrowing and exposes allocator failure/budget errors. Failed modules are discarded by the PDF.js adapter. Preview header/output policy remains an independent layer, not full JPEG 2000 conformance or a whole-browser memory limit.

The e-sYCC conversion uses PIMA 7667:2001 Annex B equations B7–B9 (printed page 13), with independent component normalization, doubled chroma magnitude and final 8-bit display clipping. The original standard, rather than an explanatory web summary, was inspected from a historical [document mirror](http://13thmonkey.org/documentation/fonts/pima7667-2001.pdf); ITU-T T.801 [Table M.25](https://www.itu.int/epublications/publication/itu-t-t-801-v3-2023-08-information-technology-jpeg-2000-image-coding-system-extensions) identifies EnumCS 24 with this standard. The standard text is not redistributed. Subsampled e-sYCC remains an explicit unsupported-layout error.


Native memory checks and retained reported/official vectors:

```sh
python3 scripts/openjpeg/test-native.py --work .agent-state/openjpeg-native-check
node scripts/pdf-jpx-official.test.cjs .agent-state/openjpeg-native-check/fixtures
```

The runner verifies the small checked-in base64 fixture hashes and downloads only the two immutable, SHA-256-pinned official vectors in `official-vectors.json`. `--fixtures DIRECTORY` reuses matching retained official files; a verified `core.tar.gz` in the new work directory avoids redownloading the core archive. GCC ASan/UBSan/LSan checks real C allocations, 25 injected owner-array failures at each of two sites, 64 terminal HT block heights, and both borrowed-buffer and owned-file PPM/PPT success/error cleanup. Only the disposable test source receives injection hooks. The generated-JS official raster hashes were compared with a separately built native OpenJPEG CLI; they are consistency evidence rather than independently certified reference pixels.
