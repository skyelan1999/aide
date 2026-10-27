# Windows native launcher

Double-click `aide-launcher.exe` beside the existing installation. `start.bat`
and `stop.bat` require it; PowerShell entrypoints have been removed. The executable calls Win32 and Docker
directly: no PowerShell, cmd command strings, host Python, .NET or separately
installed C++ runtime is needed. Docker Desktop and its Compose plugin remain
required. C++17 sources and the pinned MIT JSON dependency are in this directory.

```text
aide-launcher.exe start
aide-launcher.exe stop
aide-launcher.exe status
aide-launcher.exe open
aide-launcher.exe clean-cache --dry-run
aide-launcher.exe --root "C:\path with spaces\aide" --no-browser
aide-launcher.exe --force-build
```

The default root is the executable's directory, independent of the current
terminal directory. Source checkouts retain their existing Compose file and
`.aide-data`. In an empty directory the single executable supplies the embedded
offline Compose template and atomic data preparer; a validated local image or
complete matching offline Release archive is still required. The data preparer
runs with Python **inside an isolated Linux container**, never on the host.

The native path preserves private SID-based data ACLs, saved environment/project
identity, Unicode paths, platform/source/runtime checks, local-first startup,
SHA256-verified archive import, explicit source builds, port collision handling,
directory/LAN helper checksum checks, health checks and optional browser opening.
Logs contain the address without a login token. Child processes inherit only
their own standard handles, so persistent helpers cannot keep the launcher or
its caller waiting on output pipes. Stop affects only the current project and
retains all data. Application model configuration is never truncated to fit an
older image.

Native legacy-container/volume migration is deliberately refused before data is
changed; use the existing verified migration workflow first. Native background
port monitoring and in-app A/B update-agent parity are not included in this
delivery. Change the port in `.env` and launch again. Windows background A/B update
activation is not supported in this delivery. No extra persistent
monitor is installed by the native launcher. Existing host helpers are part of
the application and are started/stopped with it.

## Build in Docker

```text
python scripts/build-windows-launcher.py --image aide:local
python scripts/build-windows-launcher.py --image aide:local --arch arm64
```

Build orchestration requires Python 3.11+ and a local x86_64 Linux Docker image
with Python 3.12+. The pinned official Zig 0.15.2 Linux toolchain is SHA256
verified, extracted and executed in Docker, with compiler/cache storage in the
dedicated `aide-native-launcher-toolchain-v0152` volume. The compiler container
has no network and never pulls an image. There is no host compiler installation.
The AMD64 executable is written to `aide-launcher.exe`; ARM64 is written to
`bin/aide-launcher-arm64.exe`. Binaries are build artifacts, not Git sources.

## Verification

```text
aide-launcher.exe --self-test
python scripts/test_windows_native_launcher.py
```

The integration test needs real Windows and Docker Desktop, uses an isolated
Unicode path and project, exercises native bootstrap, occupied-port selection,
repeat start, status and stop, then removes only its own Compose resources.
Browser interaction, real legacy data migration, archive download/import and
ARM64 execution need separate acceptance evidence.

Automatic sign-in reads the private token without a shell, URL-encodes it and passes it only to the browser. If the system HTTPS association is missing, an installed Opera/Edge/Chrome/Firefox executable is located without changing default-browser settings. Console/log output reports token attachment without revealing the credential.
