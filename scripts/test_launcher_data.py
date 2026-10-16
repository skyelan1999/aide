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
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('launcher_data', ROOT / 'scripts/launcher-data.py')
storage = importlib.util.module_from_spec(spec)
spec.loader.exec_module(storage)


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


if __name__ == '__main__': unittest.main()
