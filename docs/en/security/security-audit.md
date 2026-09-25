# aide Security Audit Report

- Version: 0.1.11.0-RC1
- Date: 2026-09-25
- Branch: feature/permission-panel
- Method: line-by-line source-code verification (not assumptions); every finding cites `file:line`.
- Scope: authentication, cryptography, transport, at-rest storage, backup export, audit logs, memory isolation, plugins, SSH credentials, WebAuthn, data integrity.

> Risk ratings: **High** = exploitable remotely/by an unauthorized local actor leading to credential disclosure or privilege bypass; **Medium** = materializes only under specific preconditions (read-only volume forensics, browser exceptions, self-signed public exposure); **Low** = defense-in-depth gap with strict preconditions; **Info** = design status record.

---

## 1. Overview

### 1.1 Deployment and trust model

aide is a **local-first** AI workbench: the backend runs as a single container, bound by default to `127.0.0.1:8097` (`internal/server/server.go:2047`, overridable via `AIDE_ADDR`). All state lives in a named Docker volume `/data`, a single-user private volume.

- **Auth model**: Bearer Token throughout (`Authorization: Bearer <token>`); no cookies (`server.go:993-994`). The token is a 256-bit random value stored at `auth/access-token` (0600), injected by the launcher / local browser on first open.
- **Unlock model**: the account password (Argon2id PHC) unlocks an in-memory AES-256 master key derived via Argon2id; it decrypts the unified vault, voice history, and persona ciphers, and is zeroed on lock/shutdown.
- **Transport**: single-port protocol adaptive — first-byte Peek: `0x16`=TLS to the main server, otherwise 308-redirect to HTTPS (`server.go:2049-2061`).
- **Data flow**: user conversation → aide backend → the AI Provider BaseURL the user explicitly configures. **No telemetry is sent by default**; content leaves the device only when the user configures an external model API, edge-tts, or a voice-cloning service.

### 1.2 Data layering (authoritative paths: `internal/server/paths.go:9-46`)

```
/data  (0700, single-user private volume)
├── auth/            credentials (secret, 0600)
├── sessions/        active/ archived/ assistant/
├── assistant/       xiaomi private zone (voice-history encrypted envelope / voice-memory)
├── memory/          core/ cache/ feedback/
├── config/          settings.json (atomic write), backups/
├── stats/           token metering (regenerable)
├── audit/           debug-audit.jsonl / security-audit.jsonl / personality-audit.jsonl (append, 0600)
├── secrets/         vault.enc (AES-256-GCM envelope, 0600), sources-secrets.json
├── certs/           cert.pem(0644) / key.pem(0600)
├── .integrity/      SHA-256 baseline / recovery log
└── .quarantine/     quarantined corrupted user data (never auto-deleted)
```

---

## 2. Findings (item by item)

### #1 AI APIKey storage and file permissions

- **Status**: `Settings.APIKey` is stored **in plaintext** in `config/settings.json` (`server.go:49` `APIKey string json:"apiKey,omitempty"`). Other plaintext fields: `TTSAPIKey`(:83), `TTSAzureKey`(:86), `CloneTTSAPIKey`(:90). settings.json is written via `atomicJSON` (`server.go:508-530`): `os.CreateTemp` creates a 0600 temp file on Unix, then rename; the parent `config/` dir is 0700 (`paths.go:252-259`).
- **Code**: `internal/server/server.go:49,83,86,90`; `internal/server/server.go:508-530`; `internal/server/paths.go:142-143`.
- **Assessment**: the APIKey is not inside the AES-256-GCM vault (which currently holds only SSH credentials, `secret_vault.go:36-40`). It relies on 0600 file + 0700 dir + private Docker volume; it is readable if the volume is acquired by read-only forensics. This is inconsistent with how SSH credentials are handled.
- **Risk**: **Medium**.

### #2 Config backup export masking

