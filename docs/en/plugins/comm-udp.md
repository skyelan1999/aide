# UDP Communication Plugin (comm-udp)

A **long-running daemon plugin** with zero third-party dependencies, built on Node's built-in **`dgram`** module. Used to send/receive UDP datagrams on the loopback network, simulate responses, and join/leave multicast groups — aimed at UDP protocol debugging and device integration.

- Plugin directory: `plugins/comm-udp/` (`manifest.json` + `index.js` + `test.js`)
- Protocol: aide plugin protocol **v1.2 daemon** (`docs/plugins/daemon-protocol.md`)
- Shape: daemon plugin (`"daemon": true`), lifecycle managed by `DaemonManager`
- Shipping state: preinstalled, **disabled by default** (`enabled:false`)

## Tools

| Tool | Purpose | Key return |
| --- | --- | --- |
| `udp_bind` | Bind a UDP socket on a port to receive (default 127.0.0.1; `multicast:true` see below) | `{socketId, port, host}` |
| `udp_send` | Send a one-off UDP datagram to host:port | `{host, port, sent}` |
| `udp_close` | Close one or all bound sockets | `{closed}` |
| `udp_simulate_set` | Set response rules on a bound port (replies to the sender) | `{port, ruleCount}` |
| `udp_simulate_clear` | Clear rules on a port | `{port}` |
| `udp_multicast_join` | Join a multicast group on a bound socket | `{address, port}` |
| `udp_multicast_leave` | Leave a multicast group | `{address, port}` |

## State & events

- Each `udp_bind` allocates a `socketId`; internal map is `socketId → {socket, port, multicast:Set}`.
- **Rx events**: `ctx.emit('traffic', {direction:'in', socketId, remote, length, previewHex, payloadTruncated})`.
- **Tx events** (`udp_send` and simulated replies): `direction:'out'`, with destination and byte count.
- **Bind event**: `ctx.emit('event', {subtype:'bound', socketId, port, host, multicast})`.

## Simulation rules

`udp_simulate_set {port, rules}`:

```json
{ "match": "ping", "reply": "pong", "replyFormat": "utf8", "delay": 0 }
```

- `match`: substring match against received utf8/hex; `"*"` / `"echo"` echoes back to the sender.
- `reply`: response body; `replyFormat` defaults to utf8, optional hex.
- `delay`: milliseconds before replying.
- UDP is connectionless, so replies are sent back to the packet's source `(address, port)` automatically.

## Examples

**Bind and echo**
```json
{ "port": 4400 }                                   // udp_bind → socketId
{ "port": 4400, "rules": [ { "match": "*" } ] }     // udp_simulate_set → echo
```

**Send a datagram**
```json
{ "host": "127.0.0.1", "port": 4400, "data": "hello-udp" }   // udp_send
```

**Multicast (local loopback)**
```json
{ "port": 4401, "multicast": true }                              // udp_bind
{ "address": "239.0.0.1", "port": 4401 }                         // udp_multicast_join
// ... send to 239.0.0.1:4401 ...
{ "address": "239.0.0.1", "port": 4401 }                         // udp_multicast_leave
```

## Security model

- **Disabled by default**: ships `enabled:false`; must be explicitly enabled.
- **Loopback-only by default**: plain `udp_bind` binds `127.0.0.1`.
- **Multicast is the one exception**: the OS requires receiving multicast on `0.0.0.0` (INADDR_ANY) — binding 127.0.0.1 cannot receive multicast. So only when `multicast:true` is explicitly passed does it bind `0.0.0.0`, and membership `addMembership(group, '127.0.0.1')` plus multicast loopback are both scoped to the loopback interface; multicast traffic never leaves the host.
- **No privileges**: plain `dgram` sockets; no `CAP_NET_RAW`.
- **Audit**: traffic/event/log events are ring-buffered by Go; `stop()` closes every socket and releases ports.

## Platform limitations

- **Node version**: uses built-in `dgram`, requires Node ≥ 18; the container runs Node 24.
- **Connectionless semantics**: UDP has no connections; `udp_send` is a stateless one-off socket; state accumulates only on `udp_bind` sockets.
- **Multicast bind address**: as above, `multicast:true` binding `0.0.0.0` is a kernel requirement, not giving up loopback isolation — group membership and loopback stay on loopback only. Cross-machine multicast is out of scope.
- **Port release**: ports are released only on `udp_close` or plugin `stop()`; held while the daemon runs.
- **Payload preview**: events carry a ≤256-byte hex preview by default; full packets are not reported back.
