# 安全：统一凭证保险库（Secret Vault）

- 版本：0.1.11.0-RC3
- 日期：2026-09-26
- 适用范围：工作空间 SSH/SFTP 凭据（密码、私钥、私钥口令）；**模型 Provider API Key**（RC2 起并入本保险库）；未来 comm-ssh（#37）共用同一保险库
- 关联文档：[password-hashing.md](./password-hashing.md)（Argon2id 主密钥派生）、[config-backup.md](./config-backup.md)

## 1. 问题与目标

旧版把工作空间 SSH 的登录密码与私钥明文写入 `/data/workspace-secrets.json`（权限 0600，但内容可被直接读取）。一旦 `/data` 数据卷被只读取证或备份泄露，SSH 凭据即被一览无余。

0.1.11 起引入**统一加密凭证保险库**：

- 静态加密：所有凭据以 **AES-256-GCM** 信封落盘，每条目独立随机 nonce；
- 主密钥分层（RC2 起）：
  - **已设账户密码**：主密钥由**账户密码经 Argon2id 派生**（复用 `kdf.go`），运行时只驻留内存、绝不落盘；重启后 vault 保持锁定，须经 `POST /api/unlock`（密码）或 `/api/auth/verify`（密码/WebAuthn）解锁才能取 key 调模型。
  - **未设账户密码**：生成机器绑定随机主密钥（32 字节，`crypto/rand`），以 `0600` 落盘 `/data/secrets/master-key.bin`，启动时自动加载并解锁 vault。明文 API Key 仍**不入** `settings.json`；即便 `settings.json` 被拷走，无本机 `master-key.bin` 也解不出 key。
- 零信任默认：未设置账户密码时**不允许保存任何 SSH 凭据**；模型 API Key 在无密码场景由机器密钥保护，可正常使用；
- 运行时最小暴露：私钥仅在建立连接时短暂解密到受控临时文件，用完即删；
- 单一保险库：工作空间 SSH、模型 API Key 与未来 comm-ssh（#37）共用同一存储，不出现第二套加密。

## 2. 架构

```
已设密码：  账户密码 ──Argon2id(kdfSalt)──► 主密钥(32B, 内存, 重启后锁定)
未设密码：  /data/secrets/master-key.bin (32B, 0600, 机器绑定, 启动自动加载)
                                        │  seal/open (AES-256-GCM, 12B nonce)
                                        ▼
                              /data/secrets/vault.enc   (目录 0700 / 文件 0600, JSON 密文信封)
```

- 文件：`/data/secrets/vault.enc`，目录权限 `0700`、文件权限 `0600`；机器主密钥 `/data/secrets/master-key.bin` 同为 `0600`；
- 信封格式：`{ "version":1, "entries":[{id,type,name,ciphertext_b64,nonce_b64,fingerprint,createdAt,updatedAt}] }`；
- 条目类型：`ssh-password`（登录密码）、`ssh-key`（私钥加密副本）、`ssh-passphrase`（私钥口令）、`model-api-key`（模型 Provider API Key，RC2 新增）；
- 固定条目 ID：`ws:ssh-password`、`ws:ssh-key`、`ws:ssh-passphrase`、`model:api-key`；
- `List()` 只返回元数据与公钥指纹，**永不返回密文或明文**。

### 2.1 模型 API Key 迁移（RC2，#43 配套）

旧版把模型 Provider API Key 明文写在 `/data/config/settings.json`。RC2 起迁入本保险库：

- **启动自动迁移**：`New()` 检测 `settings.json`（或 `AI_API_KEY` 环境变量）里的明文 key，先把明文从内存 settings 清空（防止后续 `atomicJSON` 把明文回写磁盘），暂存内存 `pendingLegacyAPIKey`；vault 一旦解锁（有密码用户经 `/api/unlock` 或 `/api/auth/verify` 输密码；无密码用户启动即由机器密钥解锁），立即把明文加密为 `model:api-key` 条目入库，并用 `shredFile` 把 `settings.json` 原地覆写为等长随机字节后再写回无明文版本（best-effort 擦除，防普通取证/备份读到旧明文；SSD 磨损均衡下不保证物理销毁）。
- **幂等**：vault 已有 `model:api-key` 条目即跳过；vault 未解锁时保留暂存，下次解锁再迁。
- **后续首次设密码**：vault 全部条目从机器绑定主密钥 re-wrap 到 Argon2id 密码派生主密钥（与 SSH vault 的 `ReWrap` 同一机制）。
- **HTTP 接口**：
  - `POST /api/unlock` body `{password}`：有密码用户解锁 vault；成功 `{ok:true, unlocked:true}`，并顺带迁移暂存的旧明文 key。无密码用户直接 `{ok:true, unlocked:true, passwordless:true}`。
  - `GET /api/config`：`hasKey`（顶层）与 `models[].hasApiKey` 报告"是否已配置"，**绝不回显明文**；同时返回 `vaultUnlocked`（主密钥是否已驻留内存）。
  - `PUT /api/settings`：body 中 `apiKey` 非空 → 加密入 vault；`clearKey=true` → 删除 vault 条目；空且未 `clearKey` → 保持现状。`settings.json` 的 `apiKey` 字段恒为空，永不落明文。
