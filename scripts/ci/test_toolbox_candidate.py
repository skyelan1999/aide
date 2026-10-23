"""SDK bundle archive and identity contracts, without Docker or network."""
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
import zipfile
from toolbox_candidate import validate_bundle


class ToolboxBundleTests(unittest.TestCase):
    def test_inventory_identity_and_checksum_fail_closed(self):
        tag, arch, source = 'v1.2.3.4-RC1', 'arm64', 'b' * 64
        prefix = f'aide-{tag}-devtools-linux-{arch}'
        payload = b'compressed-image-fixture'
        files = {'image.tar.gz': payload,
                 'manifest.json': json.dumps(dict(format='aide-development-toolbox', releaseTag=tag,
                     platform='linux/' + arch, sourceSHA256=source, imageProfile='toolbox', imageArchive='image.tar.gz')),
                 'SHA256SUMS': hashlib.sha256(payload).hexdigest() + '  image.tar.gz\n',
                 'README.md': 'Usage', 'scripts/development-toolbox.py': 'helper',
                 'docker/Dockerfile.sdk': 'toolchain', 'LICENSE': 'license'}
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'sdk.zip'
            def write(changes=None):
                with zipfile.ZipFile(path, 'w') as archive:
                    for name, value in {**files, **(changes or {})}.items():
                        archive.writestr(prefix + '/' + name, value)
            write()
            self.assertEqual(validate_bundle(path, tag, arch, source)['status'], 'pass')
            with self.assertRaisesRegex(ValueError, 'identity mismatch'):
                validate_bundle(path, tag, arch, 'c' * 64)
            for changes in ({'image.tar.gz': b'tampered'}, {'../../escape': 'bad'}, {'private.env': 'bad'}):
                write(changes)
                with self.assertRaises(ValueError):
                    validate_bundle(path, tag, arch, source)


if __name__ == '__main__':
    unittest.main()
