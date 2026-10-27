#!/usr/bin/env python3
"""Run the C++ executable against real Docker in an isolated Unicode directory.

Uses a local runtime only. No PowerShell, downloads, production data or browser.
"""
import argparse
import json
import os
from pathlib import Path
import shutil
import socket
import struct
import subprocess
import tempfile
import time
import uuid

ROOT = Path(__file__).resolve().parents[1]


def check_native_imports(executable):
    """Check the delivered PE has no separately installed language runtime."""
    data = executable.read_bytes()
    pe = struct.unpack_from('<I', data, 0x3c)[0]
    assert data[pe:pe + 4] == b'PE\0\0'
    sections = struct.unpack_from('<H', data, pe + 6)[0]
    optional_size = struct.unpack_from('<H', data, pe + 20)[0]
    optional = pe + 24
    assert struct.unpack_from('<H', data, optional)[0] == 0x20b, 'Expected 64-bit native PE'
    table = optional + optional_size

    def offset(rva):
        for i in range(sections):
            size, start, raw_size, raw = struct.unpack_from('<IIII', data, table + 40 * i + 8)
            if start <= rva < start + max(size, raw_size):
                return raw + rva - start
        raise AssertionError('PE import RVA outside sections')

    imports_rva = struct.unpack_from('<I', data, optional + 112 + 8)[0]
    current = offset(imports_rva)
    names = []
    while any(data[current:current + 20]):
        name = offset(struct.unpack_from('<I', data, current + 12)[0])
        names.append(data[name:data.index(b'\0', name)].decode('ascii').lower())
        current += 20
    assert not any(any(runtime in name for runtime in
                       ('vcruntime', 'msvcp', 'libstdc++', 'libgcc', 'libwinpthread', 'python', 'powershell'))
                   for name in names), names
    print('PASS PE imports use Windows system DLLs: ' + ', '.join(names))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--exe', type=Path, default=ROOT / 'aide-launcher.exe')
    parser.add_argument('--image', default='aide:local')
    args = parser.parse_args()
    check_native_imports(args.exe)
    subprocess.run([str(args.exe), '--self-test'], check=True)
    base = ROOT / '.agent-state/native-acceptance'
    base.mkdir(parents=True, exist_ok=True)
    project = 'aide-native-' + uuid.uuid4().hex[:12]
    with tempfile.TemporaryDirectory(prefix='原生 启动-', dir=base) as directory:
        root = Path(directory)
        binary = root / 'aide-launcher.exe'
        shutil.copyfile(args.exe, binary)
        env = {k: v for k, v in os.environ.items()
               if not k.startswith('AIDE_') and not k.startswith('COMPOSE_')}
        env.update(COMPOSE_PROJECT_NAME=project, AIDE_IMAGE=args.image, AIDE_BUILD_FROM_SOURCE='0',
                   AIDE_OPEN_BROWSER='0', AIDE_WORKSPACE=str(root / 'workspace'),
                   AIDE_CONTEXT=str(root / 'context'), AIDE_LOCAL_ROOT=str(root / 'local'))
        # Deny host Python/PowerShell discovery. Docker is located in ProgramFiles.
        env['PATH'] = os.pathsep.join([str(Path(os.environ['SystemRoot']) / 'System32'),
                                     str(Path(os.environ['ProgramFiles']) / 'Docker/Docker/resources/bin')])
        listener = socket.socket()
        listener.setsockopt(socket.SOL_SOCKET, socket.SO_EXCLUSIVEADDRUSE, 1)
        listener.bind(('127.0.0.1', 0))
        busy = listener.getsockname()[1]
        if busy > 65400:
            listener.close()
            raise RuntimeError('Retry: ephemeral test port too close to end of range')
        env['AIDE_PORT'] = str(busy)

        def launch(action='start'):
            result = subprocess.run([str(binary), action, '--no-browser'], cwd=root, env=env,
                                    text=True, encoding='utf-8', errors='replace', capture_output=True, timeout=150)
            print(result.stdout)
            assert result.returncode == 0, result.stderr
            return result.stdout

        docker = shutil.which('docker')
        try:
            output = launch()
            assert 'is occupied' in output and 'Aide ready:' in output, output
            assert 'Automatic sign-in link ready' in output, output
            assert (root / 'compose.yaml').exists(), 'Single exe did not bootstrap Compose'
            assert (root / '.aide-data/storage/migration.json').exists(), 'Missing atomic data receipt'
            saved = (root / '.env').read_text()
            chosen = next(line.split('=', 1)[1].strip() for line in saved.splitlines() if line.startswith('AIDE_PORT='))
            assert int(chosen) > busy and int(chosen) <= busy + 100
            env.pop('AIDE_PORT')  # Saved .env must govern the next launch.
            before = subprocess.check_output([docker, 'compose', 'ps', '-q', 'aide'], cwd=root, env=env, text=True).strip()
            output = launch()
            after = subprocess.check_output([docker, 'compose', 'ps', '-q', 'aide'], cwd=root, env=env, text=True).strip()
            assert before == after, 'Repeat start unexpectedly recreated the app'
            assert (root / '.env').read_text() == saved, 'Repeat start changed saved port'
            launch('status')
            # Token must not leak to console or durable native logs.
            logs = (root / '.aide-data/native/launcher.log').read_text()
            assert '#token=' not in logs and '#token=' not in output
            launch('stop')
            state = subprocess.check_output([docker, 'compose', 'ps', '--status', 'running', '-q'], cwd=root, env=env, text=True)
            assert not state.strip(), 'Native stop left app running'
            print('PASS native single-exe bootstrap, Unicode paths, collision, repeat start, status and stop')
        finally:
            listener.close()
            subprocess.run([str(binary), 'stop', '--no-browser'], cwd=root, env=env,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=45)
            subprocess.run([docker, 'compose', 'down', '--timeout', '3'], cwd=root, env=env,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=45)


if __name__ == '__main__':
    main()
