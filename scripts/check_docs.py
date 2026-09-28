#!/usr/bin/env python3
"""Check repository-owned Markdown links, local images and legacy FR coverage."""
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
prd = (ROOT / 'docs/PRD.md').read_text()
appendix = prd.partition('## 附录 A：历史 FR 基线')[2]
ranges = re.findall(r'^\| FR-(\d+)(?:~(?:FR-)?(\d+))? \|', appendix, re.M)
covered = []
for first, last in ranges:
    start = int(first)
    end = int(last) if last else start
    covered.extend(range(start, end + 1))
if not ranges or len(covered) != len(set(covered)) or set(covered) != set(range(1, 101)):
    errors.append('PRD appendix must cover each legacy FR-01 through FR-100 exactly once')
print('\n'.join(errors) if errors else f'PASS: {len(set(files))} Markdown files, {links} local links, JPEG signatures, FR-01..100')
sys.exit(1 if errors else 0)
