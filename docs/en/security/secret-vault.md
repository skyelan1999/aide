# Security: Unified Secret Vault

- Version: 0.1.11.0-RC3
- Date: 2026-09-26
- Scope: workspace SSH/SFTP credentials (password, private key, key passphrase); **model Provider API key** (merged into this vault since RC2); future comm-ssh (#37) reuses the same vault
- Related: [password-hashing.md](./password-hashing.md) (Argon2id master-key derivation), [config-backup.md](./config-backup.md)

## 1. Problem and goals

Previously the workspace SSH login password and private key were stored in plaintext at `/data/workspace-secrets.json` (mode 0600, but readable as-is). If the `/data` volume were forensically read or a backup leaked, SSH credentials were exposed verbatim.

0.1.11 introduces a unified encrypted secret vault:

- **At-rest encryption**: every secret is sealed with **AES-256-GCM**, each entry using an independent random nonce;
- **Two-tier master key (since RC2)**:
  - *Account password set*: the master key is **derived from the account password via Argon2id** (reusing `kdf.go`), resident only in memory at runtime and never written to disk. After a restart the vault stays locked until `POST /api/unlock` (password) or `/api/auth/verify` (password/WebAuthn) unlocks it before the model can use the key.
  - *No account password*: a machine-bound random 32-byte master key (`crypto/rand`) is written `0600` to `/data/secrets/master-key.bin` and auto-loaded at boot to unlock the vault. The plaintext API key still **never** lands in `settings.json`; copying `settings.json` alone cannot reveal the key without the local `master-key.bin`.
- **Secure by default**: saving any SSH credential is **rejected when no account password is set**; the model API key works passwordless, protected by the machine key;
- **Minimal runtime exposure**: a private key is decrypted only transiently into a controlled temp file when a connection is established, then deleted;
- **One vault**: workspace SSH, the model API key, and the future comm-ssh (#37) share the same store — no second encryption scheme.

## 2. Architecture

```
account password ──Argon2id(kdfSalt)──► master key (32B, memory only)
                                             │ seal/open (AES-256-GCM, 12B nonce)
                                             ▼
                               /data/secrets/vault.enc   (dir 0700 / file 0600, JSON ciphertext envelope)
```

- File: `/data/secrets/vault.enc`, directory `0700`, file `0600`; the machine master key `/data/secrets/master-key.bin` is also `0600`;
- Envelope: `{ "version":1, "entries":[{id,type,name,ciphertext_b64,nonce_b64,fingerprint,createdAt,updatedAt}] }`;
- Entry types: `ssh-password`, `ssh-key` (encrypted key copy), `ssh-passphrase`, `model-api-key` (model Provider API key, added RC2);
- Fixed IDs: `ws:ssh-password`, `ws:ssh-key`, `ws:ssh-passphrase`, `model:api-key`;
- `List()` returns metadata and the public-key fingerprint only — **never ciphertext or plaintext**.

### 2.1 Model API key migration (RC2, alongside #43)

Previously the model Provider API key was stored in plaintext in `/data/config/settings.json`. Since RC2 it lives in this vault:

- **Startup auto-migration**: `New()` detects a plaintext key in `settings.json` (or the `AI_API_KEY` env var), immediately clears it from the in-memory settings (so a later `atomicJSON` cannot rewrite it to disk), and stages it in memory as `pendingLegacyAPIKey`. Once the vault unlocks (password users via `/api/unlock` or `/api/auth/verify`; passwordless users auto-unlock at boot via the machine key), the plaintext is sealed as a `model:api-key` entry, and `shredFile` best-effort overwrites `settings.json` with random bytes before rewriting a clean version (it defeats casual forensic/backup reads; SSD wear-leveling means it is not a physical guarantee).
- **Idempotent**: if a `model:api-key` entry already exists, migration is skipped; if the vault is still locked, the staged copy waits until the next unlock.
- **First password set later**: all vault entries are re-wrapped from the machine-bound key to the Argon2id password-derived key (same `ReWrap` mechanism as SSH).
- **HTTP surface**:
  - `POST /api/unlock` body `{password}`: password users unlock the vault; success `{ok:true, unlocked:true}` and any staged legacy plaintext key is migrated. Passwordless users get `{ok:true, unlocked:true, passwordless:true}`.
  - `GET /api/config`: top-level `hasKey` and per-model `models[].hasApiKey` report "configured" without ever echoing the plaintext; `vaultUnlocked` reports whether the master key is resident in memory.
  - `PUT /api/settings`: a non-empty `apiKey` is sealed into the vault; `clearKey=true` deletes the vault entry; empty without `clearKey` keeps the current value. The `apiKey` field in `settings.json` is always empty — never plaintext on disk.
- **Runtime key retrieval**: `provider.go`/`workflow.go` no longer read `settings.APIKey`; they call `modelAPIKeyLocked()`. If a key is configured but the vault is locked, the model call fails explicitly with "please unlock to use the model" — never silently.
- **Export/import**: `config_backup.go` exports **no** plaintext key; importing a legacy backup's plaintext `apiKey` stages it into the vault on unlock (see [config-backup.md](./config-backup.md)).

## 3. Dual-input for SSH private key

The "key" auth mode in the workspace dialog offers two inputs:

1. **Paste key**: paste the private key body → sealed into the vault;
2. **Pick file**: choose a key path inside the container (restricted to mount roots `/workspace`, `/context`, `/local`; absolute escape and `..` traversal are rejected), then choose storage:
   - **Reference path only**: read the path at runtime; the key is **not stored** and no copy is made;
   - **Import encrypted copy**: read the file contents → sealed into the vault.

In both modes the UI **never echoes the key body** and clears the input after save. On success the public-key **SHA-256 fingerprint** (computed via `ssh-keygen -lf`) is shown for manual host verification.

## 4. Key passphrase

Protected keys may carry a passphrase. It is stored as its own encrypted entry (`ssh-passphrase`) and injected at runtime via an `SSH_ASKPASS` script — **never on the command line** — so it cannot leak through `ps`/process listings.

## 5. Runtime credential lifecycle

```mermaid
flowchart LR
    A[User enters secret] --> B{Account password set?}
    B -- No --> R[Reject save<br/>prompt to set password first]
    B -- Yes --> C[AES-256-GCM seal into vault]
    C --> D[/data/secrets/vault.enc<br/>0600]
    E[Open SSH connection] --> F[vault unlock master key]
    F --> G{Reference path?}
    G -- Yes --> H[-i points to<br/>validated path]
    G -- No --> I[write 0600 temp file]
    I --> J[ssh -i temp file]
    J --> K[defer os.Remove<br/>delete after use]
    H --> L[ControlMaster reuse]
    I --> L
```

- Reference mode: uses the container path validated at save time; no temp file;
- Paste/copy mode: decrypts to a `0600` temp file under `/data/secrets/.tmp/`, `defer os.Remove` removes it as soon as the master connection is up;
- passphrase injected via `SSH_ASKPASS`;
- When the vault is locked (e.g. after restart, before unlock), the runtime cannot read the key and errors out — this is intended; the UI prompts for the account password to unlock.

## 6. Password change and backup export/import

- **Password change**: `ReWrap(oldKey, newKey)` unseals all entries with the old key and reseals with the new; the old key no longer works.
- **Export**: when "include secrets" is checked, the **encrypted envelope** (vault.enc itself) is attached — **never plaintext**;
- **Import**: the envelope is merged into the local vault (never overwriting existing usable entries); same-machine restore (same kdf-salt + account password) works directly; cross-machine restore is not decryptable due to a different kdf-salt, so the user re-enters the credentials.

## 7. Audit

Saving, clearing, unlocking, and using a secret appends an audit record to `data/vault-audit.jsonl` (0600), recording **only the action and timestamp — never any secret body**.

## 8. Compliance mapping

- **OWASP Secret Management**: at-rest encryption, least-privilege file modes, master key off disk, rotation (ReWrap on password change);
- **GDPR Art.32**: encryption and access controls when handling personal/access credentials;
- Threat model: on forensic read of `/data`, without the account password there is no master key, so credentials are unreadable.

## 9. NOT_RUN

- End-to-end real SSH login (requires a reachable sshd/container) is not run in CI (NOT_RUN); the logic is covered by `sshKeygenFingerprintFile` and temp-file-cleanup unit tests.
