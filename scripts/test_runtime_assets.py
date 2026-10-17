#!/usr/bin/env python3
"""Exercise shipped assets as the image's normal non-root user, without network."""
import subprocess
import json
import uuid
import argparse
from pathlib import Path

SHIPPED_PLUGINS = ('browser-control', 'computer-control', 'markdown-history', 'archive-tools', 'plugin-authoring')


def check_build_inputs():
    root = Path(__file__).resolve().parents[1]
    for name in ('Dockerfile', 'Dockerfile.local'):
        runtime = (root / name).read_text().split(' AS runtime', 1)[1]
        copies = {tuple(line.split()[1:]) for line in runtime.splitlines() if line.startswith('COPY ')}
        for plugin in SHIPPED_PLUGINS:
            assert (f'plugins/{plugin}', f'/opt/aide/builtin-plugins/{plugin}') in copies, (name, plugin)

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
for plugin in json.loads(os.environ['AIDE_REQUIRED_BUNDLED_PLUGINS']):
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
    parser = argparse.ArgumentParser()
    parser.add_argument('image', nargs='?', default='aide:local')
    parser.add_argument('--require-plugin', action='append', default=[])
    args = parser.parse_args()
    check_build_inputs()
    # The full profile may use a retained dependency image. New candidate
    # qualification explicitly requires newly shipped plugins in that image.
    required = list(SHIPPED_PLUGINS[:-1]) + args.require_plugin
    image = args.image
    container = 'aide-assets-' + uuid.uuid4().hex[:12]
    try:
        subprocess.run(['docker', 'run', '--rm', '--name', container, '--network', 'none',
                        '--workdir', '/tmp', '-e', 'AIDE_REQUIRED_BUNDLED_PLUGINS=' + json.dumps(required),
                        '--entrypoint', 'python3', image, '-c', PROGRAM],
                       check=True, timeout=90)
    finally:
        try:
            subprocess.run(['docker', 'rm', '-f', container], stdout=subprocess.DEVNULL,
                           stderr=subprocess.DEVNULL, timeout=10, check=False)
        except (OSError, subprocess.TimeoutExpired):
            print(f'Cleanup pending for own asset test container: {container}', flush=True)
