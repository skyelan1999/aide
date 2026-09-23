import importlib.util
import tempfile
import unittest
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
