"""Regression tests for the route's deletion and release boundaries."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from concurrent.futures import ThreadPoolExecutor
from unittest import mock

spec = importlib.util.spec_from_file_location('route', Path(__file__).with_name('agent-route.py'))
route = importlib.util.module_from_spec(spec)
spec.loader.exec_module(route)
CONFIG = json.loads((route.ROOT / 'docs/agent/router.json').read_text())


class RouteTests(unittest.TestCase):
    def test_snapshot_owner_outlives_its_execution_and_cleanup_deadlines(self):
        runner = route.runpy.run_path(str(route.ROOT / 'scripts/test_go_snapshot.py'))
        deadline = route.check_timeout(['python3', 'scripts/test_go_snapshot.py'])
        self.assertGreater(deadline, runner['HOST_TIMEOUT_SECONDS'] + 10)
        self.assertLessEqual(deadline, runner['HOST_TIMEOUT_SECONDS'] + 60)
        # Changing the child budget must move the owner budget too.
        with mock.patch.object(route.runpy, 'run_path', return_value={'HOST_TIMEOUT_SECONDS': 1234}):
            self.assertEqual(route.check_timeout(['python3', 'scripts/test_go_snapshot.py']), 1264)
        self.assertEqual(route.check_timeout(['node', 'scripts/example.test.cjs']), 600)

    def test_public_runtime_asset_gate_fails_closed(self):
        for code in (0, 1):
            with mock.patch.object(route.subprocess, 'run', return_value=mock.Mock(returncode=code)) as run:
                if code:
                    with self.assertRaises(ValueError): route.check_public_runtime_assets()
                else:
                    route.check_public_runtime_assets()
                self.assertIn('verify_prebuilt_assets.py', run.call_args.args[0][1])
                self.assertEqual(run.call_args.kwargs['timeout'], 45)
        with mock.patch.object(route.subprocess, 'run', side_effect=subprocess.TimeoutExpired('fixture', 45)):
            with self.assertRaisesRegex(ValueError, "timed out"): route.check_public_runtime_assets()

    def test_explicit_interpreter_overrides(self):
        with tempfile.TemporaryDirectory() as tmp:
            executable = Path(tmp) / 'bash.exe'
            executable.write_bytes(b'fixture')
            with mock.patch.dict('os.environ', {'AIDE_BASH': str(executable)}):
                self.assertEqual(route.resolve_check_command(['bash', '-n', 'script.sh']), [str(executable), '-n', 'script.sh'])
            with mock.patch.dict('os.environ', {'AIDE_BASH': str(executable) + '.missing'}):
                with self.assertRaises(ValueError):
                    route.resolve_check_command(['bash', '-n', 'script.sh'])
        self.assertEqual(route.resolve_check_command(['git', 'status']), ['git', 'status'])

    def test_task_path_rejects_escape(self):
        for value in ('../outside', '/tmp/a', 'a/b', 'A B', ''):
            with self.assertRaises(ValueError): route.task_path(value)

    def test_release_gate(self):
        task = {f: 'recorded' for f in ('request', 'scope', 'acceptance', 'release_authorization', 'rollback')}
        task['stages'] = {s: {'status': 'pass', 'summary': 'checked', 'evidence': ['record']} for s in CONFIG['stages'][:-1]}
        receipt = {'profile': 'full', 'status': 'pass', 'fingerprint': 'abc'}
        self.assertEqual(route.release_errors(task, receipt, CONFIG, 'abc'), [])
        self.assertTrue(route.release_errors(task, receipt, CONFIG, 'changed'))
        task['stages']['verification']['status'] = 'not_run'
        self.assertTrue(route.release_errors(task, receipt, CONFIG, 'abc'))
        receipt['profile'] = 'quick'
        self.assertTrue(route.release_errors(task, receipt, CONFIG, 'abc'))

    def test_release_gate_requires_same_head_as_full_verification(self):
        task = {f: 'recorded' for f in ('request', 'scope', 'acceptance', 'release_authorization', 'rollback')}
        task['stages'] = {s: {'status': 'pass', 'summary': 'checked', 'evidence': ['record']} for s in CONFIG['stages'][:-1]}
        receipt = {'profile': 'full', 'status': 'pass', 'fingerprint': 'abc', 'head': 'verified-head'}
        self.assertEqual(route.release_errors(task, receipt, CONFIG, 'abc', 'verified-head'), [])
        self.assertIn('The full verification receipt must match the current Git revision',
                      route.release_errors(task, receipt, CONFIG, 'abc', 'changed-head'))

    def test_full_verification_requires_at_least_one_check(self):
        self.assertFalse(route.verification_passed([], 'same', 'same'))
        self.assertTrue(route.verification_passed([{'exit': 0}], 'same', 'same'))
        self.assertFalse(route.verification_passed([{'exit': 0}], 'old', 'new'))

    def test_task_creation_is_atomic_and_does_not_follow_predictable_temp_symlink(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            target = root / 'outside.json'
            target.write_text('keep')
            tasks = root / 'docs/tasks'
            tasks.mkdir(parents=True)
            destination = tasks / 'demo.json'
            (tasks / 'demo.tmp').symlink_to(target)
            route.create_json(destination, {'winner': 'first'})
            self.assertEqual(json.loads(destination.read_text()), {'winner': 'first'})
            self.assertEqual(target.read_text(), 'keep')
            self.assertTrue((tasks / 'demo.tmp').is_symlink())
            with self.assertRaises(FileExistsError):
                route.create_json(destination, {'winner': 'second'})
            self.assertEqual(json.loads(destination.read_text()), {'winner': 'first'})

    def test_concurrent_task_creators_have_one_winner(self):
        with tempfile.TemporaryDirectory() as tmp:
            destination = Path(tmp) / 'docs/tasks/race.json'
            def create(value):
                try:
                    route.create_json(destination, {'winner': value})
                    return 'created'
                except FileExistsError:
                    return 'exists'
            with ThreadPoolExecutor(max_workers=2) as pool:
                results = list(pool.map(create, ('first', 'second')))
            self.assertCountEqual(results, ['created', 'exists'])
            self.assertIn(json.loads(destination.read_text())['winner'], ('first', 'second'))

    def test_clean_deletion_refuses_parent_symlink_swap_after_enumeration(self):
        if os.open not in os.supports_dir_fd or os.unlink not in os.supports_dir_fd or not hasattr(os, 'O_NOFOLLOW'):
            self.skipTest('platform lacks directory-handle no-follow unlink')
        original = route.ROOT
        outside = None
        try:
            with tempfile.TemporaryDirectory() as tmp:
                route.ROOT = Path(tmp)
                subprocess.run(['git', 'init', '-q', tmp], check=True)
                subtree = route.ROOT / 'sub'
                subtree.mkdir()
                candidate = subtree / '.DS_Store'
                candidate.write_text('inside')
                outside = route.ROOT.parent / (route.ROOT.name + '-outside')
                outside.mkdir()
                external = outside / '.DS_Store'
                external.write_text('outside')
                found = route.candidates(CONFIG)
                selected = next(p for p in found if p.relative_to(route.ROOT).as_posix() == 'sub/.DS_Store')
                subtree.rename(route.ROOT / 'sub-original')
                subtree.symlink_to(outside, target_is_directory=True)
                with self.assertRaises(OSError):
                    route.remove_candidate(route.ROOT, selected)
                self.assertEqual(external.read_text(), 'outside')
                self.assertEqual((route.ROOT / 'sub-original/.DS_Store').read_text(), 'inside')
        finally:
            route.ROOT = original
            if outside and outside.exists():
                for child in outside.iterdir():
                    child.unlink()
                outside.rmdir()

    def test_cleanup_preserves_unknown_protected_tracked_and_symlinks(self):
        original = route.ROOT
        with tempfile.TemporaryDirectory() as tmp:
            try:
                route.ROOT = Path(tmp)
                subprocess.run(['git', 'init', '-q', tmp], check=True)
                for folder in ('src', '.data', 'tracked', 'link'):
                    (route.ROOT / folder).mkdir()
                for name in ('.DS_Store', 'src/.DS_Store', '.data/.DS_Store', 'tracked/.DS_Store', 'unknown.txt'):
                    (route.ROOT / name).write_text('keep unless metadata')
                (route.ROOT / 'link/.DS_Store').symlink_to(route.ROOT / 'unknown.txt')
                subprocess.run(['git', '-C', tmp, 'add', 'tracked/.DS_Store'], check=True)
                self.assertEqual({str(p.relative_to(route.ROOT)) for p in route.candidates(CONFIG)}, {'.DS_Store', 'src/.DS_Store'})
            finally:
                route.ROOT = original


if __name__ == '__main__': unittest.main()
