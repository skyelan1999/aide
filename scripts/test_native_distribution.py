#!/usr/bin/env python3
"""Exercise the PE gate used by both packaging and candidate validation."""
import importlib.util
import os
from pathlib import Path
import shutil
import struct
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('candidate', ROOT / 'scripts/ci/build_candidate.py')
candidate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(candidate)


def pe(arch):
    data = bytearray(128)
    data[:2] = b'MZ'
    struct.pack_into('<I', data, 0x3c, 64)
    data[64:68] = b'PE\0\0'
    struct.pack_into('<H', data, 68, 0x8664 if arch == 'amd64' else 0xaa64)
    return data


class NativeDistributionTests(unittest.TestCase):
    def assert_asset_gate(self, data, arch, success):
        if not shutil.which('bash'):
            return  # Candidate gate still runs; shell execution is a separate capability.
        with tempfile.TemporaryDirectory(prefix='aide-pe-gate-') as directory:
            binary = Path(directory) / 'launcher.exe'
            binary.write_bytes(data)
            result = subprocess.run(['bash', str(ROOT / 'scripts/windows-launcher-asset.sh'), arch, 'unused'],
                                    env=dict(os.environ, AIDE_WINDOWS_LAUNCHER=str(binary)), capture_output=True)
            self.assertEqual(result.returncode == 0, success, result.stderr)

    def test_matching_architectures(self):
        for arch in ('amd64', 'arm64'):
            candidate.validate_windows_launcher_architecture(pe(arch), arch)
            self.assert_asset_gate(pe(arch), arch, True)

    def test_wrong_architectures(self):
        for binary, target in [('amd64', 'arm64'), ('arm64', 'amd64')]:
            with self.assertRaisesRegex(ValueError, 'architecture does not match'):
                candidate.validate_windows_launcher_architecture(pe(binary), target)
            self.assert_asset_gate(pe(binary), target, False)

    def test_malformed_headers(self):
        samples = [b'', b'MZ', bytes(128), pe('amd64')[:72]]
        invalid = pe('amd64')
        struct.pack_into('<I', invalid, 0x3c, 0xffffffff)
        samples.append(invalid)
        for data in samples:
            with self.assertRaisesRegex(ValueError, 'Malformed'):
                candidate.validate_windows_launcher_architecture(data, 'amd64')
            self.assert_asset_gate(data, 'amd64', False)


if __name__ == '__main__':
    unittest.main()
