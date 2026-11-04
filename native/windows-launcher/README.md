# Windows native host

`aide-launcher.exe` is the Windows startup and resident host entrypoint. The BAT
shortcuts call it directly. Docker Desktop with Linux containers and Compose is
required; host PowerShell, Python, .NET and an installed C++ runtime are not.
Legacy PS1 sources remain in the repository for migration reference and regression
coverage. New Windows/full packages do not ship the PS1 lifecycle chain.

```text
aide-launcher.exe start --no-browser
aide-launcher.exe host-status
aide-launcher.exe status
aide-launcher.exe open
aide-launcher.exe stop
aide-launcher.exe host-stop
aide-launcher.exe recover-stop
aide-launcher.exe clean-cache --dry-run
aide-launcher.exe --root "C:\path with spaces\aide" --project my-install
```

The default root is the executable's directory. An empty directory can bootstrap
from a validated local image or the embedded, SHA256-pinned official image
manifest. Existing Compose/environment files and data remain in place. Explicit
source builds use `build.bat` or `aide-launcher.exe build`. `start` never compiles,
even with legacy build environment variables; a missing source runtime instructs
build instead of substituting a Release. `--force-build` is valid only for build
or clean. The [standard contract](../../docs/architecture/launcher-contract.md)
defines start/stop/build/clean/docker_clean/verify/release and package differences.

Git `build.bat` and `clean.bat` bootstrap the current native EXE entirely inside Docker, including when an older EXE exists. The
native host then incrementally builds the runtime and the current host using an
installation-scoped Buildx builder. A verified immutable host under
`.aide-data/native/compiled` receives subsequent commands via a checksum-verified
receipt. Build success applies the new image before opening the workbench;
ordinary repeated start only reopens a healthy instance. New Release packages
ship start/stop/docker_clean/verify and the generated EXE, with source-only build,
clean and release omitted. No native failure silently invokes PS1.

## One distributed executable, concurrent tasks

The executable embeds the current compiled directory/LAN components, their
SHA256 values and dependency notices, Compose/data preparation resources, and the
update protocol implementation. Components are materialized into private caches
when needed; they are not separate downloads or manually installed programs.
This is one distributed EXE, not one OS process or a replacement for Docker.

When an existing installation is opened with a launcher whose embedded LAN
helper differs from the cached helper, the launcher requests a host-worker refresh.
The helper uses the fixed loopback `-remote-target` mapping for password-protected
LAN access. If that optional mapping or helper capability is unavailable, the
launcher logs a clear warning and keeps message chat running; the workbench stays
available and the settings page reports LAN access as unavailable. The launcher
compares the embedded helper's machine-readable `-capabilities` protocol with the
service `/healthz` capability manifest before forwarding the optional port. The
protocol constants are generated from the Go bridge package at build time. Older
helpers are probed through their help output; older services safely disable only
remote access. Update and restart Aide to enable it. When replacing a source-built launcher, run `stop.bat`
and then `start.bat` so the old resident controller releases its installation
lock before the new executable starts.

A controller starts bounded port and update worker processes using the **same
EXE**. Directory and LAN helpers belong to its kill-on-close Win32 Job. A worker
can restart without abandoning the helper processes or creating an unbounded
number of workers. The Job allows at most 32 active processes. Mutating lifecycle
operations share one exclusive installation lock, including directory Apply;
`host-status` reads local state without waiting for that lock or Docker.

The default is a background process in the current user's login session, with
that user's Docker context and interactive folder picker. Startup starts it;
`stop` ends the owned process tree and stops the current Compose project while
retaining data. `host-stop` ends only the native background tasks. It is not an
SCM/LocalSystem service, a login auto-start registration, or a daemon surviving
logout. No service registration or account/password storage is performed.

Ports saved in the app are checked and applied by the port task. Recreation must
pass a bounded health wait; ordinary failure restores the previous environment
and recreates the previous configuration. The update task supports existing A/B
commands, fixed-member package extraction, SHA256 and runtime/platform/source
validation, target identity/health checks, previous-slot recovery, and durable
result/canonical-ID acknowledgement retries. Interrupted transactions restore the
old marker and recover the previous image before new work. The controller holds
the legacy updater lock and imports any previous pending result; an active legacy
updater must be stopped before switching host implementations.

Migration adopts only a unique same-root container/project. Named-volume or bind
sources are mounted read-only, active/unreadable sessions prevent migration, and
the existing atomic data preparer commits a receipt before activation. Source
volumes/containers are retained. A new root cannot infer ownership of orphaned
global volumes; use an explicit `--project NAME` only for the intended legacy
project. Foreign or ambiguous container ownership refuses lifecycle operations.
Data preparation and update ZIP extraction run Python **inside a verified Linux
container**, never on the Windows host. Temporary Linux containers have random
operation names plus root/operation labels; only matching owned containers are
cleaned after interruption. A Windows process exit alone does not prove that a
Docker container stopped.

