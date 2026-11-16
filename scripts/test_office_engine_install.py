#!/usr/bin/env python3
"""Exercise offline staging, archive/binary integrity and fixed-member extraction."""
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('office_engine_install', Path(__file__).with_name('install-office-engine.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class OfficeEngineInstall(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def fixture(self, symlink=False, bad_binary=False):
        archive = self.root / 'input.tar.gz'
        content = b'unchanged bundled executable'
        with tarfile.open(archive, 'w:gz') as package:
            member = tarfile.TarInfo('upstream/collabora/Collabora_Online.AppImage')
            member.size = len(content)
            if symlink:
                member.type, member.linkname = tarfile.SYMTYPE, '/outside'
            package.addfile(member, io.BytesIO(content) if not symlink else None)
        sha = module.digest(archive)
        cache = self.root / 'cache'
        cache.mkdir()
        archive.rename(cache / (sha + '.tar.gz'))
        manifest = self.root / 'source.json'
        manifest.write_text(json.dumps({'version': 'fixture', 'sources': {'arm64': {
            'archiveSHA256': sha, 'member': member.name, 'appImageBytes': len(content),
            'appImageSHA256': '0'*64 if bad_binary else hashlib.sha256(content).hexdigest(),
            'url': 'https://never-contact.invalid/package.tar.gz'}}}))
        return manifest, cache, self.root / 'output'

    def test_offline_installs_exact_fixed_member_without_network(self):
        manifest, cache, output = self.fixture()
        with patch.object(module.urllib.request, 'urlopen', side_effect=AssertionError('network used')):
            module.install(manifest, output, cache, 'arm64', True)
        self.assertEqual((output / 'Collabora_Online.AppImage').read_bytes(), b'unchanged bundled executable')

    def test_missing_offline_archive_never_contacts_network(self):
        manifest, cache, output = self.fixture()
        next(cache.iterdir()).unlink()
        with patch.object(module.urllib.request, 'urlopen', side_effect=AssertionError('network used')):
            with self.assertRaisesRegex(ValueError, 'Missing pinned'):
                module.install(manifest, output, cache, 'arm64', True)

    def test_corrupt_archive_is_refused(self):
        manifest, cache, output = self.fixture()
        next(cache.iterdir()).write_bytes(b'corrupt')
        with self.assertRaisesRegex(ValueError, 'archive checksum'):
            module.install(manifest, output, cache, 'arm64', True)

    def test_symlink_member_is_refused(self):
        manifest, cache, output = self.fixture(symlink=True)
        with self.assertRaisesRegex(ValueError, 'Unexpected Office engine member'):
            module.install(manifest, output, cache, 'arm64', True)

    def test_wrong_binary_hash_is_refused_and_removed(self):
        manifest, cache, output = self.fixture(bad_binary=True)
        with self.assertRaisesRegex(ValueError, 'engine checksum'):
            module.install(manifest, output, cache, 'arm64', True)
        self.assertFalse((output / 'Collabora_Online.AppImage').exists())


if __name__ == '__main__':
    unittest.main()
