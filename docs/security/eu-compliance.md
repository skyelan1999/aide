# aide 欧盟合规说明（GDPR / ePrivacy / OWASP / ENISA / NIST / eIDAS）

- 版本：0.1.11.0-RC1
- 日期：2026-09-25
- 适用部署：本地优先、自托管（单用户私有 Docker 卷）
- 配套：[security-audit.md](./security-audit.md)、[privacy-policy-template.md](./privacy-policy-template.md)、[dpia-template.md](./dpia-template.md)

> 定位声明：aide 是**装在用户自己机器上的工具**，不是云服务/SaaS。默认情况下，对话、记忆、凭据都只存在用户本机卷内；只有当用户**显式**填写外部 AI Provider、edge-tts、音色克隆服务时，相应数据才发往用户指定的端点。这决定了它在 GDPR 下通常被视为"个人在家庭活动中处理数据"，但下列控制项仍按可交付工程措施如实列出。

---

## 1. GDPR 逐项对照

| 条款 | 要求 | aide 措施 | 状态 | 证据 |
| --- | --- | --- | --- | --- |
| Art.5(1)(a) 合法/公正/透明 | 处理须有合法基础并对用户透明 | 本地自托管，用户即数据主体兼控制者；隐私政策模板见 privacy-policy-template.md | 满足 | 本文档；privacy-policy-template.md |
| Art.5(1)(b) 目的限制 | 数据不得超出收集目的 | AI 对话仅用于用户发起的工作；不做广告画像、不做遥测 | 满足 | server.go 默认无外联；仅用户配置 Provider 时外发 |
| Art.5(1)(c) 数据最小化 | 仅收集必要数据 | 审计日志只记动作元数据不记正文；vault List 不回密文 | 满足 | debug.go:53-62；secret_vault.go:249-258 |
| Art.5(1)(d) 准确 | 保持数据准确 | 会话/记忆由用户可见可删；出厂重置可清空 | 满足 | factory_reset.go:213-247 |
| Art.5(1)(e) 存储限制 | 不超期保存 | 数据驻本机，用户可随时删除/导出/重置；调试令牌 365 天过期可吊销 | 部分 | debug.go:34,594-606；用户删除=删除卷 |
| Art.5(1)(f) 完整性与保密性 | 安全处理 | TLS1.2+、Argon2id、AES-256-GCM、0600 文件、vault | 满足 | 见第 3/4 节 |
| Art.25 设计隐私/默认隐私 | 默认隐私设置 | 默认不启用外部诊断接口（DebugAccessEnabled=false）；插件默认禁用；无 Cookie | 满足 | server.go:153；plugins.go:52-54；server.go:993 |
| Art.32 处理安全 | 技术与组织措施(TOMs) | 见第 3 节 TOMs 表 | 满足 | 见第 3 节 |
| Art.33 泄露通知 | 72 小时通知监管 | 本地单用户部署无集中运营方；泄露场景=本机失陷，由用户自行响应。应用内审计日志可支持事后追溯 | 部分 | debug-audit.jsonl；personality-audit.jsonl |
| Art.35 DPIA | 高风险处理须做 DPIA | 本地自托管风险低；提供 dpia-template.md 供高敏感部署填写 | 满足（提供模板） | dpia-template.md |
| Art.13/14 告知 | 向数据主体告知 | 隐私政策模板列清数据收集/位置/保留/权利 | 满足 | privacy-policy-template.md |
| Art.15-22 数据主体权利 | 访问/更正/删除/可携带/反对 | 本地直接操作：导出配置/对话、删除、出厂重置 | 满足 | config_backup.go；factory_reset.go |
| Art.28 处理者 | 与处理者签 DPA | 用户自带 Provider 时，用户与其 API 供应商之间自行约定；aide 本身不充当处理者 | 部分 | 见第三方服务节 |

---

## 2. ePrivacy 指令