- **Status**: `POST /api/config/export`, when `IncludeSecrets=false`, blanks `APIKey/TTSAPIKey/UserPasswordHash/PersonaCiphers/PersonaCipher/DebugTokenHash` (`config_backup.go:49-59`). When `IncludeSecrets=true`, the attached workspace secrets are the **encrypted envelope** `vault.enc` (`config_backup.go:81-85`); only legacy installs with no vault file fall back to the old plaintext file (:86-90). Voice history is attached only when `IncludeVoiceData=true` (:92-96). Import preserves current keys by default and adopts backup values only when explicitly requested (:157-195).
- **Code**: `internal/server/config_backup.go:49-96,157-195`.
- **Assessment**: masking list is complete; secret material stays ciphertext when attached; the rollback point `settings.json.pre-import` is written 0600 (:154).
- **Risk**: **Pass (low)**.

### #3 Audit logs containing sensitive body

- **Status**:
  - `debug-audit.jsonl` entries carry only `Time/IP/UA/Method/Path/Owner/Result` (`debug.go:53-62`) — **no request body, conversation text, or token**; written 0600 (`debug.go:191`).
  - `personality-audit.jsonl` entries carry only `At/ID/Trigger/Result/OldLen/NewLen/Evolutions/Note` (`personality_evolution.go:65-75`) — evolution result and prompt length, **not prompt text**; written 0600 (:391).
  - debug responses return only a `hasKey` boolean, never the key itself (`debug.go:223,269`); the diagnostic bundle contains no message text (`debug.go:492-561`).
- **Code**: `internal/server/debug.go:53-62,176-200`; `internal/server/personality_evolution.go:65-75,390-395`.
- **Assessment**: audit logs record actions and metadata only, matching the "actions not content" rule.
- **Risk**: **Pass**.

### #4 access-token entropy and file permissions

- **Status**: `newID()` reads 16 `crypto/rand` bytes, hex-encodes (`server.go:481-487`); access-token = `newID()+newID()` = **32 bytes = 256 bits of entropy**, 64 hex chars (`server.go:566`). Written to `auth/access-token`, mode 0600 (:567). On load, length ≥32 is enforced (:574).
- **Code**: `internal/server/server.go:481-487,564-577`.
- **Assessment**: 256-bit entropy meets the requirement for a strong local token; 0600 is correct. The debug token is likewise 32 bytes (256-bit), stored only as a SHA-256 hash (`debug.go:33,566-576`).
- **Risk**: **Pass**.

### #5 Cookie attributes

- **Status**: **No cookies.** Auth is Bearer-only; `/api/` requests are Origin-checked (cross-site Origin whose Host mismatches → 403, `server.go:1013-1019`). Because EventSource/img tags cannot set headers, only `/events` and `/api/file/raw` may use `?access_token=`; all other APIs reject URL-embedded credentials (:1020-1025).
- **Code**: `internal/server/server.go:993-1030`.
- **Assessment**: no cookies means no CSRF-cookie-borne risk; Origin check is a CSRF backstop.
- **Risk**: **Pass**.

### #6 Security response headers

- **Status** (`server.go:995-1007`):
  - `X-Content-Type-Options: nosniff` (:995)
  - `Referrer-Policy: no-referrer` (:996)
  - `Content-Security-Policy`: default `default-src 'self'; frame-ancestors 'none'; base-uri 'none'` (:1000); the drawio vendor path relaxes eval/inline (:998)
  - `Strict-Transport-Security`: emitted only on **non-loopback** HTTPS as `max-age=300` (:1004-1005, `strictTransportSecurity` :1944-1950)
  - `Cache-Control: no-store` (:1007)
- **Code**: `internal/server/server.go:995-1007,1944-1950`.
- **Assessment**: no explicit `X-Frame-Options` header, but CSP `frame-ancestors 'none'` is equivalent in modern browsers; its absence for legacy pre-CSP3 browsers is a known residual. HSTS is exempt on localhost loopback (deliberate, to avoid browsers permanently rewriting local http).
- **Risk**: **Low** (missing legacy X-Frame-Options header).

### #7 History / xiaomi records encryption

- **Status**:
  - `assistant/voice-history.json`: has an encryption envelope `{encrypted, cipher}`, AES-256-GCM base64 (`voice_agent.go:53-58`); persisted 0600 (:114); when locked it keeps only ciphertext, no plaintext in memory (:106-107); key derived from the account password via Argon2id, with automatic re-wrap from legacy SHA-256 key (:416-454).
  - `assistant/voice-memory.json`: currently deserialized as **plaintext JSON** directly (`voice_agent.go:92-94`); no encryption envelope or encrypted write path was found.
