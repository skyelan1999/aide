aide EU Compliance Statement (GDPR / ePrivacy / OWASP / ENISA / NIST / eIDAS)

- Version: 0.1.11.0-RC1
- Date: 2026-09-25
- Deployment: local-first, self-hosted (single-user private Docker volume)
- Companion: [security-audit.md](./security-audit.md), [privacy-policy-template.md](./privacy-policy-template.md), [dpia-template.md](./dpia-template.md)

> Position statement: aide is a tool **installed on the user's own machine**, not a cloud service or SaaS. By default, conversations, memories, and credentials live only on the user's local volume; data leaves the device only when the user **explicitly** configures an external AI Provider, edge-tts, or a voice-cloning service. Under GDPR this typically falls under "personal processing in the course of a household activity", but the engineering measures below are still listed as delivered controls.

---

## 1. GDPR clause-by-clause mapping

| Clause | Requirement | aide measure | Status | Evidence |
| --- | --- | --- | --- | --- |
| Art.5(1)(a) lawful/fair/transparent | lawful basis and transparency | Self-hosted: the user is both data subject and controller; privacy policy template in privacy-policy-template.md | Met | this doc; privacy-policy-template.md |
| Art.5(1)(b) purpose limitation | no purpose expansion | AI conversation serves only user-initiated work; no advertising profiling, no telemetry | Met | server.go has no default egress; egress only on user-configured Provider |
| Art.5(1)(c) data minimisation | collect only what's needed | audit logs record action metadata only, not content; vault List returns no ciphertext | Met | debug.go:53-62; secret_vault.go:249-258 |
| Art.5(1)(d) accuracy | keep data accurate | sessions/memory visible and deletable by the user; factory reset clears | Met | factory_reset.go:213-247 |
| Art.5(1)(e) storage limitation | no over-retention | data stays local, deletable/exportable/resettable at any time; debug token expires in 365 days and is revocable | Partial | debug.go:34,594-606; user deletion = deleting the volume |
| Art.5(1)(f) integrity & confidentiality | secure processing | TLS1.2+, Argon2id, AES-256-GCM, 0600 files, vault | Met | see sections 3/4 |
| Art.25 privacy by design/default | privacy by default | external diagnostics off by default (DebugAccessEnabled=false); plugins disabled by default; no cookies | Met | server.go:153; plugins.go:52-54; server.go:993 |
| Art.32 security of processing | technical & organisational measures (TOMs) | see TOMs table in section 3 | Met | section 3 |
| Art.33 breach notification | 72-hour regulator notice | local single-user deployment has no central operator; a breach means host compromise and is up to the user; in-app audit logs support post-hoc tracing | Partial | debug-audit.jsonl; personality-audit.jsonl |
| Art.35 DPIA | DPIA for high-risk processing | local self-hosting is low risk; dpia-template.md provided for high-sensitivity deployments | Met (template provided) | dpia-template.md |
| Art.13/14 informing | notify the data subject | privacy-policy-template lists data collected / location / retention / rights | Met | privacy-policy-template.md |
| Art.15-22 data subject rights | access/rectification/erasure/portability/objection | local direct actions: export config/conversations, delete, factory reset | Met | config_backup.go; factory_reset.go |
| Art.28 processor | processor agreement | when the user brings their own Provider, the user contracts with that API vendor; aide itself does not act as processor | Partial | see third-party services |

---

## 2. ePrivacy Directive

| Topic | Status | Evidence |
| --- | --- | --- |
| Cookies / terminal-device info | The aide web UI uses **no cookies** at all; pure Bearer-token auth, no tracking cookies, no beacons | server.go:993-994 |
| Confidentiality of communications | browser↔backend over TLS1.2+ throughout; HSTS not sent on loopback (to avoid permanently rewriting local http) | server.go:1869-1882,1944-1950 |
| Terminal-device storage | No persistent tracking storage is written to the device; the browser side holds only in-memory session state | no localStorage credentials |

---

## 3. OWASP ASVS v4 mapping

| Category | Key requirement | aide measure | Status | Evidence |
| --- | --- | --- | --- | --- |
| V2 Authentication | secure password storage, secure session credentials, anti-bruteforce | Argon2id PHC; 256-bit random access-token; constant-time compare | Met | kdf.go:78-88; server.go:566,1026 |
| V2 Authentication | MFA / device unlock | optional WebAuthn (Touch ID) unlock; UserVerification preferred | Met | webauthn.go:182 |
| V4 Access control | server-side access control, IDOR protection, least privilege | per-request Bearer check; bidirectional memory isolation; vault returns plaintext only when unlocked | Met | server.go:1012-1029; memory_access.go:77-96 |
| V4 Access control | secure defaults | diagnostics off by default; plugins disabled by default; admin plane accepts only the main token | Met | debug.go:100-104; plugins.go:52-54; debug.go:135-139 |
| V6 Cryptography | encryption of protected data, transport encryption, strong randomness | AES-256-GCM envelope; TLS1.2+/ECDHE/AES-GCM; crypto/rand | Met | secret_vault.go:303-314; server.go:1869-1882 |
| V6 Cryptography | weak algorithms disabled | legacy SHA-256 used only for migration decryption, upgraded to Argon2id on login; TLS<1.2 rejected | Met | kdf.go:111-124; server.go:1873 |
| V14 Configuration | security HTTP headers, no sensitive leakage | nosniff/no-referrer/CSP/no-store; audit logs record no body | Met | server.go:995-1007; debug.go:53-62 |

