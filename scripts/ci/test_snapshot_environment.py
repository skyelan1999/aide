"""A source snapshot must override stale runtime Office script locations."""
import importlib.util
import io
import os
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

    def test_invalid_parallel_resource_request_never_runs_docker(self):
        with patch.dict(os.environ, {'AIDE_TEST_WORKERS': '4;invalid'}), patch.object(snapshot.subprocess, 'check_output', return_value=b''), patch.object(snapshot.subprocess, 'run') as run:
            with self.assertRaisesRegex(ValueError, 'must be 1 or 2'):
                snapshot.main()
            run.assert_not_called()


if __name__ == '__main__':
    unittest.main()
