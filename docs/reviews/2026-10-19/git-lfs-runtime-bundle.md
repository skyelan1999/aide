# Complete runtime Git LFS delivery

Task: [git-lfs-runtime-bundle-20261019](../../tasks/git-lfs-runtime-bundle-20261019.json).

The user requested all required precompiled Docker environments in one Git-tracked bundle, only the current version, simple checks and timely push. This distribution adds no application behavior or production deployment. The previous Release upload task and its branch remain separate.

- Source baseline: main `e182e063`; runtime snapshot `f316cf8d6f4ba4243a05007f6e8ebf6f586cbd4c` / source SHA `2239861e3902cced4bdb9a772b489ee3a95340b7fb1bd70646c60ce3979d282e`.
- The existing same-source pair is the newest available precompiled combination, not a rebuild of baseline main. This limit is stated in [user instructions](../../runtime-bundle.md).
- Fixed bundle: `docker-images/aide-runtime-all-platforms.zip`, 1725320689 bytes.
- SHA256: `ecefbfc3d3d7c71d2d9e3390c30fcb51c4bf1fcbfbc960a5831d390a970448fb`.
- Git stores an LFS pointer; Git LFS stores the object. Replacing the fixed path does not rewrite remote history or purge historic LFS objects.

## Actual scoped checks

- Builder fully hashed both selected archives against `docker/runtime-bundle.json`, required one ARM64 and one AMD64 archive, and required their common source identity.
- ZIP has exactly five entries: README, identity manifest, SHA256SUMS, and both complete archives. CRC check passed for every entry.
- Inner archives were streamed from the ZIP, fully hashed, and each had 59 tar members. Neither tar had absolute/traversal paths or private runtime data files. Embedded image markers match the platform and exact image IDs.
- Git pointer OID and size match the outer ZIP; `git lfs fsck --objects HEAD` passed.
- ARM native isolated Docker smoke: existing binary executable, OCP box geometry non-null; network disabled, 512 MiB/one CPU, `--rm`. First direct Python invocation lacked the runtime's configured OCP search path and failed to import. Repeating with the documented `PYTHONPATH=/opt/aide-cad` and `LD_LIBRARY_PATH=/opt/aide-cad/system` passed. The candidate was not restarted as the production service.
- `python3 -m py_compile scripts/build-runtime-bundle.py`, `python3 scripts/check_docs.py`, `git diff --cached --check` passed.
- Full application regression and native AMD64/Windows execution were not run: user requested simple distribution checks and prompt push; no application code changed. Receiving-device startup remains unverified.

## Transfer and cleanup

Git LFS upload succeeded (one object, 1725320689 bytes). Initial implicit network routing failed; explicit use of the existing local HTTP proxy succeeded without changing system proxy settings. Authenticated download-batch metadata returned the exact object OID and size. Three remote 1 MiB ranges (first, middle and last) each matched the local file byte-for-byte; this is sampled remote verification, not a full remote download. See [sanitized proof](git-lfs-runtime-remote-proof.json). No new GitHub Release asset state is claimed. The obsolete local archive cleanup list is held locally in `.agent-state/git-lfs-runtime-cleanup-plan.json`; it targets only root-level historical AIDE versioned tar archives and their checksum sidecars, retaining the current pair and dependency seeds. Production images, volumes, sessions and workspace data are outside this cleanup.

Local cleanup completed: 138 old archive/sidecar files, 91263986080 logical bytes. The current two component archives and aggregate ZIP remain; the old unversioned September aide-local export was removed. Dependency seeds and all runtime data remain. Removed names and sizes are retained locally in the cleanup receipt, not used as rollback claims.