- **Code**: `internal/server/voice_agent.go:53-58,92-115,416-454`.
- **Assessment**: conversation history is encrypted; the long-term memory file currently does not go through an encryption envelope. Its content is the xiaomi's notes on user habits — personal data. This is a gap versus the goal of encrypting all xiaomi records.
- **Risk**: **Medium** (voice-memory.json plaintext).

### #8 TLS configuration

- **Status**: `MinVersion: tls.VersionTLS12` (`server.go:1873`); TLS1.2 cipher suites restricted to ECDHE + AES-GCM (:1874-1879, ECDSA/RSA × AES128/256); `PreferServerCipherSuites` (:1880); TLS1.3 auto-negotiated by Go. Self-signed cert SAN fixed to `DNS:localhost` + `IP:127.0.0.1` + `::1` (:1909-1910), ECDSA P-256, private key 0600 / cert 0644 (:1923-1928).
- **Code**: `internal/server/server.go:1869-1930,2047-2061`.
- **Assessment**: minimum version, forward secrecy, AEAD, and private-key permissions meet the bar. Residual: browsers do not trust the self-signed cert by default (manual exception); the 3650-day validity (:1906) is long.
- **Risk**: **Low** (self-signed + long validity; loopback-only).

### #9 Password hashing

- **Status**: `HashPassword` outputs PHC `$argon2id$v=19$m=65536,t=3,p=4$<b64salt>$<b64hash>` (`kdf.go:78-88`), params m=64MiB/t=3/p=4/salt=16B/keyLen=32 (:28-34). `VerifyPassword` accepts both PHC(Argon2id) and legacy 64hex(SHA-256); on legacy it returns `needsUpgrade=true` so the caller upgrades and re-encrypts after successful login (:111-124). Comparison uses `subtle.ConstantTimeCompare` (:154).
- **Code**: `internal/server/kdf.go:27-34,78-124,126-155`.
- **Assessment**: Argon2id parameters align with the OWASP baseline; legacy SHA-256 auto-migration closes the loop.
- **Risk**: **Pass**.

### #10 KDF key derivation

- **Status**: `DeriveAESKey` derives a 32-byte AES-256 key via Argon2id + the per-host fixed `auth/kdf-salt.bin` (16B crypto/rand, 0600, generated once and kept forever, `kdf.go:43-63`) (:67-69). This master key secures the vault, voice history, and persona ciphers; on password change the vault `ReWrap`s all entries (`secret_vault.go:262-283`).
- **Code**: `internal/server/kdf.go:36-69`; `internal/server/secret_vault.go:260-283`.
- **Assessment**: the fixed salt provides domain separation (anti-rainbow-table); the master key never touches disk; re-wrap on password change.
- **Risk**: **Pass**.

### #11 Unified secret vault

- **Status**: `secrets/vault.enc`, dir 0700 / file 0600 (`secret_vault.go:27-33`); AES-256-GCM envelope, per-entry 12-byte nonce (:303-314); atomic write (:148-167). `List()` returns metadata only (ID/type/fingerprint/timestamps), **never ciphertext or plaintext** (:63-70,249-258); `Get()` returns plaintext only when unlocked, and callers `zeroBytes` it after use (:199-208,194). The API `workspaceConfigOut` returns only `hasPassword/hasKey/fingerprint/vaultLocked` booleans (`workspace_config.go:275-288`).
- **Code**: `internal/server/secret_vault.go:27-33,139-167,249-258`; `internal/server/workspace_config.go:261-289`.
- **Assessment**: encrypted envelope, least privilege, and no plaintext leakage on read all pass.
- **Risk**: **Pass**.

### #12 Data layering and integrity

