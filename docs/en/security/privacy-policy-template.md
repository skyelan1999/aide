# aide Privacy Policy Template

> This is an **editable template**. Replace `[bracketed]` placeholders per deployment. aide's core privacy stance is: **local self-hosting, data stays on-device, full user control**.

Effective date: [2026-__-__]
Software version: 0.1.11.0-RC1
Controller: [the individual or organisation installing this software]

---

## 1. What we collect and where it lives

aide is by default a **tool that runs on your own machine**. It does not upload your conversations, memories, or credentials to us or any third-party server — unless you **explicitly** configure one of the optional services below.

| Data type | Location | Encrypted? | Retained until |
| --- | --- | --- | --- |
| Conversations (sessions/) | local Docker volume `/data/sessions/` | xiaomi history is an envelope protected by a key derived from your account password; ordinary session files sit inside the 0700 volume | you delete the session or factory reset |
| xiaomi conversation history (assistant/voice-history.json) | local volume | AES-256-GCM encrypted envelope | you clear it or reset |
| xiaomi long-term memory (assistant/voice-memory.json) | local volume | currently plaintext, protected by the 0700 volume | you clear it or reset |
| aide core memory (memory/core/) | local volume | plaintext, protected by the 0700 volume | you clear it or reset |
| Your password | local auth/password.phc | Argon2id hash (irreversible, not plaintext) | you change it or reset |
| Access token / KDF salt | local auth/, 0600 | token is random; salt derives the key | until reset |
| AI API Key | local config/settings.json (currently plaintext, protected by 0600 file + 0700 dir) | application-layer plaintext (see residual risk) | you delete it or reset |
| Third-party secrets (SSH etc.) | local secrets/vault.enc | AES-256-GCM encrypted envelope | you delete them |
| Audit logs (audit/) | local volume, 0600 | action metadata only, no conversation body | local rolling retention |
| Debug token | local settings.json | SHA-256 hash only | you revoke or disable diagnostics |

## 2. When data leaves your device

Content leaves your device only when you **explicitly** configure one of:

1. **External AI model API**: the BaseURL and API Key you enter in settings. Your prompts and attached content are sent to that endpoint to obtain model replies.
2. **edge-tts (Microsoft Edge online TTS)**: when you select the edge voice engine, the text to be spoken is sent to Microsoft's TTS endpoint.
3. **Self-hosted voice-cloning service**: when you configure the cloning service BaseURL and API Key and select a cloned voice, the audio to synthesise is sent to that service.

These third-party services' privacy practices are outside aide's control; please read each service's own privacy policy.

## 3. What we do not do

- No built-in advertising, no ad profiling.
- No telemetry, crash reporting, or usage statistics sent to our servers.
- No tracking cookies; the web UI authenticates with a one-shot Bearer token.
- We do not write your keys, password hash, or conversation body into audit logs.

## 4. Your rights and control

As a self-hosted user you have full control over your data:

- **Access / export**: settings allow exporting a config backup (masked by default, no keys/ciphertext; attach only when ticked).
- **Rectification**: edit settings directly in the UI, delete or restart sessions.
- **Erasure**: delete individual sessions, clear memory, or run **factory reset** to wipe all personal data.
- **Portability**: the exported JSON backup can be imported on another machine (encrypted envelopes are undecryptable cross-machine; re-enter keys as needed — expected behaviour).
- **Withdraw consent**: remove the external Provider config to stop egress; disable diagnostics to revoke the debug token.

## 5. Security measures summary

Transport over TLS1.2+ throughout (loopback-only binding); passwords via Argon2id slow hash; credentials via AES-256-GCM encrypted envelopes; sensitive files 0600 / dirs 0700; audit logs record actions not content. See [security-audit.md](./security-audit.md) and [eu-compliance.md](./eu-compliance.md).

## 6. Contact

[deployer contact / data protection officer email]
