"""Guard and error-path regressions; real CAD controls run separately."""
import contextlib
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SOURCE = Path(__file__).with_name('cad_step_curve_check.py')
spec = importlib.util.spec_from_file_location('step_curve_check_under_test', SOURCE)
sdk = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sdk)


class CurveCheckTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='step curve guard ')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.source = self.root / 'fixture.step'
        self.source.write_bytes(b'guard fixture: never sent to CAD parser')
        self.sha = hashlib.sha256(self.source.read_bytes()).hexdigest()

    def test_valid_numeric_result_and_failed_kernel_are_distinct(self):
        sdk.check_numeric_result(True, 0, 2e-7, .4, 0., 1., 1e-7)
        for done, error in [(False, 0), (True, 3), (False, 1)]:
            with self.subTest(done=done, error=error), self.assertRaises(ValueError):
                sdk.check_numeric_result(done, error, 0., .4, 0., 1., 0.)

    def test_nonfinite_negative_and_outside_range_are_errors(self):
        for distance, parameter in [(float('nan'), .4), (float('inf'), .4),
                                    (-1., .4), (1e-7, 2.), (1e-7, float('nan'))]:
            with self.subTest(distance=distance, parameter=parameter), self.assertRaises(ValueError):
                sdk.check_numeric_result(True, 0, distance, parameter, 0., 1., 0.)

    def test_kernel_maximum_must_not_underestimate_observed_sample(self):
        with self.assertRaisesRegex(ValueError, 'underestimates'):
            sdk.check_numeric_result(True, 0, 1e-8, .4, 0., 1., 4e-7)

    def test_sha_guard_prevents_sdk_import_and_source_mutation(self):
        before = self.source.read_bytes()
        with patch('builtins.__import__', side_effect=AssertionError('no imports expected')):
            with self.assertRaisesRegex(ValueError, 'SHA256 mismatch'):
                sdk.inspect_step(self.source, '0' * 64)
        self.assertEqual(self.source.read_bytes(), before)

    def test_symlink_non_step_and_oversized_inputs_are_refused(self):
        link = self.root / 'linked.step'
        link.symlink_to(self.source)
        wrong = self.root / 'wrong.py'
        wrong.write_bytes(self.source.read_bytes())
        huge = self.root / 'huge.step'
        with huge.open('wb') as stream:
            stream.truncate(sdk.MAX_BYTES + 1)
        for path in [link, wrong, huge]:
            with self.subTest(path=path.name), self.assertRaises(ValueError):
                sdk.checked_source(path, self.sha)

    def test_invalid_pair_budgets_are_rejected_before_loading_cad(self):
        for budget in [0, True, -1, 8193, 1.5]:
            with self.subTest(budget=budget), self.assertRaisesRegex(ValueError, 'max-pairs'):
                sdk.inspect_step(self.source, self.sha, budget)

    def test_existing_report_is_preserved_without_native_inspection(self):
        output = self.root / 'report.json'
        output.write_text('existing evidence')
        stdout = io.StringIO()
        with patch.object(sdk, 'inspect_step') as inspect, contextlib.redirect_stdout(stdout):
            code = sdk.main(['--file', str(self.source), '--sha256', self.sha,
                             '--output', str(output)])
        inspect.assert_not_called()
        self.assertEqual(code, 4)
        self.assertEqual(json.loads(stdout.getvalue())['status'], 'NOT_RUN')
        self.assertEqual(output.read_text(), 'existing evidence')


if __name__ == '__main__':
    unittest.main()
