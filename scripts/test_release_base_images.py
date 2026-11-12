import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('release_bases', ROOT / 'scripts/ci/release_base_images.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class ReleaseBases(unittest.TestCase):
    def test_real_manifest_and_sdk_scope(self):
        args = module.build_args(ROOT)
        self.assertEqual(len(args), 6)
        self.assertTrue(all('@sha256:' in value for value in args[1::2]))
        self.assertEqual(len(module.build_args(ROOT, sdk=True)), 4)

    def test_mutable_or_incomplete_manifest_refused(self):
        manifest = json.loads((ROOT / 'docker/release-base-images.json').read_text())
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'docker').mkdir()
            for name in manifest['images']:
                for field, value in [('ref', 'node:latest'), ('platforms', {'amd64': 'sha256:' + 'a'*64})]:
                    changed = json.loads(json.dumps(manifest))
                    changed['images'][name][field] = value
                    (root / 'docker/release-base-images.json').write_text(json.dumps(changed))
                    with self.assertRaises(ValueError):
                        module.build_args(root)


if __name__ == '__main__':
    unittest.main()
