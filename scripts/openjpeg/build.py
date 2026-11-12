#!/usr/bin/env python3
"""Rebuild Aide's decoder from immutable sources in a pinned offline compiler.

Downloads are checksum verified before extraction. The container has no network;
this command never builds, starts or replaces the Aide application.
"""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tarfile
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def source(pin, target, cache):
    archive = cache / (target.name + '.tar.gz')
    if not archive.is_file():
        with urllib.request.urlopen(pin['url'], timeout=60) as response, archive.open('wb') as output:
            shutil.copyfileobj(response, output)
    if digest(archive) != pin['sha256']:
        raise ValueError('Source archive SHA-256 mismatch: ' + archive.name)
    if target.exists():
        raise ValueError('Use a new build directory; source trees are never reset: ' + str(target))
    target.mkdir()
    with tarfile.open(archive) as bundle:
        for member in bundle.getmembers():
            parts = Path(member.name).parts
            if not parts or Path(member.name).is_absolute() or '..' in parts or not (member.isfile() or member.isdir()):
                raise ValueError('Unsafe source archive member')
            if len(parts) == 1:
                continue
            member.name = str(Path(*parts[1:]))
            bundle.extract(member, target)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work', type=Path, required=True, help='new isolated build directory')
    parser.add_argument('--check', action='store_true', help='require bytes identical to the checked-in artifact')
    parser.add_argument('--update', action='store_true', help='copy the rebuilt decoder into the repository')
    args = parser.parse_args()
    pins = json.loads((HERE / 'pins.json').read_text())
    work = args.work.resolve()
    work.mkdir(parents=True, exist_ok=True)
    core, wrapper = work / 'core', work / 'wrapper'
    for name, target in [('core', core), ('wrapper', wrapper)]:
        patch = HERE / (('native' if name == 'core' else name) + '-fixes.patch')
        if digest(patch) != pins[name]['patchSHA256']:
            raise ValueError('Reviewed patch changed: ' + patch.name)
        source(pins[name], target, work)
        subprocess.run(['git', 'init', '-q', str(target)], check=True)
        subprocess.run(['git', 'apply', str(patch)], cwd=target, check=True)
    (wrapper / 'output').mkdir()
    (work / 'compiler-cache').mkdir(exist_ok=True)
    command = [
        'docker', 'run', '--rm', '--network', 'none', '--entrypoint', 'sh',
        '-v', str(core) + ':/core', '-v', str(wrapper) + ':/wrapper',
        '-v', str(work / 'compiler-cache') + ':/tmp/em_cache_js', pins['compiler'],
        '-c', 'OPENJPEG=/core BUILD_TYPE=js BUILD_DIR=build_js sh /wrapper/compile_lib.sh && '
              'OPENJPEG=/core BUILD_TYPE=js INPUT=/wrapper/src OUTPUT=/wrapper/output sh /wrapper/compile.sh',
    ]
    subprocess.run(command, check=True)
    output = wrapper / 'output/openjpeg_nowasm_fallback.js'
    destination = ROOT / 'internal/server/web/vendor/pdfjs/openjpeg/aide_openjpeg_nowasm_fallback.js'
    result = {'sha256': digest(output), 'compiler': pins['compiler'], 'output': str(output)}
    if args.check and (not destination.is_file() or digest(destination) != result['sha256']):
        raise ValueError('Clean rebuild differs from checked-in decoder')
    if args.update:
        shutil.copyfile(output, destination)
    (work / 'build-receipt.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
