# 串口通讯插件（comm-serial）

基于 [serialport](https://serialport.io/)（`@serialport/bindings-cpp`）的**常驻守护（daemon）插件**，在 aide 容器内长期持有串口句柄：列出串口、打开配置、收发数据，并提供**虚拟串口对（pty）**用于在无硬件时测试与模拟设备。

- 插件目录：`plugins/comm-serial/`（`manifest.json` + `index.js` + `test.js` + 随插件安装的 `node_modules/`）
- 协议：aide 插件协议 v1.2 daemon（`docs/plugins/daemon-protocol.md`）
- 形态：daemon 插件——`enabled:true` 后由 `DaemonManager` 常驻拉起，跨多次工具调用复用已打开的串口
- 预装状态：随仓库预装、**默认禁用**（`enabled:false`）；容器内默认不映射任何真实串口设备

## 工具清单

| 工具 | 用途 | 主要参数 | 主要返回 |
| --- | --- | --- | --- |
| `serial_list` | 列出系统可见串口 | — | `{ports:[{path,manufacturer,serialNumber,vendorId,productId}]}`（无设备时 `[]`，不报错） |
| `serial_open` | 打开并配置串口 | `path`, `baudRate=9600`, `dataBits=8`, `stopBits=1`, `parity=none`, `flowControl=false` | `{ok,path,config}` |
| `serial_write` | 向已打开串口写数据 | `path`, `data`, `format=utf8\|hex` | `{ok,path,bytesWritten}` |
| `serial_close` | 关闭串口 | `path`（不传=关闭全部） | `{ok,closed:[...]}` |
| `serial_virtual_create` | 创建一对互联虚拟串口（pty） | — | `{pairId,masterPath,slavePath,backend}` |
| `serial_virtual_destroy` | 销毁虚拟串口对（连带关闭两端） | `pairId` | `{ok,pairId}` |
| `serial_replay` | 按帧向串口重放数据 | `path`, `frames:[{delay,data,format?}]`, `speed=1`, `recordingId?` | `{ok,path,bytesSent,frames}` |

## 串口参数

| 参数 | 默认 | 取值 |
| --- | --- | --- |
| `baudRate` | 9600 | 任意正整数（常见 9600/19200/38400/57600/115200/230400） |
| `dataBits` | 8 | 5 / 6 / 7 / 8 |
| `stopBits` | 1 | 1 / 1.5 / 2 |
| `parity` | none | none / even / odd / mark / space |
| `flowControl` | false | 布尔（RTS/CTS） |

> 虚拟串口（pty）在底层不校验波特率/校验位，但参数会原样传给串口驱动并在 `serial_open` 返回值中回显，便于核对配置。

## 使用示例

> 由模型通过工具循环调用；路径为**容器内**路径。

**列出并打开**
```json
// serial_list
{ "ports": [ { "path": "/dev/ttyUSB0", "manufacturer": "FTDI", ... } ] }

// serial_open
{ "path": "/dev/ttyUSB0", "baudRate": 115200 }
→ { "ok": true, "path": "/dev/ttyUSB0", "config": { "baudRate": 115200, "dataBits": 8, "stopBits": 1, "parity": "none", "flowControl": false } }
```

**收发**
```json
// serial_write（utf8）
{ "path": "/dev/ttyUSB0", "data": "AT\r\n", "format": "utf8" }
→ { "ok": true, "path": "/dev/ttyUSB0", "bytesWritten": 5 }

// serial_write（hex）
{ "path": "/dev/ttyUSB0", "data": "010300000002c40b", "format": "hex" }
```

收到的数据经 `traffic` 事件上报（见下），进程内保留每端口最多 64KB 接收缓冲。

**测试用虚拟串口对**
```json
// serial_virtual_create
{ "pairId": "vp-1", "masterPath": "/tmp/aide-comm-serial-123-1-a", "slavePath": "/tmp/aide-comm-serial-123-1-b", "backend": "socat" }

// 两端都打开后，写 master，slave 即可收到（回环测试）
// 用完销毁：serial_virtual_destroy { "pairId": "vp-1" }
```

**录制回放**
```json
// serial_replay：按帧定时发送
{ "path": "/dev/ttyUSB0", "frames": [
  { "delay": 0,   "data": "FRAME-A" },
  { "delay": 100, "data": "FRAME-B" }
], "speed": 1 }
```

## 事件上报

| type | 触发时机 | 字段 |
| --- | --- | --- |
| `traffic` | 串口收到/发出数据 | `direction:in\|out`, `length`, `path`, `payloadTruncated:true` |
| `event` | 打开/关闭/错误 | `subtype:open\|close\|error`, `message` |

出于安全与体积，**默认不上报报文正文**，事件只带字节数；Go 侧环形缓存最近 1000 条，经 `GET /api/plugins/daemons/comm-serial/events` 查询。

## 虚拟串口（测试/模拟设备）

`serial_virtual_create` 在容器内造一对互相连通的 pty：写一端，另一端即可读到，无需任何硬件，可用来：

- 调试串口协议解析逻辑；
- 模拟单片机/传感器/Modbus 从站的应答；
- 在 CI 里做端到端回环测试。

后端优先级（运行时自动探测）：

1. **socat**（若 `socat` 在 PATH）：`socat pty,raw,echo=0,link=… pty,raw,echo=0,link=…`，轮询 symlink 指向的字符设备就绪后返回；
2. **python3 标准库**（容器自带 `python3`）：`os.openpty()` 造两个 pty 并双向中继；
3. 两者都缺时报错提示安装 socat。

虚拟对用 `serial_virtual_destroy` 或随 `stop()`/容器退出自动清理。

## 安全模型

- **默认禁用**：`enabled:false`，必须在权限面板显式开启才会常驻拉起；不开启时不占用任何串口。
- **默认无设备**：compose 默认 `cap_drop:[ALL]`、不映射 `/dev`，容器内通常看不到真实串口；只有在 compose 显式加 `--device` 后 `serial_list` 才会出现真实设备。
- **写入需显式打开**：`serial_open` 是显式动作，未打开的路径不可写；`serial_write`/`serial_replay` 仅作用于已打开端口。
- **不留报文正文**：流量事件只上报长度，接收缓冲有界（64KB）且不对外暴露。
- **审计**：open/close/error/traffic 均经 daemon 事件留痕。

## 平台局限

- **真实串口需 `--device` 映射**：容器默认看不到硬件串口。Linux 主机典型设备 `/dev/ttyUSB0`、`/dev/ttyS0`；Windows 为 `COM1` 等；**macOS Docker Desktop 不支持直通 USB/串口设备**（Docker Desktop 的虚拟化层无法把宿主机 USB 串口透传进容器），macOS 上只能用虚拟 pty 对做软件测试。
- **原生模块**：依赖 `serialport` 的 C++ 预编译绑定（`@serialport/bindings-cpp`）。随插件 `npm install`，容器（Node 24 / glibc x64）有官方预编译包；其他架构需自行编译。
- **波特率**：真实设备的波特率必须与设备一致；虚拟 pty 不校验。
- **并发**：同一物理串口同时被两处打开会失败（占用即报错）；关闭后可重新打开。

## compose.yaml 可选串口映射模板

需要访问宿主机真实串口时，在 `compose.yaml` 的 `services.aide.devices` 下追加（默认不启用）：

```yaml
    devices:
      - "/dev/ttyUSB0:/dev/ttyUSB0"   # Linux USB 转串口
      # - "/dev/ttyUSB1:/dev/ttyUSB1"
```

> 加 `devices` 后记得以 root 或对设备节点有读权限的用户运行容器；macOS Docker Desktop 下该映射不生效。
