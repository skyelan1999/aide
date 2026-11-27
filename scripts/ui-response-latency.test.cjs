'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const {readAppSource,sourceIndex}=require('./fixtures/app-source.cjs');
const source=process.argv[2]?fs.readFileSync(process.argv[2],'utf8'):readAppSource();
function excerpt(start,end){const a=sourceIndex(source,start),b=sourceIndex(source,end,a);return source.slice(a,b);}
function deferred(){let resolve,reject;const promise=new Promise((a,b)=>{resolve=a;reject=b;});return {promise,resolve,reject};}
function findNode(root,predicate){
 if(predicate(root))return root;
 for(const child of root.children){
  const found=findNode(child,predicate);
  if(found)return found;
 }
 return null;
}
function node(tag='div',cls='',text=''){
 const classes=new Set(cls.split(' '));return {tag,children:[],dataset:{},attributes:{},style:{},textContent:text,classList:{contains:c=>classes.has(c),toggle(c,value){if(value===undefined)value=!classes.has(c);if(value)classes.add(c);else classes.delete(c);return value;}},
 contains(target){return !!target && !!findNode(this,child=>child===target);},
 append(...items){this.children.push(...items);},replaceChildren(...items){this.paints=(this.paints||0)+1;this.children=items.flatMap(n=>n.tag==='fragment'?n.children:[n]);},setAttribute(k,v){this.attributes[k]=v;},removeAttribute(k){delete this.attributes[k];},focus(){this.focused=true;}};
}
async function settings(){
 const nodes=Object.fromEntries(['settings-sheet','settings-backdrop','settings-sheet-close','settings-content','settings-nav'].map(k=>[k,node()]));let request=deferred(),requests=0,renders=0,frames=[];
 const c={flushSettingsAutosaves:async()=>{},cleanupSettingsRefreshers(){},settingsPanel:{schema:null,refreshers:[],trigger:null},$:id=>nodes[id],document:{querySelectorAll:()=>[]},closeTrajectory(){},t:x=>x,el:node,fetch:()=>{requests++;return request.promise;},renderSettingsSheet(){renders++;},requestAnimationFrame:fn=>frames.push(fn)};vm.createContext(c);
 vm.runInContext(excerpt('async function loadSettingsSchema() {','function renderSegmentedControl('),c);vm.runInContext(excerpt('function setSettingsOpen(open) {','function bindSettingsTrigger('),c);
 const first=c.openSettingsSheet();assert.equal(nodes['settings-sheet'].classList.contains('open'),true,'settings must open before schema download');assert.equal(nodes['settings-content'].attributes['aria-busy'],'true');assert.equal(renders,0);
 c.closeSettingsSheet();const second=c.openSettingsSheet();assert.equal(requests,1,'overlapping opens share one schema fetch');request.resolve({ok:true,json:async()=>({sections:[]})});await Promise.all([first,second]);assert.equal(renders,1,'closed predecessor cannot render into a newer opening');assert.equal(nodes['settings-content'].attributes['aria-busy'],undefined);
 c.closeSettingsSheet();for(const fn of frames)fn();await c.openSettingsSheet();assert.equal(requests,1,'cached schema opens without new network');assert.equal(renders,2);
 c.settingsPanel.schema=null;c.closeSettingsSheet();request=deferred();const failed=c.openSettingsSheet();request.resolve({ok:false});await assert.rejects(failed);assert.equal(nodes['settings-sheet'].classList.contains('open'),true);assert.equal(nodes['settings-content'].attributes['aria-busy'],undefined);request=deferred();const retry=c.openSettingsSheet();request.resolve({ok:true,json:async()=>({sections:[]})});await retry;assert.equal(requests,3,'failed schema download can be retried');
 console.log('PASS settings immediate visibility, loading/failure, fetch deduplication, close/reopen ownership and cached opening');
}
async function sessions(){
 const list=node(),header=node(),requests=[];let scope='A',reply=null,prefill='';const state={token:'test',session:{id:'main',title:'新会话'},pendingSessionId:''};
 const doc={activeElement:null,body:node(),querySelectorAll:()=>[],documentElement:{lang:'zh-CN'},createDocumentFragment:()=>node('fragment'),getElementById:id=>findNode(list,child=>child.id===id)};
 const c={state,recoveryScope:()=>scope,subGroupState:{},$:id=>id==='session-title'?header:list,el:node,t:(s,n)=>s.replace('{0}',n),renderAssistantEntry(){},renderSessionCompactionIndicators(){},action:fn=>fn,document:doc,window:{innerWidth:800,innerHeight:600,prompt:(_label,value)=>{prefill=value;return reply;}},leaveUISurface(){},setTimeout(){},api:(url,options)=>{const d=deferred();requests.push({url,options,...d});return d.promise;}};vm.createContext(c);
 vm.runInContext(excerpt('function sessionStatusPresentation','let sessionListRequest='),c);
 vm.runInContext(excerpt('let sessionListRequest=','// 全局 SSE（#60）'),c);
 const active=[{id:'main',number:42,title:'Main',preview:'Latest request',status:'completed',checked:true}],archived=Array.from({length:5},(_,i)=>({id:'sub'+i,parentId:'main',title:i?'Child '+i:'',autoArchived:true}));
 const first=c.loadSessions();requests[0].resolve(active);requests[1].resolve(archived);await first;
 const mainItem=list.children[0],preview=findNode(mainItem,n=>n.textContent==='Latest request');
 assert.equal(header.textContent,'Main');
 assert.ok(preview,'session rows expose the bounded recent prompt preview');
 assert.equal(doc.getElementById('session-select-main').attributes['aria-label'],'#42 Main','virtualized title has an explicit accessible name');
 assert.equal(doc.getElementById('session-select-sub0').attributes['aria-label'],'未命名会话','unnumbered untitled row remains named');
 const toggle=()=>list.children.find(n=>n.children.some(child=>child.textContent.startsWith('子会话'))),more=()=>list.children.find(n=>n.children.some(child=>child.textContent.startsWith('展开全部')));
 assert.equal(list.children.length,6);const before=requests.length;
 doc.activeElement=toggle();toggle().onclick();
 assert.equal(requests.length,before,'collapse must not fetch two lists');assert.equal(list.children.length,2,'collapse paints synchronously');
 assert.ok(toggle().focused,'keyboard focus follows the rebuilt disclosure');
 assert.equal(toggle().attributes['aria-expanded'],'false');
 doc.activeElement=node();toggle().onclick();assert.equal(list.children.length,6);
 assert.equal(toggle().focused,undefined,'external focus stays outside the list');
 more().onclick({stopPropagation(){}});assert.equal(list.children.length,8,'show all children without waiting for network');
 const paints=list.paints;let offset=requests.length;const unchanged=c.loadSessions();requests[offset].resolve(active);requests[offset+1].resolve(archived);await unchanged;assert.equal(list.paints,paints,'identical snapshots preserve existing DOM');
 offset=requests.length;const stale=c.loadSessions(),fresh=c.loadSessions();requests[offset+2].resolve([{...active[0],title:'潮汐来信分集剧本',preview:'做了吗'}]);requests[offset+3].resolve([]);await fresh;const current=list.children[0];requests[offset].resolve(active);requests[offset+1].resolve(archived);await stale;assert.equal(list.children[0],current,'out-of-order response cannot replace newest list');
 assert.equal(header.textContent,'潮汐来信分集剧本','late server title reaches the open header independently of the latest prompt');
 assert.equal(state.session.title,header.textContent);
 assert.equal(current.dataset.sessionTitle,header.textContent);
 assert.ok(findNode(current,n=>n.textContent==='做了吗'),'latest follow-up remains a preview');
 offset=requests.length;const other=c.loadSessions();scope='B';requests[offset].resolve(active);requests[offset+1].resolve(archived);await other;assert.equal(list.children[0],current,'old workspace response discarded');
 offset=requests.length;const oldToken=c.loadSessions();state.token='replacement';requests[offset].resolve(active);requests[offset+1].resolve(archived);await oldToken;assert.equal(list.children[0],current,'old token response discarded');
 state.pendingSessionId='sub1';header.textContent='另一会话';
 offset=requests.length;const crossing=c.loadSessions();requests[offset].resolve(active);requests[offset+1].resolve(archived);await crossing;
 assert.equal(header.textContent,'另一会话','a list response cannot replace the incoming session heading');
 assert.equal(state.session.title,'潮汐来信分集剧本');
 state.pendingSessionId='';
 offset=requests.length;const rename=c.loadSessions();state.session.title='手工项目名';header.textContent=state.session.title;
 requests[offset].resolve(active);requests[offset+1].resolve(archived);await rename;
 assert.equal(header.textContent,'手工项目名','a newer detail rename survives an older list request');
 offset=requests.length;const archivedSelection=c.loadSessions();requests[offset].resolve([]);requests[offset+1].resolve([{id:'main',title:'文档目录清单',preview:'list_files'}]);await archivedSelection;
 assert.equal(state.session.title,'文档目录清单','archived selections receive the same server title');
 const renameButton=()=>{
   const more=doc.getElementById('session-menu-button-main');more.getBoundingClientRect=()=>({right:48,bottom:80,top:56});
   more.onclick({stopPropagation(){}});
   return findNode(doc.body.children.at(-1),n=>n.textContent==='重命名');
 };
 offset=requests.length;const listAgain=c.loadSessions();requests[offset].resolve(active);requests[offset+1].resolve(archived);await listAgain;
 offset=requests.length;await renameButton().onclick();assert.equal(prefill,'Main');assert.equal(requests.length,offset,'cancel does not send a rename');
 reply=' ';await assert.rejects(renameButton().onclick(),/120/);assert.equal(requests.length,offset,'blank name is rejected before sending');
 reply=' 手工项目名 ';const saved=renameButton().onclick();assert.deepEqual(requests[offset].options.method,'PATCH');
 assert.deepEqual(JSON.parse(requests[offset].options.body),{title:'手工项目名'});requests[offset].resolve({});
 await new Promise(resolve=>setImmediate(resolve));
 requests[offset+1].resolve([{...active[0],title:'手工项目名'}]);requests[offset+2].resolve([]);await saved;
 assert.equal(header.textContent,'手工项目名');
 console.log('PASS local group expansion, atomic list paint, identical-snapshot DOM reuse and latest workspace/token request ownership');
}
(async()=>{if(!process.argv[3]||process.argv[3]==='settings')await settings();if(!process.argv[3]||process.argv[3]==='sessions')await sessions();})().catch(error=>{console.error(error);process.exitCode=1;});
