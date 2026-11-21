"""Static-checker regressions; all fixtures are text, not engineering models."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('python_static_check.py')
spec = importlib.util.spec_from_file_location('aide_python_static_check', SCRIPT)
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)


class PythonStaticCheckTests(unittest.TestCase):
    def check_names(self, code):
        return {row['name'] for row in checker.inspect_code(code)['diagnostics']}

    def test_detects_module_and_deferred_globals(self):
        self.assertEqual(self.check_names('HASHES={GRAPH:"hash"}\ndef f():\n return TopAbs_EDGE\n'),
                         {'GRAPH', 'TopAbs_EDGE'})

    def test_comprehension_variable_does_not_leak(self):
        self.assertEqual(self.check_names('xs=[i for i in range(3)]\nprint([[i+j for j in range(2)]])'), {'i'})

    def test_comprehension_scopes_in_functions_and_nested_containers(self):
        self.assertEqual(self.check_names('def f():\n xs={i:i for i in range(3)}\n return i\n'), {'i'})
        self.assertEqual(self.check_names('xs={i for i in range(3)}\nprint(i)'), {'i'})
        self.assertEqual(self.check_names('xs=[[i+j for j in range(i)] for i in range(3)]'), set())

    def test_comprehension_outer_iterable_and_walrus_binding(self):
        self.assertEqual(self.check_names('xs=[i for i in missing]\n'), {'missing'})
        self.assertEqual(self.check_names('xs=[(last:=i) for i in range(3)]\nprint(last)'), set())
        self.assertEqual(self.check_names('i=9\nxs=[i for i in range(3)]\nprint(i)'), set())

    def test_comprehension_diagnostics_keep_original_occurrence_lines(self):
        result = checker.inspect_code('# header\nxs=[i for i in range(3)]\n\nprint(i)')
        diagnostic = next(row for row in result['diagnostics'] if row['name'] == 'i')
        self.assertEqual(diagnostic['occurrenceLines'], [2, 4])
        self.assertIsNone(diagnostic['scopeLine'])

    def test_valid_closures_imports_builtins_and_module_names(self):
        code = ('from pathlib import Path\nROOT=Path(__file__).parent\n'
                'def outer(x):\n def inner(y):\n  return x+y\n return inner\n'
                'print([outer(i)(1) for i in range(3)])\n')
        self.assertEqual(checker.inspect_code(code)['status'], 'pass_static_only')

    def test_syntax_error_reports_location_without_source(self):
        result = checker.inspect_code('x=[1,2}\n')
        self.assertEqual(result['status'], 'syntax_error')
        self.assertEqual(result['diagnostics'][0]['line'], 1)
        self.assertNotIn('source', result)

    def test_dynamic_and_wildcard_names_are_not_proof(self):
        result = checker.inspect_code('from somewhere import *\nprint(possibly_supplied)')
        self.assertEqual(result['status'], 'warnings')
        self.assertTrue(result['wildcardImportsPresent'])
        self.assertFalse(result['executedInput'])

    def test_input_limit_does_not_claim_full_hash(self):
        result = checker.inspect_code(b'#' * (checker.MAX_BYTES + 1))
        self.assertEqual(result['status'], 'not_run')
        self.assertNotIn('codeSHA256', result)

    def test_invalid_utf8_is_not_checked(self):
        self.assertEqual(checker.inspect_code(b'\xff')['status'], 'not_run')

    def test_cli_never_executes_input(self):
        with tempfile.TemporaryDirectory() as directory:
            sentinel = Path(directory) / 'must-not-exist'
            code = 'from pathlib import Path\nPath(' + repr(str(sentinel)) + ').write_text("executed")\n'
            result = subprocess.run([sys.executable, '-I', '-S', str(SCRIPT), '--stdin'],
                                    input=code, text=True, capture_output=True, check=False)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertFalse(sentinel.exists())
            self.assertFalse(json.loads(result.stdout)['executedInput'])

    def test_isolated_cli_ignores_project_and_pythonpath_hooks(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            sentinel = root / 'hook-executed'
            hook = 'open(' + repr(str(sentinel)) + ',"w").write("bad")\nraise RuntimeError("shadowed")\n'
            for name in ('ast.py', 'symtable.py', 'sitecustomize.py'):
                (root / name).write_text(hook)
            env = dict(os.environ, PYTHONPATH=str(root))
            result = subprocess.run([sys.executable, '-I', '-S', str(SCRIPT), '--stdin'],
                                    cwd=root, env=env, input='print(1)', text=True,
                                    capture_output=True, check=False)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertFalse(sentinel.exists())

    def test_cli_refuses_nonisolated_invocation(self):
        result = subprocess.run([sys.executable, str(SCRIPT), '--stdin'], input='print(1)',
                                text=True, capture_output=True, check=False)
        self.assertEqual(result.returncode, 4)
        self.assertEqual(json.loads(result.stdout)['status'], 'not_run')

    def test_refusal_happens_before_shadowable_imports(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            copy = root / SCRIPT.name
            copy.write_bytes(SCRIPT.read_bytes())
            sentinel = root / 'hook-executed'
            hook = 'open(' + repr(str(sentinel)) + ',"w").write("bad")\nraise RuntimeError("shadowed")\n'
            for name in ('ast.py', 'symtable.py', 'argparse.py'):
                (root / name).write_text(hook)
            result = subprocess.run([sys.executable, str(copy), '--stdin'], input='print(1)',
                                    cwd=root, text=True, capture_output=True, check=False)
            self.assertEqual(result.returncode, 4, result.stderr)
            self.assertFalse(sentinel.exists())

    def test_cli_file_and_stdin_hash_same_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'input.py'
            raw = b'print(1)\n'
            path.write_bytes(raw)
            result = subprocess.run([sys.executable, '-I', '-S', str(SCRIPT), '--file', str(path)],
                                    text=True, capture_output=True, check=False)
            self.assertEqual(result.returncode, 0)
            self.assertEqual(json.loads(result.stdout)['codeSHA256'], checker.inspect_code(raw)['codeSHA256'])


if __name__ == '__main__':
    unittest.main()
