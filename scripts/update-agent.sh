#!/usr/bin/env bash
# Host-side A/B installer. Runs beside the launcher, never inside the aide container.
set -u
cd "$(dirname "$0")/.." || exit 1
[[ -f .aide-image ]] || exit 0
DOCKER_BIN="$(command -v docker || true)"
[[ -n "$DOCKER_BIN" ]] || [[ ! -x "$HOME/.docker/bin/docker" ]] || DOCKER_BIN="$HOME/.docker/bin/docker"
[[ -n "$DOCKER_BIN" ]] || exit 0
LOCK=.aide-update-agent.lock
mkdir "$LOCK" 2>/dev/null || exit 0
trap 'rmdir "$LOCK" 2>/dev/null || true' EXIT INT TERM

compose() { COMPOSE_FILE=compose.yaml "$DOCKER_BIN" compose "$@"; }
unset AIDE_PORT
port="$(compose port aide 8080 2>/dev/null | sed -n 's/.*:\([0-9][0-9]*\)$/\1/p' | tail -1)"
[[ "$port" =~ ^[0-9]{1,5}$ ]] || exit 0
token="$(compose exec -T aide sh -c 'cat /data/auth/access-token 2>/dev/null || cat /data/access-token 2>/dev/null' 2>/dev/null | tr -d '\r\n')"
[[ -n "$token" ]] || exit 0
base="https://127.0.0.1:$port/api/updates"
api() { curl -ksS --connect-timeout 3 --max-time 10 -H "Authorization: Bearer $token" "$@"; }
post_result() {
  local success="$1" message="$2" result_port result_token
  result_port="$(compose port aide 8080 2>/dev/null | sed -n 's/.*:\([0-9][0-9]*\)$/\1/p' | tail -1)"
  result_token="$(compose exec -T aide sh -c 'cat /data/auth/access-token 2>/dev/null || cat /data/access-token 2>/dev/null' 2>/dev/null | tr -d '\r\n')"
  [[ -n "$result_port" && -n "$result_token" ]] || return 1
  curl -ksS --max-time 10 -H "Authorization: Bearer $result_token" -H 'Content-Type: application/json' -X POST \
    --data "{\"operationId\":\"$op\",\"success\":$success,\"message\":\"$message\"}" \
    "https://127.0.0.1:$result_port/api/updates/agent/result" >/dev/null 2>&1
}

# Seed the server's shared slot record with the launcher currently on disk.
read -r old_ref old_id platform old_tag < .aide-image
if [[ "$old_ref" != aide:slot-* ]]; then old_ref=aide:slot-a; fi
"$DOCKER_BIN" image tag "$old_id" "$old_ref" >/dev/null 2>&1 || true
active_slot="${old_ref##*-}"
active_slot="${active_slot^^}"
active_version="${old_tag#v}"
active_version="${active_version/-RC/ RC}"
sync_json="{\"version\":1,\"activeSlot\":\"$active_slot\",\"slots\":{\"$active_slot\":{\"version\":\"$active_version\",\"tag\":\"${old_tag:-}\",\"imageRef\":\"$old_ref\",\"imageId\":\"$old_id\",\"platform\":\"$platform\"}}}"
api -H 'Content-Type: application/json' -X POST --data "$sync_json" "$base/agent/sync" >/dev/null 2>&1 || true

while compose ps --status running --services 2>/dev/null | grep -qx aide; do
  port="$(compose port aide 8080 2>/dev/null | sed -n 's/.*:\([0-9][0-9]*\)$/\1/p' | tail -1)"
  token="$(compose exec -T aide sh -c 'cat /data/auth/access-token 2>/dev/null || cat /data/access-token 2>/dev/null' 2>/dev/null | tr -d '\r\n')"
  [[ "$port" =~ ^[0-9]{1,5}$ && -n "$token" ]] || { sleep 3; continue; }
  base="https://127.0.0.1:$port/api/updates"
  command="$(api "$base/agent" 2>/dev/null || true)"
  if [[ -n "$command" ]]; then
    IFS=$'\t' read -r op target package_id tag image_id platform image_hash image_ref version <<< "$command"
    if [[ "$op" =~ ^[a-f0-9]{32}$ && ( "$target" == A || "$target" == B ) && ( -z "$package_id" || "$package_id" =~ ^[a-f0-9]{32}$ ) && "$image_id" =~ ^sha256:[a-f0-9]{64}$ && "$platform" =~ ^linux/(arm64|amd64)$ && "$image_ref" == "aide:slot-${target,,}" ]]; then
      old_marker="$(cat .aide-image)"
      tmp="$(mktemp -d .agent-state/update.XXXXXX 2>/dev/null || { mkdir -p .agent-state; mktemp -d .agent-state/update.XXXXXX; })"
      ready=0
      if [[ -n "$package_id" ]]; then
        archive="aide-$tag-linux-${platform#linux/}-image.tar.gz"
        if curl -ksS --connect-timeout 10 --max-time 3600 -H "Authorization: Bearer $token" -o "$tmp/package.zip" "$base/agent/packages/$package_id" && unzip -p "$tmp/package.zip" "$archive" > "$tmp/$archive" 2>/dev/null; then
          if command -v shasum >/dev/null 2>&1; then actual="$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')"; else actual="$(sha256sum "$tmp/$archive" | awk '{print $1}')"; fi
          if [[ "$actual" == "$image_hash" ]] && "$DOCKER_BIN" image load -i "$tmp/$archive" >/dev/null; then ready=1; fi
        fi
      elif [[ "$("$DOCKER_BIN" image inspect "$image_ref" --format '{{.Id}}' 2>/dev/null || true)" == "$image_id" ]]; then
        ready=1
      fi
      if (( ready )) && "$DOCKER_BIN" image tag "$image_id" "$image_ref" >/dev/null; then
          printf '%s %s %s %s\n' "$image_ref" "$image_id" "$platform" "$tag" > .aide-image
          if AIDE_OPEN_BROWSER=0 bash scripts/aide.sh start-bundle >/dev/null 2>&1; then
            new_port="$(compose port aide 8080 2>/dev/null | sed -n 's/.*:\([0-9][0-9]*\)$/\1/p' | tail -1)"
            healthy=0
            for _ in {1..45}; do
              [[ -n "$new_port" ]] && curl -ksf --max-time 3 "https://127.0.0.1:$new_port/healthz" >/dev/null 2>&1 && { healthy=1; break; }
              sleep 1
            done
            if (( healthy )); then
              post_result true 'healthz passed' || true
            else
              printf '%s\n' "$old_marker" > .aide-image
              AIDE_OPEN_BROWSER=0 bash scripts/aide.sh start-bundle >/dev/null 2>&1 || true
              post_result false 'health check timed out; restored previous slot' || true
            fi
          else
            printf '%s\n' "$old_marker" > .aide-image
            AIDE_OPEN_BROWSER=0 bash scripts/aide.sh start-bundle >/dev/null 2>&1 || true
            post_result false 'target container failed to start; restored previous slot' || true
          fi
      else
        post_result false 'package download, checksum, or image import failed' || true
      fi
      rm -rf "$tmp"
    fi
  fi
  sleep 3
done
