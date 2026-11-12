import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('verify_sherpa', ROOT / 'scripts/verify-sherpa-archive.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class SherpaBuildIntegrity(unittest.TestCase):
    def test_good_modified_unknown_and_missing_pin(self):
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / 'pkg.tar.bz2'
            archive.write_bytes(b'synthetic trusted archive')
            manifest = {'archives': {'fixture': hashlib.sha256(archive.read_bytes()).hexdigest()}}
            module.verify(archive, 'fixture', manifest)
            archive.write_bytes(archive.read_bytes() + b'changed')
            with self.assertRaisesRegex(ValueError, 'mismatch'):
                module.verify(archive, 'fixture', manifest)
            for name, pins in [('unknown', manifest), ('fixture', {'archives': {'fixture': ''}})]:
                with self.assertRaisesRegex(ValueError, 'no reviewed'):
                    module.verify(archive, name, pins)

    def test_both_platforms_pinned_and_verified_before_extract(self):
        pins = json.loads((ROOT / 'docker/sherpa/archive-digests.json').read_text())['archives']
        self.assertEqual(len(pins), 2)
        for value in pins.values():
            self.assertRegex(value, r'^[0-9a-f]{64}$')
        source = (ROOT / 'Dockerfile').read_text()
        self.assertLess(source.index('python3 /usr/local/lib/aide/verify-sherpa-archive.py'), source.index('if tar xjf pkg.tar.bz2'))


if __name__ == '__main__':
    unittest.main()
