"""Regression tests for the route's deletion and release boundaries."""
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('route', Path(__file__).with_name('agent-route.py'))
route = importlib.util.module_from_spec(spec)
spec.loader.exec_module(route)
CONFIG = json.loads((route.ROOT / 'docs/agent/router.json').read_text())


class RouteTests(unittest.TestCase):
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
