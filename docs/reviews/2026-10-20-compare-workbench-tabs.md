# Plugin workbench separate tabs — 2026-10-20

Plugin cards and embedded workbench headers now expose a separate-tab action. The new tab opens ordinary authenticated Aide with a validated plugin ID in the URL, obtains its own scoped sandbox bridge and keeps the normal export/save protocol. Credentials and bridge tickets are not transferred in the launch URL. Reload starts a fresh workbench; comparison paths and results are not persisted across tabs. Page exit sends a keepalive bridge revocation.

Actual Safari acceptance on the local source candidate: card launch, separate-tab refresh/reconnection, old bridge rejected with 403, two expected text replacements, image pair at 23,752 changed pixels / 4.723%, two changed DOCX rows with source locations, and technical HTML saved and verified on disk. The matching private Compare 0.3.1 UI separates settings from a larger result panel. Native Compare engine remains 0.3.0. No paid-model request was used for this UI change.

Twelve targeted Node bridge/client tests and Go workbench tests pass. Full release verification and architecture-specific runtime qualification are recorded separately. Actual desktop Safari was inspected; native mobile devices and the complete theme matrix were not accepted. Published Linux AMD64 runtime acceptance uses local emulation, not native AMD64 hardware.

Unrelated local Compose settings, installed plugin manifests, registry content, private plugin packages and upgrade backups are excluded from this source release. Current local runtime, data and prior binaries remain available for rollback; publication alone does not activate a new image in the existing user container.
