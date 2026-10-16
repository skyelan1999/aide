#!/usr/bin/env bash
# Sourced by launchers; never execute .env as shell code.
launcher_env_restore() {
  umask 077
  [[ ! -L .aide-data ]] || { echo '.aide-data must not be a symbolic link.' >&2; return 1; }
  mkdir -p .aide-data
  chmod 700 .aide-data
  [[ ! -L .env && ! -L .aide-data/launcher.env && ! -L .aide-data/compose-project ]] || { echo 'Launcher configuration must not be symbolic links.' >&2; return 1; }
  if [[ ! -e .env && -f .aide-data/launcher.env ]]; then cp -p .aide-data/launcher.env .env; fi
  [[ ! -L .aide-image && ! -L .aide-data/image ]] || { echo 'Image identity must not be a symbolic link.' >&2; return 1; }
  if [[ ! -e .aide-image && -f .aide-data/image ]]; then cp -p .aide-data/image .aide-image; fi
  if [[ -z "${COMPOSE_PROJECT_NAME:-}" && -f .aide-data/compose-project ]]; then
    COMPOSE_PROJECT_NAME="$(cat .aide-data/compose-project)"
    [[ "$COMPOSE_PROJECT_NAME" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || { echo 'Invalid stored Compose identity.' >&2; return 1; }
    export COMPOSE_PROJECT_NAME
  fi
  if [[ "$(uname -s)" == Linux ]]; then
    export AIDE_RUN_UID="${AIDE_RUN_UID:-$(id -u)}" AIDE_RUN_GID="${AIDE_RUN_GID:-$(id -g)}"
    if [[ "$AIDE_RUN_UID" == 0 ]]; then export AIDE_RUN_UID=1000 AIDE_RUN_GID=1000; fi
  fi
}
launcher_env_save() {
  local temporary
  if [[ -f .env ]]; then
    temporary="$(mktemp .aide-data/launcher-env.XXXXXX)"
    cp .env "$temporary"
    chmod 600 "$temporary"
    mv "$temporary" .aide-data/launcher.env
  fi
  if [[ -f .aide-image ]]; then
    temporary="$(mktemp .aide-data/image.XXXXXX)"
    cp .aide-image "$temporary"
    mv "$temporary" .aide-data/image
  fi
}
launcher_data_prepare() {
  local docker_bin="$1" image="$2" root own project legacy_type legacy_source volume target platform count stopped=0
  root="$(pwd -P)"
  if [[ -f .aide-data/storage/migration.json ]]; then
    [[ -d .aide-data/storage/data && -d .aide-data/storage/home && ! -L .aide-data/storage && ! -L .aide-data/storage/data && ! -L .aide-data/storage/home ]] || { echo 'Persistent storage is incomplete; refusing a new empty installation.' >&2; return 1; }
    "$docker_bin" run --rm --network none --user 0:0 -v "$root/.aide-data:/persist" -v "$root/scripts/launcher-data.py:/tool.py:ro" --entrypoint python3 "$image" /tool.py --uid "${AIDE_RUN_UID:-1000}" --gid "${AIDE_RUN_GID:-1000}" --platform "$(uname -s | tr '[:upper:]' '[:lower:]')"
    launcher_env_save
    return
  fi
  [[ ! -e .aide-data/storage && ! -L .aide-data/storage ]] || { echo 'Existing storage has no valid receipt; import refused before stopping anything.' >&2; return 1; }
  own="$("$docker_bin" ps -aq --filter "label=com.docker.compose.project.working_dir=$root" --filter label=com.docker.compose.service=aide)"
  count="$(printf '%s\n' "$own" | awk 'NF{n++} END{print n+0}')"
  [[ "$count" -le 1 ]] || { echo 'Multiple legacy containers for this launcher; refuse ambiguous import.' >&2; return 1; }
  project="${COMPOSE_PROJECT_NAME:-}"
  if [[ -z "$project" && -f .env ]]; then project="$(awk -F= '$1=="COMPOSE_PROJECT_NAME"{gsub(/^[ "\047]+|[ "\047]+$/, "", $2);print $2;exit}' .env)"; fi
  if [[ -n "$own" ]]; then project="$("$docker_bin" inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$own")"; fi
  if [[ -z "$project" ]]; then project="$(awk '$1=="name:"{print $2;exit}' compose.yaml)"; fi
  project="${project:-aide}"
  [[ "$project" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || { echo 'Invalid Compose project identity.' >&2; return 1; }
  export COMPOSE_PROJECT_NAME="$project"
  local mounts=(-v "$root/.aide-data:/persist" -v "$root/scripts/launcher-data.py:/tool.py:ro") args=(--root /persist --seed-home)
  for target in data home; do
    legacy_type=''; legacy_source=''
    local destination=/data
    [[ "$target" != home ]] || destination=/home/aide
    if [[ -n "$own" ]]; then
      legacy_type="$("$docker_bin" inspect --format "{{range .Mounts}}{{if eq .Destination \"$destination\"}}{{.Type}}{{end}}{{end}}" "$own")"
      if [[ "$legacy_type" == volume ]]; then
        legacy_source="$("$docker_bin" inspect --format "{{range .Mounts}}{{if eq .Destination \"$destination\"}}{{.Name}}{{end}}{{end}}" "$own")"
      elif [[ "$legacy_type" == bind ]]; then
        legacy_source="$("$docker_bin" inspect --format "{{range .Mounts}}{{if eq .Destination \"$destination\"}}{{.Source}}{{end}}{{end}}" "$own")"
        if [[ "$(uname -s)" == Darwin ]]; then legacy_source="${legacy_source#/host_mnt}"; fi
      fi
    else
      volume="${project}_aide-$target"
      if "$docker_bin" volume inspect "$volume" >/dev/null 2>&1; then legacy_type=volume; legacy_source="$volume"; fi
    fi
    if [[ -z "$legacy_source" && "$target" == data && -d .data ]]; then legacy_type=bind; legacy_source="$root/.data"; fi
    if [[ -n "$legacy_source" ]]; then
      # Do not adopt another launcher's still-running volume after a folder move.
      if [[ "$legacy_type" == volume ]]; then
        local consumers
        consumers="$("$docker_bin" ps -q --filter "volume=$legacy_source")"
        [[ -z "$consumers" || "$consumers" == "$own" ]] || { echo 'Legacy volume is used by another running container; import refused.' >&2; return 1; }
      fi
      mounts+=(-v "$legacy_source:/legacy/$target:ro"); args+=("--$target" "/legacy/$target")
    fi
  done
  if [[ -n "$own" && "$("$docker_bin" inspect --format '{{.State.Running}}' "$own")" == true ]]; then
    "$docker_bin" exec -w /tmp "$own" python3 -c 'import json,pathlib,sys
terminal={"completed","failed","cancelled","canceled","stopped","error","done","resumed",""}
for p in pathlib.Path("/data/sessions").rglob("*.json"):
 d=json.loads(p.read_text())
 if any(r and r.get("status","") not in terminal for r in (d.get("runs") or [])):sys.exit("Active or suspended work exists; finish it before data migration")'
    mkdir -p .cache; touch .cache/aide-manual-stop
    "$docker_bin" stop --time 30 "$own" >/dev/null
    stopped=1
  fi
  platform="$(uname -s | tr '[:upper:]' '[:lower:]')"
  if ! "$docker_bin" run --rm --network none --user 0:0 "${mounts[@]}" --entrypoint python3 "$image" /tool.py --uid "${AIDE_RUN_UID:-1000}" --gid "${AIDE_RUN_GID:-1000}" --platform "$platform" "${args[@]}" || [[ ! -f .aide-data/storage/migration.json ]]; then
    echo 'Persistent import failed; legacy data and staging retained.' >&2
    if [[ "$stopped" == 1 ]]; then
      if "$docker_bin" start "$own" >/dev/null; then
        rm -f .cache/aide-manual-stop
        echo 'Previous container restarted with its original mounts.' >&2
      else
        echo 'Previous container could not restart; inspect it before retrying.' >&2
      fi
    fi
    return 1
  fi
  printf '%s\n' "$project" > .aide-data/compose-project
  launcher_env_save
}