- **Status**: `paths.go` is the single authoritative source of /data paths (:6-7); `EnsureDirs` creates all layered dirs at 0700 (:252-259). `integrity.go`: `BuildBaseline` SHA-256 of the binary on first boot (:61-84); `VerifyIntegrity` checks binary hash / dir structure / parseability of key files at startup (:101-161); `SelfHeal` only rebuilds missing dirs/baseline and moves corrupted user data into `.quarantine`, **never auto-deletes** (:188-198); a 5-minute background sweep runs (:248-265).
- **Code**: `internal/server/paths.go:9-46,233-259`; `internal/server/integrity.go:61-84,101-205,248-265`.
- **Assessment**: clean layering, baseline + self-heal + quarantine boundaries are correct; user data is not auto-deleted.
- **Risk**: **Pass**.

### #13 Memory access control

- **Status**: `canAccessMemory` policy matrix (`memory_access.go:77-96`):
  - `memory/core/` (aide memory): aide read/write; xiaomi read-only.
  - `assistant/` (xiaomi private zone): aide never read or write; xiaomi read/write.
  - The decision is by **directory ownership**, not filename coincidence (:18-21). `shellTouchesAssistantZone` adds a sandbox shell-layer backstop blocking the `/data/assistant/` prefix plus a filename fallback (:109-122).
- **Code**: `internal/server/memory_access.go:56-96,109-122`.
- **Assessment**: bidirectional isolation plus a defensive assertion matches the one-way-visibility design.
- **Risk**: **Pass**.

### #14 Plugin security

- **Status**:
  - Disabled by default: with no `plugins/registry.json` the registry is empty (`plugins.go:52-54`).
  - Plugins run as node subprocesses with a scrubbed env `PATH=...; HOME=/home/aide` (`plugins.go:90,313`; `plugin_daemon.go:267`).
  - Daemon plugins are managed by a DaemonManager; the manager itself listens on no port, and protocol plugins are forced to `AIDE_DAEMON_BIND=127.0.0.1` (`plugin_daemon.go:10,267`).
  - Code size cap 256 KiB, max 50 plugins, 10s/70s timeouts (`plugins.go:24-26,98-115`).
  - No extra Linux capability such as `CAP_NET_RAW` is granted (Docker default).
- **Code**: `internal/server/plugins.go:48-95,97-131`; `internal/server/plugin_daemon.go:10,267`.
- **Assessment**: default-off, loopback binding, scrubbed env, and timeouts pass. Plugin lifecycle events go to stdout logs (`log.Printf`), not the audit JSONL — auditability is slightly weaker.
- **Risk**: **Low** (plugin audit not in structured logs; Serial plugin in progress, pending final confirmation).

### #15 SSH private key storage

- **Status**: pasted/imported private keys are encrypted into the vault via `vault.Put(VaultIDWSKey,...)` (`workspace_config.go:453,488`); at runtime they are decrypted to a 0600 temp file and `defer os.Remove` deletes it once the main connection is up (`ssh_session.go:84-98`). Only the SHA256 public-key fingerprint is shown; private-key body is never echoed or logged (`ssh_keyutil.go:6,28-51`).
- **Code**: `internal/server/workspace_config.go:442-493`; `internal/server/ssh_session.go:84-98`; `internal/server/ssh_keyutil.go:28-51`.
- **Assessment**: encrypted storage + 0600 temp file used-once-then-deleted + no echo — all pass.
- **Risk**: **Pass**.

### #16 WebAuthn

