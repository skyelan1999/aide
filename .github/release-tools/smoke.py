import subprocess,json,time,sys,hashlib
from pathlib import Path
image,arch,commit=sys.argv[1:];name='aide-windows-fix-release-'+arch
import re
web = Path('internal/server/web')
scripts = re.findall(r'<script[^>]*\bsrc=[\"\']([^\"\']+)', (web/'index.html').read_text())
paths = ['/', '/app.js', '/plugin-market.js', '/settings-schema.json', '/settings-navigation.js', '/workbench.css', *scripts]
assets = {p:hashlib.sha256((web/('index.html' if p=='/' else p.split('?',1)[0].lstrip('/'))).read_bytes()).hexdigest() for p in paths}
program=r'''
const fs=require('fs'),https=require('https'),crypto=require('crypto');
const ca=fs.readFileSync('/data/certs/cert.pem'),token=fs.readFileSync('/data/auth/access-token','utf8').trim();
function req(path,method='GET',body,auth=true){return new Promise((resolve,reject)=>{const q=https.request({hostname:'127.0.0.1',servername:'localhost',port:8080,path,method,ca,headers:{Host:'localhost:8080',...(auth?{Authorization:'Bearer '+token}:{}),'Content-Type':'application/json',Origin:'https://localhost:8080'}},r=>{let chunks=[];r.on('data',b=>chunks.push(b));r.on('end',()=>{const raw=Buffer.concat(chunks);let data;try{data=JSON.parse(raw)}catch{}resolve({status:r.statusCode,data,type:r.headers['content-type'],sha:crypto.createHash('sha256').update(raw).digest('hex')})})});q.on('error',reject);q.end(body?JSON.stringify(body):undefined)})}
(async()=>{
const health=await req('/healthz','GET',undefined,false),denied=await req('/api/sessions','GET',undefined,false),config=await req('/api/config'),slots=await req('/api/updates/slots');
if(health.status!==200||health.data.integrity!=='ok'||denied.status!==401||config.status!==200||slots.data.runtimeMode!=='release-image')throw Error(JSON.stringify({health:health.status,integrity:health.data?.integrity,denied:denied.status,config:config.status,slots:slots.data}));
const before=await req('/api/plugin-routing');const saved=await req('/api/plugin-routing','PUT',{autoInstall:!before.data.autoInstall,generation:before.data.generation});const after=await req('/api/plugin-routing');if(saved.status!==200||after.data.autoInstall===before.data.autoInstall)throw Error('routing policy save');
const expected=EXPECTED_ASSETS;for(const [p,sum]of Object.entries(expected)){const r=await req(p);if(r.status!==200||r.sha!==sum)throw Error('embedded asset mismatch '+p);if(p.includes('csharp.min.js')&&!/javascript/.test(r.type))throw Error('C# asset MIME mismatch')}
console.log(JSON.stringify({status:'pass',embeddedWorkbenchAssetsVerified:true,embeddedAssetCount:Object.keys(expected).length,csharpAssetVerified:true,health:health.status,integrity:health.data.integrity,unauthenticated:denied.status,version:config.data.version,commit:config.data.commit||config.data.buildCommit,runtimeMode:slots.data.runtimeMode,routingPolicySave:true,privatePluginInstalled:false}));
})().catch(e=>{console.error(e);process.exitCode=1});
'''.replace('EXPECTED_ASSETS',json.dumps(assets))
try:
 subprocess.run(['docker','run','-d','--network','none','--memory','1g','--cpus','2','--name',name,image],check=True,stdout=subprocess.DEVNULL)
 for _ in range(30):
  r=subprocess.run(['docker','exec',name,'curl','--cacert','/data/certs/cert.pem','-fsS','https://localhost:8080/healthz'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  if r.returncode==0:break
  time.sleep(1)
 else:raise RuntimeError('health timeout')
 r=subprocess.run(['docker','exec','-i',name,'node'],input=program,text=True,capture_output=True);assert r.returncode==0,r.stderr
 d=json.loads(r.stdout);assert d['version']=='0.1.17.5 RC3' and d['commit']==commit,d;print(json.dumps(d))
finally:subprocess.run(['docker','rm','-f',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
