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

# A slow cold start is still an existing runtime. Ordinary desktop reopen must
# not replace its image/settings merely because the HTTP server is not ready.
launcher_running_owned_instance() {
  local docker_bin="$1" instance identity
  instance="$("$docker_bin" compose ps --status running --quiet aide 2>/dev/null)" || return 1
  [[ "$instance" =~ ^[a-f0-9]{12,64}$ ]] || return 1
  identity="$("$docker_bin" inspect --format '{{.State.Running}}|{{index .Config.Labels "com.docker.compose.project.working_dir"}}|{{index .Config.Labels "com.docker.compose.service"}}' "$instance" 2>/dev/null)" || return 1
  [[ "$identity" == "true|$PROJECT_DIR|aide" ]]
}

# One deadline covers container initialization and host TLS/instance checks.
# Docker's temporary "unhealthy" during a cold start is not a process exit.
launcher_startup_begin() {
  local budget="${AIDE_STARTUP_TIMEOUT:-600}"
  if [[ ! "$budget" =~ ^[0-9]{1,4}$ ]] || (( 10#$budget < 1 || 10#$budget > 1800 )); then
    echo 'AIDE_STARTUP_TIMEOUT must be an integer from 1 to 1800 seconds.' >&2
    return 1
  fi
  LAUNCHER_STARTUP_STARTED=$SECONDS
  LAUNCHER_STARTUP_BUDGET=$((10#$budget))
  LAUNCHER_STARTUP_DEADLINE=$((SECONDS + LAUNCHER_STARTUP_BUDGET))
  LAUNCHER_STARTUP_PROGRESS=$SECONDS
}

launcher_startup_poll() {
  local phase="$1" elapsed remaining
  elapsed=$((SECONDS - LAUNCHER_STARTUP_STARTED))
  remaining=$((LAUNCHER_STARTUP_DEADLINE - SECONDS))
  if (( remaining <= 0 )); then
    echo "AIDE: 等待${phase}超时（${LAUNCHER_STARTUP_BUDGET} 秒）；实例和数据已保留，可检查启动日志。" >&2
    return 1
  fi
  if (( SECONDS >= LAUNCHER_STARTUP_PROGRESS )); then
    echo "AIDE: 正在等待${phase}（${elapsed}/${LAUNCHER_STARTUP_BUDGET} 秒），就绪后将自动继续…" >&2
    LAUNCHER_STARTUP_PROGRESS=$((SECONDS + 15))
  fi
  LAUNCHER_PROBE_TIMEOUT=3
  if (( remaining < LAUNCHER_PROBE_TIMEOUT )); then LAUNCHER_PROBE_TIMEOUT=$remaining; fi
}

launcher_health_instance() {
  local health="$1"
  [[ "$health" =~ \"service\"[[:space:]]*:[[:space:]]*\"aide\" ]] || return 1
  [[ "$health" =~ \"status\"[[:space:]]*:[[:space:]]*\"ok\" ]] || return 1
  printf '%s\n' "$health" | sed -n 's/.*"instanceId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p'
}

launcher_wait_container_ready() {
  local docker_bin="$1" instance identity health_id
  launcher_startup_begin || return 1
  while launcher_startup_poll 'Aide 服务初始化'; do
    instance="$("$docker_bin" compose ps --all --quiet aide 2>/dev/null)" || return 1
    [[ "$instance" =~ ^[a-f0-9]{12,64}$ ]] || {
      echo 'AIDE: 启动容器已不存在；请检查启动日志。' >&2; return 1;
    }
    identity="$("$docker_bin" inspect --format '{{.State.Status}}|{{index .Config.Labels "com.docker.compose.project.working_dir"}}|{{index .Config.Labels "com.docker.compose.service"}}' "$instance" 2>/dev/null)" || return 1
    if [[ "$identity" != "running|$PROJECT_DIR|aide" ]]; then
      echo 'AIDE: 启动容器已停止、正在重启或归属不匹配；请检查启动日志。' >&2
      return 1
    fi
    if CONTAINER_HEALTH="$("$docker_bin" compose exec -T aide curl --disable -fsSk --connect-timeout 1 --max-time "$LAUNCHER_PROBE_TIMEOUT" https://127.0.0.1:8080/healthz 2>/dev/null)"; then
      health_id="$(launcher_health_instance "$CONTAINER_HEALTH")" || health_id=''
      if [[ -n "$health_id" ]]; then return 0; fi
    fi
    sleep 1
  done
  return 1
}

launcher_wait_host_ready() {
  local port="$1" certificate="$2" expected="$3" health_id
  while launcher_startup_poll '宿主 HTTPS 连接'; do
    if HOST_HEALTH="$(curl --disable --silent --show-error --fail --proto '=https' --noproxy '*' --connect-timeout 1 --max-time "$LAUNCHER_PROBE_TIMEOUT" --cacert "$certificate" "https://localhost:$port/healthz" 2>/dev/null)"; then
      health_id="$(launcher_health_instance "$HOST_HEALTH")" || health_id=''
      if [[ "$health_id" == "$expected" && -n "$health_id" ]]; then return 0; fi
      echo 'AIDE: 宿主 HTTPS 响应与本安装的容器身份不匹配；浏览器尚未打开。' >&2
      return 1
    fi
    sleep 1
  done
  return 1
}

# Explicit A/B activation is the only healthy-instance reuse bypass. Ordinary
# start remains a reopen operation and must never turn a changed marker into an
# implicit image update.
launcher_may_reuse_running() {
  local mode="$1" start_built="$2" force_activation="$3"
  [[ "$start_built" != 1 && "$force_activation" != 1 && ( "$mode" == start || "$mode" == start-bundle || "$mode" == start-image ) ]]
}

# Reuse only a complete, platform-matching runtime already installed locally.
# This is startup, not an implicit software update or a source-build fallback.
launcher_local_runtime_ready() {
  local docker_bin="$1" target="$2" installation_kind="${3:-release}" platform metadata image actual src cad profile extra
  platform="$("$docker_bin" info --format '{{.OSType}}/{{.Architecture}}' | sed 's|/aarch64$|/arm64|;s|/x86_64$|/amd64|')" || return 1
  metadata="$("$docker_bin" image inspect "$target" --format '{{.Id}}|{{.Os}}/{{.Architecture}}|{{index .Config.Labels "aide.srcsha"}}|{{index .Config.Labels "aide.cad-runtime"}}|{{index .Config.Labels "aide.runtime-profile"}}' 2>/dev/null)" || return 1
  IFS='|' read -r image actual src cad profile extra <<< "$metadata"
  [[ "$image" =~ ^sha256:[a-f0-9]{64}$ && "$actual" == "$platform" && "$src" =~ ^[a-f0-9]{64}$ && -z "$extra" ]] || return 1
  if [[ "$profile" == source ]]; then
    [[ "$installation_kind" == source && ( -z "$cad" || "$cad" == '<no value>' ) ]]
  else
    [[ "$cad" == 8.0.1.1.0 || ( "$profile" == slim && ( -z "$cad" || "$cad" == '<no value>' ) ) ]]
  fi
}

# Only an explicit build request may select source compilation. Package identity
# is independent of old environment flags and a stale offline marker.
launcher_should_build_source() { [[ "${1:-auto}" == 1 ]]; }
launcher_is_source_package() { [[ -e "$1/.git" || ( -f "$1/go.mod" && -f "$1/Dockerfile" ) ]]; }
launcher_start_mode() {
  if [[ "$1" == start && -f "$3/.aide-image" ]] && ! launcher_is_source_package "$3"; then
    printf '%s\n' start-bundle
  else
    printf '%s\n' "$1"
  fi
}

launcher_prebuilt_image() {
  local docker_bin="$1" target="$2" manifest="${3:-docker/prebuilt-images.tsv}" purpose="${4:-runtime}"
  local platform row image src digest url kind extra archive actual current rollback
  [[ "$purpose" == runtime || "$purpose" == local-seed ]] || { echo 'Invalid prebuilt import purpose.' >&2; return 1; }
  [[ "$target" =~ ^[a-zA-Z0-9][a-zA-Z0-9._/:@-]*$ ]] || { echo 'Invalid prebuilt target image.' >&2; return 1; }
  platform="$("$docker_bin" info --format '{{.OSType}}/{{.Architecture}}' | sed 's|/aarch64$|/arm64|;s|/x86_64$|/amd64|')" || return 1
  [[ -f "$manifest" ]] || { echo 'Prebuilt manifest missing; download a complete Release package. Source build requires AIDE_BUILD_FROM_SOURCE=1.' >&2; return 1; }
  row="$(awk -v p="$platform" '$1==p{print; n++} END{if(n!=1)exit 1}' "$manifest")" || { echo "No unique prebuilt image for $platform; use a matching Release package. Automatic source build is disabled." >&2; return 1; }
  read -r platform image src digest url kind extra <<< "$row"
  [[ "$image" =~ ^sha256:[a-f0-9]{64}$ && "$src" =~ ^[a-f0-9]{64}$ && "$digest" =~ ^[a-f0-9]{64}$ && -z "$extra" ]] || { echo 'Invalid prebuilt manifest identity.' >&2; return 1; }
  [[ "$url" =~ ^https://github\.com/skyelan1999/aide/releases/download/[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+\.tar\.gz$ && "$kind" == bundle ]] || { echo 'Invalid prebuilt archive URL/type.' >&2; return 1; }
  current="$("$docker_bin" image inspect "$image" --format '{{.Id}}' 2>/dev/null || true)"
  if [[ "$current" != "$image" ]]; then
    [[ ! -L docker-images ]] || { echo 'Prebuilt cache must not be a symbolic link.' >&2; return 1; }
    archive="docker-images/${url##*/}"
    [[ ! -L "$archive" && ! -L "$archive.partial" ]] || { echo 'Prebuilt archive must not be a symbolic link.' >&2; return 1; }
    if [[ "$purpose" == local-seed && ! -f "$archive" ]]; then
      echo "No verified local dependency bundle: $archive. Retain/download the matching complete package before offline compilation." >&2
      return 3
    fi
    mkdir -p docker-images
    if [[ ! -f "$archive" ]]; then
      command -v curl >/dev/null || { echo 'curl required to download the prebuilt image.' >&2; return 1; }
      echo "Downloading complete prebuilt runtime ($platform), no compilation: $url" >&2
      curl --fail --location --retry 3 --connect-timeout 30 --continue-at - --output "$archive.partial" "$url" || return 1
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
    (set -o pipefail; tar -xOzf "$archive" aide-offline/docker-images/aide-local.tar | "$docker_bin" image load) || return 1
  fi
  [[ "$("$docker_bin" image inspect "$image" --format '{{.Id}}|{{.Os}}/{{.Architecture}}|{{index .Config.Labels "aide.srcsha"}}|{{index .Config.Labels "aide.cad-runtime"}}')" == "$image|$platform|$src|8.0.1.1.0" ]] || { echo 'Imported runtime identity/platform/source/CAD mismatch; activation refused.' >&2; return 1; }
  current="$("$docker_bin" image inspect "$target" --format '{{.Id}}' 2>/dev/null || true)"
  if [[ -n "$current" && "$current" != "$image" ]]; then
    rollback="aide:rollback-$(date +%Y%m%d%H%M%S)-${current#sha256:}"
    "$docker_bin" image tag "$current" "$rollback" || return 1
    echo "Previous image retained: $rollback" >&2
  fi
  "$docker_bin" image tag "$image" "$target" || return 1
  if [[ "$purpose" == local-seed ]]; then
    echo "Local dependency seed ready: $target ($platform). Continue compiling current source; installed application unchanged." >&2
  else
    echo "Prebuilt runtime ready: $target ($platform). Source compilation disabled." >&2
  fi
}

# Only the manifest-selected local archive/installed image is a dependency seed.
# Never choose by filename glob/mtime or replace the application's Compose tag.
launcher_local_build_seed() {
  local docker_bin="$1" target=aide:source-build-dependencies
  launcher_prebuilt_image "$docker_bin" "$target" docker/prebuilt-images.tsv local-seed >&2 || return $?
  printf '%s\n' "$target"
}

# Compatibility for callers of the old helper: a failed explicit source build
# remains a failure. It must never replace the installation with a Release.
launcher_build_or_release() {
  shift 2
  "$@"
}
