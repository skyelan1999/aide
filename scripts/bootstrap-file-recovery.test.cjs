'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const app=fs.readFileSync('internal/server/web/app.js','utf8');
const code=app.slice(app.indexOf('function fileListRequestKey()'),app.indexOf('function currentFileTarget()'));
class Element {
  constructor(){this.children=[];this.open=false;}
  append(...nodes){this.children.push(...nodes);}
  replaceChildren(...nodes){this.children=nodes;}
  setAttribute(k,v){this[k]=v;}
  showModal(){this.open=true;}
}
function fixture(fetch){
  const nodes=Object.fromEntries(['files','file-path','new-file','login-dialog'].map(id=>[id,new Element()]));
  const state={root:'context',source:'missing',dir:'.',fileSearch:'',fileSearchScope:'all',fileSearchMatch:'name',sources:[{id:'missing',name:'Unavailable'}],fileEntries:[{path:'stale.md'}],fileSelection:new Set(['stale.md']),fileDirs:{}};
  let scope='workspace1',renders=0,saved=0;
  const context={state,fetch,$:id=>nodes[id],document:{querySelector:()=>null},recoveryScope:()=>scope,currentFileTarget:()=>({writable:true}),el:(_tag,_cls,text)=>Object.assign(new Element(),{textContent:text}),t:(text,...args)=>text.replace(/\{(\d+)\}/g,(_,i)=>args[i]),action:fn=>async()=>{try{await fn();}catch{}},syncFileSelection(){},renderFileEntries(){renders++;nodes.files.replaceChildren();},scheduleChatRecovery(){saved++;}};
  vm.createContext(context);vm.runInContext(code,context);
  return {state,nodes,context,setScope:v=>scope=v,renders:()=>renders,saved:()=>saved};
}
const response=(status,data)=>({ok:status===200,status,headers:{get:()=>null},json:async()=>data});
(async()=>{
  let calls=0;
  const unavailable=fixture(async()=>{calls++;return response(400,{error:'<img src=x> unavailable'});});
  await unavailable.context.loadInitialFilePanel();
  assert.equal(calls,1,'root failure must not refetch same root');
  assert.equal(unavailable.nodes['login-dialog'].open,false);
  assert.equal(unavailable.state.source,'missing');
  assert.equal(unavailable.state.fileEntries.length,0);assert.equal(unavailable.state.fileSelection.size,0);
  assert.equal(unavailable.nodes['new-file'].disabled,true);
  assert.equal(unavailable.nodes.files.children[0].children[0].textContent,'文件列表暂不可用：<img src=x> unavailable','error rendered as plain text');
  unavailable.context.fetch=async()=>response(200,[]);
  await unavailable.nodes.files.children[0].children[1].onclick();
  assert.equal(unavailable.renders(),1,'successful empty retry clears error panel');
  assert.equal(unavailable.state.fileListError,false);assert.equal(unavailable.nodes['new-file'].disabled,false);
  const urls=[];const subdir=fixture(async url=>{urls.push(url);return urls.length===1?response(404,{error:'missing directory'}):response(200,[{path:'root.md'}]);});
  subdir.state.dir='gone';await subdir.context.loadInitialFilePanel();
  assert.equal(urls.length,2);assert.ok(urls[1].includes('source=missing&path=.'));assert.equal(subdir.state.source,'missing');
  assert.equal(subdir.renders(),1);
  for(const status of [401]){
    const auth=fixture(async()=>response(status,{error:'Unauthorized'}));auth.state.dir='gone';
    await assert.rejects(auth.context.loadInitialFilePanel(),error=>error.status===401);
    assert.equal(auth.nodes['login-dialog'].open,true);assert.equal(auth.state.dir,'gone');
  }
  const offline=fixture(async()=>{throw new Error('network offline');});await offline.context.loadInitialFilePanel();assert.equal(offline.state.fileListError,true);
  for(const change of [f=>f.state.source='other',f=>f.state.dir='new',f=>f.setScope('workspace2'),f=>f.state.fileSearch='new',f=>f.state.fileSearchScope='directory',f=>f.state.fileSearchMatch='content']){
    let reject;const f=fixture(()=>new Promise((_,r)=>reject=r));const loading=f.context.loadInitialFilePanel();change(f);reject(new Error('stale failure'));await loading;
    assert.equal(f.state.fileEntries[0].path,'stale.md');assert.equal(f.state.fileListError,undefined);assert.equal(f.nodes['login-dialog'].open,false);
  }
  const init=app.slice(app.indexOf('async function initialize()'),app.indexOf('\ninitialize()'));
  const f=fixture(async()=>response(400,{error:'source unavailable'}));let events=0,choices=0,workbench=0;
  Object.assign(f.context,{openRequestedWorkbench:async()=>{assert.equal(events,1);assert.equal(f.context.recoveryReady,true);workbench++;},syncPanelButtons(){},refreshConfig:async()=>{},window:{},loadSessions:async()=>{},loadProfiles:async()=>{},loadWorkspaceConfig:async()=>{},loadSourcesList:async()=>{},recoveryReady:false,refreshChatDraftChoices(){choices++;},setupGlobalEvents(){events++;}});
  vm.runInContext(init,f.context);await f.context.initialize();
  assert.equal(workbench,1,'deep link opens after initialization');assert.equal(events,1,'source error must not stop global event installation');assert.equal(choices,1);assert.equal(f.context.recoveryReady,true);
  console.log('bootstrap file recovery: PASS (source failure, auth, retry, stale context, initialization)');
})().catch(error=>{console.error(error);process.exitCode=1;});
