# aide 安全审计报告

- 版本：0.1.11.0-RC1
- 日期：2026-09-25
- 分支：feature/permission-panel
- 审计方式：逐行 Read 源码核实（非推断），所有结论附 `文件:行号`
- 审计范围：认证、密码学、传输、静态存储、备份导出、审计日志、记忆隔离、插件、SSH 凭据、WebAuthn、数据完整性

> 风险等级定义：**高**=可被远程/本地未授权利用造成凭据泄露或越权；**中**=在特定前提（本机已被读卷取证、浏览器例外、自签公网暴露）下有影响；**低**=纵深防御缺口，实际利用条件苛刻；**信息**=设计现状记录。

---

## 1. 现状概览

### 1.1 部署与信任模型

aide 是**本地优先（local-first）**的 AI 工作台：后端以单容器运行，默认绑定 `127.0.0.1:8097`（`internal/server/server.go:2047`，`AIDE_ADDR` 可覆盖）。所有状态落在命名 Docker 卷 `/data`，容器与卷均为单用户私有。

- **认证模型**：全程 Bearer Token（`Authorization: Bearer <token>`），不使用 Cookie（`server.go:993-994`）。令牌为 256 位随机数，存 `auth/access-token`（0600），由启动脚本/本地浏览器首次打开时注入。
- **解锁模型**：账户密码（Argon2id PHC）解锁后，经 Argon2id 派生 AES-256 主密钥，驻留内存，用于解密统一 vault、小秘历史与性格密文；锁屏/关闭即清零。
- **传输**：单端口协议自适应，首字节 Peek：`0x16`=TLS 走主服务，否则 308 跳转 HTTPS（`server.go:2049-2061`）。
- **数据流向**：用户对话 → aide 后端 → 用户在设置里显式填写的 AI Provider BaseURL。**默认不向任何第三方遥测回传**；仅当用户配置外部模型 API、edge-tts、音色克隆服务时，相应内容才发往用户指定端点。

### 1.2 数据分层（权威路径见 `internal/server/paths.go:9-46`）

```
/data  (0700, 单用户私有卷)
├── auth/            认证凭据（机密，0600）
├── sessions/        active/ archived/ assistant/
├── assistant/       小秘私有区（voice-history 加密信封 / voice-memory）
├── memory/          core/ cache/ feedback/
├── config/          settings.json（原子写）、backups/
├── stats/           token 计量（可再生）
├── audit/           debug-audit.jsonl / security-audit.jsonl / personality-audit.jsonl（追加，0600）
├── secrets/         vault.enc（AES-256-GCM 信封，0600）、sources-secrets.json
├── certs/           cert.pem(0644) / key.pem(0600)
├── .integrity/      SHA-256 基线 / 恢复日志
└── .quarantine/     损坏用户数据隔离（不自动删）
```

---

## 2. 审计发现（逐项）

### #1 AI APIKey 存储与文件权限

- **现状**：`Settings.APIKey` 以**明文**存于 `config/settings.json`（`server.go:49` `APIKey string json:"apiKey,omitempty"`）。同类明文字段还有 `TTSAPIKey`(:83)、`TTSAzureKey`(:86)、`CloneTTSAPIKey`(:90)。settings.json 经 `atomicJSON` 写入（`server.go:508-530`）：临时文件由 `os.CreateTemp` 创建（Unix 下 0600），写后 rename；所在 `config/` 目录为 0700（`paths.go:252-259`）。
- **代码位置**：`internal/server/server.go:49,83,86,90`；`internal/server/server.go:508-530`；`internal/server/paths.go:142-143`。
- **评估**：APIKey 未进入 AES-256-GCM vault（vault 目前只收 SSH 凭据，见 `secret_vault.go:36-40`）。它靠 0600 文件 + 0700 目录 + Docker 私有卷三层隔离保护；卷被只读取证时可被读出。这是与 SSH 凭据处理不一致的地方。
- **风险等级**：**中**。
- **RC2 后续（0.1.11.0-RC2 已闭环）**：模型 API Key 已迁入统一 vault（条目 `model:api-key`），`settings.json` 恒不存明文；主密钥分层（有密码 Argon2id 派生 / 无密码机器绑定 `/data/secrets/master-key.bin` 0600），旧明文启动自动迁移 + `shredFile` 擦除，`GET /api/config` 仅回 `hasKey`/`hasApiKey`/`vaultUnlocked`。本条"中"风险已消解，详见 [secret-vault.md](./secret-vault.md) §2.1。

