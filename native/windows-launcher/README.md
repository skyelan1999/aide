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

First Git `build.bat` can bootstrap a missing EXE entirely inside Docker. The
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
`host-status` reports worker phase and `stopRequired`. Unknown child termination
or legacy unsafe-update markers fail closed: inspect the owned process/container
state and transaction before removing the blocker; restart does not erase it.
A directory-command timeout marker is cleared only after a full stop confirms the
same controller generation and application have stopped. Docker unavailable or
unconfirmed termination keeps the blocker. Preserve `.env`, `.aide-data`, storage
receipts and original data when troubleshooting. Stop all native tasks before
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
