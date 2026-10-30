"""Fixed-member update extraction and legacy idle preflight, inside Linux Docker.

Embedded in the Windows executable. The Windows host never runs Python or PS1.
"""
import json
import hashlib
from pathlib import Path
import re
import shutil
import stat
import sys
import zipfile


def extract_full(archive, output, tag, platform):
    with zipfile.ZipFile(archive) as z:
        names = z.namelist()
        candidates = [n for n in names if n.endswith('/docker-images/manifest.json')]
        if len(candidates) != 1 or len(names) != len(set(names)):
            raise ValueError('Full package manifest must be unique')
        prefix = candidates[0][:-len('manifest.json')]
        manifest = json.loads(z.read(candidates[0]))
        name = f'aide-{tag}-linux-{platform.split("/")[-1]}-image.tar.gz'
        if (manifest.get('format') != 'aide-update-package' or manifest.get('version') != 1
                or manifest.get('releaseTag') != tag or manifest.get('platform') != platform
                or manifest.get('imageArchive') != name
                or not re.fullmatch('sha256:[a-f0-9]{64}', manifest.get('imageId', ''))):
            raise ValueError('Full package identity mismatch')
        sums = z.read(prefix + 'SHA256SUMS').decode('ascii').split()
        if len(sums) != 2 or sums[1] != name or not re.fullmatch('[a-f0-9]{64}', sums[0]):
            raise ValueError('Invalid image checksum')
        info = z.getinfo(prefix + name)
        if not 0 < info.file_size <= 2 * 1024**3 or stat.S_ISLNK(info.external_attr >> 16):
            raise ValueError('Invalid image member')
        h = hashlib.sha256()
        with z.open(info) as src, Path(output).open('xb') as dst:
            while data := src.read(65536):
                h.update(data)
                dst.write(data)
        if h.hexdigest() != sums[0]:
            raise ValueError('Full package image checksum mismatch')
        return manifest


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
    if sys.argv[1] == 'extract-full' and len(sys.argv) == 6:
        result = extract_full(*sys.argv[2:])
    elif sys.argv[1] == 'extract-update' and len(sys.argv) == 5:
        result = extract(*sys.argv[2:])
    elif sys.argv[1] == 'check-idle' and len(sys.argv) == 3:
        result = idle(sys.argv[2])
    else:
        raise ValueError('Invalid native container helper action')
    print(json.dumps(result))


if __name__ == '__main__':
    main()
