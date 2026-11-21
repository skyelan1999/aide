"""Launcher regressions use synthetic modules, not engineering builders."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SOURCE = Path(__file__).with_name('cad_sdk_python.py')
spec = importlib.util.spec_from_file_location('aide_cad_sdk_launcher', SOURCE)
launcher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(launcher)


class CADSDKLauncherTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='aide sdk fixture ')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.sdk = self.root / 'SDK tools'
        self.sdk.mkdir()
        self.entry = self.sdk / 'cad_python.py'
        self.entry.write_bytes(SOURCE.read_bytes())
        (self.sdk / 'step-engine' / 'OCP').mkdir(parents=True)

    def run_cli(self, *args, env=None):
        return subprocess.run([sys.executable, str(self.entry), *args],
                              cwd=self.root, env=env, text=True,
                              capture_output=True, timeout=15)

    def test_sdk_sibling_import_from_other_script_directory(self):
        (self.sdk / 'sdk_probe.py').write_text('VALUE="sdk module"\n')
        other = self.root / 'elsewhere'
        other.mkdir()
        script = other / 'probe.py'
        script.write_text('import sdk_probe\nprint(sdk_probe.VALUE)\n')
        result = self.run_cli(str(script))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), 'sdk module')

    def test_project_and_external_modules_remain_available(self):
        project = self.root / '03_\u4e09\u7ef4\u5efa\u6a21'
        project.mkdir()
        (project / 'project_probe.py').write_text('VALUE=7\n')
        external = self.root / 'external'
        external.mkdir()
        (external / 'external_probe.py').write_text('VALUE=9\n')
        env = dict(os.environ, PYTHONPATH=str(external), LD_LIBRARY_PATH='/existing/ld')
        code = ('import project_probe,external_probe,os,json;'
                'print(json.dumps([project_probe.VALUE,external_probe.VALUE,os.environ["LD_LIBRARY_PATH"]]))')
        result = self.run_cli('-c', code, env=env)
        self.assertEqual(result.returncode, 0, result.stderr)
        values = json.loads(result.stdout)
        self.assertEqual(values[:2], [7, 9])
        self.assertEqual(values[2], str(self.sdk / 'step-engine/system') + os.pathsep + '/existing/ld')

    def test_sdk_precedes_conflicting_external_module(self):
        (self.sdk / 'sdk_probe.py').write_text('VALUE="pinned SDK"\n')
        foreign = self.root / 'foreign'
        foreign.mkdir()
        (foreign / 'sdk_probe.py').write_text('raise RuntimeError("wrong helper")\n')
        result = self.run_cli('-c', 'import sdk_probe; print(sdk_probe.VALUE)',
                              env=dict(os.environ, PYTHONPATH=str(foreign)))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), 'pinned SDK')

    def test_flags_and_arguments_are_forwarded_exactly(self):
        result = self.run_cli('-B', '-c',
                              'import sys,json; print(json.dumps([sys.dont_write_bytecode,sys.argv]))',
                              'space argument', '\u4e2d\u6587')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout), [True, ['-c', 'space argument', '\u4e2d\u6587']])

    def test_missing_engine_prevents_target_execution(self):
        (self.sdk / 'step-engine/OCP').rmdir()
        sentinel = self.root / 'must-not-exist'
        result = self.run_cli('-c', 'from pathlib import Path; Path(' + repr(str(sentinel)) + ').touch()')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Project CAD engine is missing', result.stderr)
        self.assertFalse(sentinel.exists())

    def test_no_argument_shows_usage(self):
        result = self.run_cli()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Usage:', result.stderr)

    def test_environment_input_is_not_mutated(self):
        original = {'PYTHONPATH': '/old/python', 'LD_LIBRARY_PATH': '/old/ld', 'EXAMPLE': 'value'}
        before = dict(original)
        env = launcher.cad_environment(self.sdk, original)
        self.assertEqual(original, before)
        self.assertEqual(env['EXAMPLE'], 'value')
        self.assertEqual(env['PYTHONPATH'].split(os.pathsep)[0], str(self.sdk))


if __name__ == '__main__':
    unittest.main()
