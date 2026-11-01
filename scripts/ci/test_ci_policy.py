"""The lightweight daily path must never implicitly enter full publication."""
import os
from pathlib import Path
import unittest
from unittest.mock import patch

import basic_go
import host_validation

ROOT = Path(__file__).resolve().parents[2]


class CIPolicyTests(unittest.TestCase):
    def test_daily_keeps_gate_without_heavy_builds(self):
        ci = (ROOT / '.github/workflows/aide-ci.yml').read_text()
        self.assertIn('needs: [quick, go]', ci)
        self.assertIn('basic_go.py', ci)
        self.assertNotIn('docker build', ci)
        self.assertNotIn('test_go_snapshot.py', ci)
        self.assertNotIn('launcher_clean_environment', ci)
        self.assertTrue(any(command[:2] == ['go', 'test'] and './...' in command for command in basic_go.COMMANDS))

    def test_delivery_is_explicit_and_every_gate_is_required(self):
        delivery = (ROOT / '.github/workflows/aide-delivery.yml').read_text()
        self.assertNotIn('workflow_run:', delivery)
        self.assertIn('workflow_dispatch:', delivery)
        for name in ('source', 'native', 'windows', 'macos', 'browser', 'launcher', 'public_plugins'):
            self.assertIn('needs.' + name + ".result == 'success'", delivery)
        for platform in ('ubuntu-24.04-arm', 'ubuntu-24.04', 'windows-2025', 'macos-15-intel'):
            self.assertIn(platform, delivery)
        self.assertIn('chromium firefox webkit', delivery)

    def test_release_browser_has_real_entrypoint_and_receipt_gate(self):
        fixture = (ROOT / 'internal/server/release_browser_test.go').read_text()
        runner = (ROOT / 'scripts/browser-release-matrix.cjs').read_text()
        self.assertIn('func TestReleaseBrowserMatrix(', fixture)
        self.assertIn('httptest.NewServer(a.Handler())', fixture)
        self.assertIn("assert.equal(report.cases.length,48)", runner)
        self.assertIn("report.status='pass'", runner)
        for archived in ('release-native-transfer.yml', 'release-rc3-finish.yml'):
            workflow = (ROOT / '.github/workflows' / archived).read_text()
            self.assertNotIn('contents: write', workflow)
            self.assertNotIn('push:', workflow)
            self.assertIn('exit 1', workflow)

    def test_host_plan_retains_both_powershells_and_temporary_bridge(self):
        with patch.dict(os.environ, {'SystemRoot': 'C:/Windows'}):
            windows = host_validation.plan('windows', 'temporary')
        self.assertEqual(sum('--pwsh' in command for command in windows), 6)
        macos = host_validation.plan('macos', '/tmp/isolated')
        self.assertIn(['codesign', '--verify', '--deep', '--strict', '/tmp/isolated/Aide Computer Bridge.app'], macos)
        self.assertNotIn('./scripts/xsys-review',macos[0], 'xsys-review contains no Darwin implementation')
        self.assertIn('./internal/messaging',macos[0])

    def test_host_go_requires_actual_assertions_not_only_package_or_skips(self):
        import json
        command=['go','test','-json','./cmd/fixture']
        passing=json.dumps({'Package':'aide/cmd/fixture','Test':'TestNative','Action':'pass'})
        self.assertEqual(len(host_validation.go_receipt(passing,command)),1)
        for event in ({'Package':'aide/cmd/fixture','Action':'pass'},
                      {'Package':'aide/cmd/fixture','Test':'TestNative','Action':'skip'},
                      {'Package':'aide/other','Test':'TestNative','Action':'pass'}):
            with self.subTest(event=event),self.assertRaises(ValueError):
                host_validation.go_receipt(json.dumps(event),command)


if __name__ == '__main__':
    unittest.main()
