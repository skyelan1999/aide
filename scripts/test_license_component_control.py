import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('license-component-control.sh')


class LicenseServiceTests(unittest.TestCase):
    def test_installed_service_reopens_without_forcing_replacement(self):
        for installed, registered, fail, expected in [(False, False, False, []),
                (True, True, False, ['print', 'kickstart']),
                (True, False, False, ['print', 'bootstrap', 'kickstart']),
                (True, True, True, ['print', 'kickstart'])]:
            with self.subTest(installed=installed, registered=registered, fail=fail), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                plist = root/'Library/LaunchAgents/com.skyelan.aide.pp-offline-license.plist'
                if installed:
                    plist.parent.mkdir(parents=True)
                    plist.write_text('synthetic fixture')
                (root/'uname').write_text('#!/bin/sh\necho Darwin\n'.replace('\\n','\n'))
                ctl = root/'launchctl'
                ctl.write_text('#!/bin/sh\necho "$1" >> "$FIX_LOG"\n'
                               '[ "$1" != print ] || [ "$FIX_REGISTERED" = 1 ] || exit 1\n'
                               '[ "$1" != kickstart ] || [ "$FIX_FAIL" = 0 ]\n')
                for p in (root/'uname', ctl):
                    p.chmod(0o700)
                log = root/'calls'
                fixture = root/'control.sh'
                fixture.write_text(SCRIPT.read_text().replace('$HOME', tmp))
                env = dict(os.environ, PATH=tmp+os.pathsep+os.environ['PATH'],
                           FIX_LOG=str(log), FIX_REGISTERED=str(int(registered)), FIX_FAIL=str(int(fail)))
                result = subprocess.run(['sh', str(fixture)], env=env, capture_output=True)
                self.assertEqual(log.read_text().splitlines() if log.exists() else [], expected)
                self.assertEqual(result.returncode != 0, installed and fail)



class BridgeMountTests(unittest.TestCase):
    def test_private_bridge_is_mounted_readonly_without_device_state(self):
        import json
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root/'account'
            bridge = home/'Library/Application Support/aide/pp-offline-license/bridge'
            bridge.mkdir(parents=True)
            plist = home/'Library/LaunchAgents/com.skyelan.aide.pp-offline-license.plist'
            plist.parent.mkdir(parents=True)
            plist.write_text('synthetic')
            scripts = root/'source/scripts'
            scripts.mkdir(parents=True)
            script = scripts/'license-component-control.sh'
            script.write_text(SCRIPT.read_text().replace('$HOME', str(home)))
            uname = root/'uname'
            uname.write_text('#!/bin/sh\necho Darwin\n')
            uname.chmod(0o700)
            result = subprocess.run(['sh', str(script), 'prepare'], env=dict(os.environ, PATH=str(root)+os.pathsep+os.environ['PATH']), capture_output=True, text=True, check=True)
            config = json.loads(Path(result.stdout.strip()).read_text())
            mounts = config['services']['aide']['volumes']
            self.assertEqual(len(mounts), 1)
            self.assertEqual(mounts[0]['source'], str(bridge))
            self.assertEqual(mounts[0]['target'], '/run/pp-license')
            self.assertTrue(mounts[0]['read_only'])
            self.assertFalse(mounts[0]['bind']['create_host_path'])


if __name__ == '__main__':
    unittest.main()
