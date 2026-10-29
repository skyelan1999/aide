# Codex Bridge host lifecycle in the plugin

Issue: https://github.com/skyelan1999/aide/issues/615
Companion plugin: https://github.com/skyelan1999/aide-pkg-CodexBridge/pull/15

## User flow

Start the updated full Aide bundle, enable Codex Bridge 0.4.0, then use Install,
Start, Stop, Uninstall and Generate key in its workbench. No separate manager
window, EXE download or pairing code is required on this path. Host Node.js 18+
and an installed, logged-in Codex CLI are still prerequisites. Copy the model
key into the Aide model provider explicitly; installation does not change the
active model, log into an account or grant arbitrary host execution.

Old Aide versions retain the plugin's standalone-manager compatibility flow.
An image-only update without the companion launcher does not bootstrap the
host service: use an updated full bundle. The capability reports unavailable
rather than silently running downloads or falling back to unrestricted commands.

## Packaging and startup

`docker/codex-host-source.json` pins a public source commit, SHA-256 and version.
`scripts/build-codex-host.py` verifies the archive and builds six CGO-free native
binaries (Windows, macOS, Linux; x64 and ARM64). Each embeds the same-version
bridge payload. The runtime image carries these binaries, checksums, version
and license under `/opt/aide/host-components`. Build-time fetching is pinned;
installed users do not clone source or compile the manager.

The existing launcher invokes `scripts/codex-host-control.cjs` using Node and
argument arrays. The helper resolves the selected image to an immutable ID,
extracts only the matching native binary and its metadata, checks SHA-256 and
starts it headlessly. Existing Windows launchers remain PowerShell-based; the
new native component and Node lifecycle helper do not invoke PowerShell.

The manager listens on port 17888 (distinct from legacy port 8788). Windows and
macOS use loopback; Linux uses a verified local Docker bridge interface. It
publishes private connection metadata to the persistent data mount. Aide reads
that metadata without passing the control credential to the plugin frame.
Startup reuses a matching owned manager, replaces an authenticated older one,
and refuses an unverifiable existing service. The helper's stop action stops
only the authenticated manager and its owned bridge child. It never kills an
external bridge. Aide launcher failures produce a warning and do not prevent
the workbench from starting.

## Workbench boundary

Only enabled plugin ID `codex-bridge`, declaring
`workbench.hostComponent = "codex-bridge"`, receives capability
`host.component.codex-bridge`. The manual workbench method `host.component`
accepts exactly `{action}` from this fixed list:

- `status`, `install`, `start`, `stop`, `uninstall`, `key`
- `models`, `usage`

It has no arbitrary executable, URL, shell command or argument forwarding.
It uses existing workbench lock/scope/package/revision/expiry checks, replay
protection, single-flight execution and cancellation. Responses are checked
again against the current scope. No model tool receives this capability.
The HTTP client disables environment proxies and redirects. Bounded responses
are projected; only explicit manual `key` requests return the model credential,
which the workbench masks and clears on scope revocation or uninstall.
Remaining quota is derived from used percentage, not trusted as an independent
upstream claim. Missing or invalid quota is unavailable, not zero usage.

## Acceptance and remaining boundaries (2026-10-29)

- Companion plugin: 52 Node tests; Linux manager race tests and vet; six native
  binaries built from the pinned public archive, including Windows GUI binaries.
- Real Linux manager/bridge processes with a synthetic Codex CLI: install, key,
  start, models, stop, uninstall; running-install rejection; manager reuse.
- Chromium desktop and 390px English/dark workbench: all five controls exercised,
  masked key, download/pairing hidden, no horizontal overflow or page errors.
  This used a parent MessageChannel fixture, not a deployed production Aide.
- Aide authorization and transport tests cover allowed routes, invalid metadata,
  link rejection, replay, capability revocation, missing host and quota filtering.
- macOS ARM64 launcher acceptance: extracted the pinned binary from a disposable
  Docker image, started it headlessly, reused it, and stopped it. A UID 1000
  container read the private mounted metadata and authenticated to the host
  endpoint successfully. No production service was replaced.
- No actual Windows desktop run, real-account inference or full distribution
  install has been accepted yet. Cross-compilation is not Windows UI acceptance.
- Production 9999 is unchanged. Publishing the 0.4.0 market entry must wait for
  the companion Aide distribution; the public market remains 0.3.4 meanwhile.

Rollback: keep the previous full Aide bundle and image. Stop the authenticated
new helper before switching launchers. Bridge installation/key data are retained
unless the user explicitly chooses Uninstall; do not clean `.aide-data`.
