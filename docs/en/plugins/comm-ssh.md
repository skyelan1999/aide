# SSH Communication Plugin (comm-ssh)

A **resident daemon plugin** built on [`ssh2`](https://www.npmjs.com/package/ssh2). It turns aide into a scriptable SSH client: a long-running Node process maintains multiple SSH connections, SFTP sessions, and local port forwards, and embeds a **scriptable mock SSH server** for end-to-end testing and device/protocol dry-runs.

- Plugin directory: `plugins/comm-ssh/` (`manifest.json` + `index.js` + `test.js` + `package.json` + `node_modules/ssh2`)
- Protocol: aide plugin protocol **v1.2 daemon** (see `docs/en/plugins/daemon-protocol.md`)
- Shape: daemon plugin — one resident `node` subprocess; connections / tunnels / mock servers keep state across tool calls.
- Shipped state: preinstalled, **`enabled: false` by default**; it is only started (or auto-restored) after the user explicitly enables it in the permission panel.

## Dependency & install

- Runtime dependency is only `ssh2` (pure JS, no native compilation, compatible with Node 24).
- Install (inside the plugin directory):

  ```bash
  cd plugins/comm-ssh
  npm install ssh2   # produces node_modules, shipped with the plugin directory
  ```

- Verify (container, Node 24):

  ```bash
  docker run --rm -v "$PWD/plugins/comm-ssh":/app -w /app node:24-bookworm-slim node test.js
  ```

## Tools

| Tool | Purpose | Key params | Key result |
| --- | --- | --- | --- |
| `ssh_connect` | Open an SSH connection (password or public key) | `host`, `port?=22`, `authType?=password`, `username`, `password?`, `privateKey?`, `passphrase?`, `keyId?` | `{connectionId, host, port, username, authType}` |
| `ssh_exec` | Run a command on a connection | `connectionId`, `command`, `timeout?=30000` | `{stdout, stderr, exitCode, signal?}` |
| `ssh_sftp_list` | SFTP list a directory | `connectionId`, `path` | `{path, entries:[{name,type,size,modifyTime}]}` |
| `ssh_sftp_get` | SFTP download a remote file to local | `connectionId`, `remotePath`, `localPath` | `{remotePath, localPath, bytes}` |
| `ssh_sftp_put` | SFTP upload a local file to remote | `connectionId`, `localPath`, `remotePath` | `{localPath, remotePath, bytes}` |
| `ssh_tunnel_start` | Local port forward (local listener → over SSH → remoteHost:remotePort) | `connectionId`, `localPort`(0=random), `remoteHost`, `remotePort` | `{tunnelId, localPort, remoteHost, remotePort, bind}` |
| `ssh_tunnel_stop` | Stop a tunnel, release the local port | `tunnelId` | `{ok, closed}` |
| `ssh_close` | Close a connection; omit to close all | `connectionId?` | `{closed:[connectionId]}` |
| `ssh_simulate_start` | Start a mock SSH server on 127.0.0.1 | `port`(0=random), `hostKey?`, `authPassword?=true`, `expectedPassword?`, `publickey?=false` | `{serverId, port, host, sftpRoot, ...}` |
| `ssh_simulate_stop` | Stop a mock server | `serverId` | `{ok, stopped, recordedSessions}` |
| `ssh_simulate_set_rule` | Set mock command-response rules | `serverId`, `rules:[{commandMatch, reply, exitCode?, stderr?, delay?, regex?}]` | `{serverId, count}` |

## Examples

1. Start a mock server with a fixed password and command rules:

   ```json
   // ssh_simulate_start
   { "port": 0, "expectedPassword": "secret" }
   // -> { "serverId": "s-1-...", "port": 52210, ... }

   // ssh_simulate_set_rule
   { "serverId": "s-1-...", "rules": [
       { "commandMatch": "uname -a", "reply": "Linux mockbox 6.1.0 x86_64\n" },
       { "commandMatch": "^df ", "regex": true, "reply": "/dev/sim  1G  50M  950M  5% /\n" }
   ]}
   ```

2. Connect with a password and run a command:

   ```json
   // ssh_connect
   { "host": "127.0.0.1", "port": 52210, "username": "tester", "authType": "password", "password": "secret" }
   // -> { "connectionId": "c-1-...", ... }

   // ssh_exec
   { "connectionId": "c-1-...", "command": "uname -a" }
   // -> { "stdout": "Linux mockbox 6.1.0 x86_64\n", "stderr": "", "exitCode": 0 }
   ```

3. SFTP and tunneling:

   ```json
   // ssh_sftp_put
   { "connectionId": "c-1-...", "localPath": "/local/notes.txt", "remotePath": "/home/tester/notes.txt" }
   // ssh_tunnel_start: forward local 127.0.0.1:0 over this SSH to intranet 192.168.10.5:8080
   { "connectionId": "c-1-...", "localPort": 0, "remoteHost": "192.168.10.5", "remotePort": 8080 }
   ```

## Security model

- **Disabled by default**: `enabled: false`. The daemon manager only auto-starts plugins declared `"daemon": true` **and** `enabled: true`; when disabled it opens no ports and makes no outbound connections.
- **Loopback only**: every listening socket owned by the plugin (the mock SSH server, the tunnel's local listener) binds to `AIDE_DAEMON_BIND` (default `127.0.0.1`) — never exposed to LAN/public networks. The outbound target (`ssh_connect.host`) is supplied by the caller.
- **Credentials never echoed, never persisted**:
  - Passwords / private keys / passphrases are supplied **per call**, held only in process memory, and dropped once the connection closes (`ssh_close` or disconnect).
  - Tool results, error messages, `ctx.emit` events, and stderr audit logs **never** include password or private-key material. `ssh_exec` traffic events carry only direction and byte length (`payloadTruncated: true`) — not command/output bodies.
  - `keyId` is a **reference placeholder into the unified secret vault** (`docs/security/secret-vault.md`, `/data/secrets/vault.enc`, AES-256-GCM + Argon2id). The orchestration layer that holds the unlocked master key resolves `keyId` to `password` / `privateKey` and injects it before the call. The plugin itself holds no vault master key and cannot decrypt the vault.
- **The mock server does not touch the real host**: its SFTP subsystem is backed by an in-process temp directory (under `os.tmpdir()`), with in-root path checks; it does not read/write elsewhere in the container.
- **Audit trail**: connect/disconnect, tunnel up/down, and mock-server events are reported via `ctx.emit('event'|'traffic'|'log')`, ring-buffered (latest 1000) by the Go side, and queryable via `GET /api/plugins/daemons/comm-ssh/events`.

## What the mock SSH server is for

The `ssh_simulate_*` tools drive ssh2 as a server, for two purposes:

1. **End-to-end testing**: verify SSH client behavior (connect, auth failure, SFTP round-trip, port forwarding) without any real/external SSH server — everything over 127.0.0.1.
2. **Dry-runs/recording**: `ssh_simulate_set_rule` gives scripted responses by exact match, `*` wildcard, or regex (with optional `delay`, `exitCode`, `stderr`), to rehearse automation or replay a peer's behavior when no real device exists.
   - Auth policy: `authPassword:false` disables password login; `expectedPassword` pins a single password; `publickey:true` enables public-key login (mock mode accepts any valid private key).
   - The mock server also implements an `sftp` subsystem (temp-dir backend) and `direct-tcpip` (tunnel passthrough), so SFTP and tunnel tools can be fully verified without a real server.

## Platform limitations

- **Docker network / port mapping**: tunnels and the mock server bind only to the container's `127.0.0.1`. Reaching a container-side mock/tunnel port from the host goes through aide's published port/reverse channel — you cannot `docker -p` an extra random mapping.
- **SFTP local paths**: `ssh_sftp_get/put` `localPath` is a path **inside the container filesystem** (the plugin process's view), not the host; writes are constrained by aide's mount roots.
- **Exact public-key authorization**: in mock mode `publickey:true` accepts any valid private key; it does not pin a specific public-key fingerprint and is for testing only, not real access control.
- **Not covered (NOT_RUN)**: real external SSH servers, host public-key verification (`hostVerifier`), jump-host/multi-hop, and remote (reverse) port forwarding are not implemented.
