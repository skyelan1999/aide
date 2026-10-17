#!/usr/bin/env bash
# Sourced by aide.sh. Manifest is data, never executable shell configuration.
# A normal second launch opens the healthy instance belonging to this Compose
# project. It must not depend on a Release download or switch its running image.
launcher_running_ready() {
  local docker_bin="$1" instance health
  instance="$("$docker_bin" compose ps --status running --quiet aide 2>/dev/null)" || return 1
  [[ "$instance" =~ ^[a-f0-9]{12,64}$ ]] || return 1
  health="$("$docker_bin" compose exec -T aide curl -fsSk --connect-timeout 2 --max-time 3 https://127.0.0.1:8080/healthz 2>/dev/null)" || return 1
  [[ "$health" == *'"service":"aide"'* && "$health" == *'"status":"ok"'* ]]
}

# Reuse only a complete, platform-matching runtime already installed locally.
# This is startup, not an implicit software update or a source-build fallback.
launcher_local_runtime_ready() {
  local docker_bin="$1" target="$2" platform metadata image actual src cad extra
  platform="$("$docker_bin" info --format '{{.OSType}}/{{.Architecture}}' | sed 's|/aarch64$|/arm64|;s|/x86_64$|/amd64|')" || return 1
  metadata="$("$docker_bin" image inspect "$target" --format '{{.Id}}|{{.Os}}/{{.Architecture}}|{{index .Config.Labels "aide.srcsha"}}|{{index .Config.Labels "aide.cad-runtime"}}' 2>/dev/null)" || return 1
  IFS='|' read -r image actual src cad extra <<< "$metadata"
  [[ "$image" =~ ^sha256:[a-f0-9]{64}$ && "$actual" == "$platform" && "$src" =~ ^[a-f0-9]{64}$ && "$cad" == 8.0.1.1.0 && -z "$extra" ]]
}

# Git checkouts are development installations; bundles keep installed-image mode.
# An explicit 0 or 1 overrides automatic detection.
launcher_should_build_source() {
  case "${1:-auto}" in
    1) return 0 ;;
    auto) [[ -d "$2/.git" || -f "$2/.git" ]] ;;
    *) return 1 ;;
  esac
}

launcher_prebuilt_image() {
  local docker_bin="$1" target="$2" manifest="${3:-docker/prebuilt-images.tsv}"
  local platform row image src digest url kind extra archive actual current rollback
  [[ "$target" =~ ^[a-zA-Z0-9][a-zA-Z0-9._/:@-]*$ ]] || { echo 'Invalid prebuilt target image.' >&2; return 1; }
  platform="$("$docker_bin" info --format '{{.OSType}}/{{.Architecture}}' | sed 's|/aarch64$|/arm64|;s|/x86_64$|/amd64|')" || return
  [[ -f "$manifest" ]] || { echo 'Prebuilt manifest missing; download a complete Release package. Source build requires AIDE_BUILD_FROM_SOURCE=1.' >&2; return 1; }
  row="$(awk -v p="$platform" '$1==p{print; n++} END{if(n!=1)exit 1}' "$manifest")" || { echo "No unique prebuilt image for $platform; use a matching Release package. Automatic source build is disabled." >&2; return 1; }
  read -r platform image src digest url kind extra <<< "$row"
  [[ "$image" =~ ^sha256:[a-f0-9]{64}$ && "$src" =~ ^[a-f0-9]{64}$ && "$digest" =~ ^[a-f0-9]{64}$ && -z "$extra" ]] || { echo 'Invalid prebuilt manifest identity.' >&2; return 1; }
  [[ "$url" =~ ^https://github\.com/skyelan1999/aide/releases/download/[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+\.tar\.gz$ && "$kind" == bundle ]] || { echo 'Invalid prebuilt archive URL/type.' >&2; return 1; }
  current="$("$docker_bin" image inspect "$image" --format '{{.Id}}' 2>/dev/null || true)"
  if [[ "$current" != "$image" ]]; then
    [[ ! -L docker-images ]] || { echo 'Prebuilt cache must not be a symbolic link.' >&2; return 1; }
    mkdir -p docker-images
    archive="docker-images/${url##*/}"
    [[ ! -L "$archive" && ! -L "$archive.partial" ]] || { echo 'Prebuilt archive must not be a symbolic link.' >&2; return 1; }
    if [[ ! -f "$archive" ]]; then
      command -v curl >/dev/null || { echo 'curl required to download the prebuilt image.' >&2; return 1; }
      echo "Downloading complete prebuilt runtime ($platform), no compilation: $url" >&2
      curl --fail --location --retry 3 --connect-timeout 30 --continue-at - --output "$archive.partial" "$url" || return
    else
      echo "Using local prebuilt archive: $archive" >&2
    fi
    local candidate="$archive"
    [[ -f "$candidate" ]] || candidate="$archive.partial"
    if command -v shasum >/dev/null; then actual="$(shasum -a 256 "$candidate" | awk '{print $1}')";
    elif command -v sha256sum >/dev/null; then actual="$(sha256sum "$candidate" | awk '{print $1}')";
    else echo 'SHA256 tool missing; import refused.' >&2; return 1; fi
    [[ "$actual" == "$digest" ]] || { echo 'Prebuilt archive checksum mismatch; import refused. Retain the file for inspection or replace it with a verified download.' >&2; return 1; }
    [[ "$candidate" == "$archive" ]] || mv "$candidate" "$archive"
    # pipefail protects against failed extraction; no large uncompressed copy.
    (set -o pipefail; tar -xOzf "$archive" aide-offline/docker-images/aide-local.tar | "$docker_bin" image load) || return
  fi
  [[ "$("$docker_bin" image inspect "$image" --format '{{.Id}}|{{.Os}}/{{.Architecture}}|{{index .Config.Labels "aide.srcsha"}}|{{index .Config.Labels "aide.cad-runtime"}}')" == "$image|$platform|$src|8.0.1.1.0" ]] || { echo 'Imported runtime identity/platform/source/CAD mismatch; activation refused.' >&2; return 1; }
  current="$("$docker_bin" image inspect "$target" --format '{{.Id}}' 2>/dev/null || true)"
  if [[ -n "$current" && "$current" != "$image" ]]; then
    rollback="aide:rollback-$(date +%Y%m%d%H%M%S)-${current#sha256:}"
    "$docker_bin" image tag "$current" "$rollback" || return
    echo "Previous image retained: $rollback" >&2
  fi
  "$docker_bin" image tag "$image" "$target" || return
  echo "Prebuilt runtime ready: $target ($platform). Source compilation disabled." >&2
}
