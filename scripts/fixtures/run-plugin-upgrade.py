#!/usr/bin/env python3
"""Run the isolated real-network plugin upgrade workbench on loopback:18619."""
import os
from pathlib import Path
import subprocess
import tarfile
import uuid

ROOT = Path(__file__).resolve().parents[2]
STATE = ROOT / '.agent-state/plugin-online-upgrade'
STATE.mkdir(parents=True, exist_ok=True)
name = 'aide-plugin-upgrade-browser-' + uuid.uuid4().hex[:10]
payload = STATE / ('source-' + name + '.tar')
files = subprocess.check_output(['git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z'], cwd=ROOT).split(b'\0')
try:
    with tarfile.open(payload, 'w') as archive:
        for raw in sorted(set(filter(None, files))):
            relative = os.fsdecode(raw)
            path = ROOT / relative
            if path.is_file() or path.is_symlink():
                archive.add(path, arcname=relative, recursive=False)
    command = ['docker', 'run', '--rm', '-i', '--no-healthcheck', '--name', name,
               '--memory', '2g', '--cpus', '2', '-p', '127.0.0.1:18619:18619',
               '-e', 'AIDE_BUILTIN_PLUGINS=', '-e', 'AIDE_PLUGIN_UPGRADE_BROWSER_FIXTURE=1',
               '--entrypoint', 'bash', os.environ.get('AIDE_TEST_IMAGE', 'aide:local'), '-c',
               'set -e; mkdir /tmp/aide; tar -xf - -C /tmp/aide; cd /tmp/aide; '
               'go test -mod=vendor -count=1 -v ./internal/server -run ^TestPluginUpgradeBrowserFixture$ -timeout 30m']
    print('Isolated fixture: http://127.0.0.1:18619/fixture; container ' + name, flush=True)
    with payload.open('rb') as source:
        subprocess.run(command, stdin=source, cwd=ROOT, check=True)
finally:
    subprocess.run(['docker', 'rm', '-f', name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
    payload.unlink(missing_ok=True)
