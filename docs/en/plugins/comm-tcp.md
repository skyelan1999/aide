# TCP Communication Plugin (comm-tcp)

A **long-running daemon plugin** with zero third-party dependencies, built on Node's built-in **`net`** module. Used for TCP probing, traffic recording, replay, and simulated responses on the loopback network — aimed at protocol debugging, service/device integration, and fuzz testing.

- Plugin directory: `plugins/comm-tcp/` (`manifest.json` + `index.js` + `test.js`)
- Protocol: aide plugin protocol **v1.2 daemon** (`docs/plugins/daemon-protocol.md`) — a resident Node subprocess over line-delimited JSON-RPC on stdio
- Shape: daemon plugin (`"daemon": true` in manifest), lifecycle managed by `DaemonManager` (heartbeat, backoff restart)
- Shipping state: preinstalled, **disabled by default** (`enabled:false`); must be explicitly enabled in the permission panel

## Tools

| Tool | Purpose | Key return |
| --- | --- | --- |
| `tcp_listen` | Listen on 127.0.0.1; record new connections and bidirectional traffic | `{port, host, recordingId, connections}` |
| `tcp_connect` | Connect as a client to host:port | `{connectionId, host, port, recordingId}` |
| `tcp_send` | Send data over a connection (utf8/hex) | `{connectionId, sent}` |
| `tcp_close` | Close one or all connections | `{closed}` |
| `tcp_proxy_start` | Local MITM proxy listenPort → targetHost:targetPort; records both directions | `{proxyId, listenPort, targetHost, targetPort}` |
| `tcp_proxy_stop` | Stop proxy, release local port | `{proxyId}` |
| `tcp_simulate_set` | Set simulated-response rules on a listening port | `{port, ruleCount}` |
| `tcp_simulate_clear` | Clear rules on a port | `{port}` |
| `tcp_replay` | Replay recorded c2s frames to a new target in time order | `{sentFrames, sentBytes, responseBytes}` |
| `tcp_fuzz` | Send edge/malformed payloads to a target; record results without crashing | `{probes, survived, results[]}` |

## State & events

The plugin keeps in its resident process: `listeners` (port → `net.Server`), `connections` (connectionId → `net.Socket`), `proxies`, `simRules`, `recordings`.

- **Traffic events**: every send/recv is reported via `ctx.emit('traffic', {direction, connectionId, recordingId, length, previewHex, payloadTruncated})`; `direction` is `in` (received locally) / `out` (sent locally). By default only length and a ≤256-byte hex preview are sent — never full payloads.
- **Connection events**: connect/close/error via `ctx.emit('event', {subtype:'connected'|'closed'|'error', ...})`.
- Events are ring-buffered (last 1000) by the Go side and queryable at `GET /api/plugins/daemons/comm-tcp/events`.

## Simulation rules

`tcp_simulate_set {port, rules}` — each rule:

```json
{ "match": "foo", "reply": "bar", "replyFormat": "utf8", "delay": 200, "disconnect": false }
```

- `match`: substring match against the received utf8 text (or hex). `"*"` / `"echo"` means **echo the received bytes back**.
- `reply`: response body (ignored when `match=*`, which echoes); `replyFormat` defaults to `utf8`, optional `hex`.
- `delay`: milliseconds to wait before replying.
- `disconnect`: drop the connection after replying.
- First matching rule wins; max 64 rules.

## Examples

> Called by the model in a tool loop; all addresses are loopback.

**Start an echo service and probe it**
```json
{ "port": 4200 }                                   // tcp_listen → recordingId
{ "port": 4200, "rules": [ { "match": "*" } ] }     // tcp_simulate_set → echo
```

**Plugin as client, bidirectional I/O**
```json
{ "host": "127.0.0.1", "port": 4200 }              // tcp_connect → connectionId
{ "connectionId": "c-1", "data": "hello" }          // tcp_send
```

**Proxy record then replay**
```json
{ "listenPort": 4300, "targetHost": "127.0.0.1", "targetPort": 4200 }   // tcp_proxy_start
// external client connects to 4300 → plugin records c2s/s2c
{ "recordingId": "rec-3", "targetHost": "127.0.0.1", "targetPort": 4201, "speed": 0 }  // tcp_replay
```

**Fuzz a local port**
```json
{ "host": "127.0.0.1", "port": 4200, "count": 8 }  // tcp_fuzz
```

## Security model

- **Disabled by default**: ships `enabled:false`; must be explicitly enabled in the permission panel. `DaemonManager.restore()` only auto-starts daemons that are `enabled:true`.
- **Loopback-only binding**: `tcp_listen` / `tcp_proxy_start` always bind `127.0.0.1` (the `host` param is accepted but ignored); nothing listens on LAN/public. `tcp_connect`/`tcp_replay` targets are configurable, but the plugin never exposes an inbound port.
- **No privileges**: plain `net` sockets only; no `CAP_NET_RAW`, no packet capture, no raw frames.
- **Audit**: all traffic/event/log events are ring-buffered by Go; stopping the plugin runs `stop()`, closing every listener/connection/proxy and releasing ports.
- **Fuzz self-limits**: max 32 probes, 3s timeout per probe; results are recorded, the process must not crash.

## Platform limitations

- **Node version**: uses built-in `net`, requires Node ≥ 18; the project container runs Node 24.
- **Container network**: the plugin sees the container network; loopback tests use 127.0.0.1. To reach services outside the container, the compose network must make them reachable.
- **Port release timing**: listening ports are released only on plugin `stop()` (user stop/delete, stdin EOF / SIGTERM); they stay held while the daemon runs.
- **Payload preview**: events carry a ≤256-byte hex preview by default; full frames stay in-process in `recordings` for replay.
- **Replay semantics**: `tcp_replay` replays only c2s (client→server) frames from a recording, paced by recorded inter-frame gaps (scaled by `speed`); s2c is not replayed.
