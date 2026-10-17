#!/usr/bin/env bash
# Read-only host/engine checks; never prune, delete data or change global settings.
aide_check_disk() {
  local phase="${1:-start}" minimum=10485760 path available
  [[ "$phase" != build ]] || minimum=20971520
  for path in / "$PROJECT_DIR"; do
    available="$(LC_ALL=C df -Pk "$path" | awk 'END {print $4}')" || return 1
    if [[ ! "$available" =~ ^[0-9]+$ ]]; then
      echo "AIDE: cannot determine free disk space for $path; startup stopped." >&2; return 1
    fi
    if (( available < minimum )); then
      echo "AIDE: insufficient disk space at $path (${available} KiB free; need $((minimum / 1048576)) GiB for $phase). Free rebuildable cache, then retry; user data was not deleted." >&2
      return 1
    fi
  done
}
aide_check_engine_resources() {
  local memory cpus
  memory="$("$DOCKER_BIN" info --format '{{.MemTotal}}')" || return 1
  cpus="$("$DOCKER_BIN" info --format '{{.NCPU}}')" || return 1
  [[ "$memory" =~ ^[0-9]+$ && "$cpus" =~ ^[0-9]+$ ]] || { echo 'AIDE: invalid Docker resource report.' >&2; return 1; }
  if (( memory < 4294967296 )); then
    echo 'AIDE: Docker requires at least 4 GiB; 6 GiB is recommended with native CAD. Adjust Docker Desktop resources and retry.' >&2; return 1
  fi
  echo "Docker budget: $((memory / 1048576)) MiB / $cpus CPUs; AIDE caps: app 2 GiB + CAD 4 GiB, 2 CPUs each; rotating logs 3 x 10 MiB per service."
  if (( memory > 8589934592 || cpus > 4 )); then
    echo 'AIDE: Docker global allocation is high. On a 16 GiB Mac use about 6 GiB / 4 CPUs; check Docker Desktop disk limit too. Global settings were not changed.' >&2
  fi
}
