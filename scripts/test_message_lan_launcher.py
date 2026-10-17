"""Host launcher tests use disposable files and a fake Docker CLI, no real daemon."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
class MessageLANLauncherTests(unittest.TestCase):
    def setUp(self):
        self.temporary=tempfile.TemporaryDirectory(prefix='aide LAN space ')
        self.addCleanup(self.temporary.cleanup)
        self.root=Path(self.temporary.name)
        (self.root/'scripts').mkdir();(self.root/'bin').mkdir()
        shutil.copy2(ROOT/'scripts/message-lan-control.sh',self.root/'scripts')
        self.helper=b'''#!/bin/sh
mode=run
while [ "$#" -gt 0 ]; do
 case "$1" in -dir) dir="$2"; shift 2;; -target) shift 2;; -status) mode=status; shift;; -stop) mode=stop; shift;; *) exit 99;; esac
done
case "$mode" in status) test -f "$dir/host.json";; stop) rm -f "$dir/host.json";; run) mkdir -p "$dir"; echo ready > "$dir/host.json";; esac
'''
        (self.root/'helper').write_bytes(self.helper)
        docker=self.root/'bin/docker'
        docker.write_text('''#!/usr/bin/env python3
import sys,os,json,shutil,hashlib
from pathlib import Path
r=Path(os.environ['LAN_FIXTURE']);a=sys.argv[1:]
with (r/'calls').open('a') as f:f.write(json.dumps(a)+'\\n')
if a==['compose','ps','-q','aide']:print('current')
elif a==['compose','port','aide','45840']:print(os.environ.get('LAN_BINDING','127.0.0.1:55555'))
elif a==['inspect','current','--format','{{.Image}}']:print('sha256:'+'a'*64)
elif a==['create','--network','none','--entrypoint','/bin/true','sha256:'+'a'*64]:print('pristine')
elif a[0]=='cp':
 assert a[1].startswith('pristine:/opt/aide/message-lan/')
 name=a[1].rsplit('/',1)[-1]
 if name=='SHA256SUMS':
  digest='0'*64 if os.environ.get('LAN_BAD_HASH') else hashlib.sha256((r/'helper').read_bytes()).hexdigest()
  Path(a[2]).write_text(''.join(digest+'  aide-message-lan-'+p+'\\n' for p in ['darwin-arm64','darwin-amd64','linux-arm64','linux-amd64']))
 else:shutil.copyfile(r/'helper',a[2])
elif a==['rm','pristine']:pass
else:raise SystemExit(87)
''');docker.chmod(0o700)
        self.env=dict(os.environ,LAN_FIXTURE=str(self.root),PATH=str(self.root/'bin')+os.pathsep+os.environ['PATH'])
    def run_control(self,action='start',**environment):
        return subprocess.run(['bash',str(self.root/'scripts/message-lan-control.sh'),action],env={**self.env,**environment},capture_output=True,text=True,timeout=10)
    def test_start_uses_loopback_mapping_and_pristine_checked_image(self):
        result=self.run_control();self.assertEqual(result.returncode,0,result.stderr)
        self.assertTrue((self.root/'.aide-data/storage/data/messages/lan-bridge/host.json').exists())
        calls=[json.loads(line) for line in (self.root/'calls').read_text().splitlines()]
        self.assertIn(['create','--network','none','--entrypoint','/bin/true','sha256:'+'a'*64],calls)
        self.assertIn(['rm','pristine'],calls)
        stopped=self.run_control('stop');self.assertEqual(stopped.returncode,0,stopped.stderr)
        self.assertFalse((self.root/'.aide-data/storage/data/messages/lan-bridge/host.json').exists())
    def test_mismatched_checksum_does_not_execute_helper(self):
        result=self.run_control(LAN_BAD_HASH='1');self.assertNotEqual(result.returncode,0)
        self.assertFalse((self.root/'.aide-data/storage/data/messages/lan-bridge/host.json').exists())
    def test_non_loopback_mapping_rejected(self):
        result=self.run_control(LAN_BINDING='0.0.0.0:55555');self.assertNotEqual(result.returncode,0)
        self.assertNotIn('create',(self.root/'calls').read_text())
    def test_both_images_and_all_launch_modes_ship_bridge(self):
        for name in ('Dockerfile','Dockerfile.local'):
            source=(ROOT/name).read_text()
            self.assertIn('sh scripts/build-message-lan.sh /opt/aide-message-lan',source)
            self.assertIn('COPY --from=build /opt/aide-message-lan /opt/aide/message-lan',source)
        for name in ('compose.yaml','docker/compose.offline.yaml'):
            source=(ROOT/name).read_text();self.assertIn('"127.0.0.1::45840"',source);self.assertIn('AIDE_MESSAGE_LAN_BRIDGE:',source)
        self.assertIn('scripts/message-lan-control.sh start',(ROOT/'scripts/aide.sh').read_text())
        self.assertIn('message-lan-control.ps1',(ROOT/'start.ps1').read_text(encoding='utf-8-sig'))
        self.assertIn('message-lan-control.ps1',(ROOT/'scripts/package-release-assets.sh').read_text())
if __name__=='__main__':unittest.main()
