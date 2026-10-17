#!/usr/bin/env python3
import contextlib
import importlib.util
import io
from pathlib import Path
import sys
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('shards', Path(__file__).with_name('run_go_test_shards.py'))
shards = importlib.util.module_from_spec(spec)
spec.loader.exec_module(shards)


class ShardTests(unittest.TestCase):
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
                                      ['TestPass', 'TestFail', 'Example', 'FuzzSeeds'], root)
            self.assertEqual(len(list(root.glob('result-*'))), 3,
                             'one failed worker must not prevent the other shards from running')


if __name__ == '__main__':
    unittest.main()
