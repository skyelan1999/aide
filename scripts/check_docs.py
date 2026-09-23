#!/usr/bin/env python3
"""Check repository-owned Markdown links, local images and requirement IDs."""
from pathlib import Path
import re
import sys
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parents[1]
files = [*ROOT.glob('*.md'), *ROOT.glob('docs/**/*.md'), ROOT / 'docker-images/README.md']
errors = []
if (ROOT / 'doc').exists():
    errors.append('Use docs only; obsolete doc directory exists')
links = 0
for path in sorted(set(files)):
    text = re.sub(r'^```[^\n]*\n.*?^```\s*$', '', path.read_text(), flags=re.M | re.S)
    for match in re.finditer(r'\]\((<[^>]+>|[^)]+)\)', text):
        target = match[1].strip('<>')
        if re.match(r'^[a-zA-Z][\w+.-]*:', target):
            continue
        name = unquote(target.split('#')[0])
        if not name:
            continue
        links += 1
        destination = (path.parent / name).resolve()
        if not destination.is_relative_to(ROOT) or not destination.exists():
            errors.append(str(path.relative_to(ROOT)) + ': broken/nonportable link ' + target)
    if path.suffix == '.md' and 'archive' not in path.parts and re.search(r'(?<![\w])doc/', re.sub(r'https?://[^\s)]+', '', text)) and '不再创建 `doc/`' not in text:
        errors.append(str(path.relative_to(ROOT)) + ': obsolete doc/ path')
for image in (ROOT / 'docs/images').glob('*.jpg'):
    if not image.read_bytes().startswith(b'\xff\xd8\xff'):
        errors.append('Invalid JPEG: ' + str(image))
ids = re.findall(r'^\| FR-(\d+) \|', (ROOT / 'docs/PRD.md').read_text().split('## 3.')[0], re.M)
if len(ids) != len(set(ids)) or set(map(int, ids)) != set(range(1, 101)):
    errors.append('PRD must preserve unique FR-01 through FR-100')
print('\n'.join(errors) if errors else f'PASS: {len(set(files))} Markdown files, {links} local links, JPEG signatures, FR-01..100')
sys.exit(1 if errors else 0)
