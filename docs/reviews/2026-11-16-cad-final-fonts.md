# Remaining font recovery and preview checks

This follow-up starts from main `4737cb97`, with 123 CAD entries retaining substitution declarations. The user requested more searching for hmhz, eq_eng, eq_chs, hgcad and Proxy 2, another complete archive retest, and main merge/push. Additional requests preserve native PNG viewing and separately audit XLSX/PPT/PPTX rendering.

## Font evidence and implementation

The [HGCAD distribution page](https://www.32r.com/soft/13266.html) supplies the version 3.0.6 package. Its PE resource JSON maps `IDF_11` to `HGCAD.SHX` and `IDF_12` to `HGCADHZ.SHX`. Both are read from bounded, standard LZMA resource data; no downloaded executable is run. The primary font is an Extended Roman Simplex Unicode stroke font. The publisher-paired Big Font has 7,021 mapped characters and retains the same mapping/metrics as the earlier distributed version, with some shape data differences. The exact package, resource mapping, extracted sizes and SHA-256 values are recorded privately.

The recovered `mtproxy2.ttf` has Windows family `Proxy 2` and Macintosh family `Prx2`; its SHA-256 agrees across independent published records. The resolver honors the actual requested family before applying that platform-name alias. A synthetic font regression verifies original outline selection and direct-family precedence. No font bytes enter Git or public runtime images; only the private local profile is updated. Same-name distributed versions cannot establish byte identity with the drawing author's original installation.

Additional corpus searches include 7,436 font entries in a public repository and a 6,061-entry public collector index. Remaining unavailable files and actual before/after substitution counts are recorded after rendering, without invented aliases.

## PNG entry behavior

PNG opens in the ordinary Aide image viewer, including read-only references, knowledge-format images and versioned backups. Registered raster handlers no longer intercept this default path. Raster editing starts explicitly in the selected workbench using its existing import/open controls. Other registered formats retain their route. The plugin-preview asset cache version is advanced. A live standalone-file check found that hidden sidebars left their resize handles above image controls, blocking the Fit button even on desktop. File-only pages now hide those handles. Five routing tests pass, including stale lookups, lock/scope changes, disabled handlers, drafts, PNG bypass and retained non-PNG routing.

## Acceptance and delivery

The font/geometry regression passes 28 cases. Installed runtime, all 453 source entries, 425 CAD requests, 413 distinct SVG images, 28 PDFs / 40 pages, source numeric geometry/hashes, native/browser interaction, XLSX/PPT/PPTX observations, full checks and source delivery are recorded after execution. Original files, sessions, settings and unrelated local work remain protected by the task backup.

## Additional Office audit

A synthetic two-sheet XLSX with a formula, date/currency values, a merged/styled title, an image, 101 rows and 27 columns confirms the current native viewer is a cell-value grid, not an original-layout renderer. It shows formula source without certifying calculation, and does not preserve merges, styling or pictures. Horizontal navigation followed by a sheet change incorrectly kept column 27 on a two-column sheet and showed no cells. Sheet changes now reset both row and column; actual browser interaction confirms the second sheet starts at A1 and shows its expected values.

A valid Apache POI legacy PPT fixture and a synthetic two-slide PPTX both reach the existing unsupported-binary error path. The current native Aide UI has no PPT/PPTX page renderer, and its bootstrap error handling also exposes the connection dialog on that failure. These formats are recorded as unsupported, not falsely passed as faithful previews. The user's additional request is a rendering audit; no new Office conversion engine is installed or claimed.

The first full run was intentionally superseded after the live Fit-button obstruction was found. Only a fresh run against the final CSS/JavaScript source and image may be used for acceptance.

## Executed final-image evidence

The final installed image is `sha256:68ae36add773ae4d79ba0845bc87c69367020773becc680a8ae36f3a0408ecaa`, pinned locally as `aide-acceptance:cad-final-fonts-20261116`. Version remains `0.1.19.0 RC2`. Its build identifies base `4737cb97` plus the recorded dirty-source fingerprint, including pre-existing unrelated local changes; this is not a clean-commit release image. Runtime health and integrity are both `ok`. Production verification preserves 86 sessions, 37 plugins, five pending approvals, all 194 protected business files, settings, mounts and drafts; ProfessionalProject remains 0.4.20.

The final image passes 425 CAD API requests and 413 distinct browser SVG draws. No skipped entities, degraded fills, missing glyphs or invalid local fonts are reported. Entity metadata is unchanged. Numeric comparison of 1,764,391 model/block records across 413 drawings finds no differences; text-string decoding is excluded from that numeric comparison. All 453 source-entry hashes and the original ZIP hash remain unchanged. All 28 PDFs and 40 pages have visible browser-rendered content, including compatibility pages and same-name SimSun rendering where required. The three opt-in runtime cases for real DWG conversion, SVG rendering and PDF font rendering pass against this image.

Remaining substitution declarations decrease from 123 to 122 CAD entries; 303 have none. Primary `eq_eng.shx` remains unavailable in five entries. Big Font `hmhz.shx` remains unavailable in 121 and `eq_chs.shx` in five, with overlap. Three `hzfs` Big Font declarations remain incompatible with an outline primary font. HGCAD and Proxy 2 primary declarations now resolve to the recovered fonts. Zero missing glyphs measures drawable coverage; it does not prove every requested font was recovered.

The actual browser exercises six measurement buttons and eight desktop/narrow, classic/professional, light/dark combinations. Native Aide opens PNG in the ordinary image viewer with usable zoom/Fit controls. Its CAD sample reports 6,650 entities and six matched / zero substituted fonts. The native app reconnects through normal quit/start, without token injection or lock bypass. Original work/reference directories are restored. Explicit MapCanvas import, wall editing and trusted-host PGM/YAML ZIP export change 21 pixels in the disposable fixture and leave its source PNG unchanged.

The supplied archive also contains eight XLSX files with 23 sheets. Their first 100 rows and 26 columns total 22,518 cells; raw API values agree with source cells and browser grid checks pass for that range. Nineteen multiline cells collapse to single-line presentation. These checks verify the grid's contents, not original document layout or formula recalculation. The sheet-switch fix is verified from row 101 / column 27 to a two-column sheet at A1. Valid PPT and PPTX fixtures remain unsupported as described above.

Private evidence, source/font hashes, screenshots, whole-archive per-entry results, backup and rollback image are retained under the task state and runtime backup directory. The downloadable local HTML/JSON report includes all 453 rows and a filter for the 122 entries still using substitution. Font binaries, business spreadsheet contents and credentials are excluded from this commit. Only owned audit pages/containers and the copied reproducible test executable are removed; production and unrelated local files remain intact.

Final full verification passes all 348 commands against source fingerprint `aa2d360fe755816131fbf374e8d9646b28d64f9058633e78e7c09f004eb7f698`. The Go suite registers 1,781 cases in 24 race shards: 1,689 pass, 92 conditional skips, no failed shards; remaining packages, platform contracts and vet also succeed. Skips are not counted as executed features. The three relevant real DWG/PDF cases are separately exercised and pass. Source-only main integration is authorized; no binary Release, tag or version bump is performed.
