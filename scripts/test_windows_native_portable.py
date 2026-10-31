#!/usr/bin/env python3
"""Execute the production streaming extractor and image checks on this host.

Uses an existing C++ compiler only; does not install one. Win32/Docker Desktop
lifecycle is separate and is never claimed by these portable checks.
"""
import argparse
import binascii
import copy
import gzip
import hashlib
import importlib.util
import io
import json
import random
import struct
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--compiler', default=shutil.which('c++'))
    args = parser.parse_args()
    if not args.compiler:
        raise SystemExit('NOT_RUN: an existing C++ compiler is required')
    spec = importlib.util.spec_from_file_location('prebuilt_fixture', ROOT / 'scripts/test_windows_prebuilt_import.py')
    fixture = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(fixture)
    count = 0
    with tempfile.TemporaryDirectory(prefix='aide-native-portable-') as directory:
        base = Path(directory)
        executable = base / 'portable'
        subprocess.run([args.compiler, '-std=c++17', '-O2', str(ROOT / 'scripts/native_launcher_portable.cpp'),
                        str(ROOT / 'native/windows-launcher/third_party/miniz/miniz_tinfl.c'), '-o', str(executable)], check=True)

        def extract(name, data, success, expected=None):
            nonlocal count
            source, target = base / (name + '.tar.gz'), base / (name + '.image')
            source.write_bytes(data)
            result = subprocess.run([str(executable), 'extract', str(source), str(target)], capture_output=True, timeout=15)
            assert (result.returncode == 0) == success, (name, result.stderr)
            if success:
                assert target.read_bytes() == (fixture.PAYLOAD if expected is None else expected), name
            assert not (base / 'must-not-extract').exists(), name
            print('PASS archive ' + name)
            count += 1

        for name in ['tar-denied', 'pax-path', 'gnu-path', 'gnu-number']:
            extract(name, fixture.archive(name), True)
        for name in ['missing-member', 'symlink-member', 'large-pax', 'bad-header', 'truncated-member']:
            extract(name, fixture.archive(name), False)
        for name, data in [('bad-gzip', b'bad gzip'), ('gzip-truncated', fixture.archive('tar-denied')[:-5]),
                           ('gzip-crc', fixture.archive('tar-denied')[:-8] + b'\0' * 8),
                           ('gzip-appended', fixture.archive('tar-denied') + b'extra')]:
            extract(name, data, False)
        for form in [tarfile.USTAR_FORMAT, tarfile.GNU_FORMAT]:
            raw = io.BytesIO()
            with tarfile.open(fileobj=raw, mode='w', format=form) as tar:
                item = tarfile.TarInfo('./aide-offline/docker-images/aide-local.tar')
                item.size = len(fixture.PAYLOAD)
                tar.addfile(item, io.BytesIO(fixture.PAYLOAD))
            extract('tar-format-' + str(form), gzip.compress(raw.getvalue()), True)
        raw = io.BytesIO()
        payload = bytes(range(256)) * 32768  # Many 32KiB dictionary wraps.
        with tarfile.open(fileobj=raw, mode='w') as tar:
            item = tarfile.TarInfo('aide-offline/docker-images/aide-local.tar')
            item.size = len(payload)
            tar.addfile(item, io.BytesIO(payload))
        extract('stream-wraps', gzip.compress(raw.getvalue()), True, payload)
        raw = io.BytesIO()
        payload = random.Random(464).randbytes(2 * 1024 * 1024)
        with tarfile.open(fileobj=raw, mode='w') as tar:
            item = tarfile.TarInfo('aide-offline/docker-images/aide-local.tar')
            item.size = len(payload)
            tar.addfile(item, io.BytesIO(payload))
        extract('input-refills', gzip.compress(raw.getvalue()), True, payload)
        original = fixture.archive('tar-denied')
        header = original[:3] + bytes([30]) + original[4:10] + b'\x04\x00moreimage\0owned\0'
        header += struct.pack('<H', binascii.crc32(header) & 65535)
        extract('gzip-header-options', header + original[10:], True)
        extract('gzip-header-crc', header[:-1] + bytes([header[-1] ^ 1]) + original[10:], False)
        raw = io.BytesIO()
        with tarfile.open(fileobj=raw, mode='w') as tar:
            for _ in range(2):
                item = tarfile.TarInfo('aide-offline/docker-images/aide-local.tar')
                item.size = len(fixture.PAYLOAD)
                tar.addfile(item, io.BytesIO(fixture.PAYLOAD))
        extract('duplicate-target', gzip.compress(raw.getvalue()), False)
        canonical = 'sha256:' + 'b' * 64
        source = 'c' * 64
        metadata = dict(Id=canonical, Os='linux', Architecture='amd64',
                        Config={'Labels': {'aide.srcsha': source, 'aide.cad-runtime': '8.0.1.1.0'}})
        for name, mutate, success in [
            ('different-manifest-config-id', lambda m: None, True),
            ('wrong-id', lambda m: m.update(Id='invalid'), False),
            ('wrong-platform', lambda m: m.update(Architecture='arm64'), False),
            ('wrong-source', lambda m: m['Config']['Labels'].update({'aide.srcsha': 'd' * 64}), False),
            ('wrong-cad', lambda m: m['Config']['Labels'].update({'aide.cad-runtime': ''}), False),
            ('typed-id', lambda m: m.update(Id=4), False),
            ('typed-labels', lambda m: m['Config'].update(Labels=[]), False),
        ]:
            value = copy.deepcopy(metadata)
            mutate(value)
            result = subprocess.run([str(executable), 'identity'], input=json.dumps(dict(metadata=value, platform='linux/amd64', source=source)), text=True, capture_output=True, timeout=5)
            assert (result.returncode == 0) == success, (name, result.stderr)
            if success:
                assert result.stdout == canonical
            print('PASS identity ' + name)
            count += 1
        for name, output, success in [
            ('tag', 'Loaded image: aide:owned\r\n', True),
            ('id', 'Loaded image ID: ' + canonical, True),
            ('same-reference', 'Loaded image: aide:owned\nLoaded image: aide:owned\n', True),
            ('ambiguous', 'Loaded image: aide:one\nLoaded image: aide:two', False),
            ('empty', '', False),
            ('unanchored', 'noise Loaded image: aide:one', False),
        ]:
            result = subprocess.run([str(executable), 'receipt'], input=output, text=True, capture_output=True, timeout=5)
            assert (result.returncode == 0) == success, (name, result.stderr)
            print('PASS receipt ' + name)
            count += 1
        spec = importlib.util.spec_from_file_location('native_build', ROOT / 'scripts/build-windows-launcher.py')
        build = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(build)
        result = subprocess.run([str(executable), 'source-inputs', str(ROOT)], input=json.dumps(build.source_manifest()), text=True, capture_output=True, check=True)
        lines = ''.join(hashlib.sha256((ROOT / name).read_bytes()).hexdigest() + '  ' + name + '\n' for name in json.loads(result.stdout))
        expected = subprocess.check_output(['bash', 'scripts/aide.sh', 'source-sha'], cwd=ROOT, text=True).strip()
        assert hashlib.sha256(lines.encode()).hexdigest() == expected, 'Native/POSIX source fingerprints differ'
        print('PASS canonical native/POSIX source fingerprint')
        count += 1
        own, other = base / 'own install', base / 'other install'
        own.mkdir()
        other.mkdir()
        alias = base / 'alias'
        alias.symlink_to(own, target_is_directory=True)
        for name, reported, success in [('own', own, True), ('alias', alias, True), ('other', other, False), ('missing', base / 'missing', False)]:
            result = subprocess.run([str(executable), 'same-install', str(own), str(reported)], capture_output=True)
            assert (result.returncode == 0) == success, name
            print('PASS installation ownership ' + name)
            count += 1
        from test_windows_build_proxy import CASES
        for case in CASES:
            result = subprocess.run([str(executable), 'build-proxy'], input=json.dumps(case['input']), text=True, capture_output=True, timeout=5)
            assert (result.returncode == 0) == case.get('success', True), (case['name'], result.stdout, result.stderr)
            if result.returncode == 0:
                assert json.loads(result.stdout) == case['expected'], (case['name'], result.stdout)
            else:
                assert 'HTTP_PROXY' in result.stderr and 'HTTPS_PROXY' in result.stderr, (case['name'], result.stderr)
                for private in ('private-user', 'private-password'):
                    assert private not in result.stderr
            print('PASS build proxy ' + case['name'])
            count += 1
    print(f'PASS {count} portable native checks; Win32 lifecycle NOT_RUN')


if __name__ == '__main__':
    main()