| 主题 | 现状 | 证据 |
| --- | --- | --- |
| Cookie/终端设备信息 | aide Web 界面**不使用任何 Cookie**，纯 Bearer Token 鉴权，不设追踪 Cookie、不埋点 | server.go:993-994 |
| 通信保密 | 浏览器↔后端全程 TLS1.2+；本地回路不发 HSTS（避免误改本机 http） | server.go:1869-1882,1944-1950 |
| 终端设备接入 | 不向终端设备写入任何持久化追踪存储；浏览器侧仅内存态会话 | 前端无 localStorage 凭据 |

---

## 3. OWASP ASVS v4 对照

| 类别 | 要求要点 | aide 措施 | 状态 | 证据 |
| --- | --- | --- | --- | --- |
| V2 认证 | 安全密码存储、安全会话凭证、抗爆破 | Argon2id PHC；access-token 256 位随机；常量时间比较 | 满足 | kdf.go:78-88；server.go:566,1026 |
| V2 认证 | 多因素/设备解锁 | WebAuthn(Touch ID) 可选解锁；UserVerification preferred | 满足 | webauthn.go:182 |
| V4 访问控制 | 服务端访问控制、IDOR 防护、最小权限 | 每请求 Bearer 校验；记忆区双向隔离；vault 解锁态才取明文 | 满足 | server.go:1012-1029；memory_access.go:77-96 |
| V4 访问控制 | 安全默认配置 | 诊断接口默认关；插件默认禁；管理面只认主令牌 | 满足 | debug.go:100-104；plugins.go:52-54；debug.go:135-139 |
| V6 密码学 | 受密码保护数据加密、传输加密、强随机数 | AES-256-GCM 信封；TLS1.2+/ECDHE/AES-GCM；crypto/rand | 满足 | secret_vault.go:303-314；server.go:1869-1882 |
| V6 密码学 | 弱算法禁用 | 旧 SHA-256 仅用于迁移解密，登录即升级 Argon2id；TLS<1.2 拒绝 | 满足 | kdf.go:111-124；server.go:1873 |
| V14 配置 | 安全 HTTP 头、无敏感信息泄露 | nosniff/no-referrer/CSP/no-store；审计不记正文 | 满足 | server.go:995-1007；debug.go:53-62 |

---

## 4. OWASP Password Storage Cheat Sheet（Argon2id 参数对照）

| OWASP 建议（Argon2id） | aide 实际 | 证据 |
| --- | --- | --- |
| 算法 | Argon2id | `argon2.IDKey` | kdf.go:68 |
| 内存 m | ≥ 19 MiB（建议 6.5–64） | **65536 KiB = 64 MiB** | kdf.go:30 |
| 迭代 t | ≥ 2–3 | **3** | kdf.go:29 |
| 并行度 p | 1–4 | **4** | kdf.go:31 |
| 盐长度 | ≥ 16 字节 | **16 字节** crypto/rand | kdf.go:33,80 |
| 输出长度 | ≥ 16 字节 | **32 字节** | kdf.go:32 |
| 格式 | PHC 串 | `$argon2id$v=19$m=...,t=...,p=...$...$...` | kdf.go:84-87 |
| 常量时间比较 | 是 | `subtle.ConstantTimeCompare` | kdf.go:154 |

---

## 5. ENISA 密码算法建议对照

| ENISA 推荐 | aide 实现 | 证据 |
| --- | --- | --- |
| 对称加密用 AEAD（GCM/ChaCha20） | AES-256-GCM，12B nonce | secret_vault.go:295-314；persona.go:78-95 |
| 密钥派生用慢 KDF（Argon2id/scrypt） | Argon2id | kdf.go:67-69 |
| 传输用 TLS≥1.2、前向保密 | TLS1.2 min，ECDHE 套件 | server.go:1873-1879 |
| 随机数用密码学安全 RNG | crypto/rand | kdf.go:55；server.go:483；secret_vault.go:309 |

---

## 6. eIDAS

