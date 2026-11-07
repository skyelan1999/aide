# Proposed repair diff, after read-only audit

The following is a reviewable change list, not evidence that a rebuild or release passed.

```diff
--- a/internal/server/web/vendor/drawio/NOTICE.txt
+++ b/internal/server/web/vendor/drawio/NOTICE.txt
-Modifications: None (unmodified official static webapp build, vendored for offline use).
+Modifications: Aide modifies the vendored v31.5.2 webapp. See the dated change manifest and notices in modified files. Original JGraph/draw.io copyright and Apache-2.0 license are retained. Nested asset licenses also apply.
```

1. Add an in-file comment such as `Modified by Aide on 2026-11-07; see .../NOTICE.txt` to every of the 136 upstream WAR files that differ. Preserve existing headers and comments. For minified bundles, verify the comment does not break loading or any release hash manifest. Recompute the comparison and keep both before/after hashes.
2. Correct `AIDE_UI_PATCHES.md` line claiming `mxgraph/mxClient.js` is unchanged.
3. Add a generated `THIRD_PARTY_NOTICES` + license-text tree sourced from pinned vendored modules and verified package metadata. Stage it in `Dockerfile` and `scripts/aide.sh export-bundle`; add a release check that inspects the actual built image/bundle, including optional sherpa and host components. This requires a separate build and package acceptance run.
4. Keep MIT copyright attribution unchanged until contributor/commissioned-work evidence is obtained. DeepSeek Harness design attribution remains, with any future literal copy separately identified.

There are already unrelated uncommitted changes in `scripts/aide.sh` and web files. Before touching packaging scripts, edit against the current working tree and preserve the pre-audit snapshot. Do not stage or commit those unrelated changes as part of the audit.
