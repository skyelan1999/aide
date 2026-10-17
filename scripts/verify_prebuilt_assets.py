#!/usr/bin/env python3
"""Read-only public-asset gate; never downloads images or changes defaults.

Image identity/import and runtime acceptance remain separate release checks.
"""
import argparse
import json
from pathlib import Path
import re
import sys
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
HEX = re.compile(r'[a-f0-9]{64}')
URL = re.compile(r'https://github\.com/skyelan1999/aide/releases/download/([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+\.tar\.gz)')


def read_manifest(path):
    rows = []
    for number, line in enumerate(Path(path).read_text().splitlines(), 1):
        if not line.strip() or line.lstrip().startswith('#'):
            continue
        fields = line.split()
        if len(fields) != 6:
            raise ValueError(f'Manifest line {number}: expected six fields')
        platform, image, source, digest, url, kind = fields
        match = URL.fullmatch(url)
        if (platform not in ('linux/arm64', 'linux/amd64') or kind != 'bundle'
                or not image.startswith('sha256:') or not HEX.fullmatch(image[7:])
                or not HEX.fullmatch(source) or not HEX.fullmatch(digest) or not match):
            raise ValueError(f'Manifest line {number}: invalid identity, platform or public URL')
        tag, name = match.groups()
        suffix = f'-{platform.replace("/", "-")}-{image[7:19]}-offline.tar.gz'
        if not name.endswith(suffix):
            raise ValueError(f'Manifest line {number}: archive name does not match platform/image')
        rows.append(dict(platform=platform, image=image, source=source, digest=digest,
                         url=url, tag=tag, name=name))
    if sorted(r['platform'] for r in rows) != ['linux/amd64', 'linux/arm64']:
        raise ValueError('Default manifest requires exactly one ARM64 and one AMD64 bundle')
    if len({r['source'] for r in rows}) != 1 or len({r['tag'] for r in rows}) != 1:
        raise ValueError('Default bundles must share the same source identity and Release tag')
    return rows


def fetch_release(tag):
    url = 'https://api.github.com/repos/skyelan1999/aide/releases/tags/' + tag
    request = urllib.request.Request(url, headers={
        'User-Agent': 'aide-public-asset-verifier', 'Accept': 'application/vnd.github+json'})
    with urllib.request.urlopen(request, timeout=30) as response:
        body = response.read((4 << 20) + 1)
    if len(body) > 4 << 20:
        raise ValueError('Release metadata exceeds 4 MiB limit')
    return json.loads(body)


def verify(path, fetch=fetch_release):
    rows = read_manifest(path)
    release = fetch(rows[0]['tag'])
    if (not isinstance(release, dict) or release.get('draft') is not False
            or release.get('tag_name') != rows[0]['tag']
            or not isinstance(release.get('assets'), list)):
        raise ValueError('Release must be public, non-draft and match the requested tag')
    results = []
    for row in rows:
        matches = [a for a in release['assets'] if isinstance(a, dict) and a.get('name') == row['name']]
        if len(matches) != 1:
            raise ValueError(f'{row["platform"]}: no unique uploaded public asset')
        asset = matches[0]
        size = asset.get('size')
        if (asset.get('state') != 'uploaded' or type(size) is not int or size <= 0
                or asset.get('digest') != 'sha256:' + row['digest']
                or asset.get('browser_download_url') != row['url']):
            raise ValueError(f'{row["platform"]}: public asset status, digest or URL mismatch')
        results.append(dict(platform=row['platform'], asset=row['name'], bytes=size,
                            sha256=row['digest'], status='pass'))
    return dict(status='pass', tag=rows[0]['tag'], source=rows[0]['source'], assets=results,
                scope='Public attachment metadata only; actual image import/runtime are separate',
                mutations=False)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('manifest', nargs='?', type=Path, default=ROOT / 'docker/prebuilt-images.tsv')
    args = parser.parse_args(argv)
    try:
        result = verify(args.manifest)
    except (ValueError, OSError) as exc:
        print(json.dumps(dict(status='fail', error=str(exc), mutations=False)))
        return 1
    print(json.dumps(result, indent=2))
    return 0


if __name__ == '__main__':
    sys.exit(main())
