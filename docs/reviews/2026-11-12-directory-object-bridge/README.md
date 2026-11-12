# Directory object bridge - 2026-11-12

Addresses Aide #773, preserving selected-directory functionality as explicitly requested by the user. Baseline `ccec15d2bc015f41b492ebe39eef6a9830a16206`. No production service, installed helper or binary Release was replaced.

Native preparation pins each selected directory with `os.Root`. A loopback-only capability-addressed NFSv3 service exposes these objects through Docker local volumes. Aide and plugins keep `/mounts/<id>` paths; there is no selected-path bind fallback. An identity ledger rejects replacement across preparations until the directory is deliberately removed and selected again. Immutable helper copies preserve native process lifetime, including the Windows resident-host job. Retired workers stop only after Docker confirms their owned volumes are unused.

## Verification

- Go race tests passed for `internal/hostbridge`, `internal/localmounts` and `cmd/aide-local-mounts`: directory/parent replacement, RO and escape rejection, malformed/oversized RPC handling, revocation, preparation rollback and helper lifecycle. The pinned vendor overlay is checked separately. Existing vendor patches were preserved.
- The final native macOS ARM64 helper was rebuilt, then [isolated acceptance](acceptance.json) replaced selected-directory and parent paths before Compose started a real Aide application. Its actual file API read the original directory. A 3 MiB write, sync and rename reached the original directory with matching SHA-256; both outside directories stayed unchanged. A client requesting RW against an RO export was refused. A fresh preparation rejected the changed identity and preserved active metadata. The test cleaned its disposable Compose volumes and workers.
- Paired Windows AMD64/ARM64 launcher/helper cross-builds are recorded in [build receipt](builds.json). These are compile checks, not physical Windows acceptance.

## Limits and operations

The directory must remain accessible to the native user. Rooted access adds no permissions or browser authority. No LAN listener or control-plane export API is opened. Random exports and opaque handles are capabilities; private Compose/bridge receipts contain them. `active.json` contains no export token and is container-readable through its existing metadata mount.

NFSv3 has bounded parser/message/read/file/connection budgets and soft timeouts. Stable writes synchronize before acknowledgment; transport failure, suspension or process death can yield I/O errors. Native-host concurrent database writes are not coordinated (`nolock`); keep them stopped. Special files and foreign ownership changes are refused. This is not complete NFS certification. Protected macOS TCC directories, physical Windows filesystems, network drives, USB unplug/replug and production workloads were not exercised. Existing global mounts are outside this selected-directory change.

Rollback: preserve `.aide-data/local-mounts` configuration/identity receipts and user directories. An old helper restores the old pathname-bind race and is not an equivalent scope guarantee. Stop/reconfigure the owned installation normally before replacing its paired helper. No user data migration or deletion is required.

## Final integration

Merged source and CI identities and all 18 retained dispositions are recorded in [completion receipt](completion.json). Windows CI additionally passed isolated native UI and stop-recovery tests; this does not establish physical Windows selected-directory bridge acceptance.
