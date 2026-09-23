#!/usr/bin/env bash
# First-run setup. Never installs host packages or overwrites an existing .env.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
MODE="${1:---help}"
case "$MODE" in
  --check|--source) [[ $# -le 1 ]] || { echo 'Unexpected argument' >&2; exit 2; } ;;
  --image) [[ $# -eq 2 ]] || { echo 'Usage: bash scripts/install.sh --image /path/image.tar.gz' >&2; exit 2; } ;;
  *) cat <<'HELP'
aide first-run installer
  bash scripts/install.sh --check             Check prerequisites only
  bash scripts/install.sh --source            Configure, build and start from source
  bash scripts/install.sh --image ARCHIVE     Verify, load and start a Release image
Requires Docker Engine/Desktop and Compose v2. Source builds also require Git.
The archive directory must contain SHA256SUMS from the same GitHub Release.
Existing .env files and data volumes are preserved. Host software is never installed silently.
HELP
     [[ "$MODE" == --help ]] && exit 0 || exit 2 ;;
esac
if ! command -v docker >/dev/null; then
  if [[ -x "$HOME/.docker/bin/docker" ]]; then export PATH="$HOME/.docker/bin:$PATH";
  elif [[ -x /Applications/Docker.app/Contents/Resources/bin/docker ]]; then export PATH="/Applications/Docker.app/Contents/Resources/bin:$PATH";
  else echo 'Install Docker Desktop (macOS/Windows) or Docker Engine + Compose v2 (Linux), then rerun. See docs/installation.md.' >&2; exit 1; fi
fi
docker info >/dev/null 2>&1 || { echo 'Docker engine is not ready. Start Docker Desktop / the Docker daemon, then rerun.' >&2; exit 1; }
docker compose version >/dev/null || { echo 'Docker Compose v2 is required.' >&2; exit 1; }
if [[ "$MODE" == --source ]]; then command -v git >/dev/null || { echo 'Install Git first.' >&2; exit 1; }; fi
if [[ "$MODE" == --check ]]; then echo 'PASS: Docker engine and Compose are ready. Source builds additionally need Git.'; exit 0; fi
if [[ "$MODE" == --image ]]; then
  ARCHIVE="$2"
  [[ -f "$ARCHIVE" ]] || { echo 'Image archive not found.' >&2; exit 1; }
  command -v python3 >/dev/null || { echo 'Python 3 is required for archive checksum validation. See docs/installation.md.' >&2; exit 1; }
  python3 - "$ARCHIVE" <<'PY'
import hashlib, pathlib, sys
p=pathlib.Path(sys.argv[1]); manifest=p.parent/'SHA256SUMS'
if not manifest.is_file(): sys.exit('Missing SHA256SUMS beside image archive')
rows=[x.split(maxsplit=1) for x in manifest.read_text().splitlines() if x.strip()]
expected=[h for h,n in rows if n.lstrip('*')==p.name]
if len(expected)!=1: sys.exit('Archive must have exactly one checksum entry')
h=hashlib.sha256()
with p.open('rb') as f:
    for b in iter(lambda:f.read(1024*1024),b''): h.update(b)
if h.hexdigest()!=expected[0]: sys.exit('Checksum mismatch; image was not loaded')
print('PASS: image SHA256')
PY
  docker image load -i "$ARCHIVE"
  RELEASE_IMAGE="aide:$(bash scripts/version.sh show | sed 's/ /-/')"
  IMAGE_ARCH="$(docker image inspect "$RELEASE_IMAGE" --format '{{.Architecture}}')"
  HOST_ARCH="$(docker info --format '{{.Architecture}}')"
  case "$HOST_ARCH" in aarch64) HOST_ARCH=arm64 ;; x86_64) HOST_ARCH=amd64 ;; esac
  [[ "$IMAGE_ARCH" == "$HOST_ARCH" ]] || { echo "Image architecture $IMAGE_ARCH differs from engine $HOST_ARCH. Build with --source." >&2; exit 1; }
fi
mkdir -p context
if [[ ! -e .env ]]; then
  cp .env.example .env
  # First-run scope is this repository, not the entire host HOME.
  # Portable sed via a temporary file; only newly-created configuration is changed.
  sed -e 's|^AIDE_CONTEXT=.*|AIDE_CONTEXT=./context|' -e '/^AIDE_LOCAL_ROOT=/d' .env > .env.install-tmp
  printf 'AIDE_LOCAL_ROOT="%s"\n' "$ROOT" >> .env.install-tmp
  if [[ "$MODE" == --image ]]; then
    sed "s|^AIDE_IMAGE=.*|AIDE_IMAGE=$RELEASE_IMAGE|" .env.install-tmp > .env
    rm .env.install-tmp
  else
    mv .env.install-tmp .env
  fi
  echo 'Created .env with local workspace and context/. Edit it to select your project directories.'
else
  echo 'Keeping existing .env. Existing mounts and model settings are not overwritten.'
fi
if [[ "$MODE" == --image ]]; then
  export AIDE_IMAGE="$RELEASE_IMAGE"
  echo "Starting $AIDE_IMAGE. For subsequent start-image calls set AIDE_IMAGE=$AIDE_IMAGE in .env."
  bash scripts/aide.sh start-image
else
  bash scripts/aide.sh start
fi
