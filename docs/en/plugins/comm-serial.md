# Serial Port Plugin (comm-serial)

A **resident daemon plugin** built on [serialport](https://serialport.io/) (`@serialport/bindings-cpp`) that holds serial port handles long-term inside the aide container: list ports, open/configure, read/write data, and create **virtual serial pairs (PTY)** for testing and device simulation without hardware.

- Plugin dir: `plugins/comm-serial/` (`manifest.json` + `index.js` + `test.js` + the plugin-local `node_modules/`)
- Protocol: aide plugin protocol v1.2 daemon (`docs/en/plugins/daemon-protocol.md`)
- Shape: daemon plugin — once `enabled:true`, `DaemonManager` keeps it alive and reuses opened ports across tool calls
- Bundled state: shipped with the repo, **disabled by default** (`enabled:false`); no real serial device is mapped into the container by default

## Tools

| Tool | Purpose | Key args | Main return |
| --- | --- | --- | --- |
| `serial_list` | List visible serial ports | — | `{ports:[{path,manufacturer,serialNumber,vendorId,productId}]}` (empty `[]` when no device, no error) |
| `serial_open` | Open and configure a port | `path`, `baudRate=9600`, `dataBits=8`, `stopBits=1`, `parity=none`, `flowControl=false` | `{ok,path,config}` |
| `serial_write` | Write to an opened port | `path`, `data`, `format=utf8\|hex` | `{ok,path,bytesWritten}` |
| `serial_close` | Close a port | `path` (omit = close all) | `{ok,closed:[...]}` |
| `serial_virtual_create` | Create an interconnected virtual serial pair (PTY) | — | `{pairId,masterPath,slavePath,backend}` |
| `serial_virtual_destroy` | Destroy a virtual pair (closes both ends) | `pairId` | `{ok,pairId}` |
| `serial_replay` | Replay frames to a port | `path`, `frames:[{delay,data,format?}]`, `speed=1`, `recordingId?` | `{ok,path,bytesSent,frames}` |

## Serial parameters

| Param | Default | Values |
| --- | --- | --- |
| `baudRate` | 9600 | any positive int (common 9600/19200/38400/57600/115200/230400) |
| `dataBits` | 8 | 5 / 6 / 7 / 8 |
| `stopBits` | 1 | 1 / 1.5 / 2 |
| `parity` | none | none / even / odd / mark / space |
| `flowControl` | false | boolean (RTS/CTS) |

> Virtual (PTY) ports do not validate baud/parity at the driver level, but parameters are passed through and echoed back in `serial_open`'s result so they can be verified.

## Examples

> Driven by the model via the tool loop; paths are **inside the container**.

**List and open**
```json
// serial_list
{ "ports": [ { "path": "/dev/ttyUSB0", "manufacturer": "FTDI" } ] }

// serial_open
{ "path": "/dev/ttyUSB0", "baudRate": 115200 }
→ { "ok": true, "path": "/dev/ttyUSB0", "config": { "baudRate": 115200, "dataBits": 8, "stopBits": 1, "parity": "none", "flowControl": false } }
```

**Read / write**
```json
// serial_write (utf8)
{ "path": "/dev/ttyUSB0", "data": "AT\r\n", "format": "utf8" }
→ { "ok": true, "path": "/dev/ttyUSB0", "bytesWritten": 5 }

// serial_write (hex)
{ "path": "/dev/ttyUSB0", "data": "010300000002c40b", "format": "hex" }
```

Incoming bytes arrive as `traffic` events (see below); each port keeps an in-process receive buffer of at most 64 KB.

**Virtual pair for testing**
```json
// serial_virtual_create
{ "pairId": "vp-1", "masterPath": "/tmp/aide-comm-serial-123-1-a", "slavePath": "/tmp/aide-comm-serial-123-1-b", "backend": "socat" }

// open both ends: write master, slave receives (loopback)
// teardown: serial_virtual_destroy { "pairId": "vp-1" }
```

**Replay**
```json
{ "path": "/dev/ttyUSB0", "frames": [
  { "delay": 0,   "data": "FRAME-A" },
  { "delay": 100, "data": "FRAME-B" }
], "speed": 1 }
```

## Events

| type | When | Fields |
| --- | --- | --- |
| `traffic` | data received / sent | `direction:in\|out`, `length`, `path`, `payloadTruncated:true` |
| `event` | open / close / error | `subtype:open\|close\|error`, `message` |

By design, **payload bodies are never reported**; events only carry byte counts. Go caches the latest 1000 events per daemon, queryable at `GET /api/plugins/daemons/comm-serial/events`.

## Virtual serial pairs (testing / device simulation)

`serial_virtual_create` builds a pair of connected PTYs inside the container: write one end, read the other — no hardware needed. Use it to:

- debug serial protocol parsing;
- simulate MCU / sensor / Modbus slave responses;
- run end-to-end loopback tests in CI.

Backend priority (auto-detected at runtime):

1. **socat** (if on `PATH`): `socat pty,raw,echo=0,link=… pty,raw,echo=0,link=…`; readiness is detected by polling the symlinked char device;
2. **python3 stdlib** (image ships `python3`): `os.openpty()` creates two PTYs with a bidirectional relay;
3. if neither exists, an error tells you to install socat.

Pairs are cleaned up by `serial_virtual_destroy` or on `stop()` / container exit.

## Security model

- **Disabled by default**: `enabled:false`; must be explicitly turned on in the permission panel before it runs.
- **No device by default**: compose drops all caps and maps no `/dev`; `serial_list` stays empty until you add a `--device` mapping.
- **Writes require an explicit open**: only an opened port can be written; `serial_write`/`serial_replay` target open ports only.
- **No payload logging**: traffic events report length only; the receive buffer is bounded (64 KB) and not exposed.
- **Audit**: open/close/error/traffic are all retained as daemon events.

## Platform limitations

- **Real ports need `--device`**: the container cannot see hardware serial ports by default. Linux devices are typically `/dev/ttyUSB0`, `/dev/ttyS0`; Windows uses `COM1`, etc. **macOS Docker Desktop cannot passthrough USB/serial devices** — on macOS you can only test with virtual PTY pairs.
- **Native binding**: depends on the prebuilt C++ binding (`@serialport/bindings-cpp`). It is installed via the plugin-local `npm install`; the container (Node 24 / glibc x64) ships with official prebuilds.
- **Baud rate**: must match the real device; PTYs ignore it.
- **Exclusive open**: opening a physical port twice fails (it is already held); it can be reopened after close.

## Optional serial mapping in compose.yaml

To expose a host serial port, add to `services.aide.devices` in `compose.yaml` (off by default):

```yaml
    devices:
      - "/dev/ttyUSB0:/dev/ttyUSB0"   # Linux USB-to-serial
      # - "/dev/ttyUSB1:/dev/ttyUSB1"
```

> After adding `devices`, run the container as root or a user with read access to the device node. On macOS Docker Desktop this mapping has no effect.
