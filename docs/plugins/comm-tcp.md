# TCP 通讯插件（comm-tcp）

零第三方依赖的**常驻守护（daemon）插件**，基于 Node 内置 **`net`** 模块，用于在回环网络上做 TCP 通讯的探测、录制、回放与模拟。面向协议调试、设备/服务联调与 fuzz 测试。

- 插件目录：`plugins/comm-tcp/`（`manifest.json` + `index.js` + `test.js`）
- 协议：aide 插件协议 **v1.2 daemon**（`docs/plugins/daemon-protocol.md`）——常驻 Node 子进程，stdio 行分隔 JSON-RPC
- 形态：daemon 插件（`manifest.json` 声明 `"daemon": true`），由 `DaemonManager` 拉起、心跳与退避重启
- 预装状态：随仓库预装、**默认禁用**（`enabled:false`），须在权限面板显式开启

## 工具清单

| 工具 | 用途 | 主要返回 |
| --- | --- | --- |
| `tcp_listen` | 在 127.0.0.1 监听端口，记录新连接与双向报文 | `{port, host, recordingId, connections}` |
| `tcp_connect` | 作为客户端连接 host:port | `{connectionId, host, port, recordingId}` |
| `tcp_send` | 向指定连接发送数据（utf8/hex） | `{connectionId, sent}` |
| `tcp_close` | 关闭指定连接或全部 | `{closed}` |
| `tcp_proxy_start` | 本地 MITM 代理：listenPort → targetHost:targetPort，录制双向流量 | `{proxyId, listenPort, targetHost, targetPort}` |
| `tcp_proxy_stop` | 停止代理、释放本地端口 | `{proxyId}` |
| `tcp_simulate_set` | 为已监听端口设置模拟应答规则 | `{port, ruleCount}` |
| `tcp_simulate_clear` | 清除某端口模拟规则 | `{port}` |
| `tcp_replay` | 把录制的 c2s 报文按时间顺序重放到新目标 | `{sentFrames, sentBytes, responseBytes}` |
| `tcp_fuzz` | 对目标发送边界/异常报文，记录结果不崩 | `{probes, survived, results[]}` |

## 状态与事件

插件在常驻进程内维护：`listeners`（端口→`net.Server`）、`connections`（connectionId→`net.Socket`）、`proxies`、`simRules`、`recordings`。

- **流量事件**：每个连接收发包经 `ctx.emit('traffic', {direction, connectionId, recordingId, length, previewHex, payloadTruncated})` 上报；`direction` 为 `in`（本侧收到）/`out`（本侧发出）。默认仅上报长度与最多 256 字节 hex 预览，不带全文。
- **连接事件**：建立/关闭/异常经 `ctx.emit('event', {subtype:'connected'|'closed'|'error', ...})` 上报。
- 事件由 Go 侧环形缓存最近 1000 条，经 `GET /api/plugins/daemons/comm-tcp/events` 查询。

## 模拟应答规则

`tcp_simulate_set {port, rules}` 中每条规则：

```json
{ "match": "foo", "reply": "bar", "replyFormat": "utf8", "delay": 200, "disconnect": false }
```

- `match`：子串匹配收到报文的 utf8 文本（或 hex）。`"*"` / `"echo"` 表示**原样回显**。
- `reply`：回复内容（`match=*` 时忽略，改为回显收到的报文）；`replyFormat` 默认 `utf8`，可选 `hex`。
- `delay`：回复前延迟毫秒。
- `disconnect`：回复后断开连接。
- 按数组顺序命中第一条即停；最多 64 条。

## 使用示例

> 下列由模型通过工具循环调用；地址均为回环。

**起一个回显服务并探测**
```json
{ "port": 4200 }                                   // tcp_listen → recordingId
{ "port": 4200, "rules": [ { "match": "*" } ] }     // tcp_simulate_set → echo
```

**插件作为客户端收发**
```json
{ "host": "127.0.0.1", "port": 4200 }              // tcp_connect → connectionId
{ "connectionId": "c-1", "data": "hello" }          // tcp_send
```

**代理录制后回放**
```json
{ "listenPort": 4300, "targetHost": "127.0.0.1", "targetPort": 4200 }   // tcp_proxy_start
// 外部客户端连 4300 发报文 → 插件录制 c2s/s2c
{ "recordingId": "rec-3", "targetHost": "127.0.0.1", "targetPort": 4201, "speed": 0 }  // tcp_replay
```

**fuzz 一个本地端口**
```json
{ "host": "127.0.0.1", "port": 4200, "count": 8 }  // tcp_fuzz
```

## 安全模型

- **默认禁用**：安装后 `enabled:false`，须用户在权限面板显式开启；`DaemonManager.restore()` 只自动拉起 `enabled:true` 的 daemon。
- **仅绑回环**：`tcp_listen` / `tcp_proxy_start` 一律绑定 `127.0.0.1`（`host` 参数保留但被忽略），不监听局域网/公网。`tcp_connect`/`tcp_replay` 的目标地址可配置，但插件自身不对外开放端口。
- **无特权**：纯 `net` 普通套接字，不需要 `CAP_NET_RAW`，不抓包、不伪造原始帧。
- **审计**：所有 traffic/event/log 事件由 Go 侧环形缓存留痕；停止插件会先 `stop()` 关闭所有 listener/connection/proxy 并释放端口。
- **fuzz 自限**：连接数上限 32，每条探测 3s 超时，结果只记录不崩溃。

## 平台局限

- **Node 版本**：依赖内置 `net`，需 Node ≥ 18；本项目容器为 Node 24。
- **容器网络**：插件看到的网络是容器内网络；回环测试用 127.0.0.1。要代理到容器外服务，需按 `docs/workspace-paths.md` / compose 网络配置可达。
- **端口释放时机**：监听端口只在插件 `stop()`（用户停止/删除插件、stdin EOF / SIGTERM）时释放；daemon 运行期间持续占用。
- **载荷预览**：事件默认只带 ≤256 字节 hex 预览，不回传整包；完整报文在进程内 `recordings` 中保留供回放。
- **回放语义**：`tcp_replay` 只重放录制中的 c2s（客户端→服务端）帧，按录制时间间隔（可 `speed` 缩放）发送；不重放 s2c。
