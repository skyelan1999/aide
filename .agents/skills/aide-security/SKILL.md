---
name: aide-security
description: Review or implement Aide authentication, permissions, tokens, secrets, path safety, encryption, or other security-sensitive behavior.
---

# Aide security-sensitive changes

- Trace the server-side enforcement path; a hidden control or client-side check is not authorization. Consult `docs/security/` and the relevant source before changing policy.
- Preserve least privilege, same-origin/token checks, path containment, secret redaction, and encrypted-at-rest behavior. Treat logs, fixtures, and browser storage as potential disclosure paths.
- Add focused positive and negative-path regression coverage. Do not use real credentials or live production data. State the threat boundary and any behavior not exercised.
- Coordinate interface changes with backend/frontend owners; security-sensitive edits need coordinator review before integration.