**不适用。** aide 是本地自托管生产力工具，不提供电子签名、电子签章、时间戳、电子递送或远程身份认证等合格信任服务（QTSP/ASSP）。其 WebAuthn 解锁仅为本地设备解锁，不构成 eIDAS 下的电子身份识别手段。

---

## 7. NIST SP 800-63B / 175B 对照

| 项 | NIST 建议 | aide 实现 | 证据 |
| --- | --- | --- | --- |
| 密码哈希 | 慢哈希，自适应参数 | Argon2id（对齐 800-63B 记忆型 KDF 取向） | kdf.go:28-34 |
| 密钥管理 | 主密钥不落盘、可轮换 | 主密钥内存驻留；改密 ReWrap | secret_vault.go:107-115,262-283 |
| 凭证文件权限 | 最小权限 | 0600 文件 / 0700 目录 | secret_vault.go:31-32；paths.go:254 |
| 远程证明 | 设备绑定（可选） | WebAuthn 平台认证（Touch ID）可选 | webauthn.go:182-186 |

---

## 8. 数据分类

| 分类 | 示例 | 存储位置 | 加密状态 |
| --- | --- | --- | --- |
| 公开 | 版本号、构建提交、模型列表名 | 内存/响应；binary baseline | 无需加密（integrity.go:52-57） |
| 内部 | token 计量、挂载信息、健康状态 | stats/、healthz | 明文，0700 卷保护 |
| 机密 | AI APIKey、TTS/Azure/Clone key、SSH 密码/私钥/口令、access-token、password.phc | config/settings.json（APIKey 明文）、secrets/vault.enc（SSH AES-256-GCM）、auth/（token/salt/phc，0600） | SSH 凭据加密；APIKey 明文（见残余风险）；密码为 Argon2id 哈希 |
| 个人数据 | 对话历史、小秘记忆、性格、用户偏好、审计日志 | sessions/、assistant/voice-history.json（加密信封）、assistant/voice-memory.json（当前明文）、memory/core/ | 历史 AES-256-GCM；memory 当前明文（见残余风险） |

---

## 9. 密钥管理

| 密钥/秘密 | 生成 | 存储 | 轮换 | 销毁 |
| --- | --- | --- | --- | --- |
| KDF salt（kdf-salt.bin） | 16B crypto/rand，一次性 | auth/，0600 | **永久保留**（重生成即所有密文失效） | 随出厂重置 |
| Vault 主密钥 | Argon2id(账户密码, kdf-salt) | 仅内存，不落盘 | 改密码→ReWrap 全部条目 | Lock() zeroBytes |
| AES 数据密钥（voice/persona） | 同源派生 | 仅内存 | 改密自动重封装（voice_agent.go:443-454） | 锁定清零 |
| TLS 私钥 | ECDSA P-256 | certs/key.pem，0600 | 当前不自动轮换（自签 3650 天） | 删除 certs/ 重建 |
| access-token | 32B crypto/rand（256 位） | auth/access-token，0600 | 删除文件重启即重新生成 | 出厂重置 |
| Debug token | 32B crypto/rand | 仅存 SHA-256 哈希 | 手动 admin token 轮换/吊销 | 关闭诊断即清空哈希 |
| SSH 私钥 | 用户提供 | vault.enc AES-256-GCM；临时文件 0600 即用即删 | 重新录入 | vault.Delete |

---

## 10. 残余合规风险（如实）

1. **APIKey 明文落 settings.json**：未纳入 AES vault，卷被只读取证时可读（security-audit.md #1）。
2. **voice-memory.json 明文**：小秘长期记忆未加密信封（security-audit.md #7）。
3. **数据出境由用户负责**：配置外部 Provider/edge-tts/克隆服务即触发跨境传输，aide 不担保第三方合规。
4. **泄露通知**：无集中运营方，Art.33 的 72 小时通知义务落在本机用户身上；应用只提供审计追溯。
5. **自签证书**：仅回环可信，公网暴露不满足传输保密的浏览器信任预期。