### #2 配置备份导出脱敏

- **现状**：`POST /api/config/export` 在 `IncludeSecrets=false` 时，清空 `APIKey/TTSAPIKey/UserPasswordHash/PersonaCiphers/PersonaCipher/DebugTokenHash`（`config_backup.go:49-59`）。`IncludeSecrets=true` 时随附的工作区凭据是**加密信封** `vault.enc`（`config_backup.go:81-85`），仅在无 vault 文件的旧安装回退读旧明文文件（:86-90）。小秘历史仅在 `IncludeVoiceData=true` 时随附（:92-96）。导入侧默认保留当前密钥，仅显式勾选才采用备份值（:157-195）。
- **代码位置**：`internal/server/config_backup.go:49-96,157-195`。
- **评估**：脱敏字段清单完整；敏感材料随附时保持密文不回退明文。回滚点 `settings.json.pre-import` 写 0600（:154）。
- **风险等级**：**通过（低风险）**。

### #3 审计日志是否含敏感正文

- **现状**：
  - `debug-audit.jsonl` 条目仅含 `Time/IP/UA/Method/Path/Owner/Result`（`debug.go:53-62`），**不含请求体/对话正文/令牌**；写 0600（`debug.go:191`）。
  - `personality-audit.jsonl` 条目仅含 `At/ID/Trigger/Result/OldLen/NewLen/Evolutions/Note`（`personality_evolution.go:65-75`），记录的是演化结果与提示词长度，**不含提示词正文**；写 0600（:391）。
  - debug 响应白名单只回 `hasKey` 布尔，不回 key 本体（`debug.go:223,269`）；诊断包不含消息原文（`debug.go:492-561`）。
- **代码位置**：`internal/server/debug.go:53-62,176-200`；`internal/server/personality_evolution.go:65-75,390-395`。
- **评估**：审计日志只记动作与元数据，不记正文，符合"只记动作不记内容"。
- **风险等级**：**通过**。

### #4 access-token 生成熵与文件权限

- **现状**：`newID()` 取 16 个 `crypto/rand` 字节、hex 编码（`server.go:481-487`）；access-token = `newID()+newID()` = **32 字节 = 256 位熵**，hex 后 64 字符（`server.go:566`）。写 `auth/access-token`，权限 0600（:567）。加载后校验长度 ≥32（:574）。
- **代码位置**：`internal/server/server.go:481-487,564-577`。
- **评估**：256 位熵满足高强度本地令牌要求；0600 权限正确。调试令牌同理 32 字节 256 位，落盘仅存 SHA-256 哈希（`debug.go:33,566-576`）。
- **风险等级**：**通过**。

### #5 Cookie 属性

- **现状**：**不使用 Cookie**。认证全程 Bearer Token；对 `/api/` 做 Origin 校验（跨站 Origin Host 不匹配即 403，`server.go:1013-1019`）。SSE/图片标签无法带头时，仅 `/events` 与 `/api/file/raw` 允许 `?access_token=`，其余 API 不收 URL 凭据（:1020-1025）。
- **代码位置**：`internal/server/server.go:993-1030`。
- **评估**：无 Cookie 即无 CSRF Cookie 携带风险；Origin 校验提供 CSRF 纵深。
- **风险等级**：**通过**。

### #6 安全响应头

- **现状**（`server.go:995-1007`）：
  - `X-Content-Type-Options: nosniff`（:995）
  - `Referrer-Policy: no-referrer`（:996）
  - `Content-Security-Policy`：默认 `default-src 'self'; frame-ancestors 'none'; base-uri 'none'`（:1000）；drawio vendor 路径放宽 eval/inline（:998）
  - `Strict-Transport-Security`：仅在**非回环** HTTPS 时下发 `max-age=300`（:1004-1005，`strictTransportSecurity` :1944-1950）
  - `Cache-Control: no-store`（:1007）
- **代码位置**：`internal/server/server.go:995-1007,1944-1950`。
- **评估**：未单独下发 `X-Frame-Options`，但 CSP `frame-ancestors 'none'` 对现代浏览器等效；旧浏览器（< CSP3）缺失该头属已知残余。HSTS 对 localhost 回环豁免（合理，避免浏览器永久改写本机 http）。
- **风险等级**：**低**（缺 X-Frame-Options 旧浏览器兼容头）。

