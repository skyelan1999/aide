#!/usr/bin/env python3
"""Exercise shipped assets as the image's normal non-root user, without network."""
import subprocess
import sys

PROGRAM = r'''
import json, os, subprocess, tempfile
from pathlib import Path
from docx import Document
assert os.geteuid() != 0, 'runtime must use its normal non-root user'
root = Path('/opt/aide')
files = []
def walk_error(error):
    raise error
for directory, _, names in os.walk(root, onerror=walk_error):
    assert os.access(directory, os.R_OK | os.X_OK), directory
    for name in names:
        path = Path(directory) / name
        with path.open('rb') as source:
            source.read(1)
        files.append(path)
assert files, 'application assets are missing'
for plugin in ('browser-control', 'computer-control', 'markdown-history', 'archive-tools'):
    assert any(p.is_relative_to(root / 'builtin-plugins' / plugin) for p in files), plugin
with tempfile.TemporaryDirectory() as temporary:
    spec = Path(temporary) / 'input.json'
    output = Path(temporary) / 'output.docx'
    spec.write_text(json.dumps({'title': 'Runtime asset check', 'blocks': [{'text': 'readable'}]}))
    subprocess.run(['python3', str(root / 'office-scripts/office_artifact.py'),
                    'create', 'docx', str(spec), str(output)], check=True)
    document = Document(output)
    assert document.paragraphs[-1].text == 'readable'
print(json.dumps({'status': 'pass', 'uid': os.geteuid(), 'readableFiles': len(files),
                  'docxRoundTrip': True, 'network': 'none'}))
'''

if __name__ == '__main__':
    image = sys.argv[1] if len(sys.argv) > 1 else 'aide:local'
    subprocess.run(['docker', 'run', '--rm', '--network', 'none', '--entrypoint',
                    'python3', image, '-c', PROGRAM], check=True)
