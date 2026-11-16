#!/usr/bin/env python3
"""Build-time installation only. Runtime never downloads an Office engine."""
import argparse
import hashlib
import json
from pathlib import Path
import platform
import shutil
import tarfile
import tempfile
import urllib.request


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        while chunk := stream.read(1024 * 1024):
            h.update(chunk)
    return h.hexdigest()


def install(manifest, output, cache, arch, offline=False):
    record = json.loads(manifest.read_text())
    source = record['sources'][arch]
    cache.mkdir(parents=True, exist_ok=True)
    archive = cache / (source['archiveSHA256'] + '.tar.gz')
    if not archive.exists() and (manifest.parent / archive.name).is_file():
        shutil.copy2(manifest.parent / archive.name, archive)
    if not archive.exists():
        if offline:
            raise ValueError('Missing pinned Office engine in build cache')
        with tempfile.NamedTemporaryFile(dir=cache, delete=False) as temp:
            partial = Path(temp.name)
        try:
            with urllib.request.urlopen(source['url'], timeout=120) as response, partial.open('wb') as dest:
                shutil.copyfileobj(response, dest)
            if digest(partial) != source['archiveSHA256']:
                raise ValueError('Office archive checksum mismatch')
            partial.replace(archive)
        finally:
            partial.unlink(missing_ok=True)
    if digest(archive) != source['archiveSHA256']:
        raise ValueError('Office archive checksum mismatch')
    output.mkdir(parents=True, exist_ok=True)
    executable = output / 'Collabora_Online.AppImage'
    with tarfile.open(archive) as package:
        matches = [member for member in package.getmembers() if member.name == source['member']]
        if len(matches) != 1 or not matches[0].isfile() or matches[0].size != source['appImageBytes']:
            raise ValueError('Unexpected Office engine member')
        with package.extractfile(matches[0]) as src, executable.open('wb') as dst:
            shutil.copyfileobj(src, dst)
    if digest(executable) != source['appImageSHA256']:
        executable.unlink()
        raise ValueError('Office engine checksum mismatch')
    executable.chmod(0o755)
    shutil.copy2(manifest, output / 'source.json')
    print(f'Bundled Office {record["version"]} {arch}: {source["appImageSHA256"]}')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path, default=Path('docker/office-engine/source.json'))
    parser.add_argument('--output', type=Path, default=Path('/opt/aide/office-engine'))
    parser.add_argument('--cache', type=Path, default=Path('/root/.cache/aide/office-engine'))
    parser.add_argument('--arch', choices=['arm64', 'amd64'], default={'aarch64': 'arm64', 'arm64': 'arm64', 'x86_64': 'amd64'}.get(platform.machine()))
    parser.add_argument('--offline', action='store_true')
    args = parser.parse_args()
    install(args.manifest, args.output, args.cache, args.arch, args.offline)
