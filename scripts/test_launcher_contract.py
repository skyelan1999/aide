#!/usr/bin/env python3
"""Execute the standard launcher coordinator with owned external boundaries."""
import importlib.util
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

    def call(self, args, capture=False, check=True, timeout=180, env=None):
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
        self.assertIn('AIDE_IMAGE=' + self.launcher.image, (self.root / '.env').read_text())
        self.assertIn('USER_SETTING=preserved', (self.root / '.env').read_text())
        self.assertTrue(self.launcher.owner())

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
                         'docs/architecture/mixsoul.md','vendor/github.com/yuin/goldmark/v2/LICENSE'):
                target = root / name
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(ROOT / name, target)
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


if __name__ == '__main__':
    unittest.main()
