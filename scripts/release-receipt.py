#!/usr/bin/env python3
"""Create an operator receipt from captured gh release JSON and local assets.
This records observations; it does not download/verify remote asset contents.
"""
import argparse
import hashlib
import json
import re
from datetime import datetime, timezone
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--release-json', required=True, type=Path)
    parser.add_argument('--assets', required=True, type=Path)
    parser.add_argument('--commit', required=True)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    release = json.loads(args.release_json.read_text())
    tag = release['tagName']
    if not re.fullmatch(r'v\d+\.\d+\.\d+\.\d+-RC\d+', tag) or not re.fullmatch(r'[a-f0-9]{40}', args.commit):
        parser.error('invalid tag or commit')
    assets = []
    for remote in release.get('assets', []):
        name = remote['name']
        if Path(name).name != name or name in ('.', '..'):
            parser.error('invalid asset name')
        local = args.assets / name
        if not local.is_file() or local.is_symlink():
            parser.error('missing regular local asset: ' + name)
        digest = hashlib.sha256()
        size = 0
        with local.open('rb') as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b''):
                digest.update(chunk)
                size += len(chunk)
        if remote.get('size') != size:
            parser.error('remote metadata size mismatch: ' + name)
        assets.append({'name': name, 'sha256': digest.hexdigest(), 'bytes': size})
    if not 1 <= len(assets) <= 64:
        parser.error('expected 1–64 assets')
    receipt = {'schema': 1, 'tag': tag, 'commit': args.commit,
               'url': release['url'], 'observedAt': datetime.now(timezone.utc).isoformat(),
               'deployment': 'unknown', 'artifacts': assets}
    # Never overwrite an earlier observation silently.
    with args.output.open('x') as stream:
        json.dump(receipt, stream, ensure_ascii=False, indent=2)
        stream.write('\n')


if __name__ == '__main__':
    main()
