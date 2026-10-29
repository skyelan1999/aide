"""Migration durability and failure boundaries, entirely in owned temp folders."""
import concurrent.futures
import importlib.util
import json
import os
from pathlib import Path
import shutil
import socket
import stat
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('launcher_data', ROOT / 'scripts/launcher-data.py')
storage = importlib.util.module_from_spec(spec)
spec.loader.exec_module(storage)


class ComposeOverrideTests(unittest.TestCase):
    def append(self, dotenv, exported=None, fail=False):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / '.env').write_text(dotenv)
            docker = root / 'docker'
            docker.write_text('#!/bin/sh\n' + ('exit 97\n' if fail else 'cat .env\n'))
            docker.chmod(0o700)
            env = dict(os.environ)
            env.pop('COMPOSE_FILE', None)
            if exported is not None:
                env['COMPOSE_FILE'] = exported
            script = 'set -euo pipefail; source "$1"; launcher_append_compose_override "$2" generated.yaml; launcher_append_compose_override "$2" generated.yaml; printf "%s" "$COMPOSE_FILE"'
            return subprocess.run(['bash', '-c', script, 'fixture', str(ROOT / 'scripts/launcher-data.sh'), str(docker)], cwd=root, env=env, text=True, capture_output=True)

    def test_dotenv_compose_files_survive_generated_mount_override(self):
        result = self.append('OTHER=private-fixture\nCOMPOSE_FILE=compose.yaml:compose.macos-root.yaml\n')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, 'compose.yaml:compose.macos-root.yaml:generated.yaml')
        self.assertNotIn('private-fixture', result.stdout + result.stderr)

    def test_exported_compose_files_keep_precedence_without_resolving_dotenv(self):
        result = self.append('', exported='custom path/base.yaml:extra.yaml', fail=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, 'custom path/base.yaml:extra.yaml:generated.yaml')

    def test_default_and_resolution_failure(self):
        self.assertEqual(self.append('OTHER=value\n').stdout, 'compose.yaml:generated.yaml')
        self.assertNotEqual(self.append('', fail=True).returncode, 0)


class LauncherDataTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='aide storage ')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.source = self.root / 'legacy'
        (self.source / 'auth').mkdir(parents=True)
        key = self.source / 'auth/access-token'
        key.write_text('synthetic-private-value')
        key.chmod(0o600)
        (self.source / 'reference').symlink_to('auth/access-token')
        self.persist = self.root / 'launcher/.aide-data'

    def prepare(self):
        return storage.prepare(self.persist, {'data': self.source}, platform='fixture')

    def test_import_preserves_bytes_modes_links_and_survives_source_removal(self):
        before = storage.manifest(self.source)
        result = self.prepare()
        self.assertEqual(result['status'], 'imported')
        self.assertEqual([r for r in storage.manifest(self.persist / 'storage/data') if r[0] != 'cad-auth'], before)
        self.assertEqual(stat.S_IMODE((self.persist / 'storage/data').stat().st_mode), 0o700)
        receipt = json.loads((self.persist / 'storage/migration.json').read_text())
        self.assertNotIn('synthetic-private-value', json.dumps(receipt))
        shutil.rmtree(self.source)
        self.assertEqual(self.prepare()['status'], 'existing')
        self.assertEqual([r for r in storage.manifest(self.persist / 'storage/data') if r[0] != 'cad-auth'], before)

    def test_fresh_auth_mount_is_prepared_without_user_data_access(self):
        storage.prepare(self.persist, {}, platform='fixture')
        auth = self.persist / 'storage/data/auth'
        self.assertTrue(auth.is_dir())
        self.assertEqual(stat.S_IMODE(auth.stat().st_mode), 0o700)
        self.assertEqual(list(auth.iterdir()), [])
        self.assertTrue((auth.parent/'cad-auth').is_dir())
        self.assertEqual(list((auth.parent/'cad-auth').iterdir()), [])
        auth.rmdir();auth.symlink_to(self.source, target_is_directory=True)
        with self.assertRaisesRegex(RuntimeError, 'Authentication directory'):
            storage.prepare(self.persist, {}, platform='fixture')

    def test_transient_fifo_socket_are_not_copied(self):
        os.mkfifo(self.source / 'pipe')
        handle = socket.socket(socket.AF_UNIX)
        self.addCleanup(handle.close)
        handle.bind(str(self.source / 'socket'))
        self.prepare()
        self.assertFalse((self.persist / 'storage/data/pipe').exists())
        self.assertFalse((self.persist / 'storage/data/socket').exists())

    def test_windows_import_sets_container_user_ownership(self):
        # Force an ownership mismatch independently of the executor's UID/GID.
        uid, gid = os.getuid() + 1, os.getgid() + 1
        with mock.patch.object(storage, 'set_owner') as owner, mock.patch.object(storage.os, 'chown') as direct_owner:
            storage.prepare(self.persist, {}, uid=uid, gid=gid, platform='windows')
        owner.assert_called_once()
        self.assertEqual(owner.call_args.args[1:], (uid, gid))
        self.assertEqual({call.args[0].name for call in direct_owner.call_args_list}, {'auth','cad-auth'})
        self.assertTrue(all(call.args[1:] == (uid, gid) for call in direct_owner.call_args_list))

    def test_windows_matching_auth_ownership_does_not_chown(self):
        with mock.patch.object(storage, 'set_owner'), mock.patch.object(storage.os, 'chown') as direct_owner:
            storage.prepare(self.persist, {}, uid=os.getuid(), gid=os.getgid(), platform='windows')
        direct_owner.assert_not_called()

    def test_windows_existing_storage_repairs_root_ownership(self):
        self.prepare()
        info = (self.persist / 'storage').stat()
        uid, gid = info.st_uid + 1, info.st_gid + 1
        with mock.patch.object(storage, 'set_owner') as owner, mock.patch.object(storage.os, 'chown') as direct_owner:
            storage.prepare(self.persist, {}, uid=uid, gid=gid, platform='windows')
        owner.assert_called_once_with(self.persist / 'storage', uid, gid)
        self.assertEqual({call.args[0].name for call in direct_owner.call_args_list}, {'auth','cad-auth'})

    def test_unverified_existing_destination_is_never_overwritten(self):
        target = self.persist / 'storage'
        target.mkdir(parents=True)
        (target / 'unknown').write_text('retain')
        with self.assertRaisesRegex(RuntimeError, 'refuse'):
            self.prepare()
        self.assertEqual((target / 'unknown').read_text(), 'retain')

    def test_symlink_root_storage_and_receipt_are_rejected(self):
        self.persist.parent.mkdir(parents=True)
        self.persist.symlink_to(self.source, target_is_directory=True)
        with self.assertRaises(RuntimeError): self.prepare()
        self.persist.unlink(); self.persist.mkdir()
        (self.persist / 'storage').symlink_to(self.source, target_is_directory=True)
        with self.assertRaises(RuntimeError): self.prepare()
        (self.persist / 'storage').unlink(); self.prepare()
        receipt = self.persist / 'storage/migration.json'
        receipt.unlink(); receipt.symlink_to(self.source / 'auth/access-token')
        with self.assertRaises(RuntimeError): self.prepare()

    def test_bad_copy_never_commits_and_retains_source_and_stage(self):
        original = storage.shutil.copytree
        def corrupt(source, target, *arguments, **options):
            result = original(source, target, *arguments, **options)
            if (Path(target) / 'auth/access-token').exists(): (Path(target) / 'auth/access-token').write_text('incomplete')
            return result
        with mock.patch.object(storage.shutil, 'copytree', side_effect=corrupt):
            with self.assertRaisesRegex(RuntimeError, 'comparison failed'): self.prepare()
        self.assertFalse((self.persist / 'storage').exists())
        self.assertEqual((self.source / 'auth/access-token').read_text(), 'synthetic-private-value')
        self.assertEqual(len(list(self.persist.glob('.storage-import-*'))), 1)

    def test_simultaneous_launchers_commit_once(self):
        self.persist.mkdir(parents=True)
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            statuses = sorted(pool.map(lambda _: self.prepare()['status'], range(2)))
        self.assertEqual(statuses, ['existing', 'imported'])

    def shell(self, script):
        return subprocess.run(['bash','-c','set -euo pipefail; source "$1"; '+script, 'fixture', str(ROOT/'scripts/launcher-data.sh')], cwd=self.persist.parent, capture_output=True, text=True)

    def test_missing_image_marker_restores_exact_identity(self):
        self.persist.parent.mkdir(parents=True)
        image = self.persist.parent / '.aide-image'
        image.write_text('aide:slot-a sha256:fixture linux/arm64\n')
        self.assertEqual(self.shell('launcher_env_restore; launcher_env_save').returncode, 0)
        image.unlink()
        self.assertEqual(self.shell('launcher_env_restore').returncode, 0)
        self.assertEqual(image.read_text(), 'aide:slot-a sha256:fixture linux/arm64\n')

    def test_missing_environment_restores_literal_secret_configuration(self):
        self.persist.parent.mkdir(parents=True)
        environment = self.persist.parent / '.env'
        literal = 'AI_MODEL=fixture\nPRIVATE_LITERAL=$(touch do-not-create)\n'
        environment.write_text(literal)
        result = self.shell('launcher_env_restore; launcher_env_save')
        self.assertEqual(result.returncode, 0, result.stderr)
        environment.unlink()
        result = self.shell('launcher_env_restore')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(environment.read_text(), literal)
        self.assertFalse((environment.parent / 'do-not-create').exists())

    def test_linked_environment_is_rejected(self):
        self.persist.parent.mkdir(parents=True)
        (self.persist.parent / '.env').symlink_to(self.source / 'auth/access-token')
        self.assertNotEqual(self.shell('launcher_env_restore').returncode, 0)
        self.assertEqual((self.source / 'auth/access-token').read_text(), 'synthetic-private-value')

    def test_persistent_data_excluded_from_build_and_git(self):
        for filename in ('.gitignore','.dockerignore'):
            self.assertIn('.aide-data', (ROOT/filename).read_text().replace('/','').splitlines())
        rules=json.loads((ROOT/'docs/agent/router.json').read_text())
        self.assertIn('.aide-data',rules['cleanup']['protected_roots'])


