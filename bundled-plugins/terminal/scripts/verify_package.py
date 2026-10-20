"""Exercise real networking, native serial binding and SSH from the runtime ZIP alone."""
import argparse
import json
import shutil
import subprocess
import tempfile
import zipfile
from pathlib import Path
root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument('archive', type=Path)
args = parser.parse_args()
with tempfile.TemporaryDirectory(prefix='aide-terminal-runtime-') as temporary:
    target = Path(temporary)
    with zipfile.ZipFile(args.archive) as archive:
        assert archive.testzip() is None
        for item in archive.infolist():
            parts = Path(item.filename).parts
            assert not Path(item.filename).is_absolute() and all(p not in ('.', '..') and not p.startswith('.') for p in parts)
            destination = target / item.filename
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes(archive.read(item))
    tests = target / 'test'
    tests.mkdir()
    for name in ['communications.test.js', 'communications-security.test.js', 'ssh-auth.test.js', 'serial-devices.test.js', 'relay.test.js']:
        shutil.copyfile(root / 'test' / name, tests / name)
    subprocess.run(['node', '--test', *[str(file) for file in sorted(tests.glob('*.test.js'))]], cwd=target, check=True)
    print('PASS: tests loaded only the extracted runtime and its bundled production dependencies')
