#!/usr/bin/env python3
"""Validate already-published public packages in an isolated real Aide host.

No plugin rebuilds. The immutable catalog/package identities and mandatory
installation/function receipts are also checked again before publication.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import shlex
import subprocess
import tarfile
import tempfile
import uuid
import zipfile

ROOT = Path(__file__).resolve().parents[2]
LOCK = ROOT / 'scripts/ci/public-plugins.lock.json'


def require(condition, message):
    if not condition:
        raise ValueError(message)


def load_lock():
    data = json.loads(LOCK.read_text())
    require(re.fullmatch(r'[0-9a-f]{40}', data.get('marketRevision', '')), 'Invalid frozen market revision')
    plugins = data['plugins']
    require(plugins and len({p['id'] for p in plugins}) == len(plugins), 'Empty/duplicate public inventory')
    for item in plugins:
        require(item.get('public') is True, 'Private package in public validation')
        path = PurePosixPath(item['package'])
        require(not path.is_absolute() and '..' not in path.parts and path.parts[:2] == ('packages', item['id']), 'Unsafe package path')
        require(re.fullmatch(r'[0-9a-f]{64}', item.get('sha256', '')), 'Invalid package digest')
    return data


def verify_report(report, lock, commit=None, arch=None):
    require(report.get('status') == 'pass', 'Public plugin run did not pass')
    require(report.get('marketRevision') == lock['marketRevision'], 'Stale/crossed market evidence')
    if commit is not None:
        require(report.get('commit') == commit, 'Public plugin host commit differs')
    if arch is not None:
        require(report.get('architecture') == arch, 'Public plugin architecture differs')
    expected = {p['id']: p for p in lock['plugins']}
    rows = report.get('plugins', [])
    require(len(rows) == len(expected) and len({r.get('id') for r in rows}) == len(rows), 'Incomplete/duplicate plugin results')
    for row in rows:
        require(row.get('id') in expected, 'Unexpected plugin result')
        item = expected[row['id']]
        require((row.get('version'), row.get('sha256')) == (item['marketVersion'], item['sha256']), 'Wrong installed plugin version/bytes')
        for stage in ('inspection', 'installation', 'function'):
            require(row.get(stage) == 'pass', row['id'] + ': missing successful ' + stage)
        require(isinstance(row.get('cases'), list) and row['cases'] and all(isinstance(x, str) and x.strip() for x in row['cases']), 'Missing meaningful function cases')


def inspect_packages(market, lock):
    catalog = json.loads((market / 'catalog.json').read_text())
    require({p['id'] for p in catalog['plugins']} == {p['id'] for p in lock['plugins']}, 'Market inventory changed; freeze and review again')
    for item in lock['plugins']:
        path = market / item['package']
        raw = path.read_bytes()
        require(len(raw) == item['bytes'] and hashlib.sha256(raw).hexdigest() == item['sha256'], 'Public package identity mismatch: ' + item['id'])
        with zipfile.ZipFile(path) as bundle:
            require(bundle.testzip() is None, 'Bad package CRC')
            names = bundle.namelist()
            require(len(names) == len(set(names)), 'Duplicate ZIP member')
            for entry in bundle.infolist():
                member = PurePosixPath(entry.filename)
                require(not member.is_absolute() and '..' not in member.parts and chr(92) not in entry.filename, 'Unsafe ZIP member')
                require((entry.external_attr >> 16) & 0o170000 != 0o120000, 'ZIP symlink rejected')
            manifest = json.loads(bundle.read('manifest.json'))
            require((manifest['id'], manifest['version']) == (item['id'], item['marketVersion']), 'Manifest identity mismatch')
            require(manifest['main'] in names, 'Missing actual plugin entrypoint')


def fetch_market(destination, lock):
    destination.mkdir(parents=True, exist_ok=True)
    repo = lock['marketRepository']
    require(repo == 'skyelan1999/aide-pkg-shop', 'Untrusted market repository')
    with tempfile.TemporaryFile() as payload:
        subprocess.run(['gh', 'api', f'repos/{repo}/tarball/{lock["marketRevision"]}'], stdout=payload, check=True, timeout=180)
        payload.seek(0)
        with tarfile.open(fileobj=payload) as archive:
            for member in archive:
                parts = PurePosixPath(member.name).parts[1:]
                if not parts:
                    continue
                require('..' not in parts and not member.issym() and not member.islnk(), 'Unsafe public archive member')
                target = destination.joinpath(*parts)
                if member.isdir():
                    target.mkdir(parents=True, exist_ok=True)
                elif member.isfile():
                    target.parent.mkdir(parents=True, exist_ok=True)
                    target.write_bytes(archive.extractfile(member).read())
                else:
                    raise ValueError('Special public archive member')
    inspect_packages(destination, lock)


def run(image, out, market=None):
    lock = load_lock()
    out = out.resolve()
    out.mkdir(parents=True, exist_ok=True)
    commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    info = json.loads(subprocess.check_output(['docker', 'image', 'inspect', image], text=True))[0]
    architecture = info['Architecture']
    require(info['Os'] == 'linux' and architecture in ('amd64', 'arm64'), 'Use the native candidate Linux toolchain')
    report = {'status': 'fail', 'commit': commit, 'architecture': architecture, 'marketRevision': lock['marketRevision'], 'plugins': []}
    container = 'aide-public-qa-' + uuid.uuid4().hex[:12]
    try:
        if market is None:
            market = out / 'downloaded-market'
            fetch_market(market, lock)
        else:
            market = market.resolve()
            inspect_packages(market, lock)
        names = subprocess.check_output(['git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z'], cwd=ROOT).split(b'\0')
        with tempfile.TemporaryFile() as payload:
            with tarfile.open(fileobj=payload, mode='w', dereference=False) as archive:
                for raw in sorted(set(filter(None, names))):
                    path = ROOT / os.fsdecode(raw)
                    if path.is_file() or path.is_symlink():
                        archive.add(path, arcname=os.fsdecode(raw), recursive=False)
            payload.seek(0)
            # Install only the released optional renderer's pinned Python
            # dependencies in this disposable test container. Plugin ZIPs are
            # never rebuilt or changed. STEP installs its checksum-pinned
            # published native runtime through the actual plugin UI protocol.
            prepare = """import json,pathlib,subprocess,sys,zipfile