class LegacyOwnerSelectionTests(unittest.TestCase):
    def test_select_only_intended_service_owner_and_refuse_ambiguity(self):
        import shlex
        cases = [
            ([('main','aide','False'),('job','aide','True')], 'aide', 1, 'main'),
            ([('main','aide','False'),('qa','qa','False')], 'aide', 1, 'main'),
            ([('job','aide','true')], 'aide', 1, ''),
            ([], 'aide', 1, ''),
            ([('old','old-project','False')], 'aide', 0, 'old'),
            ([('old','old-project','False')], 'aide', 1, None),
            ([('one','aide','False'),('two','aide','False')], 'aide', 1, None),
            ([('one','qa','False'),('two','test','False')], 'aide', 0, None),
            ([('main','aide','<no value>'),('job','aide','True')], 'aide', 0, 'main'),
        ]
        for rows, project, explicit, expected in cases:
            with self.subTest(rows=rows, explicit=explicit):
                ids='\n'.join(row[0] for row in rows)
                branches=' '.join(shlex.quote(cid)+") printf '%s\\n' "+shlex.quote(proj+'|'+oneoff)+' ;;' for cid,proj,oneoff in rows)
                script='source "$1"; docker_fixture(){ case "$1" in ps) printf "%s\\n" '+shlex.quote(ids)+' ;; inspect) case "${@: -1}" in '+branches+' *) return 97;; esac;; *) return 98;; esac; }; launcher_legacy_owner docker_fixture /fixture '+shlex.quote(project)+' '+str(explicit)
                result=subprocess.run(['bash','-c',script,'test',str(ROOT/'scripts/launcher-data.sh')],text=True,capture_output=True)
                if expected is None:
                    self.assertNotEqual(result.returncode,0)
                    self.assertIn('No container stopped or data imported',result.stderr)
                    self.assertEqual(result.stdout,'')
                else:
                    self.assertEqual(result.returncode,0,result.stderr)
                    self.assertEqual(result.stdout.strip(),expected)


