# UDP 通讯插件（comm-udp）

零第三方依赖的**常驻守护（daemon）插件**，基于 Node 内置 **`dgram`** 模块，用于在回环网络上收发 UDP 数据报、做规则模拟应答与组播加入/离开。面向 UDP 协议调试与设备联调。

- 插件目录：`plugins/comm-udp/`（`manifest.json` + `index.js` + `test.js`）
- 协议：aide 插件协议 **v1.2 daemon**（`docs/plugins/daemon-protocol.md`）
- 形态：daemon 插件（`"daemon": true`），由 `DaemonManager` 管理生命周期
- 预装状态：随仓库预装、**默认禁用**（`enabled:false`）

## 工具清单

| 工具 | 用途 | 主要返回 |
| --- | --- | --- |
| `udp_bind` | 在端口上绑定 UDP socket 收包（默认 127.0.0.1；`multicast:true` 见下） | `{socketId, port, host}` |
| `udp_send` | 一次性发送 UDP 数据报到 host:port | `{host, port, sent}` |
| `udp_close` | 关闭指定或全部绑定 socket | `{closed}` |
| `udp_simulate_set` | 为已绑定端口设置模拟应答规则（回来源） | `{port, ruleCount}` |
| `udp_simulate_clear` | 清除某端口模拟规则 | `{port}` |
| `udp_multicast_join` | 在已绑定端口的 socket 上加入组播组 | `{address, port}` |
| `udp_multicast_leave` | 离开组播组 | `{address, port}` |

## 状态与事件

- 每个 `udp_bind` 分配一个 `socketId`，内部用 `Map(socketId → {socket, port, multicast:Set})` 管理。
- **收包事件**：`ctx.emit('traffic', {direction:'in', socketId, remote, length, previewHex, payloadTruncated})`。
- **发包事件**（`udp_send` 与模拟回复）：`direction:'out'`，带目标地址与字节数。
- **绑定事件**：`ctx.emit('event', {subtype:'bound', socketId, port, host, multicast})`。

## 模拟应答规则

`udp_simulate_set {port, rules}`：

```json
{ "match": "ping", "reply": "pong", "replyFormat": "utf8", "delay": 0 }
```

- `match`：子串匹配收到报文的 utf8/hex；`"*"` / `"echo"` 原样回显给来源。
- `reply`：回复内容；`replyFormat` 默认 utf8，可选 hex。
- `delay`：回复前延迟毫秒。
- UDP 无连接，回复自动发回报文来源 `(address, port)`。

## 使用示例

**绑定并回显**
```json
{ "port": 4400 }                                   // udp_bind → socketId
{ "port": 4400, "rules": [ { "match": "*" } ] }     // udp_simulate_set → echo
```

**发送数据报**
```json
{ "host": "127.0.0.1", "port": 4400, "data": "hello-udp" }   // udp_send
```

**组播（本地回环）**
```json
{ "port": 4401, "multicast": true }                              // udp_bind
{ "address": "239.0.0.1", "port": 4401 }                         // udp_multicast_join
// ... 发送到 239.0.0.1:4401 ...
{ "address": "239.0.0.1", "port": 4401 }                         // udp_multicast_leave
```

## 安全模型

- **默认禁用**：安装后 `enabled:false`，须用户显式开启。
- **默认仅绑回环**：普通 `udp_bind` 绑定 `127.0.0.1`。
- **组播是唯一例外**：操作系统要求接收组播必须绑定 `0.0.0.0`（INADDR_ANY），绑 127.0.0.1 收不到组播。因此仅当显式传 `multicast:true` 时才绑 `0.0.0.0`，且组播组成员关系 `addMembership(group, '127.0.0.1')` 与组播回环均限定在回环接口，组播数据不流出本机。
- **无特权**：普通 `dgram` 套接字，不需要 `CAP_NET_RAW`。
- **审计**：traffic/event/log 事件由 Go 侧环形缓存留痕；`stop()` 关闭全部 socket 并释放端口。

## 平台局限

- **Node 版本**：依赖内置 `dgram`，需 Node ≥ 18；容器为 Node 24。
- **无连接语义**：UDP 无连接，`udp_send` 是无状态一次性 socket；状态只在已 `udp_bind` 的 socket 上积累。
- **组播绑定地址**：如上所述，`multicast:true` 时绑 `0.0.0.0` 是内核强制要求，并非放弃回环隔离——组成员关系与回环仍限定在 loopback。跨机器组播不在支持范围。
- **端口释放**：只在 `udp_close` 或插件 `stop()` 时释放；daemon 运行期间持续占用。
- **载荷预览**：事件默认只带 ≤256 字节 hex 预览，不回传整包。