lock=json.loads(pathlib.Path('scripts/ci/public-plugins.lock.json').read_text())
entry=next(p for p in lock['plugins'] if p['id']=='file-preview')
with zipfile.ZipFile(pathlib.Path('/public-market')/entry['package']) as z:
 requirement=pathlib.Path('/tmp/released-renderer-requirements.txt')
 requirement.write_bytes(z.read('engine/requirements.txt'))
subprocess.run([sys.executable,'-m','pip','install','--user','--no-cache-dir','--index-url','https://pypi.org/simple','-r',str(requirement)],check=True,timeout=420)
"""
            command = ('set -euo pipefail; public_source=$(mktemp -d /tmp/aide-public-source.XXXXXX); '
                       'tar -xf - -C "$public_source"; cd "$public_source"; mkdir /tmp/aide-public-evidence; '
                       'export AIDE_OFFICE_SCRIPTS="$public_source/scripts/office"; '
                       'python3 -c ' + shlex.quote(prepare) + '; '
                       'go test -mod=vendor -count=1 -timeout=20m -v ./internal/server -run "^TestReleasePublicPackageFunctions$"')
            with (out / 'functions.log').open('w') as stream:
                subprocess.run(['docker', 'run', '-i', '--network', 'bridge', '--memory', '6g', '--cpus', '3',
                                '--pids-limit', '512', '--name', container, '--entrypoint', 'bash',
                                '--mount', 'type=bind,src=' + str(market) + ',dst=/public-market,readonly',
                                '-e', 'AIDE_BUILTIN_PLUGINS=', '-e', 'AIDE_RELEASE_PUBLIC_MARKET=/public-market',
                                '-e', 'AIDE_PUBLIC_PLUGIN_EVIDENCE_DIR=/tmp/aide-public-evidence', image, '-c', command],
                               stdin=payload, stdout=stream, stderr=subprocess.STDOUT, check=True, timeout=1500)
        subprocess.run(['docker', 'cp', container + ':/tmp/aide-public-evidence/.', str(out)], check=True, timeout=60)
        functions = json.loads((out / 'functions.json').read_text())
        candidate = dict(report, **functions)
        verify_report(candidate, lock, commit, architecture)
        report = candidate
    finally:
        try:
            subprocess.run(['docker', 'cp', container + ':/tmp/aide-public-evidence/.', str(out)],
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=60, check=False)
        except (OSError, subprocess.TimeoutExpired):
            pass  # Preserve the original test failure and still clean up.
        finally:
            try:
                subprocess.run(['docker', 'rm', '-f', container], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=15, check=False)
            finally:
                (out / 'report.json').write_text(json.dumps(report, indent=2) + '\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='action', required=True)
    run_parser = sub.add_parser('run')
    run_parser.add_argument('--image', required=True)
    run_parser.add_argument('--out', type=Path, required=True)
    run_parser.add_argument('--market', type=Path)
    verify = sub.add_parser('verify')
    verify.add_argument('--root', type=Path, required=True)
    args = parser.parse_args()
    if args.action == 'run':
        run(args.image, args.out, args.market)
    else:
        lock = load_lock()
        commit = os.environ['RELEASE_SHA']
        for arch in ('arm64', 'amd64'):
            path = args.root / ('release-public-plugins-' + arch) / 'report.json'
            verify_report(json.loads(path.read_text()), lock, commit, arch)
        print('PASS: every frozen public plugin inspected, installed and function-tested on both native architectures')


if __name__ == '__main__':
    main()
