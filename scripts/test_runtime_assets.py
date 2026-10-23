#!/usr/bin/env python3
"""Exercise shipped assets as the image's normal non-root user, without network."""
import subprocess
import json
import uuid
import argparse
from pathlib import Path



def check_build_inputs():
    root = Path(__file__).resolve().parents[1]
    for name in ('Dockerfile', 'Dockerfile.local'):
        runtime = (root / name).read_text().split(' AS runtime', 1)[1]
        copies = {tuple(line.split()[1:]) for line in runtime.splitlines() if line.startswith('COPY ')}
        assert 'builtin-plugins' not in runtime, name
        assert 'bundled-plugins' not in (root/name).read_text(), name
        assert ('--from=build', '/opt/aide-message-lan', '/opt/aide/message-lan') in copies, name

PROGRAM = r'''
import hashlib, json, os, shutil, subprocess, tempfile
from pathlib import Path
from docx import Document
assert os.geteuid() != 0, 'runtime must use its normal non-root user'
if os.environ.get('AIDE_RUNTIME_PROFILE') == 'slim':
    for tool in ('go', 'gcc', 'g++', 'cc', 'c++', 'make', 'npm', 'npx'):
        assert shutil.which(tool) is None, 'unexpected development tool: ' + tool
    assert not Path('/usr/local/go').exists()
    assert not Path('/usr/lib/gcc').exists()
    assert not Path('/usr/local/lib/node_modules').exists()
    for tool in ('python3', 'node', 'git', 'ssh', 'curl'):
        assert shutil.which(tool), 'missing runtime tool: ' + tool
    import sys, ezdxf, openpyxl, pptx, pypdf, cryptography
    engine = '/usr/local/bin/sherpa-onnx-offline-tts'
    assert Path(engine).is_file(), 'offline TTS engine missing'
    assert 'not found' not in subprocess.check_output(['ldd', engine], text=True)
    assert 'not found' not in subprocess.check_output(['ldd', '/usr/local/bin/node'], text=True)
    assert sys.version_info[:2] == (3, 12)
    assert subprocess.check_output(['node', '--version'], text=True).startswith('v24.')
    with tempfile.TemporaryDirectory() as dxf_directory:
        dxf_path = Path(dxf_directory) / 'roundtrip.dxf'
        drawing = ezdxf.new('R2010')
        drawing.modelspace().add_line((0, 0), (10, 20))
        drawing.saveas(dxf_path)
        assert len(ezdxf.readfile(dxf_path).modelspace().query('LINE')) == 1

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
if os.environ.get('AIDE_REQUIRE_LAN_BRIDGE') == '1':
    bridge = root / 'message-lan'
    entries = dict(line.split()[::-1] for line in (bridge / 'SHA256SUMS').read_text().splitlines())
    required = {family + '-' + platform + suffix
        for family in ('aide-message-lan', 'aide-local-mounts')
        for platform, suffix in (
            ('darwin-arm64', ''), ('darwin-amd64', ''), ('linux-arm64', ''),
            ('linux-amd64', ''), ('windows-arm64', '.exe'), ('windows-amd64', '.exe'))}
    assert set(entries) == required, 'native LAN platform inventory mismatch'
    for name, digest in entries.items():
        assert hashlib.sha256((bridge / name).read_bytes()).hexdigest() == digest, name
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
    parser.add_argument('--require-lan-bridge', action='store_true')
    args = parser.parse_args()
    check_build_inputs()
    image = args.image
    container = 'aide-assets-' + uuid.uuid4().hex[:12]
    try:
        subprocess.run(['docker', 'run', '--rm', '--name', container, '--network', 'none',
                        '--workdir', '/tmp',
                        '-e', 'AIDE_REQUIRE_LAN_BRIDGE=' + ('1' if args.require_lan_bridge else '0'),
                        '--entrypoint', 'python3', image, '-c', PROGRAM],
                       check=True, timeout=90)
    finally:
        try:
            subprocess.run(['docker', 'rm', '-f', container], stdout=subprocess.DEVNULL,
                           stderr=subprocess.DEVNULL, timeout=10, check=False)
        except (OSError, subprocess.TimeoutExpired):
            print(f'Cleanup pending for own asset test container: {container}', flush=True)
