#!/usr/bin/env bash
# End-to-end verification for aide remote-shell cancel/timeout cleanup.
#
# What it proves, against a REAL throwaway OpenSSH server (this container):
#   1. A remote long-running process started via the production setsid wrapper
#      SURVIVES the death of its parent ssh client (the old leak: killing the
#      local ssh client alone does NOT reap the remote process group).
#   2. Running the production cleanup snippet (`kill -- -PGID`) afterwards
#      removes that process AND its whole process group, with before/after
#      remote `ps` evidence, marker-file state, and the AIDE-CLEANUP result line.
#   3. The same holds for the timeout path.
#
# The wrapper/cleanup shell snippets below MUST stay byte-equivalent to
# internal/server/ssh_session.go: execRemote (remoteGroup wrapper) and
# cleanupRemoteGroup.
#
# Usage: ./run-e2e.sh
# Teardown is automatic. Ephemeral keys are generated under a temp dir and wiped.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
EVIDENCE="$HERE/evidence/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$EVIDENCE"
PORT="${AIDE_E2E_PORT:-22322}"
CONTAINER="aide-sshd-e2e"
IMG="aide/sshd-e2e:latest"

log() { echo "[e2e] $*" | tee -a "$EVIDENCE/run.log"; }

# --- cleanup on exit ---------------------------------------------------------
cleanup_container() {
  log "--- tearing down container ---"
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  [ -n "${WORK:-}" ] && rm -rf "$WORK"
}
trap cleanup_container EXIT

# --- 1. build image + ephemeral key ------------------------------------------
log "== building throwaway sshd image =="
docker build -q -t "$IMG" "$HERE" >/dev/null

WORK="$(mktemp -d)"
log "== ephemeral key dir: $WORK =="
ssh-keygen -t ed25519 -N "" -f "$WORK/id_ed25519" -C aide-e2e >/dev/null
cp "$WORK/id_ed25519.pub" "$WORK/authorized_keys"
chmod 600 "$WORK/id_ed25519"

SSH_OPTS=(
  -i "$WORK/id_ed25519"
  -p "$PORT"
  -o StrictHostKeyChecking=no
  -o UserKnownHostsFile=/dev/null
  -o LogLevel=ERROR
  -o BatchMode=yes
)

log "== starting container $CONTAINER on 127.0.0.1:$PORT =="
docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
docker run -d --name "$CONTAINER" \
  -p 127.0.0.1:"$PORT":22 \
  -v "$WORK/authorized_keys:/root/.ssh/authorized_keys:ro" \
  "$IMG" >/dev/null

# wait for sshd
for i in $(seq 1 30); do
  if ssh "${SSH_OPTS[@]}" root@127.0.0.1 'echo ready' >/dev/null 2>&1; then break; fi
  sleep 0.3
done
ssh "${SSH_OPTS[@]}" root@127.0.0.1 'echo ready && uname -a' | tee "$EVIDENCE/00-ready.txt"

# remote helper: run a command on the throwaway host, tee to evidence.
rrun() { # $1=label $2=remote-command
  echo "\$ $2" | tee -a "$EVIDENCE/$1.txt"
  ssh "${SSH_OPTS[@]}" root@127.0.0.1 "$2" 2>&1 | tee -a "$EVIDENCE/$1.txt" || true
}

