# Security: Unified Secret Vault

- Version: 0.1.11.0-RC1
- Date: 2026-09-25
- Scope: workspace SSH/SFTP credentials (password, private key, key passphrase); future comm-ssh (#37) reuses the same vault
- Related: [password-hashing.md](./password-hashing.md) (Argon2id master-key derivation), [config-backup.md](./config-backup.md)

## 1. Problem and goals

Previously the workspace SSH login password and private key were stored in plaintext at `/data/workspace-secrets.json` (mode 0600, but readable as-is). If the `/data` volume were forensically read or a backup leaked, SSH credentials were exposed verbatim.

0.1.11 introduces a unified encrypted secret vault:

- **At-rest encryption**: every secret is sealed with **AES-256-GCM**, each entry using an independent random nonce;
- **Master key**: derived from the **account password via Argon2id** (reusing `kdf.go`), resident only in memory at runtime, never written to disk;
- **Secure by default**: saving any SSH credential is **rejected when no account password is set**;
- **Minimal runtime exposure**: a private key is decrypted only transiently into a controlled temp file when a connection is established, then deleted;
- **One vault**: workspace SSH and the future comm-ssh (#37) share the same store — no second encryption scheme.

## 2. Architecture

```
account password ──Argon2id(kdfSalt)──► master key (32B, memory only)
                                             │ seal/open (AES-256-GCM, 12B nonce)
                                             ▼
                               /data/secrets/vault.enc   (dir 0700 / file 0600, JSON ciphertext envelope)
```

- File: `/data/secrets/vault.enc`, directory `0700`, file `0600`;
- Envelope: `{ "version":1, "entries":[{id,type,name,ciphertext_b64,nonce_b64,fingerprint,createdAt,updatedAt}] }`;
- Entry types: `ssh-password`, `ssh-key` (encrypted key copy), `ssh-passphrase`;
- Fixed IDs: `ws:ssh-password`, `ws:ssh-key`, `ws:ssh-passphrase`;
- `List()` returns metadata and the public-key fingerprint only — **never ciphertext or plaintext**.

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
