#!/usr/bin/env python3
"""Exercise real host helpers and lifecycle contracts on disposable CI hosts.

Uses temporary bridge destinations. Does not change OS privacy permissions or
claim real Docker Desktop, physical-device or account acceptance.
"""
import argparse
import json
import os
from pathlib import Path
import platform
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]


def plan(host, temporary):
    common = [
        ['go', 'test', '-json', '-mod=vendor', '-count=1', './cmd/aide-local-mounts', './internal/hostbridge', './internal/localmounts', './internal/messaging'] + (['./scripts/xsys-review'] if host == 'windows' else []),
        [sys.executable, 'scripts/test_reference_relay_agent.py'],
        [sys.executable, 'scripts/test_mixsoul_embedding_follow.py'],
    ]
    if host == 'windows':
        powershell = str(Path(os.environ['SystemRoot']) / 'System32/WindowsPowerShell/v1.0/powershell.exe')
        common += [[sys.executable, 'scripts/test_windows_launcher_compile.py']]
        for shell in (powershell, 'pwsh.exe'):
            for script in ('test_windows_image_slot.py', 'test_windows_directory_launcher.py', 'test_windows_update_agent.py'):
                common.append([sys.executable, 'scripts/' + script, '--pwsh', shell])
    else:
        common.append([sys.executable, 'scripts/test_launcher_contract.py'])
        bridge = str(Path(temporary) / 'Aide Computer Bridge.app')
        common += [
            ['bash', 'scripts/build-native-computer-bridge.sh'],
            ['codesign', '--verify', '--deep', '--strict', bridge],
            ['codesign', '--display', '--requirements', '-', bridge],
            ['file', str(Path(bridge) / 'Contents/MacOS/AideComputerBridge')],
        ]
    return common


def go_receipt(text, command):
    completed=[]
    for line in text.splitlines():
        try:
            event=json.loads(line)
        except json.JSONDecodeError:
            continue
        if event.get('Test') and event.get('Action') in ('pass','skip','fail'):
            completed.append({key:event[key] for key in ('Package','Test','Action')})
    for package in (value.replace('./','aide/',1) for value in command if value.startswith('./')):
        if not any(e['Package']==package and e['Action']=='pass' for e in completed):
            raise ValueError('No executed native Go assertions for '+package)
    return completed


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--host', choices=['windows', 'macos'], required=True)
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    expected = {'windows': 'win32', 'macos': 'darwin'}[args.host]
    if sys.platform != expected:
        raise ValueError('Native host validation must execute on ' + expected)
    out = args.out.resolve()
    out.mkdir(parents=True, exist_ok=True)
    commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    report = {'status': 'fail', 'commit': commit, 'host': args.host, 'machine': platform.machine(), 'checks': [],
              'limits': ['No privacy grants changed', 'No physical device, paid account or Docker Desktop lifecycle claim']}
    try:
        with tempfile.TemporaryDirectory(prefix='aide release host ') as temporary:
            env = dict(os.environ, PYTHONUTF8='1', AIDE_NATIVE_BRIDGE_APP=str(Path(temporary) / 'Aide Computer Bridge.app'))
            if args.host == 'windows':
                env['PSModulePath'] = str(Path(os.environ['SystemRoot']) / 'System32/WindowsPowerShell/v1.0/Modules')
            for index, command in enumerate(plan(args.host, temporary)):
                start = time.monotonic()
                with (out / f'{index:02d}.log').open('w', encoding='utf-8') as stream:
                    result = subprocess.run(command, cwd=ROOT, env=env, stdout=stream, stderr=subprocess.STDOUT, timeout=600)
                report['checks'].append({'command': command, 'status': 'pass' if result.returncode == 0 else 'fail',
                                         'seconds': round(time.monotonic() - start, 3)})
                if not result.returncode and command[:2] == ['go','test']:
                    report['checks'][-1]['goTests']=go_receipt((out / f'{index:02d}.log').read_text(encoding='utf-8'),command)
                if result.returncode:
                    raise RuntimeError('Host check failed: ' + ' '.join(command))
        report['status'] = 'pass'
    finally:
        (out / 'report.json').write_text(json.dumps(report, indent=2) + '\n')


if __name__ == '__main__':
    main()
