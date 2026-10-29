#!/usr/bin/env python3
"""Exercise the exact Python helper embedded in the native exe (Linux container role)."""
import importlib.util
import json
from pathlib import Path
import stat
import struct
import sys
import tempfile
import unittest
import warnings
import zipfile

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('package_image', ROOT / 'native/windows-launcher/package_image.py')
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)
NAME = 'aide-v0.1.17.5-RC12-linux-amd64-image.tar.gz'


class PackageTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix='native-package-')
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.zip = self.root / 'package.zip'
        self.output = self.root / 'image.tar.gz'

    def package(self, entries):
        with warnings.catch_warnings(), zipfile.ZipFile(self.zip, 'w') as package:
            warnings.simplefilter('ignore', UserWarning)
            for name, body in entries:
                package.writestr(name, body)

    def test_only_fixed_member_is_extracted(self):
        self.package([('../../outside', b'bad'), (NAME, b'fixed image')])
        self.assertEqual(helper.extract(self.zip, self.output, NAME), {'bytes': 11})
        self.assertEqual(self.output.read_bytes(), b'fixed image')
        self.assertEqual(set(p.name for p in self.root.iterdir()), {'package.zip', 'image.tar.gz'})

    def test_missing_or_duplicate_member(self):
        for entries in [[], [(NAME, b'a'), (NAME, b'b')]]:
            self.package(entries)
            with self.assertRaisesRegex(ValueError, 'unique'):
                helper.extract(self.zip, self.output, NAME)
            self.assertFalse(self.output.exists())

    def test_symlink_and_special_entries_rejected(self):
        for mode in [stat.S_IFLNK, stat.S_IFDIR, stat.S_IFIFO, stat.S_IFCHR]:
            entry = zipfile.ZipInfo(NAME)
            entry.create_system = 3
            entry.external_attr = (mode | 0o600) << 16
            self.package([(entry, b'target')])
            with self.assertRaisesRegex(ValueError, 'regular'):
                helper.extract(self.zip, self.output, NAME)
            self.assertFalse(self.output.exists())

    def test_existing_destination_retained(self):
        self.package([(NAME, b'new')])
        self.output.write_bytes(b'owned previous')
        with self.assertRaises(FileExistsError):
            helper.extract(self.zip, self.output, NAME)
        self.assertEqual(self.output.read_bytes(), b'owned previous')

    def test_crc_failure_and_truncation_never_succeed(self):
        self.package([(NAME, b'original')])
        original = self.zip.read_bytes()
        damaged = original.replace(b'original', b'corrupt!')
        self.zip.write_bytes(damaged)
        with self.assertRaises(zipfile.BadZipFile):
            helper.extract(self.zip, self.output, NAME)
        self.output.unlink(missing_ok=True)
        self.zip.write_bytes(original[:-25])
        with self.assertRaises(zipfile.BadZipFile):
            helper.extract(self.zip, self.output, NAME)

    def test_encrypted_and_oversize_entries_rejected(self):
        self.package([(NAME, b'a')])
        original = self.zip.read_bytes()
        central = original.index(b'PK\x01\x02')
        encrypted = bytearray(original)
        struct.pack_into('<H', encrypted, central + 8, 1)
        self.zip.write_bytes(encrypted)
        with self.assertRaisesRegex(ValueError, 'unencrypted'):
            helper.extract(self.zip, self.output, NAME)
        huge = zipfile.ZipInfo(NAME)
        huge.file_size = 65 * 1024**3
        class FakeZip:
            def __enter__(self): return self
            def __exit__(self, *args): pass
            def infolist(self): return [huge]
        from unittest.mock import patch
        with patch.object(helper.zipfile, 'ZipFile', return_value=FakeZip()):
            with self.assertRaisesRegex(ValueError, 'budget'):
                helper.extract(self.zip, self.output, NAME)

    def test_invalid_name_rejected_before_io(self):
        for name in ['../../image', 'aide-v1-linux-amd64-image.tar.gz', NAME + '/']:
            with self.assertRaisesRegex(ValueError, 'fixed image member'):
                helper.extract(self.zip, self.output, name)

    def test_idle_and_terminal_runs(self):
        self.assertEqual(helper.idle(self.root), {'idle': True})
        sessions = self.root / 'sessions/active'
        sessions.mkdir(parents=True)
        (sessions / 'session-a.json').write_text(json.dumps({'runs': [None, {'status': 'completed'}, {'status': 'resumed'}]}))
        self.assertEqual(helper.idle(self.root), {'idle': True})

    def test_active_unknown_malformed_and_flat_legacy_sessions(self):
        for body in [{'runs': [{'status': 'running'}]}, {'runs': [{'status': 'suspended'}]},
                     {'runs': [{'status': 'unknown'}]}, {'runs': {'status': 'completed'}},
                     {'runs': ['invalid']}, {'runs': 0}, {'runs': {}}, ['invalid']]:
            file = self.root / 'session-legacy.json'
            file.write_text(json.dumps(body))
            with self.assertRaises((ValueError, AttributeError)):
                helper.idle(self.root)
        file.write_text('{broken')
        with self.assertRaises(ValueError):
            helper.idle(self.root)


if __name__ == '__main__':
    unittest.main()
