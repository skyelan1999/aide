# Attachment context budget · 2026-10-29

Issue: [Aide #601](https://github.com/skyelan1999/aide/issues/601). Baseline `d1a77ac9307bcb31bd7f9576190af4e2f05d04c7`; branch `fix/attachment-context-budget-20261029`.

## Reproduced and changed

The old aggregate gate was 200000 UTF-8 bytes (displayed as 195 KB), independent of the selected model. Pre-fix tests reproduced rejection for 230000-byte ASCII, 270000-byte Chinese text and eight files totaling over 240000 bytes. The regression suite compiled and then failed with the original message before source changes.

Attachment assembly now uses a builder with an aggregate 16-MiB extracted-text processing guard including file markers, checked before copying. Selected-model preflight remains the context capacity gate. If the current input alone exceeds the window, the server reports input estimate/output reserve/window and avoids unnecessary model compaction. This check preserves session kind/persona/tools and leaves history/summary unchanged. If only history creates pressure, the existing compaction path remains operational.

Existing limits remain: eight attachments, 64-MiB raw text-file reads, 5-MiB images and 60000-rune document extraction with an explicit truncation marker. Context estimates are heuristic, not exact tokenizer measurements. This does not promise unlimited files or full document extraction.

## Executed validation

Linux ARM64 in `aide:ci-47c9e4486001-arm64` (Go 1.26), no external network:

- `go test -race -mod=vendor ./internal/server -run "^(TestAttachment|TestDirectoryAttachment|TestSourceAI|TestLocalWorkspacePathAIReadFix|TestStartTaskCompactsOverBudgetHistoryBeforeProviderCall|TestContextPreview)" -count=1` — PASS.
- `go vet -mod=vendor ./internal/server` — PASS.
- `python3 scripts/agent-route.py verify quick` — PASS, 263 commands, fingerprint `fbafb811072a103f4babb230a4410a47b4855c6ab81d2fb0e7f9d4e4b818a1e8`.
- `python3 scripts/check_docs.py` — PASS.

Synthetic API acceptance uses actual preview/start handlers and a local HTTP provider. The complete ASCII/Chinese attachment including end marker reaches the provider and finishes the task. Multi-file assembly retains all eight end markers. A small selected window rejects before any provider/compaction call, retaining existing history/summary and creating no run. More than eight attachments and the separate processing guard still reject without partial input. A history-only overflow still compacts successfully.

The initial macOS quick run was not all green: prebuild-wheels test assumes `/usr/bin/mv`, avatar test encounters macOS temporary-path canonicalization, and one Mermaid local HTTP test timed out. No unrelated test source was edited. The complete quick profile was rerun successfully under Linux with this checkout and its Git metadata mounted; the macOS failures are not labeled PASS.

Logs remain local in `.agent-state/attachment-budget-before.log`, `attachment-budget-linux.log` and `verify-20261029T030150234224Z.log`. No browser/new real-model/9999 acceptance for this patch; no release or deployment.

## Public plugin Issue policy

User requested one tracker for Aide and public-plugin bugs, requests and acceptance summaries. The repo agent instructions/workflow and new bilingual public-plugin Issue form now direct them to `skyelan1999/aide`. Implementation/design/releases remain in each plugin repo; private details and credentials remain outside public Issues. Existing Codex Bridge Issues were transferred preserving history as [#600](https://github.com/skyelan1999/aide/issues/600) and [#602](https://github.com/skyelan1999/aide/issues/602). Transfer does not mean fixed. The new quota request is [#604](https://github.com/skyelan1999/aide/issues/604).