### #7 历史/小秘记录加密

- **现状**：
  - `assistant/voice-history.json`：有加密信封 `{encrypted, cipher}`，AES-256-GCM base64（`voice_agent.go:53-58`）；持久化 0600（:114）；锁定态只保留密文不持有明文（:106-107）；密钥由账户密码 Argon2id 派生，旧 SHA-256 密钥自动迁移重封装（:416-454）。
  - `assistant/voice-memory.json`：当前以**明文 JSON** 直接反序列化加载（`voice_agent.go:92-94`），未见加密信封或写入加密逻辑。
- **代码位置**：`internal/server/voice_agent.go:53-58,92-115,416-454`。
- **评估**：对话历史已加密；长期记忆文件当前未走加密信封。其内容为小秘对用户习惯的备注，属个人数据。这是与"小秘记录全部加密"目标的差距。
- **风险等级**：**中**（voice-memory.json 明文）。

### #8 TLS 配置

- **现状**：`MinVersion: tls.VersionTLS12`（`server.go:1873`）；TLS1.2 套件仅保留 ECDHE + AES-GCM（:1874-1879，含 ECDSA/RSA × AES128/256）；`PreferServerCipherSuites`（:1880）；TLS1.3 由 Go 自动协商。自签证书 SAN 固定 `DNS:localhost` + `IP:127.0.0.1` + `::1`（:1909-1910），ECDSA P-256，私钥 0600、证书 0644（:1923-1928）。
- **代码位置**：`internal/server/server.go:1869-1930,2047-2061`。
- **评估**：最低版本、前向保密、AEAD、私钥权限均达标。残余：自签证书浏览器不默认信任（需手动例外）；证书 3650 年有效期过长（:1906）。
- **风险等级**：**低**（自签 + 长有效期；仅回环使用）。

### #9 密码哈希

- **现状**：`HashPassword` 输出 PHC `$argon2id$v=19$m=65536,t=3,p=4$<b64salt>$<b64hash>`（`kdf.go:78-88`），参数 m=64MiB/t=3/p=4/salt=16B/keyLen=32（:28-34）。`VerifyPassword` 同时识别 PHC(Argon2id) 与旧 64hex(SHA-256)，命中旧格式返回 `needsUpgrade=true`，登录成功后调用方升级并重加密（:111-124）。比较用 `subtle.ConstantTimeCompare`（:154）。
- **代码位置**：`internal/server/kdf.go:27-34,78-124,126-155`。
- **评估**：Argon2id 参数对齐 OWASP 基线；旧 SHA-256 自动迁移闭环。
- **风险等级**：**通过**。

### #10 KDF 密钥派生

- **现状**：`DeriveAESKey` 用 Argon2id + 本机固定 `auth/kdf-salt.bin`（16B crypto/rand，0600，一次性生成永久保留，`kdf.go:43-63`）派生 32B AES-256 密钥（:67-69）。该主密钥用于 vault、小秘历史、性格密文；改密码时 vault `ReWrap` 全部条目（`secret_vault.go:262-283`）。
- **代码位置**：`internal/server/kdf.go:36-69`；`internal/server/secret_vault.go:260-283`。
- **评估**：固定 salt 做域分离（防跨站彩虹表），主密钥不落盘，改密重封装。
- **风险等级**：**通过**。

### #11 统一 secret vault

- **现状**：`secrets/vault.enc`，目录 0700、文件 0600（`secret_vault.go:27-33`）；AES-256-GCM 信封，每条目独立 12B nonce（:303-314）；原子写（:148-167）。`List()` 只回元数据（ID/类型/指纹/时间），**绝不回密文或明文**（:63-70,249-258）；`Get()` 仅在已解锁时回明文，调用方用完 `zeroBytes` 清零（:199-208,194）。API 侧 `workspaceConfigOut` 只回 `hasPassword/hasKey/fingerprint/vaultLocked` 布尔（`workspace_config.go:275-288`）。
- **代码位置**：`internal/server/secret_vault.go:27-33,139-167,249-258`；`internal/server/workspace_config.go:261-289`。
- **评估**：加密信封、最小权限、GET 不泄露正文均达标。
- **风险等级**：**通过**。

### #12 数据目录分层与完整性

