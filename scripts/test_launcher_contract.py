#!/usr/bin/env python3
"""Execute the standard launcher coordinator with owned external boundaries."""
import importlib.util
import contextlib
import io
import time
import json
import os
from pathlib import Path
import shutil
import subprocess
import struct
import sys
import tempfile
import unittest
import zipfile
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('launcher_contract', ROOT / 'scripts/launcher.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
SCOPE_IMAGE = 'sha256:' + 'b' * 64
FINGERPRINT = 'c' * 64
CID = 'a' * 64


class BoundaryLauncher(module.Launcher):
    def __init__(self, root, **kwargs):
        super().__init__(root, **kwargs)
        self.commands = []
        self.fail_build = False
        self.wrong_source = False
        self.existing_builder = False
        self.containers = []
        self.foreign_project = False
        self.shared_image = False
        self.branch = 'main'
        self.dirty = ''
        self.tagged = 'd' * 40
        self.gate = True
        self.recycle_calls = []

    def recycle_build_artifacts(self, **kwargs):
        # Image/health/GC boundaries are exercised separately by RecycleTests.
        self.recycle_calls.append((len(self.commands), kwargs))

    def call(self, args, capture=False, check=True, timeout=180, env=None, progress=None):
        args = [str(x) for x in args]
        self.commands.append(args)
        short = args[1:]
        output = ''
        if args[0] == 'git':
            if args[1:] == ['rev-parse', 'HEAD']:
                output = 'd' * 40
            elif args[1:] == ['branch', '--show-current']:
                output = self.branch
            elif args[1:] == ['status', '--porcelain']:
                output = self.dirty
            elif args[1] == 'rev-parse':
                output = self.tagged
            elif args[1] == 'ls-remote':
                output = '\n'.join('d'*40 + '\t' + ref for ref in args[3:])
        elif args[:2] == ['gh', 'api']:
            output = json.dumps({'check_runs': [{'name': 'gate', 'status': 'completed',
                                               'conclusion': 'success' if self.gate else 'failure'}]})
        elif args[:3] == ['bash', 'scripts/aide.sh', 'source-sha']:
            output = FINGERPRINT
        elif short[:2] == ['buildx', 'inspect'] and not self.existing_builder:
            return subprocess.CompletedProcess(args, 1)
        elif short[:2] == ['buildx', 'create']:
            self.existing_builder = True
        elif short[:2] == ['buildx', 'build'] and self.fail_build:
            raise RuntimeError('synthetic compilation failed')
        elif short[:3] == ['compose', 'config', '--format']:
            output = json.dumps({'services': {'aide': {'build': {'dockerfile': 'Dockerfile', 'args': {}}}}})
        elif short[:2] == ['image', 'inspect']:
            output = json.dumps({'Id': SCOPE_IMAGE, 'Os': 'linux', 'Architecture': 'arm64', 'Config': {'Labels': {
                'aide.runtime-profile': 'slim',
                'aide.srcsha': 'e' * 64 if self.wrong_source else FINGERPRINT,
                'aide.installation': self.scope}}})
        elif short[:2] == ['ps', '-aq']:
            if any(x.startswith('ancestor=') for x in short):
                output = 'f' * 64 if self.shared_image else ''
            elif any(x.startswith('label=com.docker.compose.project=') for x in short):
                output = 'f' * 64 if self.foreign_project else '\n'.join(self.containers)
            else:
                output = '\n'.join(self.containers)
        elif short[:1] == ['inspect']:
            output = json.dumps({'Image': SCOPE_IMAGE, 'Config': {'Labels': {
                'com.docker.compose.project.working_dir': str(self.root),
                'com.docker.compose.project': 'owned-project'}}})
        elif short[:2] == ['image', 'ls']:
            output = SCOPE_IMAGE
        if capture:
            return output
        return subprocess.CompletedProcess(args, 0)


class LauncherContractTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='aide-contract-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        (self.root / '.git').mkdir()
        (self.root / 'Dockerfile').write_text('FROM owned-fixture\n')
        (self.root / 'version.md').write_text('# aide 版本记录\n\n**当前版本：0.1.17.6 RC1**\n')
        (self.root / '.env.example').write_text('AIDE_IMAGE=aide:previous\nUSER_SETTING=preserved\n')
        self.launcher = BoundaryLauncher(self.root)

    def test_slow_fingerprint_shows_progress_without_polluting_digest(self):
        output = io.StringIO()
        with patch.object(module, 'PROGRESS_INTERVAL', .05), contextlib.redirect_stderr(output):
            digest = module.Launcher.call(self.launcher,
                [sys.executable, '-c', 'import time;time.sleep(.16);print("c"*64)'],
                capture=True, timeout=2, progress='Computing source fingerprint')
        self.assertEqual(digest, FINGERPRINT)
        self.assertIn('仍在读取源码', output.getvalue())
        self.assertIn('源码指纹计算完成', output.getvalue())

    def test_fingerprint_timeout_kills_pipeline_descendants(self):
        pidfile = self.root / 'child.pid'
        script = 'sleep 30 & child=$!; echo "$child" > "$1"; wait'
        with patch.object(module, 'PROGRESS_INTERVAL', .05), contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(subprocess.TimeoutExpired):
                module.Launcher.call(self.launcher, ['bash', '-c', script, 'fixture', str(pidfile)],
                                    capture=True, timeout=1.5, progress='Computing source fingerprint')
        pid = pidfile.read_text().strip()
        for _ in range(20):
            state = subprocess.run(['ps', '-p', pid, '-o', 'stat='], text=True, capture_output=True).stdout.strip()
            if not state or state.startswith('Z'):
                break
            time.sleep(.05)
        self.assertTrue(not state or state.startswith('Z'), state)

    def test_build_is_explicit_scoped_and_starts_verified_source(self):
        self.launcher.build()
        calls = self.launcher.commands
        builds = [c for c in calls if c[1:3] == ['buildx', 'build']]
        self.assertEqual(len(builds), 1)
        self.assertIn(self.launcher.builder, builds[0])
        self.assertIn('aide.installation=' + self.launcher.scope, builds[0])
        self.assertIn('AIDE_SRC_SHA=' + FINGERPRINT, builds[0])
        self.assertIn('AIDE_RUNTIME_MODE=source', builds[0])
        self.assertEqual(calls[-1], ['bash', 'scripts/aide.sh', 'start-built'])
        self.assertEqual(self.launcher.recycle_calls[0][0], len(calls))
        self.assertEqual(self.launcher.recycle_calls[0][1]['expected_image'], SCOPE_IMAGE)
        self.assertIn('AIDE_IMAGE=' + self.launcher.image, (self.root / '.env').read_text())
        self.assertIn('USER_SETTING=preserved', (self.root / '.env').read_text())
        self.assertTrue(self.launcher.owner())

    def test_mac_host_build_precedes_configuration_and_start(self):
        native = self.root / 'native/macos-desktop/main.swift'
        native.parent.mkdir(parents=True)
        native.write_text('// fixture')
        with patch.object(module.sys, 'platform', 'darwin'):
            self.launcher.build()
        native_call = [sys.executable, 'scripts/build-macos-desktop.py', '--root', str(self.root), '--install']
        calls = self.launcher.commands
        self.assertIn(native_call, calls)
        self.assertLess(calls.index(native_call), calls.index(['bash', 'scripts/aide.sh', 'start-built']))
        self.assertIn('AIDE_IMAGE=' + self.launcher.image, (self.root / '.env').read_text())

    def test_mac_host_failure_keeps_previous_startup_configuration(self):
        native = self.root / 'native/macos-desktop/main.swift'
        native.parent.mkdir(parents=True)
        native.write_text('// fixture')
        original = self.launcher.call
        def fail_native(args, **kwargs):
            if len(args) > 1 and args[1] == 'scripts/build-macos-desktop.py':
                raise RuntimeError('synthetic Swift failure')
            return original(args, **kwargs)
        with patch.object(module.sys, 'platform', 'darwin'), patch.object(self.launcher, 'call', side_effect=fail_native):
            with self.assertRaisesRegex(RuntimeError, 'Swift failure'):
                self.launcher.build()
        self.assertFalse((self.root / '.env').exists())
        self.assertNotIn(['bash', 'scripts/aide.sh', 'start-built'], self.launcher.commands)

    def test_host_source_is_prepared_before_docker_without_changing_scope(self):
        self.launcher.build()
        calls = self.launcher.commands
        prepared = [c for c in calls if len(c)>1 and c[1]=='scripts/build-codex-host.py']
        self.assertEqual(len(prepared),1)
        self.assertIn('--prepare-only',prepared[0])
        self.assertIn(str(self.root/'docker/host-sources'),prepared[0])
        self.assertIn(str(self.launcher.state/'host-sources'),prepared[0])
        build = next(c for c in calls if c[1:3]==['buildx','build'])
        self.assertLess(calls.index(prepared[0]),calls.index(build))

    def test_low_disk_attempts_scoped_recovery_then_rechecks_before_compiling(self):
        original = self.launcher.call
        disk_calls = []
        def low_once(args, **kwargs):
            if args[:2] == ['bash', '-ec']:
                disk_calls.append(args)
                if len(disk_calls) == 1:
                    raise RuntimeError('synthetic low disk')
            return original(args, **kwargs)
        self.launcher.call = low_once
        self.launcher.build()
        self.assertEqual(len(disk_calls), 2)
        self.assertEqual(len(self.launcher.recycle_calls), 2)
        self.assertNotIn('expected_image', self.launcher.recycle_calls[0][1])
        self.assertEqual(self.launcher.recycle_calls[1][1]['expected_image'], SCOPE_IMAGE)

    def test_persistent_low_disk_and_failed_start_do_not_recycle_new_outputs(self):
        original = self.launcher.call
        def low_disk(args, **kwargs):
            if args[:2] == ['bash', '-ec']:
                raise RuntimeError('synthetic low disk')
            return original(args, **kwargs)
        self.launcher.call = low_disk
        with self.assertRaises(RuntimeError):
            self.launcher.build()
        self.assertFalse(any(c[1:3] == ['buildx', 'build'] for c in self.launcher.commands))
        self.assertFalse((self.root / '.env').exists())
        self.launcher.recycle_calls.clear()
        def bad_start(args, **kwargs):
            if args == ['bash', 'scripts/aide.sh', 'start-built']:
                raise RuntimeError('synthetic start failure')
            return original(args, **kwargs)
        self.launcher.call = bad_start
        with self.assertRaisesRegex(RuntimeError, 'start failure'):
            self.launcher.build()
        self.assertEqual(self.launcher.recycle_calls, [])

    def test_source_preparation_failure_preserves_current_installation(self):
        original=self.launcher.call
        def fail_source(args,**kwargs):
            if len(args)>1 and args[1]=='scripts/build-codex-host.py':
                raise RuntimeError('synthetic source download failure')
            return original(args,**kwargs)
        self.launcher.call=fail_source
        with self.assertRaisesRegex(RuntimeError,'source download failure'):
            self.launcher.build()
        self.assertFalse(any(c[1:3]==['buildx','build'] for c in self.launcher.commands))
        self.assertNotIn(['bash','scripts/aide.sh','start-built'],self.launcher.commands)
        self.assertFalse((self.root/'.env').exists())

    def test_build_executes_real_disk_child_without_inherited_project_dir(self):
        # Keep the Docker boundary disposable, but execute the actual Bash child
        # and production guard that the old all-call mock accidentally skipped.
        scripts=self.root/'scripts';scripts.mkdir()
        shutil.copy2(ROOT/'scripts/resource-guard.sh',scripts/'resource-guard.sh')
        tools=self.root/'tools';tools.mkdir()
        df=tools/'df'
        df.write_text('#!/bin/sh\n[ "$2" = / ] || [ "$2" = "$EXPECTED_PROJECT" ] || exit 42\nprintf "fixture 99999999 0 99999999 1%% /\\n"\n')
        df.chmod(0o700)
        original=self.launcher.call
        def real_disk(args,**kwargs):
            if args[:2]==['bash','-ec']:
                return module.Launcher.call(self.launcher,args,**kwargs)
            return original(args,**kwargs)
        for inherited in (None,'','/wrong-parent-project'):
            with self.subTest(inherited=inherited),patch.dict(os.environ,{'PATH':str(tools)+os.pathsep+os.environ['PATH'],'EXPECTED_PROJECT':str(self.root)}):
                os.environ.pop('PROJECT_DIR',None)
                if inherited is not None:os.environ['PROJECT_DIR']=inherited
                with patch.object(self.launcher,'call',side_effect=real_disk):self.launcher.build()

    def test_failed_build_cannot_switch_image_or_start_release(self):
        (self.root / '.env').write_text('AIDE_IMAGE=aide:previous\n')
        self.launcher.fail_build = True
        with self.assertRaisesRegex(RuntimeError, 'compilation failed'):
            self.launcher.build()
        self.assertEqual((self.root / '.env').read_text(), 'AIDE_IMAGE=aide:previous\n')
        self.assertFalse(any(c[:3] == ['bash', 'scripts/aide.sh', 'start-built'] for c in self.launcher.commands))
        self.assertEqual(self.launcher.recycle_calls, [])
        self.assertFalse(any('latest-release' in x for c in self.launcher.commands for x in c))

    def test_wrong_build_identity_retains_previous_configuration(self):
        self.launcher.wrong_source = True
        with self.assertRaisesRegex(ValueError, 'did not verify'):
            self.launcher.build()
        self.assertFalse((self.root / '.env').exists())
        self.assertNotEqual(self.launcher.commands[-1], ['bash', 'scripts/aide.sh', 'start-built'])

    def test_other_builder_is_never_adopted(self):
        self.launcher.existing_builder = True
        with self.assertRaisesRegex(ValueError, 'refusing to adopt'):
            self.launcher.build()
        self.assertFalse(any(c[1:3] == ['buildx', 'build'] for c in self.launcher.commands))

    def test_separate_checkouts_get_separate_image_and_cache_identity(self):
        other = self.root / 'another-copy'
        other.mkdir()
        launcher = BoundaryLauncher(other)
        self.assertNotEqual(launcher.builder, self.launcher.builder)
        self.assertNotEqual(launcher.image, self.launcher.image)

    def test_release_package_rejects_source_commands_before_docker(self):
        (self.root / '.git').rmdir()
        for action in ('build', 'release'):
            with self.subTest(action=action), self.assertRaisesRegex(ValueError, 'Release package'):
                getattr(self.launcher, action)()
        self.assertEqual(self.launcher.commands, [])

    def test_cleanup_preserves_data_and_never_prunes_global_resources(self):
        self.launcher.ensure_builder()
        self.launcher.containers = [CID]
        data = self.root / '.aide-data/storage/data/sentinel'
        data.parent.mkdir(parents=True)
        data.write_bytes(b'owned user data')
        self.launcher.docker_clean()
        self.assertEqual(data.read_bytes(), b'owned user data')
        self.assertTrue(any(c[1:3] == ['container', 'rm'] and c[-1] == CID for c in self.launcher.commands))
        self.assertTrue(any(c[1:3] == ['buildx', 'rm'] and c[-1] == self.launcher.builder for c in self.launcher.commands))
        for command in self.launcher.commands:
            self.assertNotIn('prune', command)
            self.assertNotIn('volume', command)
            self.assertNotIn('--force', command)
        self.assertFalse(self.launcher.receipt.exists())

    def test_foreign_project_refuses_cleanup_before_mutation(self):
        self.launcher.containers = [CID]
        self.launcher.foreign_project = True
        with self.assertRaisesRegex(ValueError, 'foreign installation'):
            self.launcher.docker_clean()
        self.assertFalse(any('rm' in c or 'stop' in c for c in self.launcher.commands))

    def test_shared_image_is_retained(self):
        self.launcher.shared_image = True
        self.launcher.docker_clean()
        self.assertFalse(any(c[1:3] == ['image', 'rm'] for c in self.launcher.commands))

    def test_dry_cleanup_only_reads_and_does_not_touch_state(self):
        self.launcher.dry_run = True
        self.launcher.containers = [CID]
        self.launcher.docker_clean()
        self.assertFalse((self.root / '.aide-data').exists())
        self.assertFalse(any('rm' in c or 'stop' in c or 'create' in c for c in self.launcher.commands))

    def test_symlink_build_state_is_rejected_before_mutation(self):
        outside = self.root / 'outside'
        outside.mkdir()
        (self.root / '.aide-data').symlink_to(outside, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, 'symbolic links'):
            self.launcher.ensure_builder()
        self.assertEqual(self.launcher.commands, [])

    def test_verify_missing_docker_reports_failure_without_start_or_build(self):
        def unavailable(*args, **kwargs):
            raise FileNotFoundError('synthetic missing Docker')
        with patch.object(self.launcher, 'call', side_effect=unavailable):
            self.assertEqual(self.launcher.verify(), 1)
        self.assertFalse((self.root / '.aide-data').exists())

    def test_verify_stopped_runtime_is_diagnostic_only(self):
        self.assertEqual(self.launcher.verify(), 1)
        self.assertFalse((self.root / '.env').exists())
        self.assertFalse(any(x in c for c in self.launcher.commands for x in ('up', 'build', 'create', 'load', 'pull')))

    def test_release_refuses_dirty_main_or_wrong_tag_without_dispatch(self):
        for name, value in [('branch', 'feature'), ('dirty', ' M file'), ('tagged', 'e'*40)]:
            with self.subTest(name=name), patch.object(self.launcher, name, value), self.assertRaises(ValueError):
                self.launcher.release()
        self.assertFalse(any(c[:3] == ['gh', 'workflow', 'run'] for c in self.launcher.commands))

    def test_release_uses_existing_notes_and_current_gate(self):
        sys.path.insert(0, str(ROOT / 'scripts/ci'))
        import delivery_gate
        with patch.object(delivery_gate, 'release_notes', return_value='reviewed') as notes:
            self.launcher.gate = False
            with self.assertRaisesRegex(ValueError, 'successful required gate'):
                self.launcher.release()
            self.assertFalse(any(c[:3] == ['gh', 'workflow', 'run'] for c in self.launcher.commands))
            self.launcher.gate = True
            self.launcher.release()
        notes.assert_called_with('v0.1.17.6-RC1')
        self.assertEqual(self.launcher.commands[-1], ['gh', 'workflow', 'run', 'aide-delivery.yml',
                         '--repo', 'skyelan1999/aide', '--ref', 'main', '-f', 'release_tag=v0.1.17.6-RC1'])


class ActualStartEntrypointTests(unittest.TestCase):
    def exercise(self, running, marker=False, action='start'):
        with tempfile.TemporaryDirectory(prefix='aide-start-contract-') as temporary:
            root = Path(temporary).resolve()
            (root / 'scripts').mkdir()
            (root / 'bin').mkdir()
            (root / '.git').mkdir()
            for name in ('aide.sh', 'prebuilt-image.sh', 'launcher-data.sh', 'launcher-data.py'):
                shutil.copyfile(ROOT / 'scripts' / name, root / 'scripts' / name)
            (root / 'scripts/resource-guard.sh').write_text('aide_check_disk(){ :; }; aide_check_engine_resources(){ :; };\n')
            (root / '.env.example').write_text('AIDE_IMAGE=aide:owned\n')
            (root / '.env').write_text('AIDE_IMAGE=aide:owned\nAIDE_BROWSER_BRIDGE_TOKEN=' + 'x'*32 + '\nAIDE_COMPUTER_BRIDGE_TOKEN=' + 'y'*32 + '\n')
            certificate = root / '.aide-data/storage/data/certs/cert.pem'
            certificate.parent.mkdir(parents=True)
            certificate.write_text('synthetic certificate')
            if marker:
                (root / '.aide-image').write_text('aide:old-slot sha256:' + 'f'*64 + ' linux/arm64 v0.1.0.0-RC1\n')
            docker = root / 'bin/docker'
            docker.write_text('#!' + sys.executable + '\n' + r'''
import json,os,sys
a=sys.argv[1:]
with open(os.environ['CALL_LOG'],'a') as f:f.write(json.dumps(a)+'\n')
if a[0]=='info':sys.exit(0)
if a[:3]==['compose','ps','--status']:
 print('a'*64 if os.environ['RUNNING']=='1' else '');sys.exit(0)
if a[:2]==['ps','-q']:sys.exit(0)
if a[:3]==['compose','config','--images']:print('aide:owned');sys.exit(0)
if a[:3]==['compose','exec','-T']:
 if 'curl' in a:print('{"service":"aide","status":"ok","instanceId":"owned-instance"}')
 else:print('synthetic-owned-token')
 sys.exit(0)
if a[:2]==['compose','port']:print('127.0.0.1:18640');sys.exit(0)
if a[:2]==['image','inspect']:sys.exit(1)
sys.exit('Unexpected Docker action: '+repr(a))
''')
            docker.chmod(0o755)
            curl = root / 'bin/curl'
            curl.write_text('#!/bin/sh\nprintf \'%s\' \'{"service":"aide","status":"ok","instanceId":"owned-instance"}\'\n')
            curl.chmod(0o755)
            env = dict(os.environ, PATH=str(root / 'bin') + os.pathsep + os.environ['PATH'],
                       CALL_LOG=str(root / 'calls'), RUNNING=str(int(running)),
                       AIDE_BUILD_FROM_SOURCE='1', AIDE_FORCE_REBUILD='1', AIDE_OPEN_BROWSER='0')
            result = subprocess.run(['bash', 'scripts/aide.sh', action], cwd=root, env=env,
                                    capture_output=True, text=True, timeout=15)
            calls = [json.loads(line) for line in (root / 'calls').read_text().splitlines()]
            success = running and action == 'start'
            self.assertEqual(result.returncode == 0, success, result.stderr)
            for command in calls:
                for forbidden in ('build', 'buildx', 'pull', 'load', 'create', 'tag', 'up'):
                    self.assertNotIn(forbidden, command, 'ordinary start invoked a compiler/image mutation')
            if not success:
                self.assertIn('build.command', result.stderr)
            return calls

    def test_healthy_source_start_ignores_legacy_compile_switches(self):
        self.exercise(True)

    def test_missing_runtime_refuses_instead_of_compiling(self):
        self.exercise(False)

    def test_stale_bundle_marker_cannot_select_release_or_trigger_import(self):
        self.exercise(True, marker=True)
        self.exercise(False, marker=True)

    def test_build_completion_cannot_reopen_previous_healthy_image(self):
        # The built image is deliberately missing. A healthy old runtime must
        # not short-circuit the post-build path and pretend it was applied.
        self.exercise(True, action='start-built')


class ReleaseAssemblyTests(unittest.TestCase):
    def test_actual_packager_ships_only_release_interfaces(self):
        # Run the real packager/exporter against a tiny synthetic image boundary.
        # This checks the assembled ZIP, not merely text in a copy command.
        with tempfile.TemporaryDirectory(prefix='aide-release-contract-') as directory:
            root = Path(directory)
            (root / 'scripts').mkdir()
            for path in (ROOT / 'scripts').iterdir():
                if path.is_file():
                    shutil.copy2(path, root / 'scripts' / path.name)
            (root / 'scripts/browser-runtime').mkdir()
            for name in ('package.json','package-lock.json'):
                shutil.copy2(ROOT / 'scripts/browser-runtime' / name, root / 'scripts/browser-runtime' / name)
            for pattern in ('*.command','*.sh','*.bat'):
                for path in ROOT.glob(pattern):
                    shutil.copy2(path, root / path.name)
            for name in ('version.md','LICENSE','.env.example','.dockerignore','Dockerfile','compose.yaml','go.mod','go.sum',
                         'docker/compose.offline.yaml','docker/offline.env.example','docker/OFFLINE.md',
                         'docs/architecture/mixsoul.md','vendor/github.com/yuin/goldmark/v2/LICENSE',
                         'internal/server/code_analysis/ACORN-LICENSE',
                         'internal/server/web/vendor/drawio/LICENSE',
                         'internal/server/web/vendor/drawio/NOTICE.txt'):
                target = root / name
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(ROOT / name, target)
            shutil.copytree(ROOT / 'legal/third_party', root / 'legal/third_party')
            for name in ('cmd','internal','scripts/office','docker/wheels','docker/sherpa'):
                (root / name).mkdir(parents=True, exist_ok=True)
            binary = bytearray(128)
            binary[:2] = b'MZ'
            struct.pack_into('<I', binary, 0x3c, 64)
            binary[64:68] = b'PE\0\0'
            struct.pack_into('<H', binary, 68, 0x8664)
            (root / 'fixture.exe').write_bytes(binary)
            (root / 'bin').mkdir()
            docker = root / 'bin/docker'
            docker.write_text('#!' + sys.executable + '\n' + r'''
import os,pathlib,sys
a=sys.argv[1:]
if a==['info']:sys.exit(0)
if a[:3]==['compose','config','--images']:print('aide:fixture');sys.exit(0)
if a[:2]==['image','inspect']:
 if a[2]=='aide:dependencies-v2':sys.exit(1)
 f=a[-1]
 print(os.environ['FIXTURE_SOURCE'] if 'aide.srcsha' in f else ('linux/amd64' if '.Os' in f else 'sha256:'+'b'*64));sys.exit(0)
if a[:2]==['image','tag']:sys.exit(0)
if a[:2]==['image','save']:
 if '-o' in a:pathlib.Path(a[a.index('-o')+1]).write_bytes(b'synthetic-image')
 else:sys.stdout.buffer.write(b'synthetic-image')
 sys.exit(0)
sys.exit('Unexpected package Docker action: '+repr(a))
''')
            docker.chmod(0o755)
            fingerprint = subprocess.check_output(['bash','scripts/aide.sh','source-sha'],cwd=root,text=True).strip()
            env = dict(os.environ, PATH=str(root / 'bin')+os.pathsep+os.environ['PATH'],
                       FIXTURE_SOURCE=fingerprint,AIDE_WINDOWS_LAUNCHER=str(root / 'fixture.exe'))
            display, tag = module.version(root)
            output = root / 'output'
            result = subprocess.run(['bash','scripts/package-release-assets.sh',tag,'aide:fixture',str(output)],
                                    cwd=root,env=env,capture_output=True,text=True,timeout=30)
            self.assertEqual(result.returncode,0,result.stdout+result.stderr)
            for target in ('macos-amd64','windows-amd64','ubuntu-amd64','full-linux-amd64'):
                with zipfile.ZipFile(output / ('aide-'+tag+'-'+target+'.zip')) as package:
                    names = {Path(n).name for n in package.namelist()}
                    suffixes = ('command','sh','bat') if target.startswith('full-') else ({'macos':'command','windows':'bat','ubuntu':'sh'}[target.split('-')[0]],)
                    for suffix in suffixes:
                        for action in ('start','stop','docker_clean','verify'):
                            self.assertIn(action+'.'+suffix,names)
                    for suffix in ('command','sh','bat'):
                        for action in ('build','clean','release','clean-docker-cache'):
                            self.assertNotIn(action+'.'+suffix,names)
                    self.assertFalse(any(n.endswith('.ps1') for n in names))
                    if target.startswith(('windows-', 'full-')):
                        self.assertIn('windows-entrypoint.bat',names)
                    self.assertNotIn('.env',names)
                    self.assertNotIn('owner.json',names)


class RecycleLauncher(module.Launcher):
    def __init__(self, root):
        super().__init__(root)
        self.commands = []
        self.active = 'sha256:' + 'a' * 64
        self.current_health = 'healthy'
        self.health_sequence = []
        self.current_owner = str(self.root)
        self.images = {}
        self.tags = {}
        self.race_image = None
        self.fail_remove = False
        self.fail_prune = False
        self.users = {CID: self.active}
        module.atomic_json(self.receipt, {'version': 1, 'scope': self.scope, 'builder': self.builder})

    def add_image(self, letter, tags=(), **labels):
        image = 'sha256:' + letter * 64
        self.images[image] = {'Id': image, 'RepoTags': list(tags), 'Config': {'Labels': {
            'aide.installation': self.scope, 'aide.build-role': 'runtime',
            'aide.runtime-profile': 'slim', **labels}}}
        self.tags.update({tag: image for tag in tags})
        return image

    def call(self, args, capture=False, check=True, **kwargs):
        args = [str(x) for x in args]
        self.commands.append(args)
        c = args[1:]
        output = ''
        if c[:2] == ['compose', 'ps']:
            output = CID
        elif c[0] == 'inspect':
            user = c[1]
            if user == CID and self.health_sequence:
                self.current_health = self.health_sequence.pop(0)
            output = json.dumps({'Image': self.users[user], 'State': {
                'Running': user == CID, 'Health': {'Status': self.current_health}},
                'Config': {'Labels': {'com.docker.compose.project.working_dir': self.current_owner}}})
        elif c[0] == 'exec':
            output = json.dumps({'service': 'aide', 'status': 'ok'})
        elif c[:2] == ['ps', '-aq']:
            ancestor = next((x.split('=', 1)[1] for x in c if x.startswith('ancestor=')), None)
            output = '\n'.join(user for user, image in self.users.items()
                               if ancestor is None or image == ancestor)
            if ancestor and ancestor == self.race_image:
                output = 'f' * 64
        elif c[:2] == ['image', 'ls']:
            # Include a foreign result to test the second ownership check.
            output = '\n'.join(image for image, data in self.images.items()
                               if '-a' in c or data['RepoTags'])
        elif c[:2] == ['image', 'inspect']:
            image = self.tags.get(c[2], c[2])
            if image not in self.images:
                if check:
                    raise RuntimeError('missing image')
                return subprocess.CompletedProcess(args, 1)
            output = json.dumps(self.images[image])
        elif c[:2] == ['image', 'rm']:
            ref = c[2]
            if self.fail_remove:
                return subprocess.CompletedProcess(args, 1)
            if ref in self.tags:
                image = self.tags.pop(ref)
                self.images[image]['RepoTags'].remove(ref)
                # Deliberately retain an untagged object so full-ID cleanup runs.
            elif ref in self.images:
                assert ref not in self.users.values(), 'must never remove referenced image'
                assert not self.images[ref]['RepoTags'], 'must detach all obsolete tags first'
                del self.images[ref]
        elif c[:2] == ['buildx', 'prune']:
            if self.fail_prune:
                raise RuntimeError('synthetic prune failure')
            output = 'Total: 1GB'
        else:
            raise AssertionError('Unexpected recycling command: ' + str(c))
        return output if capture else subprocess.CompletedProcess(args, 0)


class RecycleTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='aide-recycle-')
        self.addCleanup(self.tmp.cleanup)
        self.launcher = RecycleLauncher(Path(self.tmp.name))
        self.current = self.launcher.add_image('a', ['aide:source-current'])

    def removals(self):
        return [c for c in self.launcher.commands if c[1:3] == ['image', 'rm']]

    def test_deletes_obsolete_tags_and_dangling_ids_preserving_all_dependencies(self):
        old = self.launcher.add_image('b', ['aide:old-one', 'aide:old-two'])
        dangling = self.launcher.add_image('c')
        seed = self.launcher.add_image('d', ['aide:sdk-seed-fixture'])
        toolbox = self.launcher.add_image('e', ['aide:release-ci-test-tools'], **{'aide.runtime-profile': 'toolbox'})
        foreign = self.launcher.add_image('f', ['aide:foreign'], **{'aide.installation': 'other-scope'})
        referenced = self.launcher.add_image('1', ['aide:used-by-exited-container'])
        self.launcher.users['2' * 64] = referenced
        configured = self.launcher.add_image('3', ['aide:custom-compile-input'])
        self.launcher.recycle_build_artifacts(expected_image=self.current, dependencies=['aide:custom-compile-input'])
        self.assertEqual(set(self.launcher.images), {self.current, seed, toolbox, foreign, referenced, configured})
        targets = [c[3] for c in self.removals()]
        self.assertIn(old, targets)
        self.assertIn(dangling, targets)
        self.assertIn('-a', next(c for c in self.launcher.commands if c[1:3] == ['image', 'ls']))
        self.assertIn('aide:old-one', targets)
        self.assertIn('aide:old-two', targets)
        prune = next(c for c in self.launcher.commands if c[1:3] == ['buildx', 'prune'])
        self.assertIn(self.launcher.builder, prune)
        self.assertIn('type=regular', prune)
        self.assertIn('until=24h', prune)
        self.assertIn('20GB', prune)
        receipt = json.loads((self.launcher.state / 'recycle-last.json').read_text())
        self.assertEqual(set(receipt['removedImages']), {old, dangling})
        self.assertEqual(receipt['activeImage'], self.current)

    def test_unhealthy_foreign_or_wrong_runtime_never_deletes(self):
        self.launcher.add_image('b', ['aide:old'])
        for bad in ('unhealthy', 'foreign', 'wrong-image'):
            with self.subTest(bad=bad):
                self.launcher.commands.clear()
                self.launcher.current_health = 'unhealthy' if bad == 'unhealthy' else 'healthy'
                self.launcher.current_owner = '/foreign' if bad == 'foreign' else str(self.launcher.root)
                with self.assertRaises(ValueError):
                    self.launcher.recycle_build_artifacts(expected_image='sha256:'+'b'*64 if bad == 'wrong-image' else self.current)
                self.assertFalse(self.removals())
                self.assertFalse(any('prune' in c for c in self.launcher.commands))

    def test_reference_race_and_other_repository_tag_are_retained(self):
        old = self.launcher.add_image('b', ['aide:old'])
        alias = self.launcher.add_image('c', ['aide:old-alias', 'other-project:dependency'])
        self.launcher.race_image = old
        self.launcher.recycle_build_artifacts(expected_image=self.current)
        self.assertIn(old, self.launcher.images)
        self.assertIn(alias, self.launcher.images)
        self.assertFalse(self.removals())

    def test_starting_health_is_waited_for_and_timeout_does_not_delete(self):
        old = self.launcher.add_image('b', ['aide:old'])
        self.launcher.health_sequence = ['starting', 'starting', 'healthy']
        with patch.object(module.time, 'sleep') as pause:
            self.launcher.recycle_build_artifacts(expected_image=self.current)
        self.assertEqual(pause.call_count, 2)
        self.assertNotIn(old, self.launcher.images)
        self.launcher.commands.clear()
        self.launcher.current_health = 'starting'
        with patch.object(module.time, 'sleep') as pause, self.assertRaisesRegex(ValueError, 'health check'):
            self.launcher.recycle_build_artifacts(expected_image=self.current)
        self.assertEqual(pause.call_count, 30)
        self.assertFalse(self.removals())
        self.assertFalse(any('prune' in c for c in self.launcher.commands))

    def test_removal_failure_and_gc_failure_are_visible_and_not_forced(self):
        self.launcher.add_image('b', ['aide:old'])
        self.launcher.fail_remove = True
        self.launcher.fail_prune = True
        errors = io.StringIO()
        with contextlib.redirect_stderr(errors):
            self.launcher.try_recycle_build_artifacts(expected_image=self.current)
        self.assertIn('synthetic prune failure', errors.getvalue())
        self.assertTrue(all('--force' not in c and '-f' not in c for c in self.removals()))
        self.assertEqual(len(self.launcher.images), 2)

    def test_default_builder_and_dry_run_never_prune_shared_cache(self):
        self.launcher.recycle_build_artifacts(expected_image=self.current, builder_mode='default')
        self.assertFalse(any('prune' in c for c in self.launcher.commands))
        self.launcher.commands.clear()
        self.launcher.dry_run = True
        self.launcher.recycle_build_artifacts()
        self.assertEqual(self.launcher.commands, [])


if __name__ == '__main__':
    unittest.main()
