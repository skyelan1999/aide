#!/usr/bin/env python3
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('shards', Path(__file__).with_name('run_go_test_shards.py'))
shards = importlib.util.module_from_spec(spec)
spec.loader.exec_module(shards)


class ShardTests(unittest.TestCase):
    def test_launcher_delegates_without_runtime_mounts_or_legacy_timeout(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / 'scripts').mkdir()
            shutil.copyfile(Path(__file__).with_name('aide.sh'), root / 'scripts/aide.sh')
            (root / 'scripts/test_go_snapshot.py').write_text(
                'import os,sys\nfrom pathlib import Path\n'
                'assert os.environ["AIDE_TEST_TOOLCHAIN_IMAGE"]=="sdk:fixture"\n'
                'Path("delegated").write_text("snapshot")\n'
                'sys.exit(int(os.environ["FIXTURE_EXIT"]))\n')
            # No Docker/Go binary or production files are needed to delegate.
            for legacy in ('0', '1'):
                for code in (0, 7):
                    env = dict(os.environ, AIDE_VERIFY_IN_CONTAINER=legacy,
                               AIDE_TEST_TOOLCHAIN_IMAGE='sdk:fixture', FIXTURE_EXIT=str(code))
                    result = subprocess.run(['bash', 'scripts/aide.sh', 'test'],
                                            cwd=root, env=env, capture_output=True, text=True, timeout=10)
                    self.assertEqual(result.returncode, code, result.stderr)
                    self.assertEqual((root / 'delegated').read_text(), 'snapshot')
                    self.assertFalse((root / '.env').exists())

    def test_receipts_keep_skips_separate_and_missing_execution_fails(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            binary = root / 'binary.py'
            binary.write_text('import sys\n'
                              'print("--- PASS: TestDone (0.01s)")\n'
                              'print("--- SKIP: TestNeedsDevice (0.00s)")\n')
            with contextlib.redirect_stdout(io.StringIO()):
                shards.run_shards([sys.executable, str(binary)], ['TestDone', 'TestNeedsDevice'],
                                  root, count=1, report_dir=root / 'evidence')
            report = json.loads((root / 'evidence/report.json').read_text())
            self.assertEqual(report['status'], 'pass')
            self.assertEqual(report['passed'], ['TestDone'])
            self.assertEqual(report['skipped'], ['TestNeedsDevice'])
            self.assertTrue((root / 'evidence/shard-01.log').exists())
            with contextlib.redirect_stdout(io.StringIO()), self.assertRaisesRegex(RuntimeError, 'shards failed'):
                shards.run_shards([sys.executable, str(binary)], ['TestDone', 'TestMissing'],
                                  root, count=1, report_dir=root / 'incomplete')
            report = json.loads((root / 'incomplete/report.json').read_text())
            self.assertEqual(report['status'], 'fail')
            self.assertEqual(report['shards'][0]['missing'], ['TestMissing'])

    def test_inventory_preserves_examples_and_fuzz_seeds(self):
        names = shards.test_names('TestOne\nTest中文\nFuzzParser\nExample\nExampleWriter\nBenchmarkOne\nPASS\n')
        self.assertEqual(names, ['TestOne', 'Test中文', 'FuzzParser', 'Example', 'ExampleWriter'])
        for count in (1, 2, 3, 20):
            groups = shards.partition_tests(names, count)
            flat = [name for group in groups for name in group]
            self.assertCountEqual(flat, names)
            self.assertEqual(len(flat), len(set(flat)))
            self.assertTrue(all(groups))
        for invalid in ('', 'TestOne\nTestOne'):
            with self.assertRaises(ValueError): shards.test_names(invalid)

    def test_default_workers_do_not_overlap(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            fake = root / 'serial.py'
            fake.write_text('import time\nfrom pathlib import Path\n'
                            'lock=Path("active")\nlock.mkdir()\n'
                            'time.sleep(0.03)\nlock.rmdir()\n')
            with contextlib.redirect_stdout(io.StringIO()):
                shards.run_shards([sys.executable, str(fake)],
                                  ['TestA', 'TestB', 'TestC'], root)
            self.assertFalse((root / 'active').exists())

    def test_all_shards_finish_and_failure_is_not_hidden(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            fake = root / 'test_binary.py'
            fake.write_text('import os,sys\nfrom pathlib import Path\n'
                            'pattern=next(a for a in sys.argv if a.startswith("-test.run="))\n'
                            'assert "-test.paniconexit0" in sys.argv\n'
                            'assert Path(os.environ["PWD"]).resolve()==Path.cwd()\n'
                            'Path("result-"+str(os.getpid())).write_text(pattern)\n'
                            'sys.exit(1 if "TestFail" in pattern else 0)\n')
            with contextlib.redirect_stdout(io.StringIO()):
                with self.assertRaisesRegex(RuntimeError, 'shards failed'):
                    shards.run_shards([sys.executable, str(fake)],
                                      ['TestPass', 'TestFail', 'Example', 'FuzzSeeds'], root, count=3, workers=2)
            self.assertEqual(len(list(root.glob('result-*'))), 3,
                             'one failed worker must not prevent the other shards from running')


if __name__ == '__main__':
    unittest.main()
