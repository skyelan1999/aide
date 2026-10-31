'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/app.js','utf8');
function fn(name){let at=source.indexOf('function '+name+'(');assert(at>=0,name);if(source.slice(at-6,at)==='async ')at-=6;let end=source.indexOf('\n}',at);let first=source.slice(at,source.indexOf('\n',at));return first.endsWith('}')?first:source.slice(at,end+2);}
const nodes={};
function el(tag,cls='',text=''){return {tag,className:cls,textContent:text,children:[],dataset:{},value:'',checked:false,disabled:false,append(...items){for(const item of items){item.parentNode=this;this.children.push(item);}},prepend(...items){for(const item of items)item.parentNode=this;this.children.unshift(...items);},replaceChildren(){this.children=[];},set id(id){this._id=id;nodes[id]=this;},get id(){return this._id;},click(){downloads.push(this.download);}};}
for(const id of ['src-type','src-rw','src-fields','src-name','source-form','source-dialog'])nodes[id]=el('input');
nodes['src-type'].options=[];nodes['src-type'].value='relay';nodes['source-dialog'].close=()=>{};
const state={token:'synthetic',root:'context',source:'system-docs',sources:[],config:{}},requests=[],notices=[],downloads=[];
let fileLoads=0,fetcher,blobReady=async()=>({size:1});
const c={state,$:id=>nodes[id],el,t:(s,...a)=>s.replace(/\{(\d+)\}/g,(_,i)=>a[i]),action:fn=>fn,editingSourceID:'',Math,JSON,Error,setTimeout:fn=>fn(),toast:s=>notices.push(s),URL:{createObjectURL:()=> 'blob:test',revokeObjectURL(){}},document:{createElement:tag=>el(tag)},fetch:(...a)=>fetcher(...a),api:async(url,opts={})=>{requests.push({url,...opts});if(url.endsWith('/relay-probe'))return {online:true,rw:true,remoteWritable:false};return {};},loadSourcesList:async()=>{const last=requests.filter(x=>x.url==='/sources').at(-1);if(last)state.sources=JSON.parse(last.body).sources;},loadFiles:async()=>{fileLoads++;}};
vm.createContext(c);
c.lockScreen={locked:false};c.chatRecoveryEpoch=0;c.recoveryScope=()=> 'fixture';
for(const name of ['sourceCanSetRW','renderSourceFields','suggestedSourceName','sourceUpdatePayload','downloadSourceRelay','probeSourceRelay'])vm.runInContext(fn(name),c);
const form=source.slice(source.indexOf("$('source-form').onsubmit = action("),source.indexOf('/* ── Markdown 渲染'));
vm.runInContext(form,c);
(async()=>{
 c.renderSourceFields();assert.equal(nodes['src-rw'].disabled,false);assert.equal(nodes['src-rw'].checked,false);assert.equal(nodes['src-port'].value,'8767');assert.equal(nodes['src-relay-key'].type,'password');
 nodes['src-host'].value='example.test';nodes['src-remote'].value='/remote/refs';
 await nodes['source-form'].onsubmit({preventDefault(){}});
 assert.equal(fileLoads,0,'offline save performs no file connection');assert.equal(state.sources[0].rw,false);assert.equal(state.sources[0].config.port,8767);assert(!requests[0].body.includes('secrets'),'blank key generated on server');
 const src=state.sources[0];fetcher=async(url,options)=>{assert.match(url,/relay-script$/);assert.equal(options.headers.Authorization,'Bearer synthetic');return {ok:true,blob:blobReady};};
 await c.downloadSourceRelay(src);assert.equal(downloads.length,1);assert.match(downloads[0],/\.py$/);
 fetcher=async()=>({ok:true,blob:async()=>{state.token='changed';return {};}});
 await assert.rejects(c.downloadSourceRelay(src),/来源配置已变化/);assert.equal(downloads.length,1,'stale auth never downloads credential script');state.token='synthetic';
 fetcher=async()=>({ok:true,blob:async()=>{c.lockScreen.locked=true;return {};}});
 await assert.rejects(c.downloadSourceRelay(src),/来源配置已变化/);assert.equal(downloads.length,1,'locked workbench never exports pending credential script');c.lockScreen.locked=false;
 await c.probeSourceRelay(src);assert.match(notices.at(-1),/远程脚本只读/);
 c.editingSourceID=src.id;state.source=src.id;nodes['src-rw'].checked=true;nodes['src-relay-key'].value='replacement-key';
 await nodes['source-form'].onsubmit({preventDefault(){}});assert.equal(fileLoads,1);const saved=JSON.parse(requests.filter(x=>x.url==='/sources').at(-1).body);assert.equal(saved.sources[0].rw,true);assert.equal(saved.secrets[src.id].key,'replacement-key');
 c.editingSourceID='';nodes['src-type'].value='sftp';nodes['src-rw'].checked=false;c.renderSourceFields();
 nodes['src-sftp-mode'].value='relay';nodes['src-sftp-mode'].onchange();
 assert.equal(nodes['src-sftp-relay-host'].parentNode.hidden,false);assert.equal(nodes['src-sftp-relay-key'].type,'password');
 assert.equal(nodes['src-sftp-relay-port'].value,'8768');
 nodes['src-host'].value='sftp.internal';nodes['src-port'].value='22';nodes['src-user'].value='reader';nodes['src-remote'].value='/refs';nodes['src-password'].value='ssh-only-secret';
 nodes['src-key'].value='';nodes['src-sftp-relay-host'].value='bridge.example';const beforeLoads=fileLoads;await nodes['source-form'].onsubmit({preventDefault(){}});
 const relaySource=state.sources.at(-1);assert.equal(relaySource.type,'sftp');assert.equal(relaySource.rw,false);assert.equal(fileLoads,beforeLoads,'SFTP relay saves offline without accessing target');
 assert.equal(relaySource.config.relayPort,8768); // Target and relay endpoints remain distinct.
 fetcher=async()=>({ok:true,blob:async()=>({})});await c.downloadSourceRelay(relaySource);assert.match(downloads.at(-1),/^aide-sftp-relay-/);
 await c.probeSourceRelay(relaySource);assert.match(notices.at(-1),/认证和主机密钥/);assert.equal(fileLoads,beforeLoads);
 nodes['src-sftp-mode'].value='direct';nodes['src-sftp-mode'].onchange();assert.equal(nodes['src-sftp-relay-host'].parentNode.hidden,true);
 console.log('Relay UI PASS: default RO, offline save, credential download, stale-auth rejection, remote RO notice and configuration edit');
})().catch(e=>{console.error(e);process.exitCode=1;});
