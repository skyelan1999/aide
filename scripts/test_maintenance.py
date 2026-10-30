"""Maintenance must never stop other projects or delete Docker data/images."""
import json, os, shutil, subprocess, tempfile, unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
class MaintenanceTests(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory(prefix='aide maintenance ');self.addCleanup(self.tmp.cleanup)
  self.root=Path(self.tmp.name);(self.root/'scripts').mkdir();(self.root/'bin').mkdir()
  shutil.copy(ROOT/'scripts/aide-maintenance.sh',self.root/'scripts')
  shutil.copy(ROOT/'scripts/aide.sh',self.root/'scripts')
  shutil.copy(ROOT/'scripts/watch-port.sh',self.root/'scripts')
  for name in ['stop.command','clean-docker-cache.command']:shutil.copy(ROOT/name,self.root/name)
  docker=self.root/'bin/docker';docker.write_text('''#!/usr/bin/env python3
import os,json,sys
with open(os.environ['CALL_LOG'],'a') as f:f.write(json.dumps(sys.argv[1:])+'\\n')
a=sys.argv[1:]
if a==['info']:sys.exit(int(os.environ.get('ENGINE_EXIT','0')))
if a[:2]==['ps','-q']:
 print(os.environ.get('IDS','one\\ntwo'));sys.exit(0)
if a[:1]==['stop']:sys.exit(int(os.environ.get('STOP_EXIT','0')))
if a==['builder','prune','--all','--force']:sys.exit(int(os.environ.get('PRUNE_EXIT','0')))
sys.exit('Forbidden unexpected operation: '+repr(a))
''');docker.chmod(0o755)
  self.env=dict(os.environ,PATH=str(self.root/'bin')+os.pathsep+os.environ['PATH'],CALL_LOG=str(self.root/'calls'))
 def run_action(self,name,*args):return subprocess.run(['bash',name,*args],cwd='/',env=self.env,text=True,capture_output=True)
 def calls(self):return [json.loads(s) for s in (self.root/'calls').read_text().splitlines()]
 def launcher(self,name):return str(self.root/name)
 def test_stop_exact_root_and_service(self):
  r=self.run_action(self.launcher('stop.command'));self.assertEqual(r.returncode,0,r.stderr)
  self.assertEqual(self.calls()[1],['ps','-q','--filter','label=com.docker.compose.project.working_dir='+str(self.root)])
  self.assertFalse(any('label=com.docker.compose.service=' in x for a in self.calls() for x in a), 'Stop must include owned hosted plugin services')
  self.assertEqual([a for a in self.calls() if a[0]=='stop'],[['stop','--time','30','one'],['stop','--time','30','two']]);self.assertTrue((self.root/'.cache/aide-manual-stop').exists())
 def test_existing_cli_dispatch_uses_same_scoped_stop(self):
  r=self.run_action(str(self.root/'scripts/aide.sh'),'stop','--dry-run');self.assertEqual(r.returncode,0,r.stderr);self.assertEqual(self.calls()[1][0:2],['ps','-q']);self.assertFalse((self.root/'.cache').exists())
 def test_already_stopped_is_idempotent(self):
  self.env['IDS']='';r=self.run_action(self.launcher('stop.command'));self.assertEqual(r.returncode,0);self.assertFalse(any(a[0]=='stop' for a in self.calls()))
 def test_dry_run_does_not_stop_or_write(self):
  r=self.run_action(self.launcher('stop.command'),'--dry-run');self.assertEqual(r.returncode,0);self.assertTrue(all(a[0] in ['info','ps'] for a in self.calls()));self.assertFalse((self.root/'.cache').exists())
 def test_stop_marker_blocks_port_watcher(self):
  (self.root/'.cache').mkdir();(self.root/'.cache/aide-manual-stop').touch()
  r=self.run_action(str(self.root/'scripts/watch-port.sh'));self.assertEqual(r.returncode,0,r.stderr);self.assertFalse((self.root/'calls').exists());self.assertFalse((self.root/'.aide-port-watcher.lock').exists())
 def test_cache_only_builder_no_docker_data(self):
  r=self.run_action(self.launcher('clean-docker-cache.command'));self.assertEqual(r.returncode,0,r.stderr);self.assertEqual(self.calls(),[['info'],['builder','prune','--all','--force']])
 def test_cache_preview(self):
  r=self.run_action(self.launcher('clean-docker-cache.command'),'--dry-run');self.assertEqual(r.returncode,0);self.assertEqual(self.calls(),[['info']])
 def test_engine_unavailable_still_stops_owned_helpers_and_blocks_restart(self):
  (self.root/'scripts/message-lan-control.sh').write_text('#!/bin/sh\ntouch helper-stopped\n')
  self.env['ENGINE_EXIT']='1';r=self.run_action(self.launcher('stop.command'));self.assertNotEqual(r.returncode,0);self.assertEqual(self.calls(),[['info']]);self.assertTrue((self.root/'.cache/aide-manual-stop').exists());self.assertTrue((self.root/'helper-stopped').exists())
 def test_helper_failure_still_stops_application_containers(self):
  (self.root/'scripts/message-lan-control.sh').write_text('#!/bin/sh\nexit 9\n')
  r=self.run_action(self.launcher('stop.command'));self.assertEqual(r.returncode,9);self.assertEqual(len([c for c in self.calls() if c[0]=='stop']),2)
 def test_errors_propagate(self):
  self.env['PRUNE_EXIT']='7';r=self.run_action(self.launcher('clean-docker-cache.command'));self.assertEqual(r.returncode,7)
  self.env['STOP_EXIT']='8';r=self.run_action(self.launcher('stop.command'));self.assertEqual(r.returncode,8)
if __name__=='__main__':unittest.main()
