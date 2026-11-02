#!/usr/bin/env python3
"""Image selection stays explicit and asset checks never pull implicitly."""
import contextlib
import io
import os
from pathlib import Path
import runpy
import sys
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]


class RuntimeAssetImageTests(unittest.TestCase):
    def invocation(self, script, environment, arguments=()):
        with mock.patch.dict(os.environ):
            os.environ.pop('AIDE_TEST_IMAGE', None)
            if environment is not None:
                os.environ['AIDE_TEST_IMAGE'] = environment
            with mock.patch.object(sys, 'argv', [script, *arguments]), \
                    mock.patch('subprocess.run') as execute, \
                    contextlib.redirect_stdout(io.StringIO()):
                runpy.run_path(str(ROOT / 'scripts' / script), run_name='__main__')
        commands = [call.args[0] for call in execute.call_args_list]
        runs = [command for command in commands if command[:2] == ['docker', 'run']]
        self.assertEqual(len(runs), 1)
        command = runs[0]
        self.assertIn('--pull=never', command)
        self.assertTrue('--network=none' in command or
                        command[command.index('--network') + 1] == 'none')
        return command[command.index('--entrypoint') + 2]

    def test_default_and_configured_images_are_consistent(self):
        for script in ('test_runtime_assets.py', 'test_optional_step.py'):
            for environment, expected in ((None, 'aide:local'), ('', 'aide:local'),
                                          ('aide:test-fixture', 'aide:test-fixture')):
                with self.subTest(script=script, environment=environment):
                    self.assertEqual(self.invocation(script, environment), expected)

    def test_explicit_image_overrides_environment(self):
        for script in ('test_runtime_assets.py', 'test_optional_step.py'):
            with self.subTest(script=script):
                self.assertEqual(self.invocation(script, 'aide:environment', ('aide:argument',)),
                                 'aide:argument')


if __name__ == '__main__':
    unittest.main()
