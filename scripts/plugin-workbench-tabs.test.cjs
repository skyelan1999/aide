'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs');
const code=fs.readFileSync('internal/server/web/plugin-workbench-tabs.js','utf8');
function fixture(){
 const calls=[],errors=[],page={opener:{}};
 let blocked=false,workspace='workspace_1',popup=page;
 const window={AidePluginWorkbench:{selection(value){if(!value?.left?.path||value.left.path.includes('..'))return undefined;return value;}},open:(...args)=>(calls.push(args),popup)};
 const context={window,URL,URLSearchParams,location:{href:'https://localhost:9999/#token=private-fixture',hash:''}};
 vm.runInNewContext(code,context);
 return {tabs:window.AidePluginWorkbenchTabs,calls,errors,page,context,services:{t:x=>x,toast:e=>errors.push(e),blocked:()=>blocked,workspace:()=>workspace},scope:value=>workspace=value,block:()=>blocked=true,popup:()=>popup=null,foreign:()=>workspace='../foreign'};
}
const plugin={id:'compare',enabled:true,workbench:{protocol:1}};
test('workbench opens a fresh same-origin page with scoped selection and no credentials',()=>{
 const f=fixture(),selection={left:{source:'workspace',path:'docs/a b.md',directory:false}};
 assert.equal(f.tabs.open(plugin,{...f.services,selection}),true);
 const [address,target]=f.calls[0],url=new URL(address),route=f.tabs.read(url.hash);
 assert.equal(url.origin,'https://localhost:9999');assert.equal(url.pathname,'/');assert.equal(target,'_blank');
 assert.equal(route.id,'compare');assert.equal(route.workspace,'workspace_1');assert.equal(JSON.stringify(route.selection),JSON.stringify(selection));
 assert(!address.includes('private-fixture'));assert.equal(f.page.opener,null);
 f.tabs.open(plugin,f.services);assert.equal(f.calls.length,2);assert.equal(f.calls[1][1],'_blank');
});
test('blocked popup reports failure without replacing current workbench',()=>{
 const f=fixture();f.popup();assert.equal(f.tabs.open(plugin,f.services),false);assert.equal(f.errors.length,1);
 assert.equal(f.calls.length,1);
});
test('actual local and SSH workspace identities round-trip without becoming paths or credentials',()=>{
 for(const scope of ['local||','local|/Users/test/Library/Mobile Documents/工作区 & notes#1|','ssh|/srv/projects/研发|user@example:2222','local|C:\\Users\\test\\AI Workspace|']){
  const f=fixture();f.scope(scope);assert.equal(f.tabs.open(plugin,f.services),true);
  const url=new URL(f.calls[0][0]);assert.equal(url.pathname,'/');assert.equal(url.search,'');assert.equal(f.tabs.read(url.hash).workspace,scope);
  assert.equal(new URLSearchParams(url.hash.slice(1)).get('workspace'),scope);assert(!f.calls[0][0].includes('private-fixture'));
 }
});
test('oversized and control-bearing workspace identities never open a page or parse',()=>{
 for(const scope of ['', 'ssh||', 'local|', 'local|/workspace\nsecret|', 'ssh|/workspace|host\x00','local|/'+ 'a'.repeat(4096)+'|']){
  const f=fixture();f.scope(scope);assert.equal(f.tabs.open(plugin,f.services),false);assert.equal(f.calls.length,0);
  assert.equal(f.tabs.read('#'+new URLSearchParams({pluginWorkbench:'compare',workspace:scope})),null);
 }
});
test('locked, disabled, broken or malformed plugin routes cannot open a page',()=>{
 for(const variant of [{...plugin,enabled:false},{...plugin,error:'bad'},{...plugin,id:'../other'},{...plugin,workbench:{protocol:2}}]){
  const f=fixture();assert.equal(f.tabs.open(variant,f.services),false);assert.equal(f.calls.length,0);
 }
 const f=fixture();f.block();assert.equal(f.tabs.open(plugin,f.services),false);assert.equal(f.calls.length,0);
 const g=fixture();g.foreign();assert.equal(g.tabs.open(plugin,g.services),false);assert.equal(g.calls.length,0);
});
test('deep link parser rejects traversal, bad selections and missing workspace',()=>{
 const f=fixture();
 for(const hash of ['','#pluginWorkbench=compare','#pluginWorkbench=../other&workspace=valid','#pluginWorkbench=compare&workspace=../foreign','#pluginWorkbench=compare&workspace=valid&selection=%7Bbad','#pluginWorkbench=compare&workspace=valid&selection='+encodeURIComponent(JSON.stringify({left:{path:'../outside'}}))])assert.equal(f.tabs.read(hash),null,hash);
});
test('routed page rechecks workspace, current enabled plugin and lock before creating a bridge',async()=>{
 const app=fs.readFileSync('internal/server/web/app.js','utf8'),start=app.indexOf('let pluginPageOpening='),end=app.indexOf('async function loadPluginsPanel()',start);
 const calls=[],guides=[],errors=[],state={token:'fixture',plugins:[]},lockScreen={locked:false};let scope='local|/Users/test/工作区 with spaces|';
 const route={id:'compare',workspace:scope,selection:undefined};
 const context={window:{AidePluginGuide:{fileFirst:id=>id==='video-preview',open:async p=>guides.push(p.id)},AidePluginWorkbenchTabs:{read:()=>route},AidePluginWorkbench:{open:async(p,s)=>(calls.push({p,s}),true)}},state,lockScreen,recoveryScope:()=>scope,chatRecoveryEpoch:0,
  loadPluginsPanel:async()=>{state.plugins=[plugin];},pluginWorkbenchServices:()=>({}),pluginGuideServices:()=>({}),document:{title:''},t:x=>x,toast:e=>errors.push(e)};
 vm.runInNewContext(app.slice(start,end),context);
 lockScreen.locked=true;await context.openRoutedPluginWorkbench();assert.equal(calls.length,0);
 lockScreen.locked=false;scope='foreign';await context.openRoutedPluginWorkbench();assert.equal(calls.length,0);assert.equal(errors.length,1);
 scope='local|/Users/test/工作区 with spaces|';await context.openRoutedPluginWorkbench();assert.equal(calls.length,1);assert.equal(calls[0].s.inPage,true);
 await context.openRoutedPluginWorkbench();assert.equal(calls.length,1,'no duplicate after repeated unlock/reconciliation');
 calls[0].s.onClose();context.loadPluginsPanel=async()=>{state.plugins=[{...plugin,enabled:false}];};await context.openRoutedPluginWorkbench();assert.equal(calls.length,1);assert.deepEqual(guides,['compare']);
 route.id='video-preview';context.loadPluginsPanel=async()=>{state.plugins=[{...plugin,id:'video-preview'}];};await context.openRoutedPluginWorkbench();assert.equal(calls.length,1);assert.deepEqual(guides,['compare','video-preview']);
});
test('manual login resumes a plugin deep link after authentication and closes the login dialog',async()=>{
 const app=fs.readFileSync('internal/server/web/app.js','utf8');const line=app.split('\n').find(s=>s.startsWith("$('login-form').onsubmit ="));
 const calls=[],nodes={'login-form':{},'access-token':{value:'fixture-only'},'login-dialog':{open:true,close(){this.open=false;calls.push('close');}},'login-error':{}};
 const context={$:id=>nodes[id],state:{},initialize:async()=>calls.push('initialize'),localStorage:{setItem:(k,v)=>calls.push(k)},openRoutedPluginWorkbench:async()=>{assert.equal(nodes['login-dialog'].open,false);calls.push('route');}};
 vm.runInNewContext(line,context);await nodes['login-form'].onsubmit({preventDefault(){}});
 assert.deepEqual(calls,['initialize','aide-token','close','route']);assert.equal(nodes['access-token'].value,'');
});
