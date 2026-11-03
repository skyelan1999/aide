#!/usr/bin/env python3
"""Actual Windows APIs and production CLI with an isolated synthetic Docker CLI."""
from pathlib import Path
import ctypes
from ctypes import wintypes
import json
import os
import platform
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
KNOWN = 'Native child termination unconfirmed; action stopped'


def main():
    if platform.system() != 'Windows':
        raise SystemExit('NOT_RUN: Windows process/job/file-sharing APIs are required')
    artifacts = ROOT / '.agent-state/windows-desktop'
    executable = artifacts / 'aide-launcher.exe'
    fixture = artifacts / 'stop-recovery.exe'
    results = {}
    with tempfile.TemporaryDirectory(prefix='Aide 停止恢复 空格 ') as temporary:
        base = Path(temporary)
        tools = base / 'tools'
        tools.mkdir()
        shutil.copy2(fixture, tools / 'docker.exe')
        # SearchPathW searches the executable's directory before PATH/system
        # Docker installations. Keep both copies together so the production CLI
        # can only resolve our synthetic executable. No real engine is used.
        shutil.copy2(executable, tools / 'aide-launcher.exe')
        executable = tools / 'aide-launcher.exe'
        subprocess.run([str(fixture), str(base / 'faults')], check=True, timeout=30)
        results['windowsAPIs'] = json.loads((base / 'faults/fault-results.json').read_text(encoding='utf-8'))
        environment = os.environ.copy()
        environment['PATH'] = str(tools) + os.pathsep + environment.get('PATH', '')
        for name in list(environment):
            if name.startswith(('COMPOSE_', 'AIDE_')):
                environment.pop(name)
        environment['COMPOSE_PROJECT_NAME'] = 'aide-fixture'

        def case(name, action='recover-stop', marker=KNOWN, extra=None, prepare=None, success=False):
            root = base / name
            state = root / '.aide-data/native'
            state.mkdir(parents=True)
            (root / 'compose.yaml').write_text('services: {}\n')
            (root / '.env').write_text('COMPOSE_PROJECT_NAME=aide-fixture\n')
            blocker = state / 'host-stop-required'
            blocker.write_text(marker)
            if prepare:
                prepare(root, state)
            env = environment.copy()
            env.update(extra or {})
            result = subprocess.run([str(executable), action, '--root', str(root), '--no-browser'], env=env,
                                    capture_output=True, text=True, encoding='utf-8', errors='replace', timeout=40)
            (artifacts / f'stop-{name}.log').write_text(result.stdout + result.stderr, encoding='utf-8')
            assert (result.returncode == 0) == success, (name, result.returncode, result.stdout, result.stderr)
            if success:
                assert not blocker.exists(), name
                archives = [p for p in state.glob('host-stop-required.recovered-*') if p.suffix != '.json']
                assert len(archives) == 1 and archives[0].read_text(encoding='utf-8') == marker, name
                audit = json.loads(Path(str(archives[0]) + '.json').read_text(encoding='utf-8'))
                assert audit['controllerAndWorkersExited'] and audit['projectContainersStopped'] == 1, audit
                status = subprocess.run([str(executable), 'host-status', '--root', str(root)], env=env,
                                        capture_output=True, text=True, encoding='utf-8', check=True, timeout=10)
                assert json.loads(status.stdout)['stopRequired'] is False, status.stdout
            else:
                assert blocker.read_text(encoding='utf-8') == marker and not list(state.glob('*.recovered-*')), name
            results[name] = True

        case('explicit-recovery', success=True)
        case('ordinary-stop', action='stop', success=True)
        case('new-job-diagnostic', marker='Native process tree termination unconfirmed (Windows error 5, observation error 0); stop required', success=True)
        case('unknown-marker', marker='Unattached native child termination unconfirmed')
        case('active-update', prepare=lambda root, state: ((state / 'update').mkdir(), (state / 'update/transaction.json').write_text('{}')))
        case('legacy-blocker', prepare=lambda root, state: ((root / '.aide-data/update-agent').mkdir(), (root / '.aide-data/update-agent/launcher-stop-required').write_text('unsafe')))
        case('docker-unavailable', extra={'AIDE_FIXTURE_STOP_CASE': 'engine-off'})
        case('stop-failed', extra={'AIDE_FIXTURE_STOP_CASE': 'stop-fail'})
        case('container-still-running', extra={'AIDE_FIXTURE_STOP_CASE': 'running'})
        case('unrelated-container', extra={'AIDE_FIXTURE_STOP_CASE': 'foreign'})
        case('null-task', prepare=lambda root, state: (state / 'task-port.json').write_text('null'))
        case('malformed-controller', prepare=lambda root, state: (state / 'host-status.json').write_text(json.dumps({'version': 1, 'root': str(root), 'state': 'stopped'})))
        case('malformed-task', prepare=lambda root, state: (state / 'task-port.json').write_text('{"pid":1}'))
        # A worker from a different executable must still block recovery. Path
        # comparison and running:false are deliberately insufficient here.
        kernel = ctypes.WinDLL('kernel32', use_last_error=True)
        kernel.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
        kernel.OpenProcess.restype = wintypes.HANDLE
        kernel.GetProcessTimes.argtypes = [wintypes.HANDLE] + [ctypes.POINTER(wintypes.FILETIME)] * 4
        kernel.CloseHandle.argtypes = [wintypes.HANDLE]
        kernel.CreateFileW.argtypes = [wintypes.LPCWSTR, wintypes.DWORD, wintypes.DWORD,
                                       ctypes.c_void_p, wintypes.DWORD, wintypes.DWORD, wintypes.HANDLE]
        kernel.CreateFileW.restype = wintypes.HANDLE
        held = []
        def locked_controller(root, state):
            handle = kernel.CreateFileW(str(state / 'controller.lock'), 0xC0000000, 0, None, 4, 0x80, None)
            assert handle not in (None, ctypes.c_void_p(-1).value)
            held.append(handle)
        try:
            case('held-controller-lock', prepare=locked_controller)
        finally:
            for handle in held:
                kernel.CloseHandle(handle)
        def directory_state(root, state):
            (state / 'host-status.json').write_text(json.dumps({'version': 1, 'root': str(root),
                'generation': 'a' * 32, 'state': 'stopped', 'pid': 4294967295, 'created': 1}))
        directory_marker = json.dumps({'source': 'directory-apply', 'controllerGeneration': 'a' * 32})
        case('directory-generation', marker=directory_marker, prepare=directory_state, success=True)
        case('directory-wrong-generation', marker=json.dumps({'source': 'directory-apply',
            'controllerGeneration': 'b' * 32}), prepare=directory_state)
        child = subprocess.Popen([str(fixture), '--sleep', '60000', '0'])
        try:
            handle = kernel.OpenProcess(0x1000, False, child.pid)
            assert handle
            times = [wintypes.FILETIME() for _ in range(4)]
            assert kernel.GetProcessTimes(handle, *[ctypes.byref(item) for item in times])
            kernel.CloseHandle(handle)
            created = (times[0].dwHighDateTime << 32) | times[0].dwLowDateTime
            identity = {'pid': child.pid, 'created': created, 'phase': 'stopped', 'running': False}
            case('live-foreign-executable', prepare=lambda root, state: (state / 'task-port.json').write_text(json.dumps(identity)))
            # Same PID with a different creation time is an exited old lifetime;
            # recovery must neither reject nor terminate the unrelated new one.
            identity['created'] += 1
            case('reused-pid', prepare=lambda root, state: (state / 'task-port.json').write_text(json.dumps(identity)), success=True)
            assert child.poll() is None
        finally:
            child.terminate()
            child.wait(timeout=10)
    (artifacts / 'stop-recovery-results.json').write_text(json.dumps(results, indent=2))
    print(json.dumps(results, indent=2))


if __name__ == '__main__':
    main()
