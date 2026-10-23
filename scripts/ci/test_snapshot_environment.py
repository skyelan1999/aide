"""A source snapshot must override stale runtime Office script locations."""
import importlib.util
import io
import os
import re
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('snapshot', Path(__file__).parents[1] / 'test_go_snapshot.py')
snapshot = importlib.util.module_from_spec(spec)
spec.loader.exec_module(snapshot)


class SnapshotEnvironmentTests(unittest.TestCase):
    def test_current_snapshot_office_overrides_image_environment(self):
        data = io.BytesIO()
        with tarfile.open(fileobj=data, mode='w') as archive:
            member = tarfile.TarInfo('scripts/office/current-source-marker')
            member.size = len(b'current checkout')
            archive.addfile(member, io.BytesIO(b'current checkout'))
        # Execute the actual extraction/environment preamble, without costly Go work.
        preamble = snapshot.COMMAND.split('timeout --signal', 1)[0]
        check = 'test "$(cat "$AIDE_OFFICE_SCRIPTS/current-source-marker")" = "current checkout"'
        with tempfile.TemporaryDirectory() as tmp:
            result = subprocess.run(['bash', '-c', preamble + 'trap \'rm -rf "$snapshot"\' EXIT; ' + check],
                                    input=data.getvalue(), cwd=tmp,
                                    env=dict(os.environ, AIDE_OFFICE_SCRIPTS='/stale/image/office'),
                                    capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_host_deadline_allows_suite_and_cleanup(self):
        suite_seconds = int(re.search(r"--kill-after=10s (\d+)s", snapshot.COMMAND).group(1))
        self.assertGreaterEqual(snapshot.HOST_TIMEOUT_SECONDS, suite_seconds + 20)
        workflow = (Path(__file__).parents[2] / ".github/workflows/aide-ci.yml").read_text()
        job_minutes = int(re.search(r"  go:.*?timeout-minutes: (\d+)", workflow, re.S).group(1))
        self.assertGreater(job_minutes * 60, snapshot.HOST_TIMEOUT_SECONDS + 5 * 60)

    def test_invalid_parallel_resource_request_never_runs_docker(self):
        with patch.dict(os.environ, {'AIDE_TEST_WORKERS': '4;invalid'}), patch.object(snapshot.subprocess, 'check_output', return_value=b''), patch.object(snapshot.subprocess, 'run') as run:
            with self.assertRaisesRegex(ValueError, 'must be 1 or 2'):
                snapshot.main()
            run.assert_not_called()


class NativeDeliveryGateTests(unittest.TestCase):
    def test_build_stage_full_suite_has_explicit_package_deadline(self):
        root = Path(__file__).parents[2]
        for name in ('Dockerfile', 'Dockerfile.local'):
            source = (root / name).read_text()
            self.assertIn('go test -mod=vendor -count=1 -timeout=30m ./...', source)
            self.assertIn('AIDE_RUN_TESTS', source)

    def test_both_helper_families_are_complete_and_tampering_is_rejected(self):
        import hashlib
        spec = importlib.util.spec_from_file_location('assets', Path(__file__).parents[1] / 'test_runtime_assets.py')
        assets = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(assets)
        # Run the actual shipped-inventory gate, including its checksum reads.
        gate = 'if os.environ.get' + assets.PROGRAM.split('if os.environ.get', 1)[1].split('with tempfile.TemporaryDirectory()', 1)[0]
        with tempfile.TemporaryDirectory() as temporary, patch.dict(os.environ, {'AIDE_REQUIRE_LAN_BRIDGE': '1'}):
            root = Path(temporary); bridge = root / 'message-lan'; bridge.mkdir()
            entries = []
            for family in ('aide-message-lan', 'aide-local-mounts'):
                for platform in ('darwin-arm64', 'darwin-amd64', 'linux-arm64', 'linux-amd64', 'windows-arm64.exe', 'windows-amd64.exe'):
                    path = bridge / (family + '-' + platform)
                    path.write_bytes(b'native fixture ' + path.name.encode())
                    entries.append(hashlib.sha256(path.read_bytes()).hexdigest() + '  ' + path.name)
            sums = bridge / 'SHA256SUMS'; sums.write_text('\n'.join(entries) + '\n')
            namespace = {'root': root, 'os': os, 'hashlib': hashlib}
            exec(gate, namespace)
            sums.write_text('\n'.join(entries[:-1]) + '\n')
            with self.assertRaisesRegex(AssertionError, 'inventory mismatch'):
                exec(gate, namespace)
            sums.write_text('\n'.join(entries) + '\n')
            (bridge / 'aide-local-mounts-linux-arm64').write_bytes(b'tampered')
            with self.assertRaises(AssertionError):
                exec(gate, namespace)


if __name__ == '__main__':
    unittest.main()