State lives under `.aide-data/native`, with current-user SID DACLs, atomic writes,
reparse checks, generation/PID/creation-time identity and bounded/redacted logs.
`host-status` reports worker phase, `stopRequired`, a redacted reason and the
`recover-stop` command. A full `stop` automatically recovers recognized stale
assigned-Job termination markers; `recover-stop --root "C:\path to\aide"` runs the
same explicit stop and fails if the marker cannot be safely recovered. It checks
exclusive lifecycle/controller/legacy-agent ownership, exact PID creation-time
lifetimes (including workers from older EXEs), absence of update transactions,
confirmed helper stop, every owned project container's exited state, and absence
of temporary containers. Directory timeout markers additionally require the same
stopped controller generation. Successful recovery archives the original marker
as `host-stop-required.recovered-<pid>-<tick>` alongside a durable JSON audit; it
does not delete data, volumes, image markers or update journals. Start can then
be retried. Unknown/unattached-child or legacy unsafe-update markers remain
blocked for inspection. Docker unavailable, unreadable process state, active
workers/containers, or failed audit/archive writes also retain the blocker.

Atomic state replacement retries transient Windows sharing/lock/access failures
for at most two seconds, then reports the numeric Windows error while retaining
the previous file. Process cleanup verifies retained process handles and empty
Job accounting even when termination races with natural exit; failed termination
alone neither proves failure nor proves safety. Preserve `.env`, `.aide-data`,
storage receipts and original data when troubleshooting. Stop all native tasks before
replacing a running launcher EXE; application A/B updates change the image, not
the host executable.

## Build and verify

Build prerequisites are Python 3.11+, an **existing Go 1.26+ compiler** (PATH,
`--go`, or `AIDE_NATIVE_GO`), and a local Linux Docker image with Python 3.12+.
The script builds current Windows Go components, collects their licenses, and
cross-compiles C++17 with the SHA256-pinned Zig 0.15.2 toolchain in a networkless
Docker container. It does not install a compiler on the host. Its dedicated
`aide-native-launcher-toolchain-v0152` volume caches the compiler.

```text
python scripts/build-windows-launcher.py --image aide:local
python scripts/build-windows-launcher.py --image aide:local --arch arm64
python scripts/test_windows_native_host.py
python scripts/test_windows_native_package.py
python scripts/test_windows_native_portable.py
```

Outputs are ignored build artifacts: `aide-launcher.exe` for AMD64 and
`bin/aide-launcher-arm64.exe` for ARM64. `--licenses` prints the embedded JSON/miniz
MIT notices and Go dependency notices without starting Aide. The gzip/tar reader
extracts only the fixed regular image member with bounded memory and CRC/header
checks; update ZIP extraction also refuses duplicate, encrypted and special
members. Pinned published digest lookups may resolve a canonical config ID only
after platform/source/runtime verification; local offline markers remain strict.

On **real Windows**, with a disposable local runtime image, run:

```text
aide-launcher.exe --self-test
python scripts/test_windows_native_launcher.py --image aide:local
go test ./cmd/aide-local-mounts
```

The harness owns an isolated Unicode directory/project and tests bootstrap,
occupied ports, repeated start, foreign-root refusal, resident-task identity,
nonblocking status, stop/restart and state/token hygiene. Cross-compilation,
portable transaction fault tests and Linux Go tests do **not** establish Win32
Job/NTFS ACL behavior, Docker Desktop, folder dialogs, actual legacy migration,
real A/B updates, ARM64 execution or macOS behavior. These require platform
acceptance; no claim that every Windows computer is supported follows from a
successful build.

The root BAT shortcuts use `scripts/windows-entrypoint.bat` to display results and
retain an Explorer-style CMD window until acknowledgement. Set
`AIDE_LAUNCHER_NO_PAUSE=1` for unattended automation; exit codes are preserved.
Retired PowerShell is archived under `scripts/fixtures/legacy-windows/` for tests
only and is excluded from runtime packages. The Windows SDK test now executes
the actual BAT shortcuts, not only direct EXE dry runs (Issue #646).


## Windows workbench window

The current source host opens an installation-scoped WebView2 desktop window,
with app tabs for local plugin pages and Windows Hello via the system WebAuthn
API. It no longer opens the default browser for the main workbench. The WebView2
Runtime must already be installed. The SHA256-pinned SDK/Loader is a build input;
normal start never downloads it. Account passwords remain in the existing server
verification flow, never native storage. See [desktop behavior and limits](../../docs/architecture/windows-desktop.md)
and [task acceptance](../../docs/tasks/windows-desktop-20261103.json).

The `Windows Desktop Check` CI also builds `--stop-test-output` as a separate,
unshipped fault fixture. `scripts/ci/windows_stop_recovery.py` exercises actual
Windows sharing locks and Jobs, plus the production CLI against a synthetic
Docker executable in a disposable Unicode root. These tests do not establish
physical Docker Desktop startup or ARM64 execution.
