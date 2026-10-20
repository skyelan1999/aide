#!/usr/bin/env python3
"""Build an uploadable skill plugin bundle; never install it or mutate registry."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[2]
PLUGIN = Path(__file__).resolve().parent

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--output', type=Path)
    options = parser.parse_args()
    manifest = json.loads((PLUGIN / 'manifest.json').read_text())
    entry = PLUGIN / manifest['main']
    subprocess.run(['node', '--check', str(entry)], check=True)
    result = subprocess.run(['node', '-e', (ROOT / 'internal/server/plugin_host.js').read_text(),
                             'validate', str(entry)], check=True, capture_output=True, text=True)
    validation = json.loads(result.stdout)
    if not validation.get('ok'):
        raise SystemExit(validation)
    with tempfile.TemporaryDirectory(prefix='aide-skill-package-') as temporary:
        surface = Path(temporary) / 'surface.json'
        subprocess.run(['node', '-e', (ROOT / 'internal/server/plugin_host.js').read_text(),
                        'run', str(PLUGIN.parent), json.dumps([manifest]), str(surface)],
                       check=True, capture_output=True, text=True)
        loaded = json.loads(surface.read_text())['plugins'][0]
        if loaded.get('error') or not any(tool['name'] == 'skill_library' and tool.get('executable')
                                           for tool in loaded['tools']):
            raise SystemExit(loaded)
    output = options.output or ROOT / '.agent-state/packages' / ('aide-pkg-skill-v' + manifest['version'] + '.zip')
    output = output.resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    files = {'manifest.json': (PLUGIN / 'manifest.json').read_bytes(),
             'index.js': entry.read_bytes(), 'README.md': (PLUGIN / 'README.md').read_bytes(),
             'LICENSE': (ROOT / 'LICENSE').read_bytes()}
    if len(files['index.js']) > 256 * 1024:
        raise SystemExit('entry exceeds Aide protocol limit')
    with zipfile.ZipFile(output, 'w', zipfile.ZIP_DEFLATED) as bundle:
        for name in sorted(files):
            info = zipfile.ZipInfo(name, date_time=(2026, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = 0o100644 << 16
            bundle.writestr(info, files[name])
    with zipfile.ZipFile(output) as bundle:
        assert bundle.testzip() is None and set(bundle.namelist()) == set(files)
    print(json.dumps({'path': str(output), 'bytes': output.stat().st_size,
                      'sha256': hashlib.sha256(output.read_bytes()).hexdigest(),
                      'hostValidation': validation}, ensure_ascii=False))

if __name__ == '__main__':
    main()
