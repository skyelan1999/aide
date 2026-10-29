#!/usr/bin/env node
'use strict';
// Aide launcher bootstrap. Fixed component, verified image payload, argv-only
// native execution. No shell, PowerShell, browser, login or provider mutation.
const fs=require('node:fs'),path=require('node:path'),os=require('node:os'),crypto=require('node:crypto');
const {execFileSync,spawn}=require('node:child_process');
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
function target(platform=process.platform,arch=process.arch){const a={x64:'amd64',arm64:'arm64'}[arch];if(!a||!['win32','darwin','linux'].includes(platform))throw Error('Unsupported host platform');return `codex-bridge-${platform==='win32'?'windows':platform}-${a}${platform==='win32'?'.exe':''}`;}
function verify(file,sums){const name=path.basename(file),line=sums.split(/\r?\n/).find(x=>x.trim().endsWith('  '+name));if(!line||!/^[a-f0-9]{64}  /.test(line))throw Error('Missing component checksum');const info=fs.lstatSync(file);if(!info.isFile()||info.isSymbolicLink())throw Error('Invalid component file');const sha=crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');if(sha!==line.slice(0,64))throw Error('Component checksum mismatch');return sha;}
function noLinks(file){let p=path.resolve(file);for(;;){if(fs.existsSync(p)&&fs.lstatSync(p).isSymbolicLink())throw Error('Host component state must not use symbolic links');const parent=path.dirname(p);if(parent===p)break;p=parent;}}
async function rpc(connection,route,method='GET',bind='127.0.0.1'){const value=JSON.parse(fs.readFileSync(connection,'utf8'));if(value.protocol!==1||value.service!=='codex-bridge'||value.port!==17888||!/^[a-f0-9]{64}$/.test(value.token))throw Error('Invalid component connection');const response=await fetch(`http://${bind}:17888/api/${route}`,{method,headers:{Host:'host.docker.internal:17888',Authorization:'Bearer '+value.token,'X-Manager-Owner-UI':'1'},redirect:'error',signal:AbortSignal.timeout(2500)});if(!response.ok)throw Error('Host component not ready');return response.json();}
async function main(argv){
 const action=argv.shift();if(!['start','stop'].includes(action))throw Error('Expected start or stop');
 const options={};while(argv.length){const k=argv.shift();if(!['--root','--image','--docker'].includes(k)||!argv.length||options[k])throw Error('Invalid launcher arguments');options[k]=argv.shift();}
 if(!options['--root']||!path.isAbsolute(options['--root']))throw Error('Absolute Aide root required');
 const root=options['--root'], docker=options['--docker']||'docker';
 const state=path.join(root,'.aide-data','host-components'),connection=path.join(root,'.aide-data','storage','data','host-components','codex-bridge.json');noLinks(state);noLinks(connection);
 let bind='127.0.0.1';
 const run=args=>execFileSync(docker,args,{encoding:'utf8',timeout:30000,stdio:['ignore','pipe','pipe'],windowsHide:true}).trim();
 if(process.platform==='linux'){
  // Bind only the local Docker bridge interface, never all LAN interfaces.
  const gateway=run(['network','inspect','bridge','--format','{{(index .IPAM.Config 0).Gateway}}']);
  if(!/^(10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.)/.test(gateway)||!os.networkInterfaces||!Object.values(os.networkInterfaces()).flat().some(x=>x?.address===gateway))throw Error('No local private Docker gateway found');bind=gateway;
 }
 if(action==='stop'){await rpc(connection,'quit','POST',bind);console.log('Codex host component stopped.');return;}
 if(!options['--image'])throw Error('Selected Aide image required');
 const image=run(['image','inspect','--format','{{.Id}}',options['--image']]);if(!/^sha256:[a-f0-9]{64}$/.test(image))throw Error('Invalid image identity');
 fs.mkdirSync(state,{recursive:true,mode:0o700});const destination=path.join(state,image.slice(7));noLinks(destination);fs.mkdirSync(destination,{recursive:true,mode:0o700});
 const name=target(),binary=path.join(destination,name),sums=path.join(destination,'SHA256SUMS'),versionFile=path.join(destination,'VERSION');
 if(!fs.existsSync(binary)||!fs.existsSync(sums)||!fs.existsSync(versionFile)){
  const temporary=fs.mkdtempSync(path.join(state,'extract-'));let container;
  try{container=run(['create','--network','none',image]);if(!/^[a-f0-9]{64}$/.test(container))throw Error('Invalid extraction container');
   for(const item of [name,'SHA256SUMS','VERSION'])run(['cp',container+':/opt/aide/host-components/'+item,path.join(temporary,item)]);
   verify(path.join(temporary,name),fs.readFileSync(path.join(temporary,'SHA256SUMS'),'utf8'));
   for(const item of [name,'SHA256SUMS','VERSION'])fs.renameSync(path.join(temporary,item),path.join(destination,item));
  }finally{if(container&&/^[a-f0-9]{64}$/.test(container)){try{run(['rm',container]);}catch{}}fs.rmSync(temporary,{recursive:true,force:true});}
 }
 verify(binary,fs.readFileSync(sums,'utf8'));const version=fs.readFileSync(versionFile,'utf8').trim();if(!/^\d+\.\d+\.\d+$/.test(version))throw Error('Invalid component version');
 try{const status=await rpc(connection,'status','GET',bind);if(status.service!=='aide-codex-bridge-manager')throw Error('Unexpected service');if(status.version===version){console.log('Codex host component ready (reused).');return;}await rpc(connection,'quit','POST',bind);for(let i=0;i<60&&fs.existsSync(connection);i++)await sleep(100);if(fs.existsSync(connection))throw Error('Previous host component is still stopping');}catch(e){if(e.code==='ENOENT'||e.cause?.code==='ECONNREFUSED'){if(fs.existsSync(connection))fs.unlinkSync(connection);}else if(fs.existsSync(connection))throw Error('Existing host component cannot be verified; inspect it before retrying');}
 if(process.platform!=='win32')fs.chmodSync(binary,0o700);
 const log=fs.openSync(path.join(state,'manager.log'),'a',0o600);
 const child=spawn(binary,['--aide-managed',connection,bind],{detached:true,stdio:['ignore',log,log],windowsHide:true});let launchError;child.on('error',e=>{launchError=e;});child.unref();fs.closeSync(log);
 for(let i=0;i<80;i++){if(launchError)throw Error('Cannot launch Codex host component');await sleep(100);try{const status=await rpc(connection,'status','GET',bind);if(status.service==='aide-codex-bridge-manager'&&status.version===version){console.log('Codex host component ready; manage it in the Codex Bridge plugin.');return;}}catch{}}
 throw Error('Codex host component did not become ready; see .aide-data/host-components/manager.log');
}
module.exports={target,verify,noLinks,main};
if(require.main===module)main(process.argv.slice(2)).catch(e=>{console.error('Codex Bridge: '+(e?.code?'launcher operation failed ('+e.code+')':e.message));process.exitCode=1;});
