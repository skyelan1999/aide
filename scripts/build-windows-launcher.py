#!/usr/bin/env python3
"""Cross-compile the native C++ launcher inside a local Docker image.

No host compiler, SDK, PowerShell invocation, or system installation is used.
The resulting executable needs only Windows and Docker Desktop at runtime.
"""
import argparse
import hashlib
import os
from pathlib import Path
import subprocess
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
VERSION = '0.15.2'
ARCHIVE = f'zig-x86_64-linux-{VERSION}.tar.xz'
SHA256 = '02aa270f183da276e5b5920b1dac44a63f1a49e55050ebde3aecc9eb82f93239'
JSON_SHA256 = 'aaf127c04cb31c406e5b04a63f1ae89369fccde6d8fa7cdda1ed4f32dfc5de63'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image', default=os.environ.get('AIDE_LAUNCHER_BUILD_IMAGE', 'aide:local'))
    parser.add_argument('--arch', choices=('amd64', 'arm64'), default='amd64')
    args = parser.parse_args()
    build = ROOT / '.agent-state/native-toolchain'
    build.mkdir(parents=True, exist_ok=True)
    archive = build / f'zig-linux-{VERSION}.tar.xz'
    if not archive.exists():
        partial = archive.with_suffix('.partial')
        urllib.request.urlretrieve(f'https://ziglang.org/download/{VERSION}/{ARCHIVE}', partial)
        if hashlib.file_digest(partial.open('rb'), 'sha256').hexdigest() != SHA256:
            raise SystemExit('Official Linux compiler archive SHA256 mismatch')
        partial.replace(archive)
    if hashlib.file_digest(archive.open('rb'), 'sha256').hexdigest() != SHA256:
        raise SystemExit('Cached Linux compiler SHA256 mismatch')
    header = ROOT / 'native/windows-launcher/third_party/json.hpp'
    if hashlib.file_digest(header.open('rb'), 'sha256').hexdigest() != JSON_SHA256:
        raise SystemExit('Pinned nlohmann/json 3.12.0 header SHA256 mismatch')
    embedded = '#pragma once\n'
    for name, path in [('compose', ROOT / 'docker/compose.offline.yaml'),
                       ('data', ROOT / 'scripts/launcher-data.py')]:
        text = path.read_text(encoding='utf-8-sig')
        if ')AIDE_EMBED"' in text:
            raise SystemExit('Embedded source delimiter collision')
        embedded += f'inline const char* embedded_{name}=R"AIDE_EMBED({text})AIDE_EMBED";\n'
    (build / 'embedded.hpp').write_text(embedded, encoding='utf-8')
    compiler = f'/toolchain/zig-x86_64-linux-{VERSION}/zig'
    container_code = r'''
import os, pathlib, subprocess, tarfile
compiler=pathlib.Path(COMPILER)
ready=pathlib.Path('/toolchain/extracted-0.15.2')
if not ready.exists():
    with tarfile.open('/build/zig-linux-0.15.2.tar.xz') as archive:
        archive.extractall('/toolchain', filter='data')
    ready.write_text('verified compiler extracted')
os.environ['ZIG_GLOBAL_CACHE_DIR']='/toolchain/zig-cache'
subprocess.run([str(compiler),'c++','-std=c++17','-O2','-target',TARGET,
    '-municode','-static','-I/build','-ladvapi32','-lbcrypt','-lws2_32','-lshell32','-lole32','-luuid',
    '/src/native/windows-launcher/main.cpp','-o',OUTPUT],check=True)
'''.replace('COMPILER', repr(compiler)).replace('TARGET', repr('x86_64-windows-gnu' if args.arch == 'amd64' else 'aarch64-windows-gnu'))
    output = '/src/aide-launcher.exe' if args.arch == 'amd64' else '/src/bin/aide-launcher-arm64.exe'
    (ROOT / 'bin').mkdir(exist_ok=True)
    container_code = container_code.replace('OUTPUT', repr(output))
    identity = subprocess.check_output(['docker', 'image', 'inspect', args.image, '--format', '{{.Id}}'], text=True).strip()
    if not identity.startswith('sha256:'):
        raise SystemExit('A local Linux build image with Python 3.12+ is required')
    subprocess.run(['docker', 'run', '--rm', '--pull', 'never', '--network', 'none',
                    '--memory', '4g', '--cpus', '4', '--user', '0:0', '--no-healthcheck',
                    '-v', f'{ROOT}:/src', '-v', f'{build}:/build',
                    '--mount', 'type=volume,source=aide-native-launcher-toolchain-v0152,target=/toolchain',
                    '--entrypoint', 'python3', identity, '-c', container_code], check=True)
    destination = ROOT / ('aide-launcher.exe' if args.arch == 'amd64' else 'bin/aide-launcher-arm64.exe')
    symbols = destination.with_suffix('.pdb')
    if symbols.exists():
        symbols.replace(build / f'aide-launcher-{args.arch}.pdb')
    print(f'Built {destination} ({destination.stat().st_size} bytes)')
    print('SHA256 ' + hashlib.file_digest(destination.open('rb'), 'sha256').hexdigest())


if __name__ == '__main__':
    main()
