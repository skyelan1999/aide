"""Exercise offline bootstrap without using the user's Docker engine or data."""
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
IMAGE_ID = 'sha256:' + 'a' * 64


class OfflineBundleTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='aide bundle ')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        for name in ('scripts', 'docker-images', 'bin'):
            (self.root / name).mkdir()
        shutil.copy(ROOT / 'start.command', self.root)
        shutil.copy(ROOT / 'scripts/aide.sh', self.root / 'scripts')
        for name in ('launcher-data.sh', 'launcher-data.ps1', 'launcher-data.py', 'resource-guard.sh', 'prebuilt-image.sh'):
            shutil.copy(ROOT / 'scripts' / name, self.root / 'scripts')
        # Native extraction/preparation is exercised independently against real Docker.
        # This fixture tests launcher orchestration without running host programs.
        (self.root / 'scripts/local-mounts-control.sh').write_text('#!/bin/sh\nif [ "$1" = prepare ]; then\n mkdir -p .aide-data/local-mounts\n printf \'{"services":{"aide":{}}}\\n\' > .aide-data/local-mounts/compose.json\n printf \'%s/.aide-data/local-mounts/compose.json\\n\' "$PWD"\nfi\n')
        shutil.copy(ROOT / 'docker/compose.offline.yaml', self.root / 'compose.yaml')
        shutil.copy(ROOT / 'docker/offline.env.example', self.root / '.env.example')
        (self.root / '.aide-image').write_text(f'aide:offline-aaaa {IMAGE_ID} linux/arm64\n')
        archive = self.root / 'docker-images/aide-local.tar'
        archive.write_bytes(b'image fixture')
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        (archive.parent / 'SHA256SUMS').write_text(f'{digest}  aide-local.tar\n')
        docker = self.root / 'bin/docker'
        docker.write_text('''#!/usr/bin/env python3
import json, os, pathlib, sys
a=sys.argv[1:]; root=pathlib.Path(os.environ['FIXTURE_ROOT'])
with (root/'calls').open('a') as f: f.write(json.dumps(a)+'\\n')
if a == ['info']: pass
elif a[:1] == ['ps']: pass
elif a[:2] == ['info','--format']:
    if a[2] == '{{.MemTotal}}': print(6*1024**3)
    elif a[2] == '{{.NCPU}}': print(4)
    else: print(os.environ.get('ENGINE_PLATFORM','linux/aarch64'))
elif a[:2] == ['image','inspect']:
    if not (root/'image-id').exists(): sys.exit(1)
    print((root/'image-id').read_text())
elif a[:2] == ['image','load']:
    (root/'image-id').write_text(os.environ.get('LOADED_ID',os.environ['EXPECTED_ID']))
elif a[:2] == ['volume','inspect']: sys.exit(1)
elif a[:1] == ['run']:
    import subprocess
    subprocess.run([sys.executable,str(root/'scripts/launcher-data.py'),'--root',str(root/'.aide-data'),'--platform','fixture'],check=True)
    cert=root/'.aide-data/storage/data/certs/cert.pem'
    cert.parent.mkdir(parents=True,exist_ok=True)
    if os.environ.get('NO_HOST_CERT') != '1':
        cert.write_text('fixture certificate for mocked host curl')
elif a[:3] == ['compose','config','--images']: print('aide:slot-a')
elif a[:3] == ['compose','port','aide']:
    print(os.environ.get('PORT_OUTPUT','127.0.0.1:18097'))
    sys.exit(int(os.environ.get('PORT_EXIT','0')))
elif a[:4] == ['compose','exec','-T','aide'] and 'curl' in a:
    print(json.dumps({'status':'ok','service':'aide','instanceId':os.environ.get('CONTAINER_INSTANCE','fixture-instance')}))
elif a[:2] == ['compose','up']:
    assert '--no-build' in a and '--pull' in a and a[a.index('--pull')+1] == 'never'
elif a[:2] in (['compose','ps'], ['compose','run']): pass
elif a[:2] in (['compose','exec'], ['compose','stop']): pass
else: sys.exit('Unexpected docker operation: '+repr(a))
''')
        docker.chmod(0o755)
        opener = self.root / 'bin/open'
        opener.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$FIXTURE_ROOT/opened-url"\n')
        opener.chmod(0o755)
        # Linux must remain isolated too; never invoke the host desktop opener.
        shutil.copy(opener, self.root / 'bin/xdg-open')
        curl = self.root / 'bin/curl'
        curl.write_text('''#!/usr/bin/env python3
import json, os, pathlib, sys
a=sys.argv[1:]; root=pathlib.Path(os.environ['FIXTURE_ROOT'])
with (root/'host-probes').open('a') as f: f.write(json.dumps(a)+'\\n')
assert '--cacert' in a and '--noproxy' in a and '--max-time' in a
assert not any(x in a for x in ['-k','--insecure','-L','--location'])
assert a[-1] == 'https://localhost:18097/healthz'
print(json.dumps({'status':'ok','service':'aide','instanceId':os.environ.get('HOST_INSTANCE','fixture-instance')}))
sys.exit(int(os.environ.get('HOST_HEALTH_EXIT','0')))
''')
        curl.chmod(0o755)
        self.env = dict(os.environ, PATH=str(docker.parent) + os.pathsep + os.environ['PATH'],
                        FIXTURE_ROOT=str(self.root), EXPECTED_ID=IMAGE_ID, AIDE_OPEN_BROWSER='0', AIDE_PORT='18097')

    def run_start(self):
        return subprocess.run(['bash', 'start.command'], cwd=self.root, env=self.env,
                              capture_output=True, text=True, errors='replace')

    def calls(self):
        return (self.root / 'calls').read_text()

    def test_first_start_loads_checked_image_without_building(self):
        result = self.run_start()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('"image", "load"', self.calls())
        self.assertIn('"--no-build", "--pull", "never"', self.calls())
        self.assertTrue((self.root / '.env').exists())

    def test_repeat_start_keeps_configuration_and_skips_load(self):
        (self.root / 'image-id').write_text(IMAGE_ID)
        saved = 'AIDE_PORT=19097\nAIDE_BROWSER_BRIDGE_TOKEN=' + 'b'*32 + '\nAIDE_COMPUTER_BRIDGE_TOKEN=' + 'c'*32 + '\n'
        (self.root / '.env').write_text(saved)
        (self.root / '.cache').mkdir()
        (self.root / '.cache/aide-manual-stop').touch()
        result = self.run_start()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn('"image", "load"', self.calls())
        self.assertEqual((self.root / '.env').read_text(), saved)
        self.assertFalse((self.root / '.cache/aide-manual-stop').exists())

    def test_architecture_mismatch_stops_before_loading(self):
        self.env['ENGINE_PLATFORM'] = 'linux/x86_64'
        result = self.run_start()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('架构', result.stdout + result.stderr)
        self.assertNotIn('"load"', self.calls())
        self.assertNotIn('"compose"', self.calls())

    def test_invalid_published_ports_never_report_ready_or_open(self):
        for address in ('', 'invalid IP:0', '127.0.0.1:0', '127.0.0.1:65536',
                        '127.0.0.1:no', '127.0.0.1:18097\n[::]:18098'):
            with self.subTest(address=address):
                self.env.update(PORT_OUTPUT=address, AIDE_OPEN_BROWSER='1')
                result = self.run_start()
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertNotIn('aide 已就绪', result.stdout)
                self.assertFalse((self.root/'opened-url').exists())
                self.assertNotIn('cat /data/auth/access-token', self.calls())
                self.assertFalse((self.root/'host-probes').exists())

    def test_failed_port_query_never_reports_ready(self):
        self.env.update(PORT_EXIT='1', AIDE_OPEN_BROWSER='1')
        result = self.run_start()
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('aide 已就绪', result.stdout)
        self.assertFalse((self.root/'opened-url').exists())

    def test_host_connection_or_certificate_failure_never_opens(self):
        for code in ('7', '60'):
            with self.subTest(curl_exit=code):
                self.env.update(HOST_HEALTH_EXIT=code, AIDE_OPEN_BROWSER='1')
                result = self.run_start()
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn('aide 已就绪', result.stdout)
                self.assertFalse((self.root/'opened-url').exists())
                self.assertNotIn('cat /data/auth/access-token', self.calls())

    def test_other_host_instance_never_opens(self):
        self.env.update(HOST_INSTANCE='another-instance', AIDE_OPEN_BROWSER='1')
        result = self.run_start()
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('aide 已就绪', result.stdout)
        self.assertFalse((self.root/'opened-url').exists())

    def test_missing_container_identity_never_opens_or_probes(self):
        self.env.update(CONTAINER_INSTANCE='', AIDE_OPEN_BROWSER='1')
        result = self.run_start()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root/'opened-url').exists())
        self.assertFalse((self.root/'host-probes').exists())

    def test_missing_saved_certificate_never_opens_or_probes(self):
        self.env.update(NO_HOST_CERT='1', AIDE_OPEN_BROWSER='1')
        result = self.run_start()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root/'opened-url').exists())
        self.assertFalse((self.root/'host-probes').exists())

    def test_ipv4_and_ipv6_bindings_share_one_verified_port(self):
        self.env.update(PORT_OUTPUT='0.0.0.0:18097\n[::]:18097', AIDE_OPEN_BROWSER='1')
        result = self.run_start()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('aide 已就绪', result.stdout)
        self.assertEqual((self.root/'opened-url').read_text().strip(), 'https://localhost:18097/')

    def test_corrupt_archive_never_loads(self):
        (self.root / 'docker-images/aide-local.tar').write_bytes(b'corrupt')
        self.assertNotEqual(self.run_start().returncode, 0)
        self.assertNotIn('"load"', self.calls())
        self.assertNotIn('"compose"', self.calls())

    def test_wrong_loaded_identity_never_starts(self):
        self.env['LOADED_ID'] = 'sha256:' + 'b' * 64
        self.assertNotEqual(self.run_start().returncode, 0)
        self.assertNotIn('"compose"', self.calls())

    def test_release_image_download_resumes_after_interruption(self):
        tag = 'v0.1.14.0-RC99'
        archive_name = f'aide-{tag}-linux-arm64-image.tar.gz'
        payload = b'release image fixture for resumable download'
        expected = hashlib.sha256(payload).hexdigest()
        (self.root / 'docker-images/aide-local.tar').unlink()
        (self.root / 'docker-images/SHA256SUMS').unlink()
        (self.root / '.aide-image').write_text(f'aide:offline-aaaa {IMAGE_ID} linux/arm64 {tag}\n')
        (self.root / 'image-payload').write_bytes(payload)
        curl = self.root / 'bin/curl'
        curl.write_text('''#!/usr/bin/env python3
import os, pathlib, sys
a=sys.argv[1:]; root=pathlib.Path(os.environ['FIXTURE_ROOT'])
if a[-1].endswith('/healthz'):
    print('{"status":"ok","service":"aide","instanceId":"fixture-instance"}')
    sys.exit(0)
out=pathlib.Path(a[a.index('--output')+1]); url=a[-1]
if url.endswith('/SHA256SUMS'):
    name=os.environ['ARCHIVE_NAME']; digest=os.environ['ARCHIVE_HASH']
    out.write_text(f'{digest}  {name}\\n')
    sys.exit(0)
payload=(root/'image-payload').read_bytes(); calls=root/'curl-image-calls'
count=int(calls.read_text())+1 if calls.exists() else 1; calls.write_text(str(count))
if count == 1:
    out.write_bytes(payload[:len(payload)//2])
    sys.exit(18)
assert '--continue-at' in a and a[a.index('--continue-at')+1] == '-'
with out.open('ab') as f: f.write(payload[len(out.read_bytes()):])
''')
        curl.chmod(0o755)
        self.env.update(ARCHIVE_NAME=archive_name, ARCHIVE_HASH=expected)
        self.env['AIDE_OPEN_BROWSER'] = '1'
        result = self.run_start()
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        self.assertEqual((self.root / 'docker-images' / archive_name).read_bytes(), payload)
        self.assertEqual((self.root / 'curl-image-calls').read_text(), '2')
        self.assertIn('"image", "load"', self.calls())
        self.assertEqual((self.root / 'opened-url').read_text().strip(), 'https://localhost:18097/')


if __name__ == '__main__':
    unittest.main()
