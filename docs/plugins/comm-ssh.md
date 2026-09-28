# SSH 通讯插件（comm-ssh）

基于 [`ssh2`](https://www.npmjs.com/package/ssh2) 的**常驻守护（daemon）插件**，把 aide 变成一个可脚本化的 SSH 客户端：在一个长驻 Node 进程里维护多条 SSH 连接、SFTP 会话与本地端口转发，并内置一个**可脚本化应答的模拟 SSH 服务端**，用于端到端测试与设备/协议演练。

- 插件目录：`plugins/comm-ssh/`（`manifest.json` + `index.js` + `test.js` + `package.json` + `node_modules/ssh2`）
- 协议：aide 插件协议 **v1.2 daemon**（见 `docs/plugins/daemon-protocol.md`）
- 形态：daemon 插件——一个长驻 `node` 子进程，连接/隧道/模拟服务端在多次工具调用之间保持状态。
- 预装状态：随仓库预装、**默认 `enabled: false`**，须在权限面板显式开启才会被拉起或自动恢复。

## 依赖与安装

- 运行时依赖仅 `ssh2`（纯 JS，无原生编译，兼容 Node 24）。
- 安装（插件目录内）：

  ```bash
  cd plugins/comm-ssh
  npm install ssh2   # 生成 node_modules，随插件目录一起打包
  ```

- 验证（容器内，Node 24）：

  ```bash
  docker run --rm -v "$PWD/plugins/comm-ssh":/app -w /app node:24-bookworm-slim node test.js
  ```

## 工具清单

| 工具 | 用途 | 主要参数 | 主要返回 |
| --- | --- | --- | --- |
| `ssh_connect` | 建立 SSH 连接（密码或公钥） | `host`, `port?=22`, `authType?=password`, `username`, `password?`, `privateKey?`, `passphrase?`, `keyId?` | `{connectionId, host, port, username, authType}` |
| `ssh_exec` | 在连接上执行命令 | `connectionId`, `command`, `timeout?=30000` | `{stdout, stderr, exitCode, signal?}` |
| `ssh_sftp_list` | SFTP 列目录 | `connectionId`, `path` | `{path, entries:[{name,type,size,modifyTime}]}` |
| `ssh_sftp_get` | SFTP 下载远端文件到本地 | `connectionId`, `remotePath`, `localPath` | `{remotePath, localPath, bytes}` |
| `ssh_sftp_put` | SFTP 上传本地文件到远端 | `connectionId`, `localPath`, `remotePath` | `{localPath, remotePath, bytes}` |
| `ssh_tunnel_start` | 本地端口转发（本地监听 → 经 SSH → remoteHost:remotePort） | `connectionId`, `localPort`(0=随机), `remoteHost`, `remotePort` | `{tunnelId, localPort, remoteHost, remotePort, bind}` |
| `ssh_tunnel_stop` | 停止隧道、释放本地端口 | `tunnelId` | `{ok, closed}` |
| `ssh_close` | 关闭指定连接；省略则关闭全部 | `connectionId?` | `{closed:[connectionId]}` |
| `ssh_simulate_start` | 在 127.0.0.1 启动模拟 SSH 服务端 | `port`(0=随机), `hostKey?`, `authPassword?=true`, `expectedPassword?`, `publickey?=false` | `{serverId, port, host, sftpRoot, ...}` |
| `ssh_simulate_stop` | 停止模拟服务端 | `serverId` | `{ok, stopped, recordedSessions}` |
| `ssh_simulate_set_rule` | 设置模拟命令应答规则 | `serverId`, `rules:[{commandMatch, reply, exitCode?, stderr?, delay?, regex?}]` | `{serverId, count}` |

## 使用示例

1. 起一个模拟服务端，固定密码，设置命令应答：

   ```json
   // ssh_simulate_start
   { "port": 0, "expectedPassword": "secret" }
   // → { "serverId": "s-1-...", "port": 52210, ... }

   // ssh_simulate_set_rule
   { "serverId": "s-1-...", "rules": [
       { "commandMatch": "uname -a", "reply": "Linux mockbox 6.1.0 x86_64\n" },
       { "commandMatch": "^df ", "regex": true, "reply": "/dev/sim  1G  50M  950M  5% /\n" }
   ]}
   ```

2. 用密码连接并执行：

   ```json
   // ssh_connect
   { "host": "127.0.0.1", "port": 52210, "username": "tester", "authType": "password", "password": "secret" }
   // → { "connectionId": "c-1-...", ... }

   // ssh_exec
   { "connectionId": "c-1-...", "command": "uname -a" }
   // → { "stdout": "Linux mockbox 6.1.0 x86_64\n", "stderr": "", "exitCode": 0 }
   ```

3. SFTP 与隧道：

   ```json
   // ssh_sftp_put
   { "connectionId": "c-1-...", "localPath": "/local/notes.txt", "remotePath": "/home/tester/notes.txt" }
   // ssh_tunnel_start：把本地 127.0.0.1:0 经该 SSH 转发到内网 192.168.10.5:8080
   { "connectionId": "c-1-...", "localPort": 0, "remoteHost": "192.168.10.5", "remotePort": 8080 }
   ```

## 安全模型

- **默认禁用**：`enabled: false`。daemon 管理器只对同时声明 `"daemon": true` 且 `enabled: true` 的插件自动拉起；未开启时不会监听任何端口、不会外连。
- **仅绑回环**：插件自身的所有监听套接字（模拟 SSH 服务端、隧道本地监听）一律绑定 `AIDE_DAEMON_BIND`（默认 `127.0.0.1`），不暴露到局域网/公网。外连目标（`ssh_connect.host`）由调用方指定。
- **凭证不回显、不落盘**：
  - 密码 / 私钥 / 私钥口令按连接调用**临时传入**，仅驻留本进程内存；连接关闭（`ssh_close` 或断开）即丢弃引用。
  - 工具返回值、错误信息、`ctx.emit` 事件与 stderr 审计中**绝不包含**密码或私钥正文；`ssh_exec` 流量事件只上报方向与字节数（`payloadTruncated: true`），不上报命令/输出正文。
  - `keyId` 是**对统一 secret vault**（`docs/security/secret-vault.md`，`/data/secrets/vault.enc`，AES-256-GCM + Argon2id）的引用占位：持有解锁主密钥的编排层在调用前把 `keyId` 解析为 `password` / `privateKey` 再注入参数。插件本身不持有 vault 主密钥、不能解密 vault。
- **模拟服务端不碰真实主机**：SFTP 子系统后端为进程内临时目录（`os.tmpdir()` 下），路径做根内校验；不读写容器其它位置。
- **审计留痕**：连接建立/断开、隧道上下线、模拟服务端事件经 `ctx.emit('event'|'traffic'|'log')` 上报，由 Go 侧环形缓存（最近 1000 条）留痕，可经 `GET /api/plugins/daemons/comm-ssh/events` 查询。

## 模拟 SSH 服务端的用途

`ssh_simulate_*` 系列把 ssh2 反向用作服务端，面向两类场景：

1. **端到端测试**：不依赖任何真实/外网 SSH 服务器即可验证 SSH 客户端行为（连接、认证失败、SFTP 往返、端口转发），全部走 127.0.0.1。
2. **协议演练/录制**：`ssh_simulate_set_rule` 按命令精确匹配、`*` 通配或正则给出脚本化应答（可带 `delay`、`exitCode`、`stderr`），用于在没有真实设备时演练自动化脚本、复现对端行为。
   - 认证策略：`authPassword:false` 关闭密码登录；`expectedPassword` 限定唯一密码；`publickey:true` 开启公钥登录（演练态接受任意合法私钥）。
   - 模拟服务端还实现了 `sftp` 子系统（临时目录后端）与 `direct-tcpip`（隧道透传），使隧道与 SFTP 工具可在无真实服务器时被完整验证。

## 平台局限

- **Docker 网络/端口映射**：隧道与模拟服务端只绑容器内 `127.0.0.1`；从宿主机访问容器内的模拟端口或隧道需经 aide 已发布的端口/反向通道，不能直接 `docker -p` 额外映射一个随机端口。
- **SFTP 本地路径**：`ssh_sftp_get/put` 的 `localPath` 是**容器内文件系统**路径（插件进程视角），不是宿主机路径；写入位置受 aide 挂载根约束。
- **公钥精确授权**：模拟服务端 `publickey:true` 当前接受任意合法私钥（演练态）；不校验具体公钥指纹，仅供测试，不作为真实访问控制。
- **未覆盖（NOT_RUN）**：真实外部 SSH 服务器连接、主机公钥指纹校验（`hostVerifier`）、跳板机/多跳连接、X11/端口反向转发（remote→local）未实现，需要时再扩展。