- **现状**：`paths.go` 是 /data 命名卷路径的唯一权威来源（:6-7 注释）；`EnsureDirs` 全量 0700 创建分层目录（:252-259）。`integrity.go`：首次启动 `BuildBaseline` 对二进制算 SHA-256（:61-84）；启动 `VerifyIntegrity` 校验二进制哈希/目录结构/关键文件可解析（:101-161）；`SelfHeal` 只重建缺失目录与基线，损坏用户数据移入 `.quarantine` **绝不自动删**（:188-198）；后台每 5 分钟巡检（:248-265）。
- **代码位置**：`internal/server/paths.go:9-46,233-259`；`internal/server/integrity.go:61-84,101-205,248-265`。
- **评估**：分层清晰、基线+自愈+隔离边界正确，用户数据不被自动删除。
- **风险等级**：**通过**。

### #13 记忆访问控制

- **现状**：`canAccessMemory` 策略矩阵（`memory_access.go:77-96`）：
  - `memory/core/`（aide 记忆）：aide 读写；小秘只读不写。
  - `assistant/`（小秘私有区）：aide 一律禁读禁写；小秘读写。
  - 判断依据是**目录归属**而非文件名巧合（:18-21）。`shellTouchesAssistantZone` 在沙箱 shell 层补强，拦截 `/data/assistant/` 前缀及文件名兜底（:109-122）。
- **代码位置**：`internal/server/memory_access.go:56-96,109-122`。
- **评估**：双向隔离 + 纵深断言，符合单向可见设计。
- **风险等级**：**通过**。

### #14 插件安全

- **现状**：
  - 默认禁用：无 `plugins/registry.json` 时注册表为空（`plugins.go:52-54`）。
  - 插件经 node 子进程运行，环境被裁剪为 `PATH=...; HOME=/home/aide`（`plugins.go:90,313`；`plugin_daemon.go:267`）。
  - daemon 插件由 DaemonManager 托管；管理器自身不监听端口，协议插件被强制 `AIDE_DAEMON_BIND=127.0.0.1`（`plugin_daemon.go:10,267`）。
  - 代码体积上限 256KiB、最多 50 个、10s/70s 超时（`plugins.go:24-26,98-115`）。
  - 未见 `CAP_NET_RAW` 等额外 Linux capability（Docker 默认不授予）。
- **代码位置**：`internal/server/plugins.go:48-95,97-131`；`internal/server/plugin_daemon.go:10,267`。
- **评估**：默认禁用、回环绑定、裁剪环境、超时均达标。插件生命周期事件写 stdout 日志（`log.Printf`），未写入 audit JSONL，审计完整性略弱。
- **风险等级**：**低**（插件审计未入结构化日志；Serial 插件进行中，待最终确认）。

### #15 SSH 私钥存储

- **现状**：粘贴/导入副本的私钥经 `vault.Put(VaultIDWSKey,...)` 加密入 vault（`workspace_config.go:453,488`）；运行时解密写 0600 临时文件、`defer os.Remove` 主连接建立后即删（`ssh_session.go:84-98`）。指纹仅回显 SHA256 公钥指纹，私钥正文不回显、不入日志（`ssh_keyutil.go:6,28-51`）。
- **代码位置**：`internal/server/workspace_config.go:442-493`；`internal/server/ssh_session.go:84-98`；`internal/server/ssh_keyutil.go:28-51`。
- **评估**：加密存储 + 临时文件 0600 即用即删 + 不回显，达标。
- **风险等级**：**通过**。

### #16 WebAuthn

- **现状**：RPID 固定 `localhost`（IP 字面量不合法，`webauthn.go:81`）；RPOrigins 含 `https://localhost:<port>`、`https://127.0.0.1:<port>`、`http://localhost:<port>`（:83-86）；`UserVerification: VerificationPreferred`（:182）；`ConveyancePreference: PreferNoAttestation`（:186）。凭证存 `auth/webauthn-credentials.json`，经 `atomicJSON` 写 0600（:111-113）。
- **代码位置**：`internal/server/webauthn.go:75-86,111-113,182-186`。
- **评估**：RPID/RPOrigins 正确绑定本机回环；含一项 `http://localhost` origin 是为兼容浏览器旧标签（自签 HTTPS 切换前的缓存页面），属有意取舍。
- **风险等级**：**低**（http://localhost origin 白名单项）。

---

## 3. 已修复项（#29 安全批次及后续任务）

