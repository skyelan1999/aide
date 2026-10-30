#!/usr/bin/env python3
"""Opt-in real Docker acceptance of all seven POSIX launcher interfaces.

Run only against a fresh disposable source checkout. Uses a new HOME, Docker
client config, data directories, Compose project, port and Buildx cache. Does
not prune the Docker engine or dispatch a real release. Raw logs stay private.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import ssl
import subprocess
import time
import urllib.request


class Acceptance:
    def __init__(self, root, evidence, pip_index=None):
        self.root=root.resolve();self.evidence=evidence.resolve()
        self.scope=hashlib.sha256(os.fsencode(str(self.root))).hexdigest()[:20]
        self.image='aide:source-'+self.scope;self.builder='aide-'+self.scope
        self.extension='.command' if os.uname().sysname=='Darwin' else '.sh'
        self.pip_index=pip_index
        self.commands=[];self.env={};self.result={'status':'in_progress','root':str(self.root),'scope':self.scope,'commands':self.commands}

    def save(self):
        self.evidence.mkdir(parents=True,exist_ok=True)
        (self.evidence/'result.json').write_text(json.dumps(self.result,ensure_ascii=False,indent=2)+'\n')

    def docker(self,*args,check=True):
        p=subprocess.run([self.docker_exe,*args],env=self.env or None,cwd=self.root,stdin=subprocess.DEVNULL,
                         capture_output=True,text=True,timeout=60)
        if check and p.returncode:raise RuntimeError('Docker acceptance probe failed: '+str(args[:2])+'; '+p.stderr[-1000:])
        return p.stdout.strip() if check else p

    def prepare(self):
        if subprocess.check_output(['git','status','--porcelain'],cwd=self.root,text=True).strip():
            raise RuntimeError('A clean committed source checkout is required')
        branch=subprocess.check_output(['git','branch','--show-current'],cwd=self.root,text=True).strip()
        if branch=='main':raise RuntimeError('Use a disposable acceptance branch/detached checkout, never the user main checkout')
        for name in ('.env','.aide-data','.data','.cache'):
            if (self.root/name).exists():raise RuntimeError('Not a fresh installation: '+name)
        if (self.evidence/'environment.json').exists():raise RuntimeError('Use a new acceptance evidence directory')
        self.evidence.mkdir(parents=True,exist_ok=True);self.evidence.chmod(0o700)
        self.docker_exe=shutil.which('docker')
        if not self.docker_exe:raise RuntimeError('Docker CLI is required')
        endpoint=subprocess.check_output([self.docker_exe,'context','inspect','--format','{{.Endpoints.docker.Host}}'],text=True).strip()
        if not endpoint.startswith('unix://'):raise RuntimeError('Acceptance requires the local Unix Docker engine, not a remote context')
        plugins=json.loads(subprocess.check_output([self.docker_exe,'info','--format','{{json .ClientInfo.Plugins}}'],text=True))
        plugin_dirs=sorted({str(Path(p['Path']).parent) for p in plugins if p.get('Path')})
        for name in ('home','docker','tools','tmp','workspace','context','local'):(self.evidence/name).mkdir()
        for name,target in (('docker',self.docker_exe),('python3',shutil.which('python3')),('bash','/bin/bash'),('git',shutil.which('git'))):
            if not target:raise RuntimeError('Missing prerequisite '+name)
            (self.evidence/'tools'/name).symlink_to(Path(target).resolve())
        (self.evidence/'docker/config.json').write_text(json.dumps({'cliPluginsExtraDirs':plugin_dirs}))
        with socket.socket() as probe:
            probe.bind(('127.0.0.1',0));port=probe.getsockname()[1]
        locale='en_US.UTF-8' if os.uname().sysname=='Darwin' else 'C.UTF-8'
        self.env=dict(HOME=str(self.evidence/'home'),PATH=str(self.evidence/'tools')+os.pathsep+os.defpath,
            TMPDIR=str(self.evidence/'tmp'),LANG=locale,LC_ALL=locale,DOCKER_HOST=endpoint,DOCKER_CONFIG=str(self.evidence/'docker'),
            COMPOSE_PROJECT_NAME='aide654-'+self.scope[:12],COMPOSE_FILE='compose.yaml',AIDE_IMAGE=self.image,
            AIDE_PORT=str(port),AIDE_OPEN_BROWSER='0',AIDE_WORKSPACE=str(self.evidence/'workspace'),
            AIDE_CONTEXT=str(self.evidence/'context'),AIDE_LOCAL_ROOT=str(self.evidence/'local'),BUILDKIT_PROGRESS='plain')
        assert 'PROJECT_DIR' not in self.env
        if self.pip_index:self.env['PIP_INDEX_URL']=self.pip_index
        (self.evidence/'environment.json').write_text(json.dumps(self.env,ensure_ascii=False,indent=2)+'\n')
        (self.evidence/'environment.json').chmod(0o600)
        self.baseline_images=json.loads('['+self.docker('image','ls','--format','json').replace('\n',',')+']')
        assert self.docker('buildx','inspect',self.builder,check=False).returncode!=0,'Own builder already exists'
        assert self.docker('image','inspect',self.image,check=False).returncode!=0,'Own runtime already exists'
        self.result.update(platform=os.uname().sysname,extension=self.extension,port=port,
            head=subprocess.check_output(['git','rev-parse','HEAD'],cwd=self.root,text=True).strip(),
            initial_state='new HOME/config/data/project/image/builder; no inherited PROJECT_DIR or model credentials',
            pip_index='explicit CLI override' if self.pip_index else 'repository default',
            windows='DEFERRED_BY_USER: no Windows host currently available')
        self.save()

    def run(self,action,expected=0):
        index=len(self.commands)+1;log=self.evidence/('%02d-%s.log'%(index,action))
        record={'action':action,'entry':action+self.extension,'expected_exit':expected,'log':log.name,
                'started':datetime.now(timezone.utc).isoformat()}
        self.commands.append(record);self.save();print('RUN '+record['entry'],flush=True)
        with log.open('wb') as output:
            p=subprocess.run([str(self.root/record['entry'])],cwd=self.evidence/'home',env=self.env,
                             stdin=subprocess.DEVNULL,stdout=output,stderr=subprocess.STDOUT,timeout=7200)
        record.update(exit=p.returncode,seconds=round((datetime.now(timezone.utc)-datetime.fromisoformat(record['started'])).total_seconds(),2))
        self.save()
        if p.returncode!=expected:raise RuntimeError('%s exit %s, expected %s; see %s'%(record['entry'],p.returncode,expected,log))
        print('PASS '+record['entry']+' exit '+str(p.returncode),flush=True)
        return log.read_text(errors='replace')

    def runtime(self):
        cid=self.docker('compose','ps','--status','running','-q','aide')
        if not cid:return None
        value=json.loads(self.docker('inspect',cid))[0]
        assert value['Config']['Labels']['com.docker.compose.project.working_dir']==str(self.root)
        return {'container':cid,'image':value['Image']}

    def health(self):
        cert=self.root/'.aide-data/storage/data/certs/cert.pem'
        context=ssl.create_default_context(cafile=str(cert))
        opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPSHandler(context=context))
        base='https://localhost:'+self.env['AIDE_PORT']
        with opener.open(base+'/healthz',timeout=10) as response:body=json.load(response)
        assert body['service']=='aide' and body['status']=='ok'
        with opener.open(base+'/',timeout=10) as response:
            assert response.status==200 and b'<html' in response.read().lower()
        token=self.docker('compose','exec','-T','aide','sh','-c','cat /data/auth/access-token')
        request=urllib.request.Request(base+'/api/sessions',headers={'Authorization':'Bearer '+token})
        with opener.open(request,timeout=10) as response:
            assert response.status==200;json.load(response)
        self.result.setdefault('health_samples',[]).append({'instanceId':body.get('instanceId'),'https_certificate':'verified','html':200,'authenticated_sessions':200})
        self.save();return body

    def execute(self):
        self.prepare()
        text=self.run('verify',1);assert 'not running' in text
        text=self.run('release',1);assert 'clean main' in text
        self.run('stop');self.run('docker_clean')
        text=self.run('start',1);assert 'build.command' in text and 'start' in text
        assert self.docker('buildx','inspect',self.builder,check=False).returncode!=0
        self.run('build');first=self.runtime();assert first;self.health();self.run('verify')
        self.run('start');assert self.runtime()==first;self.health()
        sentinel=self.root/'.aide-data/storage/data/acceptance-654-sentinel'
        sentinel.write_text('fresh acceptance data must survive clean and docker_clean\n')
        expected=sentinel.read_bytes()
        for name in ('workspace','context','local'):(self.evidence/name/'sentinel').write_text('preserve '+name)
        self.run('stop');assert self.runtime() is None
        self.run('verify',1);self.run('start');self.health()
        self.run('clean');assert self.runtime();self.health();self.run('verify')
        assert sentinel.read_bytes()==expected,'clean removed persistent user data'
        self.run('docker_clean');assert self.runtime() is None
        assert sentinel.read_bytes()==expected,'docker_clean removed persistent user data'
        assert self.docker('buildx','inspect',self.builder,check=False).returncode!=0
        assert self.docker('image','inspect',self.image,check=False).returncode!=0
        for name in ('workspace','context','local'):assert (self.evidence/name/'sentinel').read_text()=='preserve '+name
        self.run('verify',1);self.run('start',1);self.run('stop')
        for value in self.baseline_images:
            assert self.docker('image','inspect',value['ID'],check=False).returncode==0,'Unrelated pre-existing image was removed'
        self.result.update(status='pass',release='real rejection gate tested; no delivery dispatched',
            persistent_data='sentinels preserved across clean and docker_clean',cleanup='owned runtime, image and builder removed; evidence/data retained')
        self.save();print('PASS all seven real POSIX launcher interfaces in isolated clean environment',flush=True)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root',type=Path,required=True)
    parser.add_argument('--evidence',type=Path,required=True)
    parser.add_argument('--allow-real-build',action='store_true',help='Allow two real cold builds and this isolated runtime lifecycle')
    parser.add_argument('--pip-index-url',help='Explicit existing build index override; ambient host proxy/model/index variables are not inherited')
    args=parser.parse_args()
    if not args.allow_real_build:parser.error('--allow-real-build is required')
    acceptance=Acceptance(args.root,args.evidence,args.pip_index_url)
    try:acceptance.execute()
    except Exception as error:
        acceptance.result.update(status='failed',error=str(error));acceptance.save();raise


if __name__=='__main__':main()
