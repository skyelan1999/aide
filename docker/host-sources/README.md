# Pinned host source staging

The source launcher stages the archive named `<sha256>.tar.gz` here after
verifying `docker/codex-host-source.json`. Archives are generated, ignored by
Git, and copied into Docker build context for offline component compilation.
They are never trusted without checking the lock checksum again.

For direct Docker or native Windows source builds, prepare this directory on a
network-connected machine with `python3 scripts/build-codex-host.py --prepare-only`,
or place the exact pinned source archive here under its SHA-256 filename.
Without a staged archive, Docker falls back to its verified source cache and a
bounded download attempt. Existing runtime/release packages do not download source.
