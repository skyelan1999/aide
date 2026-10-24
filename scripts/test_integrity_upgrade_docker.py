"""Isolated readiness/upgrade/rollback smoke test. Never mounts live Aide data.

Usage: python3 scripts/test_integrity_upgrade_docker.py ARTIFACT_DIR BASE_IMAGE
ARTIFACT_DIR must contain aide-old and aide-new compiled with distinct identities.
"""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time


def run(*args, check=True):
    return subprocess.run(args, check=check, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    artifacts = Path(sys.argv[1]).resolve()
    base = sys.argv[2]
    ident = 'aide-integrity-test-' + str(os.getpid())
    images, containers = [], []
    try:
        with tempfile.TemporaryDirectory(prefix='aide-integrity-fixture-') as tmp:
            root = Path(tmp)
            for folder in ['data', 'work', 'context']:
                (root / folder).mkdir(mode=0o777)
                (root / folder).chmod(0o777)
            for name in ['old', 'new', 'tampered']:
                image = ident + ':' + name
                binary = 'aide-old' if name == 'old' else 'aide-new'
                dockerfile = artifacts / 'Dockerfile.integrity-test'
                dockerfile.write_text(
                    'FROM ' + base + '\nUSER root\n'
                    'COPY ' + binary + ' /usr/local/bin/aide\n'
                    'RUN chmod 0755 /usr/local/bin/aide && sha256sum /usr/local/bin/aide > /usr/local/bin/aide.sha256 && chmod 0444 /usr/local/bin/aide.sha256\n'
                    + ("RUN printf 'unexpected-change' >> /usr/local/bin/aide\n" if name == 'tampered' else '')
                    + 'USER aide\n')
                run('docker', 'build', '--network=none', '-f', str(dockerfile), '-t', image, str(artifacts))
                images.append(image)

            def start(kind, invalid=False):
                name = ident + '-' + kind + ('-failed' if invalid else '')
                args = ['docker', 'run', '-d', '--name', name, '--network=none', '--no-healthcheck', '--memory=512m', '--cpus=1']
                for folder, dest in [('data', '/data'), ('work', '/workspace'), ('context', '/context')]:
                    args += ['-v', str(root / folder) + ':' + dest]
                if invalid:
                    args += ['-e', 'AIDE_ADDR=invalid-address']
                run(*args, ident + ':' + kind)
                containers.append(name)
                if not invalid:
                    # The root page confirms serving readiness without invoking /healthz.
                    for _ in range(80):
                        q = run('docker', 'exec', name, 'curl', '-kfsS', 'https://127.0.0.1:8080/', check=False)
                        if q.returncode == 0:
                            return name
                        time.sleep(0.25)
                    raise AssertionError('fixture did not become ready: ' + run('docker', 'logs', name).stdout[-1000:])
                return name

            def health(name):
                return json.loads(run('docker', 'exec', name, 'curl', '-kfsS', 'https://127.0.0.1:8080/healthz').stdout)

            def stop(name):
                run('docker', 'rm', '-f', name)
                containers.remove(name)

            baseline = root / 'data/.integrity/baseline.json'
            old = start('old')
            assert health(old)['integrity'] == 'ok'
            sentinel = root / 'work/existing.pp'
            sentinel.write_text('existing-project-bytes')
            protected = [sentinel, root / 'data/config/settings.json', root / 'data/auth/access-token']
            before = {str(p.relative_to(root)): digest(p) for p in protected}
            old_baseline = baseline.read_bytes()
            stop(old)
            failed = start('new', invalid=True)
            for _ in range(80):
                if run('docker', 'inspect', '-f', '{{.State.Running}}', failed).stdout.strip() == 'false':
                    break
                time.sleep(0.1)
            else:
                raise AssertionError('failed-readiness fixture unexpectedly stayed running')
            assert baseline.read_bytes() == old_baseline
            stop(failed)
            new = start('new')
            assert baseline.read_bytes() == old_baseline, 'baseline changed before health confirmation'
            assert health(new)['integrity'] == 'ok'
            new_baseline = baseline.read_bytes()
            assert new_baseline != old_baseline
            assert json.loads(new_baseline)['binarySha256'] == digest(artifacts / 'aide-new')
            stop(new)
            bad = start('tampered')
            assert health(bad)['integrity'] == 'degraded'
            assert baseline.read_bytes() == new_baseline, 'unverified binary was blessed'
            stop(bad)
            rollback = start('old')
            assert baseline.read_bytes() == new_baseline
            assert health(rollback)['integrity'] == 'ok'
            assert json.loads(baseline.read_bytes())['binarySha256'] == digest(artifacts / 'aide-old')
            assert before == {str(p.relative_to(root)): digest(p) for p in protected}
            backups = list((root / 'data/.integrity').glob('baseline-before-*.json'))
            assert {old_baseline, new_baseline} <= {p.read_bytes() for p in backups}
            stop(rollback)
            result = dict(status='PASS', normalUpgrade=True, deferredUntilHealth=True,
                          failedReadinessPreservesBaseline=True, tamperedBinary='degraded',
                          verifiedRollback=True, priorBaselinesBackedUp=True,
                          existingProjectSettingsAndTokenUnchanged=True, liveDataMounted=False)
            (artifacts / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
            print(json.dumps(result))
    finally:
        for name in containers:
            run('docker', 'rm', '-f', name, check=False)
        for image in images:
            run('docker', 'image', 'rm', image, check=False)



if __name__ == '__main__':
    main()
