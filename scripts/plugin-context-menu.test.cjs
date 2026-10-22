'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs');
const source=fs.readFileSync('internal/server/web/app.js','utf8'),code=source.slice(source.indexOf('let compareLeftSelection=null;'),source.indexOf('let fileOpenRequest=0;'));
const enabled={id:'compare',enabled:true,settings:{fileContextMenu:true},workbench:{protocol:1}};
function fixture(){
 const pending=[],opened=[],notices=[],state={plugins:[],token:'fixture-token',root:'workspace',config:{workspaceId:'A'},fileEntries:[{path:'a.txt',dir:false},{path:'b.txt',dir:false}],fileSelection:new Set(['a.txt'])};
 const element=(tag,cls='',text='')=>({tag,className:cls,textContent:text,children:[],style:{},offsetHeight:350,classList:{add(){}},append(...children){this.children.push(...children);},insertBefore(child,before){const i=this.children.indexOf(before);assert(i>=0);this.children.splice(i,0,child);},setAttribute(){}});
 const body=element('body'),c={state,lockScreen:{locked:false},chatRecoveryEpoch:0,recoveryScope:()=>state.config.workspaceId,fileCtxMenuEl:null,fileCtxSubmenuEl:null,innerWidth:1200,innerHeight:800,document:{body,addEventListener(){}},setTimeout:()=>1,
 api:()=>new Promise((resolve,reject)=>pending.push({resolve,reject})),el:element,t:x=>x,toast:x=>notices.push(x),action:fn=>fn,currentFileTarget:()=>({writable:true,source:''}),isSupportedArchivePath:()=>false,closeFileContextMenu:()=>{c.fileCtxMenuEl=null;},pluginWorkbenchServices:()=>({}),window:{AidePluginWorkbench:{open:async(p,s)=>opened.push({p,s})}}};
 for(const f of ['attachFileEntry','downloadFileEntry','openFileTransferPicker','beginInlineRename','showFileProperties','deleteFileEntry'])c[f]=()=>{};
 vm.createContext(c);vm.runInContext(code,c);
 const show=()=>{c.openFileContextMenu({clientX:10,clientY:10},{},{},state.fileEntries[0]);return c.fileCtxMenuEl;};
 const labels=m=>m.children.map(x=>x.textContent),answer=plugins=>pending.shift().resolve({plugins}),flush=()=>new Promise(resolve=>setImmediate(resolve)),click=node=>node.onclick({stopPropagation(){}});
 return {c,state,pending,opened,notices,show,labels,answer,flush,click};
}
test('native menu is immediate and default/disabled/absent/error/incompatible Compare stays absent',async()=>{
 for(const plugin of [null,{...enabled,enabled:false},{...enabled,settings:{}},{...enabled,settings:{fileContextMenu:false}},{...enabled,settings:{fileContextMenu:'true'}},{...enabled,error:'broken'},{...enabled,workbench:{protocol:2}}]){
  const f=fixture(),menu=f.show();assert(f.labels(menu).includes('下载'));assert(!f.labels(menu).some(x=>x.includes('Compare')));f.answer(plugin?[plugin]:[]);await f.flush();assert(!f.labels(menu).some(x=>x.includes('Compare')));
 }
});
test('explicit opt-in enables entries in order; both selection actions revalidate before use',async()=>{
 const f=fixture(),menu=f.show();f.answer([enabled]);await f.flush();const labels=f.labels(menu);assert(labels.indexOf('用 Compare 比较…')<labels.indexOf('复制到…'));
 const left=f.click(menu.children.find(x=>x.textContent==='设为 Compare 左侧'));f.answer([enabled]);await left;
 const next=f.show();f.answer([enabled]);await f.flush();assert(f.labels(next).includes('与 Compare 左侧比较'));
 const open=f.click(next.children.find(x=>x.textContent==='与 Compare 左侧比较'));f.answer([enabled]);await open;assert.equal(f.opened.length,1);assert.equal(f.opened[0].s.selection.left.path,'a.txt');
});
test('revocation after menu render blocks launch and left-side storage, then removes prior selection',async()=>{
 for(const label of ['用 Compare 比较…','设为 Compare 左侧']){
  const f=fixture(),menu=f.show();f.answer([enabled]);await f.flush();const click=f.click(menu.children.find(x=>x.textContent===label));f.answer([{...enabled,settings:{fileContextMenu:false}}]);await click;assert.equal(f.opened.length,0);assert.equal(f.notices.length,1);
  const next=f.show();f.answer([enabled]);await f.flush();assert(!f.labels(next).includes('与 Compare 左侧比较'));
 }
});
test('late lookup cannot populate closed/replaced menus or cross workspace, token, lock/recovery epoch',async()=>{
 for(const change of [f=>f.c.closeFileContextMenu(),f=>{f.c.fileCtxMenuEl={};},f=>{f.state.config.workspaceId='B';},f=>{f.state.token='other';},f=>{f.c.lockScreen.locked=true;},f=>{f.c.chatRecoveryEpoch++;}]){
  const f=fixture(),menu=f.show();change(f);f.answer([enabled]);await f.flush();assert(!f.labels(menu).some(x=>x.includes('Compare')));assert.equal(f.state.plugins.length,0);
 }
});
test('lookup failure preserves native actions; context root also uses opt-in',async()=>{
 const f=fixture(),menu=f.show();f.pending.shift().reject(Error('offline'));await f.flush();assert(f.labels(menu).includes('下载'));assert(!f.labels(menu).some(x=>x.includes('Compare')));
 const g=fixture();g.state.root='context';const reference=g.show();g.answer([enabled]);await g.flush();assert(g.labels(reference).includes('用 Compare 比较…'));
});
test('direct stale entry launch rechecks activation, settings and scope',async()=>{
 const f=fixture();const launch=f.c.openCompareSelection({left:{path:'a.txt',directory:false}},'A');f.state.config.workspaceId='B';f.answer([enabled]);await launch;assert.equal(f.opened.length,0);
 const g=fixture();const denied=g.c.openCompareSelection({left:{path:'a.txt',directory:false}},'A');g.answer([{...enabled,enabled:false}]);await denied;assert.equal(g.opened.length,0);
});
