# sshd-test — throwaway OpenSSH e2e for remote-shell cleanup

Proves that aide's remote-shell **cancel** and **timeout** paths actually reap the
REMOTE process group on a real OpenSSH server (not just the local `ssh` client).

## Run

```sh
./run-e2e.sh
# optional: override host port
AIDE_E2E_PORT=22322 ./run-e2e.sh
```

Requirements: Docker + an `ssh`/`ssh-keygen` client on the host. The script:

1. Builds `aide/sshd-e2e` from `Dockerfile` (debian:bookworm-slim + openssh-server + procps).
2. Generates an **ephemeral** ed25519 keypair under a fresh `mktemp -d` (no secrets committed).
3. Starts a throwaway container `aide-sshd-e2e`, publishing `127.0.0.1:$PORT:22`,
   mounting the generated `authorized_keys` read-only.
4. For BOTH `cancel` and `timeout`:
   - Starts a remote long process through the production `setsid` wrapper, which
     records its PGID to `/tmp/.aide-e2e-<tag>.pgid` and runs `sleep 240`.
   - Captures remote `ps` **before**.
   - Kills the LOCAL ssh client (simulating `exec.CommandContext` cancel) with NO
     cleanup, then shows the remote process/group **survives** (the old leak).
   - Runs the production cleanup snippet (`kill -TERM/-KILL -- -PGID`) and shows
     `AIDE-CLEANUP:reaped:<pgid>`.
   - Captures remote `ps` **after** (no process), and confirms the marker file was
     never written (sleep never finished) and the pgid file was removed.
5. Tears down the container and temp key dir automatically.

Evidence (one timestamped dir per run):

```
evidence/<YYYYMMDD-HHMMSS>/
  run.log                 # overall log + PASS/FAIL
  00-ready.txt
  cancel.1-before.txt / cancel.2-after-local-kill-no-cleanup.txt /
  cancel.3-cleanup.txt / cancel.4-after-cleanup.txt
  timeout.*.txt (same four)
```

## How this maps to production

The wrapper and cleanup snippets in `run-e2e.sh` mirror, respectively:

- `internal/server/ssh_session.go` `execRemote` → `setsid bash -c 'echo $$ > PGID; ...; rm PGID'`
- `internal/server/ssh_session.go` `cleanupRemoteGroup` → read PGID, TERM→KILL the
  negative process group, verify, remove marker, echo `AIDE-CLEANUP:<res>:<pgid>`.

Go-level tests in `internal/server/remote_group_cleanup_test.go` prove the
production code actually emits these exact commands on cancel/timeout; this harness
proves the commands work against a real sshd.

## Real public-internet host

Not covered here: no long-lived host/credentials were available. Verified only
against the localhost container. A real remote host must be retried with
user-provided host/credentials before production sign-off.
