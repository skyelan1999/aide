"""Build an unpublished runtime candidate using the market's deterministic ZIP format."""
import hashlib
import io
import json
from pathlib import Path
import zipfile
import subprocess
root = Path(__file__).resolve().parents[1]
manifest = json.loads((root / 'manifest.json').read_text())
names = ['index.js', 'sessions.js', 'pty-worker.py', 'manifest.json', 'workbench.html', 'README.md', 'LICENSE']
names += ['package.json', 'relay/client.js', 'relay/agent.py']
names += [str(p.relative_to(root)) for p in (root/'communications').glob('*.js')]
names += [str(p.relative_to(root)) for p in (root / 'vendor').glob('*-LICENSE')]
names += json.loads(subprocess.check_output(['node',str(root/'scripts/runtime-files.cjs')],text=True))
assert len(names)<=256, f'{len(names)} runtime files exceeds host limit'
buffer = io.BytesIO()
with zipfile.ZipFile(buffer, 'w') as archive:
    for name in sorted(names):
        item = zipfile.ZipInfo(name, (2026, 1, 1, 0, 0, 0))
        item.compress_type = zipfile.ZIP_DEFLATED
        item.external_attr = 0o100644 << 16
        archive.writestr(item, (root / name).read_bytes())
raw = buffer.getvalue()
dist = root / 'dist'
dist.mkdir(exist_ok=True)
name = f'aide-pkg-{manifest["id"]}-v{manifest["version"]}-candidate.zip'
(dist / name).write_bytes(raw)
(dist / 'SHA256SUMS-candidate').write_text(hashlib.sha256(raw).hexdigest() + '  ' + name + '\n')
print(name, len(raw), 'bytes, SHA-256', hashlib.sha256(raw).hexdigest())
