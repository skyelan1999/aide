#!/usr/bin/env python3
"""Opt-in real Docker rehearsal. Only UUID-owned fixture containers/volumes are removed."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import tempfile
import time
import uuid

ROOT = Path(__file__).resolve().parents[1]
STATE = ROOT / '.agent-state/launcher-data-persistence'
STATE.mkdir(parents=True, exist_ok=True)
tag = 'aide:slot-persist-test-' + uuid.uuid4().hex[:10]
project = 'aide-persist-test-' + uuid.uuid4().hex[:10]
folder = Path(tempfile.mkdtemp(prefix='storage-docker-', dir=STATE))
env = {key: value for key, value in os.environ.items()
       if not key.startswith(('AIDE_', 'AI_', 'COMPOSE_'))}
env['AIDE_OPEN_BROWSER'] = '0'
env['PATH'] = str(Path(shutil.which('docker')).parent) + ':/usr/bin:/bin:/usr/sbin:/sbin'


def command(args, check=True):
    return subprocess.run(args, cwd=folder, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=check)


def compose(*args):
    return command(['docker', 'compose', '-p', project, *args])


def container():
    return compose('ps', '-q', 'aide').stdout.decode().strip()


def ready():
    for _ in range(100):
        cid = container()
        if cid and command(['docker', 'exec', '-w', '/tmp', cid, 'curl', '-fsSk', 'https://127.0.0.1:8080/healthz'], False).returncode == 0:
            return cid
        time.sleep(.2)
    raise RuntimeError('Fixture did not become healthy')


def snapshot(cid):
    program = '''import pathlib,hashlib,json,stat
paths=list(pathlib.Path('/data/auth').rglob('*'))+list(pathlib.Path('/data/certs').rglob('*'))+[pathlib.Path('/data/config/settings.json'),pathlib.Path('/data/config/fixture-core.bin'),pathlib.Path('/home/aide/.ssh/fixture'),pathlib.Path('/home/aide/.cache/fixture-core')]
print(json.dumps({str(p):[hashlib.sha256(p.read_bytes()).hexdigest(),stat.S_IMODE(p.stat().st_mode)] for p in paths if p.is_file()}))'''
    return json.loads(command(['docker', 'exec', '-w', '/tmp', cid, 'python3', '-c', program]).stdout)


def start(name):
    result = command(['bash', 'start.command'], False)
    (STATE / name).write_bytes(result.stdout + result.stderr)
    return result


decoys = []
success = False
try:
    info = json.loads(command(['docker', 'image', 'inspect', 'aide:local']).stdout)[0]
    command(['docker', 'image', 'tag', info['Id'], tag])
    with socket.socket() as probe:
        probe.bind(('127.0.0.1', 0))
        port = probe.getsockname()[1]
    (folder / 'workspace').mkdir(); (folder / 'context').mkdir(); (folder / 'scripts').mkdir()
    for name in ('start.command',): shutil.copy2(ROOT / name, folder / name)
    for name in ('aide.sh', 'launcher-data.sh', 'launcher-data.ps1', 'launcher-data.py', 'watch-port.sh'):
        shutil.copy2(ROOT / 'scripts' / name, folder / 'scripts' / name)
    shutil.copy2(ROOT / 'docker/offline.env.example', folder / '.env.example')
    (folder / '.aide-image').write_text(f'{tag} {info["Id"]} {info["Os"]}/{info["Architecture"]}\n')
    (folder / '.env').write_text(f'COMPOSE_PROJECT_NAME={project}\nAIDE_PORT={port}\nAIDE_IMAGE={tag}\nAIDE_WORKSPACE=./workspace\nAIDE_CONTEXT=./context\nAIDE_LOCAL_ROOT=./workspace\nAI_BASE_URL=https://example.invalid\n')
    (folder / 'compose.yaml').write_text('''name: aide-offline
services:
  aide:
    image: ${AIDE_IMAGE}
    pull_policy: never
    ports: ["127.0.0.1:${AIDE_PORT}:8080"]
    volumes:
      - ./workspace:/workspace
      - ./context:/context:ro
      - ./workspace:/local
      - aide-data:/data
      - aide-home:/home/aide
volumes:
  aide-data:
  aide-home:
''')
    compose('up', '-d', '--no-build', '--pull', 'never')
    cid = ready()
    program = '''from pathlib import Path
for name in ['/data/config/fixture-core.bin','/home/aide/.ssh/fixture','/home/aide/.cache/fixture-core']:
 p=Path(name);p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(b'portable-core-fixture\\x00\\n');p.chmod(0o600)
p=Path('/data/sessions/active/persistence-guard.json');p.parent.mkdir(parents=True,exist_ok=True);p.write_text('{"runs":[{"status":"running"}]}')'''
    command(['docker', 'exec', '-w', '/tmp', cid, 'python3', '-c', program])
    before = snapshot(cid)
    # Same launcher path can own historical one-off jobs and QA projects.
    for suffix, owner_project, oneoff in [('job',project,'True'),('qa',project+'-qa','False')]:
        name=project+'-'+suffix
        command(['docker','create','--name',name,'--label','com.docker.compose.project.working_dir='+str(folder),
                 '--label','com.docker.compose.service=aide','--label','com.docker.compose.project='+owner_project,
                 '--label','com.docker.compose.oneoff='+oneoff,'--entrypoint','sleep',tag,'infinity'])
        decoys.append(name)

    shutil.copy2(ROOT / 'docker/compose.offline.yaml', folder / 'compose.yaml')
    failed = start('docker-active-guard.log')
    assert failed.returncode != 0, 'active fixture must prevent migration'
    assert command(['docker', 'inspect', '--format', '{{.State.Running}}', cid]).stdout.strip() == b'true'
    assert not (folder / '.aide-data/storage').exists()
    command(['docker', 'exec', '-w', '/tmp', cid, 'python3', '-c', 'from pathlib import Path;Path("/data/sessions/active/persistence-guard.json").unlink()'])
    helper = folder / 'scripts/launcher-data.py'
    helper.write_text('raise RuntimeError("synthetic import failure")\n')
    rejected = start('docker-failed-import.log')
    assert rejected.returncode != 0
    assert command(['docker', 'inspect', '--format', '{{.State.Running}}', cid]).stdout.strip() == b'true'
    assert snapshot(cid) == before
    assert not (folder / '.cache/aide-manual-stop').exists()
    shutil.copy2(ROOT / 'scripts/launcher-data.py', helper)
    migrated = start('docker-import.log')
    assert migrated.returncode == 0, 'see private docker-import.log'
    cid = ready(); assert snapshot(cid) == before, 'protected bytes or permissions changed'
    mounts = json.loads(command(['docker', 'inspect', '--format', '{{json .Mounts}}', cid]).stdout)
    for destination in ('/data', '/home/aide'):
        mount = next(m for m in mounts if m['Destination'] == destination)
        assert mount['Type'] == 'bind' and '/.aide-data/storage/' in mount['Source']
    environment_digest = hashlib.sha256((folder / '.env').read_bytes()).hexdigest()
    compose('down')
    for name in (project + '_aide-data', project + '_aide-home'):
        label = command(['docker', 'volume', 'inspect', '--format', '{{index .Labels "com.docker.compose.project"}}', name]).stdout.decode().strip()
        assert label == project
        command(['docker', 'volume', 'rm', name])
    shutil.rmtree(folder / '.cache', ignore_errors=True)
    (folder / '.env').unlink(); (folder / '.aide-image').unlink()
    recovered = start('docker-recovery.log')
    assert recovered.returncode == 0, 'see private docker-recovery.log'
    cid = ready(); assert snapshot(cid) == before
    assert hashlib.sha256((folder / '.env').read_bytes()).hexdigest() == environment_digest
    assert (folder / '.aide-image').is_file()
    receipt = json.loads((folder / '.aide-data/storage/migration.json').read_text())
    result = {'status': 'pass', 'image': info['Id'], 'platform': info['Os'] + '/' + info['Architecture'],
              'oneoffAndForeignProjectContainersIgnored': True, 'activeTaskRefusedBeforeStop': True, 'failedImportRestoresOriginalContainer': True, 'protectedFiles': len(before),
              'bytesAndPermissionsPreserved': True, 'dataAndHomeBindUnderLauncher': True,
              'ownedLegacyVolumesDeletedAfterMigration': True, 'containerEnvironmentImageMarkerAndCacheRecreated': True,
              'environmentRestoredExactly': True, 'sources': receipt['sources'], 'userDataUsed': False}
    (STATE / 'docker-proof.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result)); success = True
finally:
    # Exact UUID fixture names only. Never prune the engine or unrelated volumes.
    command(['docker', 'compose', '-p', project, 'down'], False)
    for name in (project + '_aide-data', project + '_aide-home'):
        command(['docker', 'volume', 'rm', name], False)
    for name in decoys: command(['docker','rm',name],False)
    command(['docker', 'image', 'rm', tag], False)
    if success: shutil.rmtree(folder)
    else: print('Fixture failed; private evidence retained at', folder)
