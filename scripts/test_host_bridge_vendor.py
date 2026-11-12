#!/usr/bin/env python3
"""Verify the bounded native bridge parser overlay after vendoring."""
from pathlib import Path
import hashlib,json
root=Path(__file__).resolve().parents[1]
for name,pin in json.loads((root/'internal/hostbridge/vendor-patches.json').read_text()).items():
    assert hashlib.sha256((root/name).read_bytes()).hexdigest()==pin['patchedSHA256'],name+': bridge parser overlay changed or missing'
print('PASS: pinned NFS/XDR bridge request, opaque/list/read bounds and connection cleanup overlay')
