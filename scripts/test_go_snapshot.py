#!/usr/bin/env python3
"""Run Go race/vet from a private, disposable container-local source snapshot.

Docker Desktop shared mounts can stall in filesystem calls even after a Go
context is cancelled. Transfer current Git-listed files once through stdin;
compilation and tests then use Linux-local files, with no network or user data
mounts. Missing working-tree files remain missing in the snapshot.
"""
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import uuid

ROOT = Path(__file__).resolve().parents[1]
COMMAND = ('set -euo pipefail; snapshot=$(mktemp -d /tmp/aide-verify.XXXXXX); '
           'tar --no-same-owner -xf - -C "$snapshot"; cd "$snapshot"; '
           'timeout --signal=TERM --kill-after=10s 20m bash -c '
           "'go test -mod=vendor -race -count=1 -timeout=15m ./... && go vet -mod=vendor ./...'")


def main():
    names = subprocess.check_output(
        ['git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z'], cwd=ROOT).split(b'\0')
    state = ROOT / '.agent-state'
    state.mkdir(exist_ok=True)
    image = os.environ.get('AIDE_TEST_IMAGE', 'aide:local')
    container = 'aide-verify-' + uuid.uuid4().hex[:12]
    with tempfile.TemporaryFile(dir=state) as payload:
        count = 0
        with tarfile.open(fileobj=payload, mode='w', dereference=False) as archive:
            for raw in sorted(set(filter(None, names))):
                name = os.fsdecode(raw)
                path = ROOT / name
                if path.is_file() or path.is_symlink():
                    archive.add(path, arcname=name, recursive=False)
                    count += 1
        payload.seek(0)
        print(f'Go snapshot: {count} current files; container-local source; network none', flush=True)
        try:
            subprocess.run(['docker', 'run', '--rm', '-i', '--network', 'none', '--no-healthcheck',
                            '--name', container, '--workdir', '/tmp', '-e', 'AIDE_BUILTIN_PLUGINS=', '--entrypoint',
                            'bash', image, '-c', COMMAND], stdin=payload, check=True, timeout=1250)
        finally:
            # A failed run or interrupt must not leave this runner's container behind.
            try:
                subprocess.run(['docker', 'rm', '-f', container], stdout=subprocess.DEVNULL,
                               stderr=subprocess.DEVNULL, timeout=10, check=False)
            except (OSError, subprocess.TimeoutExpired):
                print(f'Cleanup pending for own test container: {container}', flush=True)


if __name__ == '__main__':
    main()