| 项 | 旧状态 | 新状态 | 证据 |
| --- | --- | --- | --- |
| 密码哈希 | 裸 SHA-256（64hex） | Argon2id PHC（m=64MiB,t=3,p=4），旧哈希登录即升级重加密 | `kdf.go:4-12,111-124` |
| 传输加密 | 无 TLS，明文 HTTP | TLS≥1.2 + ECDHE/AES-GCM，单端口 mux 自动 308 跳转 | `server.go:1869-1882,2049-2061` |
| 配置备份脱敏 | 早期版本可能漏带敏感字段 | 导出默认清空 APIKey/密码哈希/Persona 密文/DebugTokenHash；随附凭据保持加密信封 | `config_backup.go:49-59,79-90` |
| 审计日志缺失 | 无外部接入审计 | debug-audit.jsonl（只记元数据）+ personality-audit.jsonl（只记演化结果） | `debug.go:53-62`、`personality_evolution.go:65-75` |
| SSH 凭据明文 | workspace-secrets.json 明文 | 统一 AES-256-GCM vault，旧明文自动迁移并改名归档 | `secret_vault.go`、`workspace_config.go:107-136` |
| 数据平铺混乱 | /data 根平铺 | 分层目录 auth/sessions/assistant/memory/config/stats/audit/secrets/certs + 迁移归位 | `paths.go`、`migration.go:44-58` |
| 无完整性校验 | 无 | SHA-256 二进制基线 + 启动校验 + 5 分钟巡检 + 损坏隔离不自动删 | `integrity.go` |
| 记忆越权 | aide 可读小秘区 | 单向可见：aide 禁读 assistant/，小秘只读 aide 记忆；shell 层补强 | `memory_access.go:77-122` |

---

## 4. 残余风险（如实列明）

1. **自签证书公网暴露**：证书为自签 SAN=localhost/127.0.0.1，仅回环可信。若运维方用端口映射把 8097 暴露到公网（`AIDE_ADDR=0.0.0.0`），浏览器不信任证书且存在中间人风险。**默认绑定 127.0.0.1 已缓解**；公网部署须换受信证书。
2. **HSTS localhost 豁免**：回环主机不发 HSTS（`server.go:1946`），这是刻意设计，但意味着本机 http→https 跳转依赖每次 308，非浏览器客户端需自行升级。
3. **GDPR 数据出境**：当用户配置外部 AI Provider（如 DeepSeek/云端模型）或 edge-tts/音色克隆时，对话内容会发往用户指定的第三方端点。aide 不做跨境传输担保，数据出境合法性由用户与第三方之间的协议负责（见 eu-compliance.md）。
4. **eIDAS 不适用**：aide 是本地自托管工具，不提供电子签名/远程身份认证服务，不构成 eIDAS 下的合格信任服务提供商（QTSP）。
5. **edge-tts 服务端风控**：edge-tts 复用微软 Edge 在线朗读端点，受其服务端限流/风控影响；这是外部服务可用性问题，非 aide 安全缺陷。
6. **插件无 CAP_NET_RAW（可选）**：当前 comm 插件不授予 raw socket capability；若未来某插件确需抓包能力，须单独显式授权并重新评估。
7. ~~**APIKey 明文落 settings.json**：见 #1，未纳入 AES vault。~~ **RC2 已闭环**：模型 API Key 迁入 vault（见 #1 RC2 后续）。
8. **voice-memory.json 明文**：见 #7，长期记忆未走加密信封。
9. **WebAuthn 含 http://localhost origin**：见 #16，为兼容旧标签的有意取舍。
10. **插件审计未入结构化日志**：见 #14，插件生命周期事件走 stdout，未进 audit JSONL。

---

## 5. 后续建议

1. ~~将 AI `APIKey` 迁入统一 vault，与 SSH 凭据对齐（对应 #1）。~~ **RC2 已完成**（模型 API Key 入 vault；`TTSAPIKey/TTSAzureKey/CloneTTSAPIKey` 仍为预留字段，后续按需迁移）。
2. 为 `voice-memory.json` 增加与 voice-history 一致的 AES-256-GCM 加密信封（对应 #7）。
3. 补一个 `X-Frame-Options: DENY` 头覆盖旧浏览器（对应 #6）。
4. 插件生命周期事件追加写入 `audit/security-audit.jsonl`，与 debug/security 审计体系统一（对应 #14）。
5. 公网部署文档明确：必须换受信证书、不得依赖自签；提供证书挂载路径说明。
6. 缩短自签证书有效期或改为首次启动生成、可用户导出手动信任。
