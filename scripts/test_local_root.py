import importlib.util
import tempfile
import unittest
from unittest.mock import patch
from pathlib import Path

spec = importlib.util.spec_from_file_location('configure_root', Path(__file__).with_name('configure-local-root.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

class LocalRootTests(unittest.TestCase):
    def test_preserves_settings_and_quotes_spaces(self):
        with tempfile.TemporaryDirectory() as temp:
            project = Path(temp)
            directory = project / 'aide space $literal'
            directory.mkdir()
            (project / '.env').write_text('AI_MODEL=example\nAIDE_LOCAL_ROOT=/old\n')
            module.configure(project, directory)
            text = (project / '.env').read_text()
            self.assertIn('AI_MODEL=example\n', text)
            self.assertIn("AIDE_LOCAL_ROOT='" + str(directory.resolve()) + "'", text)
            self.assertNotIn('/old', text)
    def test_macos_root_and_return_to_scoped_directory(self):
        with tempfile.TemporaryDirectory() as temp, patch.object(module.sys, 'platform', 'darwin'):
            project = Path(temp)
            (project / '.env').write_text('COMPOSE_FILE=compose.yaml\nAI_MODEL=keep\n')
            module.configure(project, '/')
            self.assertIn('COMPOSE_FILE=compose.yaml:compose.macos-root.yaml', (project / '.env').read_text())
            self.assertTrue((project / '.agent-state/host-root').is_dir())
            module.configure(project, project)
            self.assertIn('COMPOSE_FILE=compose.yaml\n', (project / '.env').read_text())
            self.assertNotIn('compose.macos-root.yaml', (project / '.env').read_text())
            self.assertIn('AI_MODEL=keep', (project / '.env').read_text())
    def test_invalid_root_keeps_configuration(self):
        with tempfile.TemporaryDirectory() as temp:
            project = Path(temp)
            env = project / '.env'
            env.write_text('AIDE_LOCAL_ROOT=/original\n')
            with self.assertRaises(FileNotFoundError):
                module.configure(project, project / 'missing')
            self.assertEqual(env.read_text(), 'AIDE_LOCAL_ROOT=/original\n')

if __name__ == '__main__':
    unittest.main()
