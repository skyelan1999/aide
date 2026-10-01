"""Static regression checks for aide's documented source layout and moved fixtures."""
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[1]


class CodeLayoutTests(unittest.TestCase):
    def test_navigation_documents_and_local_links_exist(self):
        documents = [
            ROOT / 'docs/architecture/code-layout.md',
            ROOT / 'internal/server/README.md',
            ROOT / 'scripts/README.md',
        ]
        for document in documents:
            self.assertTrue(document.is_file(), document)
            for target in re.findall(r'\[[^]]+\]\(([^)]+)\)', document.read_text()):
                if target.startswith(('https://', 'http://', '#', 'mailto:')):
                    continue
                local = target.split('#', 1)[0]
                self.assertTrue((document.parent / local).resolve().is_file(), f'{document}: {target}')
        self.assertIn('architecture/code-layout.md', (ROOT / 'docs/architecture.md').read_text())

    def test_package_and_embedded_ui_boundaries_match_the_map(self):
        self.assertRegex((ROOT / 'internal/server/server.go').read_text(), r'(?m)^package server\b')
        self.assertIn('//go:embed web/*', (ROOT / 'internal/server/server.go').read_text())
        self.assertTrue((ROOT / 'internal/server/web/index.html').is_file())
        self.assertTrue((ROOT / 'internal/server/web/locales').is_dir())
        self.assertTrue((ROOT / 'internal/server/web/themes').is_dir())
        tts_sources = list((ROOT / 'internal/server/tts').glob('*.go'))
        self.assertTrue(tts_sources)
        self.assertTrue(any(re.search(r'(?m)^package tts\b', p.read_text()) for p in tts_sources))
        self.assertTrue(any(p.is_dir() for p in (ROOT / 'plugins').iterdir()))

    def test_stable_host_entrypoints_remain_at_their_documented_paths(self):
        for path in ('start.command', 'scripts/aide.sh', 'scripts/agent-route.py', 'scripts/version.sh'):
            self.assertTrue((ROOT / path).is_file(), path)
        scripts_map = (ROOT / 'scripts/README.md').read_text()
        self.assertIn('`fixtures/`', scripts_map)
        self.assertIn('`mocks/`', scripts_map)

    def test_mock_services_are_in_fixtures_and_docker_copy_resolves(self):
        mocks = ('mock_provider.py', 'mock_clone_server.py')
        destination = ROOT / 'scripts/fixtures/mocks'
        for name in mocks:
            self.assertTrue((destination / name).is_file(), name)
            compile((destination / name).read_text(), str(destination / name), 'exec')
            self.assertFalse((ROOT / 'scripts' / name).exists(), f'old root path remains: {name}')
        dockerfile = (ROOT / 'docker/voice-clone/mock/Dockerfile').read_text()
        self.assertIn('COPY scripts/fixtures/mocks/mock_clone_server.py /app/mock_clone_server.py', dockerfile)
        self.assertTrue((ROOT / 'scripts/fixtures/mocks/mock_clone_server.py').is_file())

    def test_active_docs_and_source_comments_use_new_mock_paths(self):
        files = (
            'docs/verification.md',
            'docs/security/voice-cloning.md',
            'docs/en/security/voice-cloning.md',
            'internal/server/personality_evolution_test.go',
            'scripts/fixtures/mocks/mock_clone_server.py',
            'scripts/fixtures/mocks/mock_provider.py',
        )
        for rel in files:
            content = (ROOT / rel).read_text()
            self.assertNotIn('scripts/mock_provider.py', content, rel)
            self.assertNotIn('scripts/mock_clone_server.py', content, rel)
        self.assertIn('scripts/fixtures/mocks/mock_provider.py', (ROOT / 'docs/verification.md').read_text())
        self.assertIn('scripts/fixtures/mocks/mock_clone_server.py', (ROOT / 'docs/security/voice-cloning.md').read_text())


if __name__ == '__main__':
    unittest.main(verbosity=2)
