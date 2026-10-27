"""Fixed-member update extraction and legacy idle preflight, inside Linux Docker.

Embedded in the Windows executable. The Windows host never runs Python or PS1.
"""
import json
from pathlib import Path
import re
import shutil
import stat
import sys
import zipfile


def extract(archive, output, name):
    if not re.fullmatch(r'aide-v\d+\.\d+\.\d+\.\d+-RC\d+-linux-(?:amd64|arm64)-image\.tar\.gz', name):
        raise ValueError('Invalid fixed image member name')
    output = Path(output)
    with zipfile.ZipFile(archive) as package:
        matches = [entry for entry in package.infolist() if entry.filename == name]
        if len(matches) != 1:
            raise ValueError('Image member must be unique')
        entry = matches[0]
        mode = entry.external_attr >> 16
        if entry.is_dir() or entry.flag_bits & 1 or stat.S_ISLNK(mode) or (stat.S_IFMT(mode) not in (0, stat.S_IFREG)):
            raise ValueError('Image member must be unencrypted and regular')
        if entry.file_size <= 0 or entry.file_size > 64 * 1024**3:
            raise ValueError('Image member size exceeds supported update budget')
        if shutil.disk_usage(output.parent).free < entry.file_size + 256 * 1024**2:
            raise ValueError('Insufficient owned staging space')
        with package.open(entry) as source, output.open('xb') as target:
            written = 0
            while data := source.read(65536):
                written += len(data)
                if written > entry.file_size:
                    raise ValueError('Image member exceeds declared size')
                target.write(data)
            if written != entry.file_size:
                raise ValueError('Image member is truncated')
    return {'bytes': written}


def idle(data):
    terminal = {'completed', 'failed', 'cancelled', 'canceled', 'stopped', 'error', 'done', 'resumed', ''}
    paths = list((Path(data) / 'sessions').rglob('*.json')) + list(Path(data).glob('session-*.json'))
    for path in paths:
        record = json.loads(path.read_text(encoding='utf-8'))
        if not isinstance(record, dict):
            raise ValueError('Unreadable session prevents migration')
        runs = record.get('runs')
        if runs is None:
            runs = []
        if not isinstance(runs, list) or any(not isinstance(run, dict) or run.get('status', '') not in terminal for run in runs if run is not None):
            raise ValueError('Active, suspended or unreadable work prevents migration')
    return {'idle': True}


def main():
    if sys.argv[1] == 'extract-update' and len(sys.argv) == 5:
        result = extract(*sys.argv[2:])
    elif sys.argv[1] == 'check-idle' and len(sys.argv) == 3:
        result = idle(sys.argv[2])
    else:
        raise ValueError('Invalid native container helper action')
    print(json.dumps(result))


if __name__ == '__main__':
    main()