- **运行时取 key**：`provider.go`/`workflow.go` 不再读 `settings.APIKey`，改经 `modelAPIKeyLocked()` 从 vault 解密；已配置但 vault 未解锁时返回明确错误"请先解锁以使用模型"（不静默失败）。
- **导出/导入**：`config_backup.go` 导出**不含**任何明文 key；导入旧备份里的明文 `apiKey` 时自动暂存、解锁后入 vault（见 [config-backup.md](./config-backup.md)）。

## 3. SSH 私钥双输入

工作空间配置弹窗的“密钥”认证方式提供两种输入：

1. **粘贴私钥**：直接粘贴私钥正文 → 加密存入保险库；
2. **选择文件**：在容器内选择私钥路径（限定 `/workspace`、`/context`、`/local` 挂载根内，拒绝绝对越权与 `..` 路径遍历），再选存储方式：
   - **仅引用路径**：运行时直接读取该路径，私钥**不入库**、不落地副本；
   - **导入加密副本**：读取文件内容 → 加密存入保险库。

无论哪种方式，界面**永不回显私钥正文**，保存后清空输入框。保存成功后展示公钥 **SHA-256 指纹**（`ssh-keygen -lf` 计算），便于人工核对主机身份。

## 4. 私钥口令（passphrase）

受保护的私钥可设置 passphrase。它作为独立条目（`ssh-passphrase`）加密存库，运行时经 `SSH_ASKPASS` 脚本注入 ssh，**不出现在命令行参数**中，避免 `ps`/进程列表泄露。

## 5. 运行时凭据生命周期

```mermaid
flowchart LR
    A[用户输入凭据] --> B{已设账户密码?}
    B -- 否 --> R[拒绝保存<br/>引导先设密码]
    B -- 是 --> C[AES-256-GCM 加密入 vault]
    C --> D[/data/secrets/vault.enc<br/>0600]
    E[建立 SSH 连接] --> F[vault 解密主密钥]
    F --> G{引用路径?}
    G -- 是 --> H[直接 -i 指向<br/>已校验路径]
    G -- 否 --> I[写 0600 临时文件]
    I --> J[ssh -i 临时文件]
    J --> K[defer os.Remove<br/>用完即删]
    H --> L[连接复用 ControlMaster]
    I --> L
```

- 引用路径模式：直接使用保存时校验过的容器路径，不写临时文件；
- 粘贴/副本模式：解密后写入 `/data/secrets/.tmp/` 下 `0600` 临时文件，`defer os.Remove` 确保主连接建立后立即删除；
- passphrase 经 `SSH_ASKPASS` 注入；
- 保险库锁定（如重启后未解锁）时，运行时取不到私钥会报错——这是预期安全行为，前端引导用户输入账户密码解锁。

## 6. 改密码与导出/导入

- **改密码**：`ReWrap(oldKey, newKey)` 用旧密钥解开全部条目、新密钥重加密；旧密钥此后不可解。
- **导出备份**：勾选“包含密钥”时随附**加密信封**（即 vault.enc 本身），**绝不回退明文**；
- **导入备份**：加密信封合并进本地保险库（不覆盖已有可用条目）；同机恢复（同 kdf-salt、同账户密码）可直接解锁使用，跨机恢复因 kdf-salt 不同而不可解，用户重新录入凭据即可。

## 7. 审计

保存、清除、解锁、使用凭据会追加到 `data/vault-audit.jsonl`（0600），**只记录动作与时间，不记录任何正文**。

## 8. 合规对照

- **OWASP Secret Management**：静态加密、最小权限文件权限、主密钥不落盘、轮换（改密码 ReWrap）；
- **GDPR Art.32**：处理个人/访问凭据时采取加密与访问控制等适当技术措施；
- 威胁模型：`/data` 卷被只读取证时，无账户密码即无主密钥，凭据不可读。

## 9. NOT_RUN

- 端到端真实 SSH 登录（需可连的 sshd/容器）未在 CI 运行，记 NOT_RUN；由 `ssh_keygenFingerprintFile`/临时文件清理单测覆盖逻辑。
