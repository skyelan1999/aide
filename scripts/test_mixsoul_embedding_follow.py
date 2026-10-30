#!/usr/bin/env python3
import copy
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('follow', ROOT / 'scripts/mixsoul-embedding-follow.py')
follow = importlib.util.module_from_spec(spec)
spec.loader.exec_module(follow)
CFG = {'endpoint':'http://127.0.0.1:32141/v1/embeddings', 'model':'sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2',
       'revision':'test', 'dimensions':384, 'token':'0'*64}


class FakeDocker:
    def __init__(self):
        self.main = {'Id':'main-id', 'State':{'Running':True,'StartedAt':'first'},
                     'Config':{'Labels':{'com.docker.compose.project':'fixture','com.docker.compose.service':'aide'},
                               'Env':['AIDE_ASSISTANT_ENGINE=mixsoul','AIDE_MIXSOUL_LOCAL_EMBEDDING='+json.dumps(CFG)]}}
        self.old = None
        self.calls = []

    def inspect(self, name):
        return copy.deepcopy(self.main if name == 'main' else self.old)

    def run(self, *args, environment=None):
        self.calls.append(args)
        if args[0] == 'exec': return b'net:[same]'
        if args[0] == 'rm': self.old = None
        if args[0] == 'run':
            labels = dict(args[i+1].split('=',1) for i,a in enumerate(args) if a == '--label')
            self.old = {'Config':{'Labels':labels},'State':{'Running':True}}
            self.credential = environment.get('MIXSOUL_EMBEDDING_TOKEN')
        return b'ok'


class FollowerTests(unittest.TestCase):
    def setUp(self): self.d = FakeDocker()
    def reconcile(self): return follow.reconcile(self.d,'main','sidecar',Path('/models'),'image:fixture','fixture','owner')

    def test_restart_rebinds_and_retains_credentials_off_command_line(self):
        self.assertEqual(self.reconcile(),'rebound')
        self.assertEqual(self.d.credential, CFG['token'])
        self.assertNotIn(CFG['token'],repr(self.d.calls))
        self.assertEqual(self.reconcile(),'ready')
        self.d.main['State']['StartedAt']='second'
        self.assertEqual(self.reconcile(),'rebound')
        self.assertEqual(len([c for c in self.d.calls if c[0]=='run']),2)
        self.assertTrue(any(c[0]=='rm' for c in self.d.calls))
        self.assertFalse(any('-p' in c or '--publish' in c for c in self.d.calls))

    def test_stopped_or_absent_main_has_no_mutation(self):
        self.d.main['State']['Running']=False
        self.assertEqual(self.reconcile(),'main-stopped');self.assertFalse(self.d.calls)
        self.d.main=None
        self.assertEqual(self.reconcile(),'main-stopped');self.assertFalse(self.d.calls)

    def test_foreign_project_or_sidecar_is_retained(self):
        self.d.main['Config']['Labels']['com.docker.compose.project']='foreign'
        with self.assertRaises(ValueError): self.reconcile()
        self.assertFalse(self.d.calls)
        self.d=FakeDocker();self.d.old={'Config':{'Labels':{}},'State':{'Running':True}}
        with self.assertRaises(ValueError): self.reconcile()
        self.assertFalse(self.d.calls)

    def test_remote_endpoint_or_disabled_engine_is_refused(self):
        for endpoint in ['https://example.invalid/v1/embeddings','http://localhost:32141/v1/embeddings',
                         'http://127.0.0.1:32141/v1/embeddings?secret=x']:
            self.d=FakeDocker();cfg=dict(CFG,endpoint=endpoint)
            self.d.main['Config']['Env'][1]='AIDE_MIXSOUL_LOCAL_EMBEDDING='+json.dumps(cfg)
            with self.assertRaises(ValueError): self.reconcile()
            self.assertFalse(self.d.calls)
        self.d=FakeDocker();self.d.main['Config']['Env'][0]='AIDE_ASSISTANT_ENGINE=builtin'
        with self.assertRaises(ValueError): self.reconcile()

    def test_main_replacement_during_inspection_has_no_mutation(self):
        original=self.d.inspect;count=0
        def inspect(name):
            nonlocal count
            result=original(name)
            if name=='main':
                count+=1
                if count==2: result['Id']='replacement'
            return result
        self.d.inspect=inspect
        self.assertEqual(self.reconcile(),'main-changing');self.assertFalse(self.d.calls)

    @unittest.skipUnless(shutil.which('docker'),'Docker Compose CLI unavailable')
    def test_source_and_release_compose_forward_opt_in_without_shell_evaluation(self):
        if subprocess.run(['docker','compose','version'],capture_output=True).returncode:
            self.skipTest('Docker Compose CLI unavailable')
        with tempfile.TemporaryDirectory() as t:
            base={k:v for k,v in os.environ.items() if not k.startswith(('AIDE_','AI_','COMPOSE_'))}
            for file in ['compose.yaml','docker/compose.offline.yaml']:
                for enabled in [False,True]:
                    env=dict(base,AIDE_IMAGE='aide:synthetic-fixture',AIDE_LOCAL_ROOT=t,AIDE_WORKSPACE=t,AIDE_CONTEXT=t)
                    if enabled: env.update(AIDE_ASSISTANT_ENGINE='mixsoul',AIDE_MIXSOUL_LOCAL_EMBEDDING=json.dumps(CFG))
                    r=subprocess.run(['docker','compose','--project-directory',t,'-f',str(ROOT/file),'config','--format','json'],
                                     env=env,text=True,capture_output=True,check=True)
                    actual=json.loads(r.stdout)['services']['aide']['environment']
                    self.assertEqual(actual['AIDE_ASSISTANT_ENGINE'],'mixsoul' if enabled else 'builtin')
                    self.assertEqual(actual['AIDE_MIXSOUL_LOCAL_EMBEDDING'],json.dumps(CFG) if enabled else '')


if __name__=='__main__': unittest.main()