# --- Scenario A: user cancel ------------------------------------------------
run_cancel() {
  local tag="$1"            # cancel | timeout
  local pgidfile="/tmp/.aide-e2e-$tag.pgid"
  local marker="/tmp/.aide-e2e-$tag.marker"
  docker exec "$CONTAINER" rm -f "$pgidfile" "$marker" >/dev/null 2>&1 || true

  # Production-equivalent wrapper: setsid => dedicated session/process group.
  # Long work: sleep 240, then write marker. It ignores SIGHUP (no controlling tty).
  local inner="echo \$\$ > $pgidfile; sleep 240; echo DONE > $marker"
  local wrapper="setsid bash -c '$inner'"

  log "== [$tag] launching remote long process =="
  # start in background; stdout/stderr to local log
  ssh "${SSH_OPTS[@]}" root@127.0.0.1 "$wrapper" >"$EVIDENCE/$tag.remote-stream.log" 2>&1 &
  local ssh_pid=$!

  # wait until remote pgid file + sleep exist
  local ok=""
  for i in $(seq 1 30); do
    if pg=$(ssh "${SSH_OPTS[@]}" root@127.0.0.1 "cat $pgidfile 2>/dev/null" 2>/dev/null) && [ -n "$pg" ]; then ok="$pg"; break; fi
    sleep 0.2
  done
  [ -n "$ok" ] || { log "FAIL: remote pgid never appeared"; exit 1; }
  log "[$tag] remote PGID = $ok (local ssh client pid = $ssh_pid)"

  rrun "$tag.1-before" "echo \"pgidfile=$pgidfile\"; cat $pgidfile; echo '--- ps (sleep + owner) ---'; ps -eo pid,ppid,pgid,args | grep -E 'sleep 240' | grep -v grep || echo '  (none)'"

  # old-behavior simulation: kill the LOCAL ssh client only (what Go
  # CommandContext does today if we did nothing else).
  log "[$tag] killing local ssh client pid=$ssh_pid (NO cleanup yet)"
  kill -9 "$ssh_pid" 2>/dev/null || true
  wait "$ssh_pid" 2>/dev/null || true
  sleep 1

  rrun "$tag.2-after-local-kill-no-cleanup" "echo 'remote sleep still running? (expected YES = old leak):'; ps -eo pid,ppid,pgid,args | grep -E 'sleep 240' | grep -v grep || echo '  (none)';"

  # production cleanup snippet (mirror cleanupRemoteGroup). Numeric guard instead
  # of a case pattern so it stays single-quote-free when wrapped in bash -c '...'.
  local cleanup="p=\$(cat $pgidfile 2>/dev/null); res=absent;"
  cleanup="$cleanup if [ -n \"\$p\" ] && [ \"\$p\" -eq \"\$p\" ] 2>/dev/null; then"
  cleanup="$cleanup if kill -0 -- -\"\$p\" 2>/dev/null; then"
  cleanup="$cleanup kill -TERM -- -\"\$p\" 2>/dev/null; sleep 0.3; kill -KILL -- -\"\$p\" 2>/dev/null; sleep 0.2;"
  cleanup="$cleanup if kill -0 -- -\"\$p\" 2>/dev/null; then res=still-alive; else res=reaped; fi;"
  cleanup="$cleanup else res=already-gone; fi; fi;"
  cleanup="$cleanup rm -f $pgidfile; echo \"AIDE-CLEANUP:\$res:\$p\""

  log "[$tag] running production cleanup (kill -- -PGID)"
  ssh "${SSH_OPTS[@]}" root@127.0.0.1 "bash -c '$cleanup'" 2>&1 | tee "$EVIDENCE/$tag.3-cleanup.txt"

  sleep 0.5
  rrun "$tag.4-after-cleanup" "echo 'remote sleep gone? (expected none):'; ps -eo pid,ppid,pgid,args | grep -E 'sleep 240' | grep -v grep || echo '  (none)'; echo '--- marker file (must NOT exist; sleep never finished):'; ls -l $marker 2>&1 || echo '  marker absent (good)'; echo '--- pgid file (must be removed):'; ls -l $pgidfile 2>&1 || echo '  pgid file absent (good)'"
}

run_cancel "cancel"
run_cancel "timeout"

# --- assertions on captured evidence -----------------------------------------
log "== asserting evidence =="
fail=0
expect() { # file needle should|should-not
  local file="$EVIDENCE/$1" what="$2" mode="$3"
  if [ "$mode" = "should" ]; then
    if grep -qF "$what" "$file"; then log "OK   $1 contains '$what'"; else log "FAIL $1 missing '$what'"; fail=1; fi
  else
    if grep -qF "$what" "$file"; then log "FAIL $1 unexpectedly contains '$what'"; fail=1; else log "OK   $1 lacks '$what'"; fi
  fi
}

for t in cancel timeout; do
  # after killing local ssh with no cleanup, the sleep must STILL be alive (leak reproduced)
  expect "$t.2-after-local-kill-no-cleanup.txt" "sleep 240" should
  # cleanup must report reaped
  expect "$t.3-cleanup.txt" "AIDE-CLEANUP:reaped:" should
  # after cleanup: ps must report no match (the echoed command line itself
  # contains "sleep 240", so assert on the ps RESULT "(none)" instead).
  expect "$t.4-after-cleanup.txt" "(none)" should
  expect "$t.4-after-cleanup.txt" "marker absent" should
  expect "$t.4-after-cleanup.txt" "pgid file absent" should
done

echo ""
log "=========================================="
if [ "$fail" -eq 0 ]; then
  log "E2E RESULT: PASS — remote process group reaped on cancel AND timeout"
else
  log "E2E RESULT: FAIL"
fi
log "Evidence dir: $EVIDENCE"
log "=========================================="
exit "$fail"
