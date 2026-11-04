#!/usr/bin/env python3
"""Guard the Windows native bootstrap's persistent, checkout-owned BuildKit cache."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
bootstrap = (ROOT / 'scripts/windows-bootstrap.bat').read_text(encoding='utf-8')
dockerfile = (ROOT / 'Dockerfile').read_text(encoding='utf-8')

assert 'AIDE_BOOT_SCOPE=%%H' in bootstrap, 'Builder identity must be stable for this checkout'
assert 'AIDE_BOOT_RECEIPT=' in bootstrap and 'findstr.exe /x /l' in bootstrap, 'Existing builders require a matching ownership receipt'
assert 'buildx inspect "%AIDE_BOOT_BUILDER%"' in bootstrap, 'Builder reuse must be checked before build'
assert 'buildx create --name "%AIDE_BOOT_BUILDER%"' in bootstrap, 'A missing owned builder must be initialized'
assert 'buildx rm "%AIDE_BOOT_BUILDER%"' not in bootstrap, 'Bootstrap must preserve builder cache across builds'
assert 'buildx stop "%AIDE_BOOT_BUILDER%"' in bootstrap, 'Idle bootstrap must stop its builder without deleting its cache'
assert '%RANDOM%' not in bootstrap.split('AIDE_BOOT_IMAGE=', 1)[0], 'Builder name must not be randomized per build'
assert 'RUN --mount=type=cache,target=/root/.cache/go-build' in dockerfile and 'go build -mod=vendor' in dockerfile, 'Go cache mount must remain enabled for compilation'
print('PASS Windows bootstrap keeps a stable, receipt-validated BuildKit cache between builds')
