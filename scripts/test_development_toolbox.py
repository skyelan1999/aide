"""Contracts for optional development tools; no Docker calls or user data."""
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('toolbox', ROOT / 'scripts/development-toolbox.py')
toolbox = importlib.util.module_from_spec(spec)
spec.loader.exec_module(toolbox)


class ToolboxTests(unittest.TestCase):
    def test_explicit_argv_workspace_only_and_network_opt_in(self):
        with tempfile.TemporaryDirectory(prefix='workspace with spaces ') as temporary:
            command = toolbox.run_command('sha256:test', Path(temporary), ['printf', '$(secret); literal'])
            self.assertEqual(command[-3:], ['printf', 'sha256:test', '$(secret); literal'])
            self.assertEqual(command.count('--mount'), 1)
            self.assertIn('type=bind,src=' + str(Path(temporary).resolve()) + ',dst=/workspace', command)
            self.assertEqual(command[command.index('--network') + 1], 'none')
            self.assertIn('--read-only', command)
            self.assertIn('no-new-privileges', command)
            self.assertFalse(any('docker.sock' in x or '/data' in x for x in command))
            connected = toolbox.run_command('sha256:test', Path(temporary), ['npm', 'install'], True)
            self.assertEqual(connected[connected.index('--network') + 1], 'bridge')
            with self.assertRaises(ValueError):
                toolbox.run_command('sha256:test', Path(temporary), [])

    def test_export_preserves_existing_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / 'sdk.tar.gz'
            output.write_bytes(b'existing')
            with patch.object(toolbox.subprocess, 'Popen') as process:
                with self.assertRaises(ValueError):
                    toolbox.export_image({'Id': 'sha256:test'}, output)
                process.assert_not_called()
            self.assertEqual(output.read_bytes(), b'existing')

    def test_default_release_has_fresh_base_and_toolbox_is_separate(self):
        source = (ROOT / 'Dockerfile').read_text()
        self.assertIn('FROM ${PYTHON_BASE} AS runtime', source)
        runtime = source.split(' AS runtime', 1)[1]
        self.assertNotIn('COPY --from=go', runtime)
        self.assertNotIn('/usr/local/lib/node_modules', runtime)
        sdk = (ROOT / 'docker/Dockerfile.sdk').read_text()
        self.assertIn('FROM ${AIDE_RUNTIME_IMAGE}', sdk)
        self.assertIn('HEALTHCHECK NONE', sdk)
        self.assertIn('ENTRYPOINT []', sdk)


if __name__ == '__main__':
    unittest.main()
