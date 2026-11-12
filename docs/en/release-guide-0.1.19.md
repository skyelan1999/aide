# 0.1.19.0 RC2 usage and migration

RC1 remains an unpublished candidate source tag. RC2 restores the frozen marketplace lock in Docker test inputs and passed fresh validation. RC2 or newer satisfies the paired plugins' declared minimum host RC1.

[简体中文](../release-guide-0.1.19.md). [v0.1.19.0-RC2 prerelease](https://github.com/skyelan1999/aide/releases/tag/v0.1.19.0-RC2) was published on 2026-11-12 from `a9f8f412`. The seven Release assets include architectures, checksums and full validation reports; see the [publication receipt](../reviews/2026-11-12-release-preparation/publication-rc2.json).

## Update order

Back up data/configuration and keep the old full package/image. Match the Linux Docker engine architecture. Release installations use the existing full-package A/B health/rollback flow; source installations explicitly build after updating Git. Daily start never compiles, and a page refresh does not replace the binary.

Update the native launcher and helper together for selected-directory object retention. Then update SQLite 1.0.3 and StarLink 0.1.6 in the marketplace; both market entries require Aide `0.1.19.0 RC1`. Aide does not replace installed plugins automatically.

## Files, workspaces and references

Declared plugin file workbenches can browse/open/save through authorized workspace/reference channels. Reference writes require RW; AI access additionally follows workspace visibility. Conflicts preserve drafts and require rereading before assuming a save. Professional Resume 0.1.1 uses this controlled channel; its password and model-sharing boundaries remain separate.

Selected local directories keep `/mounts/<id>` paths. The native helper holds actual `os.Root` objects and serves them through loopback NFSv3 Docker volumes. Path or parent replacement after preparation retains the original directory; changed identities on a later preparation require deliberate removal/reselection. Existing global mounts are outside this change. RO is enforced by the server; stable writes synchronize. Sleep, process death or disk failures may yield I/O errors. `nolock` does not coordinate native-host database writers: stop them. Special files and foreign ownership changes are unsupported. macOS temporary-directory acceptance and Windows compile/isolated UI checks do not establish Windows directory, TCC, network-drive or unplug/replug hardware acceptance.

## SSH plugins

On Windows, a live directory handle may prevent renaming that directory or its parent. Remove the selection and normally apply/stop the owned installation, confirm its bridge has exited, then change the host path. Do not delete user data to clear an in-use error.

Workspace SSH, independent SFTP references and device SSH connections have separate identities. Disabled/hidden/RO or replaced connections cannot be authorized by old cached state.

- SQLite binds local relative paths to the selected workspace. Remote files use bounded per-call checkout and guarded host writeback: four slots, 64 MiB per file and total; nonempty WAL/journal is refused. Stop external writers and checkpoint until the call completes. This is optimistic file conflict detection, not distributed CAS/database locking or an atomic transaction across export slots.
- StarLink uses the matching AI-scoped local/SSH graph and full Unicode folding/normalization. Excerpts preserve original text; visibility, lock and policy changes invalidate caches. An incomplete index does not establish absence, and static relations do not prove execution.
- DOCX structure/comment/resolve tools and document/image attachments bind the selected source around staging/writeback without a local fallback.
- Terminal device sessions retain independent credentials. Fixture checks do not establish customer SSH, serial or X Server acceptance.

## Documents and PDF

Context preflight retains history according to the actual remaining request window; an oversized current input still fails. Generated requirement and phase indexes preserve complete Unicode titles. Windows source paths containing Chinese characters can validate and reuse their owned build cache; incorrect or extra receipt lines are refused.

Large Markdown uses paths, summaries and bounded excerpts with on-demand reads; model-window budgets still apply. Attachment-only tasks and long-paste conversion do not imply unbounded context or that all bytes were sent to a model.

Markdown PDF export preserves searchable text, paginated tables, separate images and discoverable embedded attachments. Actual browser evidence includes nine mixed-language pages, a 100-row table, PNG/SVG and two 50 MiB attachments. The 100 MiB describes aggregate attachments, not visible text or a universal memory guarantee.

JPEG2000 fallback is rebuilt from pinned source/patch/compiler inputs, with reproducibility, native sanitizer and allocation-fault evidence. Matching an independently built native CLI is a consistency check, not independent standards certification. Historical download crash #776 was only temporarily closed as unreproduced; its root cause remains unknown.

## Evidence and delivery

See [decoder/PDF](../reviews/2026-11-12-final-native/README.md), [SSH/plugin matrix](../reviews/2026-11-12-final-ssh/README.md), [directory bridge](../reviews/2026-11-12-directory-object-bridge/README.md) and [full Delivery contract](../testing/release-validation.md). The exact-source local full profile passed 340 commands. All ten required jobs in [Delivery attempt 2](https://github.com/skyelan1999/aide/actions/runs/43208363951/attempts/2) passed, including both native runtime/SDK architectures, race/vet, public packages, three native hosts, 48 browser cells and isolated clean Linux lifecycle. The 32 public entries include one retired package with expected refusal; conditional skips are explicit in the reports.

[#871](https://github.com/skyelan1999/aide/issues/871) recorded an internal resource-request error in one narrow WebKit 26.6 cell: the first attempt ran 34 cells, with 33 passes, one failure and 14 remaining cells not run. Two subsequent unchanged-source runs each passed all 48 cells, with no application, assertion or engine change. The [original report](../reviews/2026-11-12-release-preparation/webkit-871-attempt1.json) and [failure screenshot](../reviews/2026-11-12-release-preparation/webkit-871-failure.png) remain available. It is temporarily closed as unreproduced, with root cause unknown; this is not a claimed product crash fix. Linux WebKit and synthetic providers do not establish native Safari, OS permissions or paid-model acceptance.

Publication does not upgrade the maintainer's production installation or installed plugins. Keep backups and rollback assets.
