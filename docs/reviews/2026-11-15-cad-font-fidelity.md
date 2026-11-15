# CAD/PDF font fidelity follow-up · 2026-11-15

The preview previously replaced every CAD typeface with one CJK font. Unembedded
PDF fonts also used a fallback even when the original family was available
locally. This follow-up preserves declared faces and text metrics, restores
recoverable text encoding, and repeats the private archive regression.

## Implementation

- Resolve a registered original file, then the declared family and requested
  regular/bold/italic style. Keep original style declarations, width factors,
  alignment and inline formatting. Measure the same mixed font runs that are
  drawn, including stroke-to-outline transitions.
- Prefer configured local fonts without turning drawing font names into paths.
  Flat profile limits: 32 files, 32 MiB per file, 128 MiB total; symlinks and
  invalid fonts are skipped. Report actual matches, substitutes and unavailable
  glyphs separately.
- Bundle four unchanged, hash-pinned LibreCAD LFF source fonts for explicit
  TXT/ROMANS/SIMPLEX/ISOCP stroke substitution. Corresponding source and GPL
  notices are retained. Private proprietary font bytes are not distributed.
- The sixth pinned LibreDWG patch preserves UTF-8/escape continuation boundaries
  and exact 250-byte final groups. It initializes the output codepage from the
  source header, decodes legacy multiline text before Unicode conversion, and
  escapes Latin-1 symbols instead of writing invalid bytes. Renderer-side
  Unicode decoding and lossless fragment repair occur only in temporary data.
- Inspect PDF fonts through the existing protected preview endpoint. Embedded
  fonts stay in PDF.js. Actual matching unembedded families use bounded local
  Poppler rendering with child-only fontconfig and the same source identity,
  cancellation and concurrency guards. Failure falls back with an explicit
  notice. PDF pager input width is bounded inside file dialogs.

## Verification evidence

Private raw evidence is retained under
`.agent-state/cad-font-fidelity-20261115/`; customer paths, drawings, rendered
geometry and private fonts are deliberately excluded from Git.

| Check | Observed result |
| --- | --- |
| Archive inventory | 453 records: 285 DWG, 140 DWG-content backups, 28 PDF; nested archives included |
| Actual CAD endpoint | 425 HTTP 200; zero skipped entities, degraded fills or missing glyphs |
| Browser SVG decoding/drawing | 413 distinct CAD contents, all visible |
| Browser PDF pages | 28 documents / 40 pages, no blank page or font-inspection failure |
| PDF fonts | 21 documents retain embedded fonts; one unembedded SimSun document uses an actual local matching family |
| CAD font coverage | 140 files contain matched faces; all 425 also contain declarations requiring explicit substitution |
| Renderer regression | 22 tests, including original face/metrics, mixed runs, profile bounds, encoding and regular/bold/italic selection |
| Actual converter functions | Compiled MIF/codepage, GBK/UTF-8 multiline, plus/minus and continuation-boundary checks pass |
| Configured runtime integration | Two real DWG tests and one original-font PDF test pass, with opt-in conditions actually enabled |
| Geometry source comparison | 413 drawings / 1,764,391 model-and-block records retain entity order/type, numeric geometry, references and binary groups |
| Original files | Original archive and all 453 drawing SHA-256 values unchanged |
| UI | Six direct measurement tools, zoom/fit, eight desktop/narrow palette/theme combinations and cancellation/failure paths exercised |
| Native host | Final installed font build displays the original readonly drawing; owned test views closed and file-browser locations restored |
| Runtime preservation | 86 sessions, 37 plugins and 194 protected files preserved; healthy/integrity OK |

Whole-repository verification ran all 348 configured checks. Go race/vet and other packages passed: 1,781 registered server entries, 1,689 passed and 92 conditional skips. Three relevant opt-ins were separately enabled and passed in the configured final runtime. The initial full command failed only because this newly linked review did not yet exist when the documentation check ran; after adding it, the documentation check passed (534 Markdown files / 2,555 links). The task retains the original failed receipt plus a separate successful documentation retry and a composite receipt with the unchanged code fingerprint. The original full receipt is not rewritten as a successful command. Source implementation `d4547563b7356658eb46ef6f5d2b4ffa0166bf71` was fast-forwarded into local main and read back from origin/main after push. Existing account rule bypass accepted the direct main push; branch rules were not changed. This later documentation receipt keeps the verified code fingerprint unchanged. Final remote Actions results are tracked separately against the final main revision.
Earlier candidates and their interrupted or superseded checks are not final
acceptance evidence. One superseded full run completed Go but lost its old
untagged runtime image to normal build recycling before its final asset check;
the final acceptance image is retained with an explicit task tag.

## Fidelity boundary and delivery

The supplied ZIP and nested RARs contain no font files. Matching local TTF family
names improves fidelity but does not prove the original historical font version.
Missing SHX and bigfont files remain explicit stroke/per-glyph substitutes;
zero missing glyphs does not mean every font is original. Sparse PDF embedded
subsets remain in their own PDFs and are never installed as complete fonts.
Preview success and source-geometry checks do not claim pixel-identical CAD
printing or restore unavailable external references.

The local font profile contains ten existing, locally available faces and is
stored only in the application's private data directory. The standard image
contains the open fallback fonts and their notices. Existing CAD/PDF resource,
authentication and source-revocation limits remain enforced.

Source integration follows the user's existing explicit main merge/push request.
No version bump, tag, binary Release or distribution of private fonts is requested.
The installed source build retains its precise dirty-content identity separately
from the eventual main commit. Runtime/data and pre-integration source recovery
points are retained; unrelated working files must keep their exact bytes.
