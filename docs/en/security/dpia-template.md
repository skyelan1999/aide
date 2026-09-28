# aide Data Protection Impact Assessment (DPIA) Template · GDPR Art.35

> A DPIA is normally mandatory only for high-risk processing. aide is local-first and on-device, so inherent risk is low. This template is for deployments that plug aide into high-sensitivity environments (e.g. patient/employee/minor data, public multi-user exposure).

Version: 0.1.11.0-RC1
Date: [2026-__-__]
Assessor: [name/role]
Deployment: [single-user local / shared team server / public exposure]

---

## 1. Description of processing

| Item | Content |
| --- | --- |
| Purpose | [e.g. assist developers to write code, assist knowledge-base Q&A] |
| Data subjects | [e.g. the device user themselves] |
| Data types | conversation text, uploaded files, memory, credentials |
| Nature | user-initiated; local orchestration; optional egress to a user-configured model API |
| Data flow | see security-audit.md §1.2; egress happens only when the user configures an external Provider/edge-tts/cloning service |
| Cross-border? | [no (pure local) / yes (sent to __ service in __)] |

## 2. Necessity and proportionality

- Is processing necessary for the purpose? [yes/no + rationale]
- Can it be done with less data? [default minimisation: audit logs record no body, vault List returns no ciphertext]
- Is retention necessary? [local data is deletable by the user; debug token expires in 365 days]

## 3. Risk identification

| Scenario | Likelihood | Impact | Existing mitigation | Residual |
| --- | --- | --- | --- | --- |
| Read-only volume forensics | medium (needs physical/admin access) | APIKey and conversations readable | 0600/0700, AES-GCM envelopes, Argon2id | medium (APIKey not in vault) |
| Browser MITM | low (loopback) / high (public) | token/conversation eavesdropped | TLS1.2+, self-signed SAN, 127.0.0.1 bind | rises on public exposure |
| External Provider breach | medium | conversations sent to third party | user-chosen endpoint, no telemetry | user/third-party responsibility |
| Unauthorised read of xiaomi memory | low | privacy boundary breached | memory_access.go one-way isolation + shell block | low |
| Plugin poisoning | low | local command execution | default-off, scrubbed env, loopback bind, timeout | low |
| Weak account password | medium | derived key brute-forced | Argon2id 64MiB/t=3/p=4 | low-medium |

## 4. Mitigations (GDPR Art.32)

1. Transport: TLS≥1.2, ECDHE+AES-GCM, loopback binding.
2. At rest: SSH credentials AES-256-GCM vault; xiaomi history encrypted envelope; passwords Argon2id.
3. Least privilege: 0600 files / 0700 dirs; vault List returns no ciphertext.
4. Privacy by default: diagnostics off, plugins disabled, no cookies.
5. Traceability: audit logs record actions, not content.
6. User control: export / delete / factory reset.

## 5. Conclusion and recommendations

- [ ] Risk acceptable, may deploy
- [ ] Must first: move APIKey into the vault, encrypt voice-memory, use a trusted certificate for public deployments

Note: single-user local use typically does not trigger a mandatory Art.35 DPIA; this form serves as a self-assessment record for high-sensitivity / public deployments.