class PrebuiltImportTests(unittest.TestCase):
    def setUp(self):
        import tarfile,io,hashlib,sys
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name);(self.root/'docker-images').mkdir();(self.root/'docker').mkdir()
        self.image='sha256:'+'1'*64;self.source='2'*64
        self.archive=self.root/'payload.tar.gz'
        with tarfile.open(self.archive,'w:gz') as t:
            data=b'owned synthetic docker image stream';info=tarfile.TarInfo('aide-offline/docker-images/aide-local.tar');info.size=len(data);t.addfile(info,io.BytesIO(data))
        self.digest=hashlib.sha256(self.archive.read_bytes()).hexdigest()
        self.url='https://github.com/skyelan1999/aide/releases/download/v0.1.17.0-RC2/fixture.tar.gz'
        self.manifest=self.root/'docker/prebuilt-images.tsv';self.write_manifest()
        self.bin=self.root/'bin';self.bin.mkdir()
        program='#!'+sys.executable+r"""
import os,sys,pathlib,json,shutil
args=sys.argv[1:];root=pathlib.Path(os.environ['FIX_ROOT'])
with (root/'calls').open('a') as f:f.write(json.dumps([pathlib.Path(sys.argv[0]).name]+args)+'\n')
if pathlib.Path(sys.argv[0]).name=='curl':
 shutil.copyfile(root/'payload.tar.gz',root/args[args.index('--output')+1]);sys.exit(0)
image='sha256:'+'1'*64
if args[0]=='info':print('linux/arm64');sys.exit(0)
if args[:2]==['image','load']:
 sys.stdin.buffer.read()
 if os.environ.get('FIX_LOAD_EXIT'):sys.exit(int(os.environ['FIX_LOAD_EXIT']))
 (root/'loaded').touch();sys.exit(0)
if args[:2]==['image','tag']:sys.exit(0)
if args[:2]==['image','inspect']:
 if args[2]=='aide:local':print('sha256:'+'3'*64);sys.exit(0)
 if not (root/'loaded').exists():sys.exit(1)
 print(image+'|linux/arm64|'+'2'*64+'|8.0.1.1.0' if '|' in args[-1] else image);sys.exit(0)
sys.exit(91)
"""
        for name in ['docker','curl']:
            f=self.bin/name;f.write_text(program);f.chmod(0o755)
        self.env=dict(os.environ,PATH=str(self.bin)+os.pathsep+os.environ['PATH'],FIX_ROOT=str(self.root))
    def write_manifest(self,platform='linux/arm64',url=None):
        self.manifest.write_text(' '.join([platform,self.image,self.source,self.digest,url or self.url,'bundle'])+'\n')
    def run_import(self):
        return subprocess.run(['bash','-ec','source "$1"; launcher_prebuilt_image docker aide:local','fixture',str(ROOT/'scripts/prebuilt-image.sh')],cwd=self.root,env=self.env,text=True,capture_output=True)
    def calls(self):return (self.root/'calls').read_text() if (self.root/'calls').exists() else ''
    def test_existing_image_needs_no_download_or_load(self):
        (self.root/'loaded').touch();r=self.run_import();self.assertEqual(r.returncode,0,r.stderr)
        self.assertNotIn('"curl"',self.calls());self.assertNotIn('"load"',self.calls());self.assertIn('rollback-',self.calls())
    def test_local_archive_imports_without_network_or_build(self):
        shutil.copyfile(self.archive,self.root/'docker-images/fixture.tar.gz');r=self.run_import();self.assertEqual(r.returncode,0,r.stderr)
        self.assertIn('"load"',self.calls());self.assertNotIn('"curl"',self.calls());self.assertNotIn('"build"',self.calls())
    def test_missing_archive_downloads_verifies_and_streams(self):
        r=self.run_import();self.assertEqual(r.returncode,0,r.stderr);self.assertIn('"curl"',self.calls());self.assertTrue((self.root/'docker-images/fixture.tar.gz').is_file());self.assertNotIn('"build"',self.calls())
    def test_bad_checksum_never_loads_or_retags(self):
        (self.root/'docker-images/fixture.tar.gz').write_bytes(b'corrupt');r=self.run_import();self.assertNotEqual(r.returncode,0);self.assertIn('checksum mismatch',r.stderr);self.assertNotIn('"load"',self.calls());self.assertNotIn('"tag"',self.calls())
    def test_wrong_platform_and_untrusted_url_never_download(self):
        for platform,url in [('linux/amd64',self.url),('linux/arm64','https://example.invalid/image.tar.gz')]:
            self.write_manifest(platform,url);r=self.run_import();self.assertNotEqual(r.returncode,0);self.assertNotIn('"curl"',self.calls())
    def test_duplicate_manifest_is_refused(self):
        self.manifest.write_text(self.manifest.read_text()*2);r=self.run_import();self.assertNotEqual(r.returncode,0);self.assertNotIn('"load"',self.calls())
    def run_source_seed(self):
        return subprocess.run(['bash','-ec','source "$1"; launcher_local_build_seed docker','fixture',str(ROOT/'scripts/prebuilt-image.sh')],cwd=self.root,env=self.env,text=True,capture_output=True)
    def test_source_seed_uses_local_bundle_without_activating_old_runtime(self):
        shutil.copyfile(self.archive,self.root/'docker-images/fixture.tar.gz')
        r=self.run_source_seed();self.assertEqual(r.returncode,0,r.stderr)
        self.assertEqual(r.stdout.strip(),'aide:source-build-dependencies')
        calls=[json.loads(line) for line in self.calls().splitlines()]
        tags=[row for row in calls if row[1:3]==['image','tag']]
        self.assertTrue(tags);self.assertTrue(all(row[-1]!='aide:local' for row in tags))
        self.assertNotIn('"curl"',self.calls());self.assertNotIn('"build"',self.calls())
        self.assertIn('Continue compiling current source',r.stderr)
        self.assertNotIn('Source compilation disabled',r.stderr)
    def test_missing_source_seed_archive_never_downloads_or_changes_tags(self):
        r=self.run_source_seed();self.assertEqual(r.returncode,3,r.stderr)
        self.assertIn('No verified local dependency bundle',r.stderr)
        for mutation in ('"curl"','"load"','"tag"','"build"'):self.assertNotIn(mutation,self.calls())
    def test_invalid_source_seed_fails_without_fallback(self):
        (self.root/'docker-images/fixture.tar.gz').write_bytes(b'corrupt')
        r=self.run_source_seed();self.assertEqual(r.returncode,1,r.stderr)
        self.assertIn('checksum mismatch',r.stderr)
        for mutation in ('"curl"','"load"','"tag"'):self.assertNotIn(mutation,self.calls())
    def test_source_seed_rejects_dangling_archive_or_partial_symlink(self):
        for name in ['fixture.tar.gz','fixture.tar.gz.partial']:
            link=self.root/'docker-images'/name;link.symlink_to(self.root/'absent')
            r=self.run_source_seed();self.assertEqual(r.returncode,1,r.stderr)
            self.assertIn('symbolic link',r.stderr);link.unlink()
        self.assertNotIn('"load"',self.calls());self.assertNotIn('"curl"',self.calls())
    def test_source_seed_import_error_is_not_classified_as_missing_archive(self):
        shutil.copyfile(self.archive,self.root/'docker-images/fixture.tar.gz')
        self.env['FIX_LOAD_EXIT']='3';r=self.run_source_seed()
        self.assertEqual(r.returncode,1,r.stderr);self.assertNotIn('"tag"',self.calls());self.assertNotIn('"curl"',self.calls())
    def test_installed_source_seed_needs_no_archive(self):
        (self.root/'loaded').touch();r=self.run_source_seed();self.assertEqual(r.returncode,0,r.stderr)
        self.assertNotIn('"curl"',self.calls());self.assertNotIn('"load"',self.calls())
        self.assertEqual(r.stdout.strip(),'aide:source-build-dependencies')
    def test_duplicate_source_seed_manifest_never_imports(self):
        self.manifest.write_text(self.manifest.read_text()*2)
        r=self.run_source_seed();self.assertEqual(r.returncode,1,r.stderr)
        self.assertNotIn('"load"',self.calls());self.assertNotIn('"curl"',self.calls())
    def test_source_recovery_does_not_replace_build_or_force_semantics(self):
        shell=(ROOT/'scripts/aide.sh').read_text()
        self.assertIn('RECOVERED_SEED="$(launcher_local_build_seed "$DOCKER_BIN")"',shell)
        self.assertIn('scripts/prepare-local-base.sh "$BUILD_SEED"',shell)
        self.assertNotIn('prepare-cad-base.sh',shell)
        self.assertIn('"${AIDE_FORCE_REBUILD:-0}" != 1',shell)
        self.assertIn('"$DOCKER_BIN" compose build aide',shell)
        self.assertIn('"$DOCKER_BIN" compose up -d --no-build --pull never',shell)
    def run_build_recovery(self, status, strict=False):
        env=dict(self.env,FIX_BUILD_EXIT=str(status),AIDE_RELEASE_FALLBACK='0' if strict else '1')
        return subprocess.run(['bash','-ec','source "$1"; build_source() { echo BUILD_ATTEMPT; return "$FIX_BUILD_EXIT"; }; launcher_build_or_release docker aide:local build_source','fixture',str(ROOT/'scripts/prebuilt-image.sh')],cwd=self.root,env=env,text=True,capture_output=True)
    def test_successful_build_does_not_contact_release(self):
        r=self.run_build_recovery(0);self.assertEqual(r.returncode,0,r.stderr);self.assertEqual(self.calls(),'')
    def test_failed_build_recovers_verified_local_release_archive(self):
        shutil.copyfile(self.archive,self.root/'docker-images/fixture.tar.gz')
        r=self.run_build_recovery(7);self.assertEqual(r.returncode,0,r.stderr)
        self.assertIn('BUILD_ATTEMPT',r.stdout);self.assertIn('"load"',self.calls());self.assertIn('"tag"',self.calls());self.assertNotIn('"curl"',self.calls())
    def test_failed_build_fetches_release_only_when_no_cache(self):
        r=self.run_build_recovery(7);self.assertEqual(r.returncode,0,r.stderr)
        self.assertIn('"curl"',self.calls());self.assertIn('"load"',self.calls())
    def test_strict_build_preserves_failure_and_never_downloads(self):
        r=self.run_build_recovery(7,strict=True);self.assertEqual(r.returncode,7,r.stderr);self.assertEqual(self.calls(),'')
    def test_corrupt_release_cache_remains_fatal_after_build_failure(self):
        (self.root/'docker-images/fixture.tar.gz').write_bytes(b'corrupt')
        r=self.run_build_recovery(7);self.assertEqual(r.returncode,1,r.stderr)
        self.assertNotIn('"load"',self.calls());self.assertNotIn('"tag"',self.calls())
    def test_native_runtime_reuse_is_bypassed_by_force_and_source_modes(self):
        source=(ROOT/'scripts/aide.sh').read_text()
        start=source.index('    # Normal startup prefers')
        end=source.index('    if [[ "$MODE" == start ]] && ! launcher_should_build_source',start)
        script='set -euo pipefail\nfixture_docker() { echo aide:local; }; launcher_local_runtime_ready() { return "$FIX_LOCAL"; };\n'+source[start:end]+'echo RESULT:$MODE\n'
        for force,mode,local,expected in [('0','auto','0','start-image'),('1','auto','0','start'),('0','1','0','start'),('0','auto','1','start')]:
            with self.subTest(force=force,mode=mode,local=local):
                env=dict(os.environ,MODE='start',DOCKER_BIN='fixture_docker',AIDE_FORCE_REBUILD=force,AIDE_BUILD_FROM_SOURCE=mode,FIX_LOCAL=local)
                r=subprocess.run(['bash','-c',script],env=env,text=True,capture_output=True)
                self.assertEqual(r.returncode,0,r.stderr);self.assertIn('RESULT:'+expected,r.stdout)
    def test_source_checkout_and_windows_entry_build_policy(self):
        shell=(ROOT/'scripts/aide.sh').read_text();ps=(ROOT/'start.ps1').read_text(encoding='utf-8-sig')
        self.assertIn('launcher_should_build_source "${AIDE_BUILD_FROM_SOURCE:-auto}"',shell);self.assertIn('launcher_prebuilt_image "$DOCKER_BIN" "$IMG"',shell)
        self.assertIn("$env:AIDE_BUILD_FROM_SOURCE -ne '0'",ps);self.assertIn('if ((Invoke-Compose build aide).Code -ne 0)',ps);self.assertIn("$env:AIDE_RELEASE_FALLBACK -eq '0'",ps)
        self.assertIn('AIDE_BUILD_FROM_SOURCE=1 bash scripts/aide.sh start',(ROOT/'scripts/install.sh').read_text())

class RunningRuntimeReuseTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        root=Path(self.temp.name);self.docker=root/'docker'
        self.docker.write_text("#!"+sys.executable+"\n"+r"""
import sys,os,json
args=sys.argv[1:];state=os.environ.get('FIX_STATE','healthy')
if args[:3]==['compose','ps','--status']:
 print('a'*64 if state!='stopped' else '');sys.exit(0)
if args[:3]==['compose','exec','-T']:
 if state=='unhealthy':sys.exit(22)
 print(json.dumps({'service':'other' if state=='foreign' else 'aide','status':'ok'},separators=(',',':')));sys.exit(0)
if args[0]=='info':print('linux/arm64');sys.exit(0)
if args[:2]==['image','inspect']:
 if state=='missing':sys.exit(1)
 print('sha256:'+'1'*64+'|'+('linux/amd64' if state=='wrong-platform' else 'linux/arm64')+'|'+'2'*64+'|'+('' if state=='incomplete' else '8.0.1.1.0'));sys.exit(0)
sys.exit(90)
""");self.docker.chmod(0o755)
    def probe(self,kind,state):
        return subprocess.run(['bash','-ec','source "$1"; '+kind+' "$2" aide:local','fixture',str(ROOT/'scripts/prebuilt-image.sh'),str(self.docker)],env=dict(os.environ,FIX_STATE=state),capture_output=True,text=True)
    def test_source_selection_respects_checkout_and_explicit_override(self):
        root=self.docker.parent
        def selected(mode):
            return subprocess.run(['bash','-ec','source "$1"; launcher_should_build_source "$2" "$3"','fixture',str(ROOT/'scripts/prebuilt-image.sh'),mode,str(root)],capture_output=True).returncode==0
        self.assertFalse(selected('auto'));self.assertTrue(selected('1'))
        marker=root/'.git';marker.mkdir();self.assertTrue(selected('auto'));self.assertFalse(selected('0'))
        marker.rmdir();marker.write_text('gitdir: owned-worktree-fixture')
        self.assertTrue(selected('auto'));self.assertFalse(selected('0'))
    def test_running_instance_requires_local_service_health(self):
        for state,success in [('healthy',True),('stopped',False),('unhealthy',False),('foreign',False)]:
            with self.subTest(state=state):self.assertEqual(self.probe('launcher_running_ready',state).returncode==0,success)
    def test_installed_runtime_requires_platform_and_complete_metadata(self):
        for state,success in [('healthy',True),('missing',False),('wrong-platform',False),('incomplete',False)]:
            with self.subTest(state=state):self.assertEqual(self.probe('launcher_local_runtime_ready',state).returncode==0,success)

if __name__ == '__main__': unittest.main()