- **Status**: RPID fixed to `localhost` (IP literals are not valid RPID, `webauthn.go:81`); RPOrigins include `https://localhost:<port>`, `https://127.0.0.1:<port>`, `http://localhost:<port>` (:83-86); `UserVerification: VerificationPreferred` (:182); `ConveyancePreference: PreferNoAttestation` (:186). Credentials stored at `auth/webauthn-credentials.json` via `atomicJSON` 0600 (:111-113).
- **Code**: `internal/server/webauthn.go:75-86,111-113,182-186`.
- **Assessment**: RPID/RPOrigins correctly pin to local loopback; the single `http://localhost` origin is an intentional accommodation for stale browser tabs (cached pages from before the self-signed-HTTPS switch).
- **Risk**: **Low** (http://localhost origin entry).

---

## 3. Items fixed (#29 security batch and follow-up tasks)

| Item | Before | After | Evidence |
| --- | --- | --- | --- |
| Password hashing | Bare SHA-256 (64hex) | Argon2id PHC (m=64MiB,t=3,p=4); legacy hash upgraded + re-wrapped on login | `kdf.go:4-12,111-124` |
| Transport | No TLS, plain HTTP | TLS≥1.2 + ECDHE/AES-GCM, single-port mux auto 308 redirect | `server.go:1869-1882,2049-2061` |
| Backup masking | Early builds might leak secrets | Export blanks APIKey/password hash/Persona ciphers/DebugTokenHash by default; attached secrets stay encrypted envelope | `config_backup.go:49-59,79-90` |
| Audit logs missing | No external-access audit | debug-audit.jsonl (metadata only) + personality-audit.jsonl (evolution result only) | `debug.go:53-62`, `personality_evolution.go:65-75` |
| SSH plaintext creds | workspace-secrets.json plaintext | Unified AES-256-GCM vault; old plaintext auto-migrated and archived | `secret_vault.go`, `workspace_config.go:107-136` |
| Flat data layout | Files scattered in /data root | Layered dirs auth/sessions/assistant/memory/config/stats/audit/secrets/certs + migration | `paths.go`, `migration.go:44-58` |
| No integrity check | None | SHA-256 binary baseline + startup verify + 5-min sweep + quarantine without auto-delete | `integrity.go` |
| Memory cross-read | aide could read xiaomi zone | One-way visibility: aide barred from assistant/, xiaomi read-only on aide memory; shell backstop | `memory_access.go:77-122` |

---

## 4. Residual risks (stated honestly)

1. **Self-signed cert on public exposure**: the cert is self-signed SAN=localhost/127.0.0.1 and is trusted only on loopback. If an operator maps 8097 to the public internet (`AIDE_ADDR=0.0.0.0`), browsers will distrust it and MITM becomes possible. **Default 127.0.0.1 binding mitigates**; public deployments must use a trusted cert.
2. **HSTS localhost exemption**: HSTS is not sent on loopback hosts (`server.go:1946`) — deliberate, but means http→https relies on a per-connection 308; non-browser clients must upgrade themselves.
3. **GDPR data export**: when the user configures an external AI Provider (e.g. DeepSeek/cloud model) or edge-tts/voice cloning, conversation content is sent to the user-chosen third-party endpoint. aide does not warrant cross-border transfers; export legality rests between the user and that third party (see eu-compliance.md).
4. **eIDAS not applicable**: aide is a self-hosted local tool; it provides no electronic signature or remote identity service and is not a Qualified Trust Service Provider under eIDAS.
5. **edge-tts server-side throttling**: edge-tts reuses Microsoft Edge's online TTS endpoint and is subject to its rate limits / risk control; this is external-service availability, not an aide flaw.
6. **No CAP_NET_RAW (optional)**: comm plugins currently are not granted raw-socket capability; any future plugin needing packet capture must be explicitly authorized and re-evaluated.
7. **APIKey plaintext in settings.json**: see #1, not moved into the AES vault.
8. **voice-memory.json plaintext**: see #7, the long-term memory does not use an encryption envelope.
9. **WebAuthn includes http://localhost origin**: see #16, an intentional accommodation for stale tabs.
10. **Plugin audit not in structured logs**: see #14, plugin lifecycle events go to stdout, not the audit JSONL.

---

## 5. Recommendations

1. Move AI `APIKey/TTSAPIKey/TTSAzureKey/CloneTTSAPIKey` into the unified vault (or apply application-layer encryption to the sensitive settings fields), aligning with SSH-credential handling (see #1).
2. Give `voice-memory.json` an AES-256-GCM envelope consistent with voice-history (see #7).
3. Add an `X-Frame-Options: DENY` header for legacy-browser coverage (see #6).
4. Append plugin lifecycle events to `audit/security-audit.jsonl`, unifying with the debug/security audit system (see #14).
5. Document explicitly for public deployments: a trusted certificate is mandatory; do not rely on the self-signed cert; provide a cert-mount path.
6. Shorten the self-signed cert validity or generate it on first boot and let the user export/trust it manually.
