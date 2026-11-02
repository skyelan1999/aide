# Codex host source cache — issue 743 acceptance

Implementation commit: `92e95b842981cce06c8641d39a6330eabbd1d520`.

The reported failure was a refused connection while downloading pinned Codex Bridge source inside the Docker build. The same URL returned HTTP 200 from the host. The launcher now prepares a SHA-256-verified archive before Docker; unchanged host components build in their own stage. Source pin and integrity requirements remain unchanged.

## Actual checks

- `python3 scripts/test_codex_host_source.py`: 8 passed, including offline reuse, transient retry, permanent errors, corruption, size limits and symlinks.
- `python3 scripts/test_launcher_contract.py`: 25 passed, including preparation order and failure preserving startup configuration.
- `python3 scripts/agent-route.py verify quick`: PASS at source commit above; receipt fingerprint `73c7bf84d7d8555e3c4d5ed939dda6958b0cbd81e480f9d111e0c398dd4f8792`.
- `python3 scripts/check_docs.py`: PASS.
- Real `codex-host` stage built with `--network none`: all six Darwin/Linux/Windows amd64/arm64 binaries produced and `SHA256SUMS` verified; component version 0.4.0. Registry metadata resolution still used the Docker registry, so this is not a fully air-gapped cold build claim.
- Temporary application-file comment probe: host-stage RUN remained CACHED; original bytes restored afterward.
- Full canonical launcher source build completed with normal image-identity validation. Only configuration writing and `start-built` were intercepted for build-only acceptance. Go vet, application compilation and image export passed.
- Final runtime image component checksums: six passed. Isolated no-network startup with writable temporary data/workspace passed `/healthz` with integrity/status `ok`; test container removed afterward.

Candidate: `aide:source-cache-743-92e95b8`, Linux arm64.
Image ID: `sha256:105987563a0be0d3de5259f050eb3cb18a61eca8b70296eadfa1a5233b61b538`.
Source label: `56f4560d122941fde1f8fda6aaff06952fe05ed0c5c128b4b1b137fc583c7ab9`.

The first smoke probe had an unwritable default tmpfs. After explicitly setting its mode, the deliberately low 256 MiB limit caused OOM during fresh startup; a 768 MiB bounded probe passed. These test conditions did not affect the user's container or data. Dockerfile.local has stage-structure coverage, but its complete image build and native Windows launcher execution were not run.

## Service and memory observations

No production startup, restart, remote push or release occurred. Existing container ID/image and .env checksum stayed unchanged. The existing Aide container had already exited at 14:39:41 UTC, before this build-only validation; it was not OOM-killed.

After the build, Docker VM host RSS was about 6.9 GiB. Linux reported about 4.1 GiB cached files, 6.8 GiB MemAvailable out of 7.75 GiB MemTotal; BuildKit reported about 335 MiB working memory with its 4 GiB container limit. These measurements distinguish reclaimable guest cache from container working memory; they do not establish long-term absence of a memory leak. macOS swap usage was about 2.9 GiB at observation time. No global Docker memory, proxy or TLS settings were changed.

Raw logs and image/snapshot identities are retained under ignored `.agent-state/host-source-cache/` and `.agent-state/host-source-cache-743/` in the task worktree and installation checkout.
