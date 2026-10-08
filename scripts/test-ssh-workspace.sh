#!/usr/bin/env bash
# Real SSH/SFTP acceptance using two disposable servers and no published ports.
set -euo pipefail
cd "$(dirname "$0")/.."
ssh_test_root="$PWD"
ssh_test_docker="${DOCKER_BIN:-docker}"
ssh_test_fixture_image="${AIDE_SSH_FIXTURE_IMAGE:-aide-source-fixtures:test}"
ssh_test_runtime_image="${AIDE_SSH_RUNTIME_IMAGE:-aide:local}"
ssh_test_repeat="${AIDE_SSH_REPEAT:-2}"
if [[ ! "$ssh_test_repeat" =~ ^[1-9][0-9]*$ ]] || (( ssh_test_repeat > 10 )); then
  printf '%s\n' 'AIDE_SSH_REPEAT must be an integer between 1 and 10' >&2
  exit 2
fi
# No implicit builds or pulls: fixture credentials/data must stay on this network.
"$ssh_test_docker" image inspect "$ssh_test_fixture_image" "$ssh_test_runtime_image" >/dev/null
ssh_test_id="aide-ssh-test-$(date +%Y%m%d%H%M%S)-$$"
ssh_test_network="$ssh_test_id"
ssh_test_server_a="$ssh_test_id-a"
ssh_test_server_b="$ssh_test_id-b"
ssh_test_client="$ssh_test_id-client"
ssh_test_evidence="${AIDE_SSH_TEST_EVIDENCE_DIR:-.agent-state/ssh-storage-audit/live}"
mkdir -p "$ssh_test_evidence"
ssh_test_log="$ssh_test_evidence/$ssh_test_id.log"
ssh_test_inputs="$ssh_test_evidence/$ssh_test_id.inputs.sha256"
snapshot_inputs() {
  shasum -a 256 internal/server/*.go scripts/test-ssh-workspace.sh go.mod go.sum
}
snapshot_inputs > "$ssh_test_inputs"
cleanup() {
  ssh_test_status=$?
  snapshot_inputs > "$ssh_test_inputs.after"
  if ! cmp -s "$ssh_test_inputs" "$ssh_test_inputs.after"; then
    printf '%s\n' 'SOURCE_CHANGED: test input files changed during this run; acceptance receipt is invalid' | tee -a "$ssh_test_log"
    ssh_test_status=1
  fi
  if [[ -f "$ssh_test_log" ]]; then
    shasum -a 256 "$ssh_test_log" > "$ssh_test_log.sha256"
    printf 'Evidence: %s\n' "$ssh_test_log"
  fi
  "$ssh_test_docker" rm -f "$ssh_test_client" "$ssh_test_server_a" "$ssh_test_server_b" >/dev/null 2>&1 || true
  "$ssh_test_docker" network rm "$ssh_test_network" >/dev/null 2>&1 || true
  trap - EXIT
  exit "$ssh_test_status"
}
trap cleanup EXIT
"$ssh_test_docker" network create --internal "$ssh_test_network" >/dev/null
for ssh_test_role in a b; do
  ssh_test_server="$ssh_test_id-$ssh_test_role"
  "$ssh_test_docker" run --pull=never -d --name "$ssh_test_server" --network "$ssh_test_network" \
    --network-alias "ssh-fixture-$ssh_test_role" "$ssh_test_fixture_image" >/dev/null
  ssh_test_ready=false
  for ssh_test_attempt in {1..40}; do
    if "$ssh_test_docker" exec "$ssh_test_server" curl -fsS http://127.0.0.1:8080/root.txt >/dev/null 2>&1; then
      ssh_test_ready=true
      break
    fi
    sleep 1
  done
  if [[ "$ssh_test_ready" != true ]]; then
    "$ssh_test_docker" logs "$ssh_test_server"
    exit 1
  fi
  # These paths belong only to the fresh fixture container; no external volumes.
  "$ssh_test_docker" exec "$ssh_test_server" bash -c \
    'printf "SERVER-%s\n" "$1" > /srv/refs/server-id.txt; mkdir -p /srv/ssh-denied /srv/ssh-list-denied /home/fixture/.ssh; chmod 0555 /srv/ssh-denied; chmod 0000 /srv/ssh-list-denied; chown fixture /home/fixture/.ssh; chmod 0700 /home/fixture/.ssh' \
    fixture "$(printf '%s' "$ssh_test_role" | tr '[:lower:]' '[:upper:]')"
done
{
  printf 'fixture image: '
  "$ssh_test_docker" image inspect "$ssh_test_fixture_image" --format '{{.Id}}'
  printf 'client image: '
  "$ssh_test_docker" image inspect "$ssh_test_runtime_image" --format '{{.Id}}'
  printf 'source SHA-256:\n'
  cat "$ssh_test_inputs"
  printf 'real dual-server SSH test; race enabled; repeat=%s; no external network or user data\n' "$ssh_test_repeat"
  "$ssh_test_docker" run --pull=never --rm --name "$ssh_test_client" --user 0:0 --read-only \
    --network "$ssh_test_network" --cap-drop ALL --security-opt no-new-privileges:true \
    --tmpfs /tmp:rw,exec,size=1g --tmpfs /home/aide:rw,size=64m \
    -e AIDE_SSH_FIXTURE_A=ssh-fixture-a -e AIDE_SSH_FIXTURE_B=ssh-fixture-b \
    -e AIDE_SSH_TEST_ISOLATED=1 -e GOFLAGS=-buildvcs=false -e GOCACHE=/go-cache \
    -v aide-ssh-workspace-go-cache:/go-cache -v "$ssh_test_root:/src:ro" -w /src \
    --entrypoint bash "$ssh_test_runtime_image" -c \
    'mkdir -p /home/aide/.ssh; go test -mod=vendor -race -v ./internal/server -run "^TestSSHLiveWorkspaceAndIndependentSource$" -count="$1" -timeout=8m' fixture "$ssh_test_repeat"
} 2>&1 | tee "$ssh_test_log"
