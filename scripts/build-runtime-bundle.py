#!/usr/bin/env python3
"""Combine the explicitly selected full runtimes; never select by timestamp."""
import hashlib
import json
from pathlib import Path
import zipfile

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'docker-images/aide-runtime-all-platforms.zip'

def sha256(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(chunk)
    return digest.hexdigest()

def main():
    manifest = json.loads((ROOT / 'docker/runtime-bundle.json').read_text())
    rows = manifest['archives']
    if {r['platform'] for r in rows} != {'linux/arm64', 'linux/amd64'} or len(rows) != 2:
        raise ValueError('Exactly one complete runtime per supported architecture is required')
    if len({r['source_sha256'] for r in rows}) != 1:
        raise ValueError('Architectures must use the same source snapshot')
    for row in rows:
        name = row['filename']
        if Path(name).name != name or not name.endswith('-offline.tar.gz'):
            raise ValueError('Invalid archive basename')
        path = ROOT / 'docker-images' / name
        if path.is_symlink() or sha256(path) != row['archive_sha256']:
            raise ValueError('Missing or corrupt selected runtime: ' + name)
    temporary = OUT.with_suffix('.zip.partial')
    try:
        with zipfile.ZipFile(temporary, 'w', compression=zipfile.ZIP_STORED) as archive:
            archive.write(ROOT / 'docs/runtime-bundle.md', 'README.md')
            archive.writestr('runtime-bundle.json', json.dumps(manifest, indent=2) + '\n')
            archive.writestr('SHA256SUMS', ''.join(r['archive_sha256'] + '  ' + r['filename'] + '\n' for r in rows))
            for row in rows:
                archive.write(ROOT / 'docker-images' / row['filename'], row['filename'])
        if temporary.stat().st_size >= 2_000_000_000:
            raise ValueError('Bundle exceeds the conservative Git LFS 2 GB limit')
        temporary.replace(OUT)
    finally:
        temporary.unlink(missing_ok=True)
    digest = sha256(OUT)
    OUT.with_suffix('.zip.sha256').write_text(digest + '  ' + OUT.name + '\n')
    print(json.dumps(dict(path=str(OUT), bytes=OUT.stat().st_size, sha256=digest)))

if __name__ == '__main__':
    main()
