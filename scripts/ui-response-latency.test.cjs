'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync(process.argv[2]||'internal/server/web/app.js','utf8');
function excerpt(start,end){const a=source.indexOf(start),b=source.indexOf(end,a);assert.ok(a>=0&&b>a,start);return source.slice(a,b);}
function deferred(){let resolve,reject;const promise=new Promise((a,b)=>{resolve=a;reject=b;});return {promise,resolve,reject};}
function node(tag='div',cls='',text=''){
 const classes=new Set(cls.split(' '));return {tag,children:[],dataset:{},attributes:{},textContent:text,classList:{contains:c=>classes.has(c),toggle(c,value){if(value===undefined)value=!classes.has(c);if(value)classes.add(c);else classes.delete(c);return value;}},
 append(...items){this.children.push(...items);},replaceChildren(...items){this.paints=(this.paints||0)+1;this.children=items.flatMap(n=>n.tag==='fragment'?n.children:[n]);},setAttribute(k,v){this.attributes[k]=v;},removeAttribute(k){delete this.attributes[k];},focus(){this.focused=true;}};
}
async function settings(){
 const nodes=Object.fromEntries(['settings-sheet','settings-backdrop','settings-sheet-close','settings-content','settings-nav'].map(k=>[k,node()]));let request=deferred(),requests=0,renders=0,frames=[];
 const c={settingsPanel:{schema:null,refreshers:[],trigger:null},$:id=>nodes[id],document:{querySelectorAll:()=>[]},closeTrajectory(){},t:x=>x,el:node,fetch:()=>{requests++;return request.promise;},renderSettingsSheet(){renders++;},requestAnimationFrame:fn=>frames.push(fn)};vm.createContext(c);
 vm.runInContext(excerpt('async function loadSettingsSchema() {','function renderSegmentedControl('),c);vm.runInContext(excerpt('function setSettingsOpen(open) {','function bindSettingsTrigger('),c);
 const first=c.openSettingsSheet();assert.equal(nodes['settings-sheet'].classList.contains('open'),true,'settings must open before schema download');assert.equal(nodes['settings-content'].attributes['aria-busy'],'true');assert.equal(renders,0);
 c.closeSettingsSheet();const second=c.openSettingsSheet();assert.equal(requests,1,'overlapping opens share one schema fetch');request.resolve({ok:true,json:async()=>({sections:[]})});await Promise.all([first,second]);assert.equal(renders,1,'closed predecessor cannot render into a newer opening');assert.equal(nodes['settings-content'].attributes['aria-busy'],undefined);
 c.closeSettingsSheet();for(const fn of frames)fn();await c.openSettingsSheet();assert.equal(requests,1,'cached schema opens without new network');assert.equal(renders,2);
 c.settingsPanel.schema=null;c.closeSettingsSheet();request=deferred();const failed=c.openSettingsSheet();request.resolve({ok:false});await assert.rejects(failed);assert.equal(nodes['settings-sheet'].classList.contains('open'),true);assert.equal(nodes['settings-content'].attributes['aria-busy'],undefined);request=deferred();const retry=c.openSettingsSheet();request.resolve({ok:true,json:async()=>({sections:[]})});await retry;assert.equal(requests,3,'failed schema download can be retried');
 console.log('PASS settings immediate visibility, loading/failure, fetch deduplication, close/reopen ownership and cached opening');
}
async function sessions(){
 const list=node(),requests=[];let scope='A';const state={token:'test',session:{id:'main'},pendingSessionId:''};
 const c={state,recoveryScope:()=>scope,subGroupState:{},$:()=>list,el:node,t:(s,n)=>s.replace('{0}',n),renderAssistantEntry(){},action:fn=>fn,document:{documentElement:{lang:'zh-CN'},createDocumentFragment:()=>node('fragment')},api:url=>{const d=deferred();requests.push({url,...d});return d.promise;}};vm.createContext(c);
 vm.runInContext(excerpt(source.includes('let sessionListRequest=')?'let sessionListRequest=':'async function loadSessions() {','// 全局 SSE（#60）'),c);
 const active=[{id:'main',title:'Main',status:'completed',checked:true}],archived=Array.from({length:5},(_,i)=>({id:'sub'+i,parentId:'main',title:'Child '+i,autoArchived:true}));
 const first=c.loadSessions();requests[0].resolve(active);requests[1].resolve(archived);await first;
 const toggle=()=>list.children.find(n=>n.children.some(child=>child.textContent.startsWith('子会话'))),more=()=>list.children.find(n=>n.children.some(child=>child.textContent.startsWith('展开全部')));
 assert.equal(list.children.length,6);const before=requests.length;toggle().onclick();assert.equal(requests.length,before,'collapse must not fetch two lists');assert.equal(list.children.length,2,'collapse paints synchronously');toggle().onclick();assert.equal(list.children.length,6);more().onclick({stopPropagation(){}});assert.equal(list.children.length,8,'show all children without waiting for network');
 const paints=list.paints;let offset=requests.length;const unchanged=c.loadSessions();requests[offset].resolve(active);requests[offset+1].resolve(archived);await unchanged;assert.equal(list.paints,paints,'identical snapshots preserve existing DOM');
 offset=requests.length;const stale=c.loadSessions(),fresh=c.loadSessions();requests[offset+2].resolve([{...active[0],title:'New'}]);requests[offset+3].resolve([]);await fresh;const current=list.children[0];requests[offset].resolve(active);requests[offset+1].resolve(archived);await stale;assert.equal(list.children[0],current,'out-of-order response cannot replace newest list');
 offset=requests.length;const other=c.loadSessions();scope='B';requests[offset].resolve(active);requests[offset+1].resolve(archived);await other;assert.equal(list.children[0],current,'old workspace response discarded');
 offset=requests.length;const oldToken=c.loadSessions();state.token='replacement';requests[offset].resolve(active);requests[offset+1].resolve(archived);await oldToken;assert.equal(list.children[0],current,'old token response discarded');
 console.log('PASS local group expansion, atomic list paint, identical-snapshot DOM reuse and latest workspace/token request ownership');
}
(async()=>{if(!process.argv[3]||process.argv[3]==='settings')await settings();if(!process.argv[3]||process.argv[3]==='sessions')await sessions();})().catch(error=>{console.error(error);process.exitCode=1;});
