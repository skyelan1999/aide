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
import json

ROOT = Path(__file__).resolve().parents[1]
# Keep every server test while avoiding one package-wide deadline for the
# growing suite. Compile race instrumentation once, then run disjoint shards
# in separate processes. Other packages and vet retain their ordinary paths.
# Allow the complete server inventory, remaining race packages, and vet to finish.
# The host deadline includes extraction and graceful container termination.
SUITE_TIMEOUT_SECONDS = 75 * 60
HOST_TIMEOUT_SECONDS = SUITE_TIMEOUT_SECONDS + 50
COMMAND = ('set -euo pipefail; snapshot=$(mktemp -d /tmp/aide-verify.XXXXXX); '
           'tar --no-same-owner -xf - -C "$snapshot"; cd "$snapshot"; '
           'export AIDE_OFFICE_SCRIPTS="$snapshot/scripts/office"; '
           f'timeout --signal=TERM --kill-after=10s {SUITE_TIMEOUT_SECONDS}s bash -c '
           "'mkdir -p /tmp/aide-test-evidence && go test -mod=vendor -race -c -o server.test ./internal/server && "
           'python3 scripts/run_go_test_shards.py "$PWD/server.test" --cwd internal/server --workers="${AIDE_TEST_WORKERS:-1}" --report-dir /tmp/aide-test-evidence/server && '
           'go test -mod=vendor -race -count=1 -timeout=18m '
           "$(go list -mod=vendor ./... | sed \"\\|^aide/internal/server$|d\") && "
           'python3 scripts/test_xsys_platform_contracts.py && '
           "go vet -mod=vendor ./...'")



def main():
    names = subprocess.check_output(
        ['git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z'], cwd=ROOT).split(b'\0')
    state = ROOT / '.agent-state'
    state.mkdir(exist_ok=True)
    image = os.environ.get('AIDE_TEST_TOOLCHAIN_IMAGE', os.environ.get('AIDE_TEST_IMAGE', 'aide:dev'))
    workers = os.environ.get('AIDE_TEST_WORKERS', '1')
    if workers not in ('1', '2'):
        raise ValueError('AIDE_TEST_WORKERS must be 1 or 2')
    memory, cpus = ('8g', '4') if workers == '2' else ('4g', '2')
    container = 'aide-verify-' + uuid.uuid4().hex[:12]
    evidence = Path(os.environ.get('AIDE_TEST_EVIDENCE_DIR', state / ('go-suite-' + uuid.uuid4().hex[:12]))).resolve()
    evidence.mkdir(parents=True, exist_ok=True)
    report = {'status': 'fail', 'image': image, 'workers': workers,
              'notice': 'Complete server inventory including fuzz seeds/examples; skips retained separately. Other packages, platform contracts and vet must also succeed.'}
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
            subprocess.run(['docker', 'run', '-i', '--network', 'none', '--no-healthcheck',
                            '--memory', memory, '--memory-swap', memory, '--cpus', cpus, '--pids-limit', '512',
                            '--log-driver', 'local', '--log-opt', 'max-size=10m', '--log-opt', 'max-file=3',
                            '--name', container, '--workdir', '/tmp', '-e', 'AIDE_BUILTIN_PLUGINS=', '-e', 'AIDE_TEST_WORKERS=' + workers,
                            # Reserve headroom for race shadow memory outside the Go heap.
                            '-e', 'GOMEMLIMIT=2GiB', '-e', 'GOGC=50', '--entrypoint',
                            'bash', image, '-c', COMMAND], stdin=payload, check=True, timeout=HOST_TIMEOUT_SECONDS)
            subprocess.run(['docker', 'cp', container + ':/tmp/aide-test-evidence/.', str(evidence)], check=True, timeout=60)
            report['server'] = json.loads((evidence / 'server/report.json').read_text())
            if report['server'].get('status') != 'pass':
                raise ValueError('Server completion evidence did not pass')
            report['status'] = 'pass'
        finally:
            # Copy stopped-container evidence without changing host permissions.
            try:
                subprocess.run(['docker', 'cp', container + ':/tmp/aide-test-evidence/.', str(evidence)],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=60, check=False)
            except (OSError, subprocess.TimeoutExpired):
                pass
            finally:
                try:
                    subprocess.run(['docker', 'rm', '-f', container], stdout=subprocess.DEVNULL,
                                   stderr=subprocess.DEVNULL, timeout=15, check=False)
                finally:
                    (evidence / 'report.json').write_text(json.dumps(report, indent=2) + '\n')


if __name__ == '__main__':
    main()