---

## 4. OWASP Password Storage Cheat Sheet (Argon2id parameters)

| OWASP recommendation (Argon2id) | aide actual | Evidence |
| --- | --- | --- |
| Algorithm | Argon2id | `argon2.IDKey` — kdf.go:68 |
| Memory m | ≥ 19 MiB (recommended 6.5–64) | **65536 KiB = 64 MiB** — kdf.go:30 |
| Iterations t | ≥ 2–3 | **3** — kdf.go:29 |
| Parallelism p | 1–4 | **4** — kdf.go:31 |
| Salt length | ≥ 16 bytes | **16 bytes** crypto/rand — kdf.go:33,80 |
| Output length | ≥ 16 bytes | **32 bytes** — kdf.go:32 |
| Format | PHC string | `$argon2id$v=19$m=...,t=...,p=...$...$...` — kdf.go:84-87 |
| Constant-time compare | yes | `subtle.ConstantTimeCompare` — kdf.go:154 |

---

## 5. ENISA cryptographic recommendations

| ENISA recommendation | aide implementation | Evidence |
| --- | --- | --- |
| AEAD symmetric encryption (GCM/ChaCha20) | AES-256-GCM, 12-byte nonce | secret_vault.go:295-314; persona.go:78-95 |
| Slow KDF (Argon2id/scrypt) | Argon2id | kdf.go:67-69 |
| TLS≥1.2 with forward secrecy | TLS1.2 min, ECDHE suites | server.go:1873-1879 |
| CSPRNG | crypto/rand | kdf.go:55; server.go:483; secret_vault.go:309 |

---

## 6. eIDAS

**Not applicable.** aide is a self-hosted local productivity tool. It provides no qualified trust services (QTSP/ASSP) such as electronic signatures, seals, timestamps, electronic delivery, or remote identity authentication. Its WebAuthn unlock is local device unlock only and does not constitute eIDAS remote identity means.

---

## 7. NIST SP 800-63B / 175B mapping

| Item | NIST recommendation | aide implementation | Evidence |
| --- | --- | --- | --- |
| Password hashing | slow, adaptive KDF | Argon2id (aligned with 800-63B memory-hard KDF orientation) | kdf.go:28-34 |
| Key management | master key never at rest, rotatable | master key in memory; ReWrap on password change | secret_vault.go:107-115,262-283 |
| Credential file permissions | least privilege | 0600 files / 0700 dirs | secret_vault.go:31-32; paths.go:254 |
| Remote attestation | device binding (optional) | optional WebAuthn platform attestation (Touch ID) | webauthn.go:182-186 |

---

## 8. Data classification

| Class | Examples | Location | Encryption status |
| --- | --- | --- | --- |
| Public | version, build commit, model list names | memory/responses; binary baseline | none needed (integrity.go:52-57) |
| Internal | token metering, mount info, health | stats/, healthz | plaintext, 0700 volume protection |
| Confidential | AI APIKey, TTS/Azure/Clone keys, SSH password/private key/passphrase, access-token, password.phc | config/settings.json (APIKey plaintext), secrets/vault.enc (SSH AES-256-GCM), auth/ (token/salt/phc, 0600) | SSH creds encrypted; APIKey plaintext (see residual); password is Argon2id hash |
| Personal data | conversation history, xiaomi memory, personality, user preferences, audit logs | sessions/, assistant/voice-history.json (encrypted envelope), assistant/voice-memory.json (currently plaintext), memory/core/ | history AES-256-GCM; memory currently plaintext (see residual) |

---

## 9. Key management

| Key / secret | Generation | Storage | Rotation | Destruction |
| --- | --- | --- | --- | --- |
| KDF salt (kdf-salt.bin) | 16B crypto/rand, once | auth/, 0600 | **kept forever** (regeneration invalidates all ciphertext) | with factory reset |
| Vault master key | Argon2id(account password, kdf-salt) | memory only, never on disk | password change → ReWrap all entries | Lock() zeroBytes |
| AES data key (voice/persona) | derived from same source | memory only | auto re-wrap on password change (voice_agent.go:443-454) | zeroed on lock |
| TLS private key | ECDSA P-256 | certs/key.pem, 0600 | not auto-rotated today (self-signed 3650 days) | delete certs/ to regenerate |
| access-token | 32B crypto/rand (256-bit) | auth/access-token, 0600 | delete file + restart to regenerate | factory reset |
| Debug token | 32B crypto/rand | SHA-256 hash only | manual admin token rotate/revoke | disabling diagnostics clears the hash |
| SSH private key | user-provided | vault.enc AES-256-GCM; 0600 temp file used-once-then-deleted | re-enter credential | vault.Delete |

---

## 10. Residual compliance risks (stated honestly)

1. **APIKey plaintext in settings.json**: not moved into the AES vault; readable on read-only volume forensics (security-audit.md #1).
2. **voice-memory.json plaintext**: xiaomi long-term memory has no encryption envelope (security-audit.md #7).
3. **Data export is the user's responsibility**: configuring an external Provider/edge-tts/cloning service triggers cross-border transfer; aide does not warrant third-party compliance.
4. **Breach notification**: there is no central operator; the Art.33 72-hour duty rests on the local user; the app provides audit tracing only.
5. **Self-signed certificate**: trusted only on loopback; public exposure does not meet browser-trusted transport confidentiality expectations.
