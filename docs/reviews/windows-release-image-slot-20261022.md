# Windows offline image identity fix

The RC2 AMD64 package records `sha256:a6c4f7031025a1f0f111b0fa265dd6a889ac25643e34f8972f390c9b1dabc07b`. The user's Windows Docker Desktop containerd image store imported its release tag with local ID `sha256:780817d1bf54693cccb2745869afeb11cad6bd81f9f809e32e35823cc762d247`. Startup then attempted to tag the unavailable original index ID and stopped before initializing the A/B slot.

`start.ps1` retains the archive SHA256 verification and engine platform check. After a successful verified import it first inspects the expected identity, then, if unavailable, resolves the single image reference announced by this specific `docker image load`. It validates the returned local ID and platform before assigning the slot, checks the slot identity again, and only then persists the local ID to `.aide-image`. It does not search arbitrary installed images or trust an unrelated existing release tag. Failed, ambiguous or wrong-platform imports leave the marker unchanged. Existing data paths are unchanged.

Focused verification uses actual PowerShell 7.6.7 on macOS ARM64 with isolated mocked Docker responses and temporary package fixtures. The actual bootstrap block is extracted from `start.ps1`, including archive hashing, tagging, readback and marker persistence. The pre-fix script reproduced the reported A/B initialization exception. The fixed script passed 12 cases: changed ID, legacy stable ID, ID-form load output, repeated start, bad archive hash, failed load, ambiguous output, empty output, wrong platform, missing imported image, failed tag and failed readback. Additional launcher checks passed: Windows port lifecycle (4), launcher data (38), offline bundle (13), resource guard (7).

Windows PowerShell 5.1 and a physical Windows Docker Desktop installation are NOT_RUN. The script retains its UTF-8 BOM and uses PowerShell 5.1-compatible constructs. Mocked Docker cases do not replace a Windows-native end-to-end test. The user's machine provides the actual incident evidence. Linux/macOS launchers and the in-app updater's archive importer are outside this Windows startup patch.

Rollback: restore the backed-up `start.ps1` and `.aide-image` marker if needed. Do not delete `.aide-data`, `.env`, workspace directories or Docker volumes. No daily production container was restarted for this fix.

Upstream context: https://github.com/moby/moby/issues/48401 documents differing image IDs after archive save/load across Docker image stores.

## Focused regression output

```text
Loaded image: aide:0.1.17.5-RC2-linux-amd64
PASS changed-id
Loaded image: aide:0.1.17.5-RC2-linux-amd64
PASS legacy
Loaded image ID: sha256:7777777777777777777777777777777777777777777777777777777777777777
PASS id-output
PASS repeat
PASS bad-hash
PASS load-failure
Loaded image: aide:0.1.17.5-RC2-linux-amd64
Loaded image: other:tag
PASS ambiguous

PASS empty-output
Loaded image: aide:0.1.17.5-RC2-linux-amd64
PASS wrong-platform
Loaded image: aide:0.1.17.5-RC2-linux-amd64
PASS missing
Loaded image: aide:0.1.17.5-RC2-linux-amd64
PASS tag-failure
Loaded image: aide:0.1.17.5-RC2-linux-amd64
PASS readback-failure
PASS: 12 actual PowerShell offline bootstrap cases (mock Docker; not native Windows)
```

## Release gate

All 145 full checks passed. The server race suite executed all 1109 top-level tests, fuzz seed sets and examples across 12 disjoint shards; other packages, vet, runtime assets and CAD geometry also passed. Content fingerprint: `d907beba99282c9fa5e2ec596e54f6007539ff1b9c4823d27874fb09653b3ee9`. Actual PowerShell native-command integration additionally matched the existing real Docker image identity through read-only inspection; this does not represent an additional archive import or a Windows-native test.
