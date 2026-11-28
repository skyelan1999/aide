#!/usr/bin/env bash
# Real isolated Docker/BuildKit; the install command is a fixture, not a real index.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
tmp=$(mktemp -d)
tag="aide-private-pip-fixture:$(basename "$tmp" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9')"
cleanup() {
  docker image rm "$tag" >/dev/null 2>&1 || true
  rm -rf -- "$tmp"
}
trap cleanup EXIT
mkdir "$tmp/context"
umask 077
marker="aide-pip-fixture-$(python3 -c 'import secrets; print(secrets.token_hex(16))')"
printf 'https://fixture:%s@private.invalid/simple/\n' "$marker" >"$tmp/pip-index"
cp "$root/scripts/pip-install-retry.sh" "$tmp/context/"
cat >"$tmp/context/Dockerfile" <<'DOCKER'
FROM python:3.12-slim-bookworm@sha256:392307d22300de8b5986851a12d9176dfc0fc073e65bf6523ebd7dcbeb23564e
COPY pip-install-retry.sh /fixture/
ENV AIDE_PRIVATE_PIP=1
RUN --mount=type=secret,id=aide_pip_index sh /fixture/pip-install-retry.sh sh -ec 'test "$PIP_INDEX_URL" = "$(cat /run/secrets/aide_pip_index)"; test "$PIP_CONFIG_FILE" = /dev/null; test "$PIP_NO_CACHE_DIR" = 1; test -z "$PIP_EXTRA_INDEX_URL"; printf "%s\n" "$PIP_INDEX_URL" >&2; touch /fixture/fixture-passed'
RUN test -f /fixture/fixture-passed && test ! -e /run/secrets/aide_pip_index
DOCKER
build=(docker build --network=none --no-cache --progress=plain)
if ! DOCKER_BUILDKIT=1 "${build[@]}" --secret "id=aide_pip_index,src=$tmp/pip-index" -t "$tag" "$tmp/context" >"$tmp/build.log" 2>&1; then
  echo 'BuildKit secret fixture build failed; raw output withheld.' >&2
  exit 1
fi
if DOCKER_BUILDKIT=1 "${build[@]}" "$tmp/context" >"$tmp/missing.log" 2>&1; then
  echo 'Build unexpectedly succeeded without the required secret.' >&2
  exit 1
fi
grep -Fq 'Authenticated Python index secret is missing or empty.' "$tmp/missing.log"
docker history --no-trunc "$tag" >"$tmp/history"
docker image inspect "$tag" >"$tmp/inspect"
docker image save -o "$tmp/image.tar" "$tag"
python3 - "$tmp" "$marker" <<'PY'
import json, pathlib, sys, tarfile
root = pathlib.Path(sys.argv[1])
marker = sys.argv[2].encode()
for name in ("build.log", "missing.log", "history", "inspect"):
    if marker in (root/name).read_bytes():
        raise SystemExit(f"Synthetic credential leaked into {name}")
with tarfile.open(root/"image.tar") as saved:
    images = json.load(saved.extractfile("manifest.json"))
    if len(images) != 1 or not images[0]["Layers"]:
        raise SystemExit("Did not receive a complete Docker image export")
    image = images[0]
    if marker in saved.extractfile(image["Config"]).read():
        raise SystemExit("Synthetic credential leaked into the exported image config")
    for layer in image["Layers"]:
        with tarfile.open(fileobj=saved.extractfile(layer), mode="r|*") as archive:
            for member in archive:
                if member.isfile():
                    reader = archive.extractfile(member)
                    tail = b""
                    while chunk := reader.read(1024 * 1024):
                        window = tail + chunk
                        if marker in window:
                            raise SystemExit("Synthetic credential leaked into an exported layer")
                        tail = window[-len(marker)+1:]
print("PASS: real BuildKit secret mount, missing-secret failure, build log, history, config and image layers")
PY
