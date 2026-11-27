'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const app=require('./fixtures/app-source.cjs').readAppSource();
function fn(name){const start=app.search(new RegExp('(?:async )?function '+name+'\\('));assert(start>=0,name);return app.slice(start,app.indexOf('\n}',start)+2);}
function verifyLockModal(){
 const markup=fs.readFileSync(require.resolve('../internal/server/web/index.html'),'utf8');
 assert.match(markup,/<dialog\b[^>]*id="lock-screen"[^>]*>/);
 const lockMarkup=markup.match(/<dialog id="lock-screen"[\s\S]*?<\/dialog>/)[0];
 assert.match(lockMarkup,/id="lock-assistant-name"[^>]*data-i18n="助理">助理</);
 assert.match(lockMarkup,/任务在后台继续运行，助理已暂停听写与朗读。/);
 const lockRole={textContent:'Assistant'},chatTitle={},state={session:{kind:'assistant'}};
 const naming={state,assistantEntrySession:{},voiceAssistantDisplayName:()=>'小绵',$:id=>({'lock-assistant-name':lockRole,'session-title':chatTitle}[id])};
 vm.runInNewContext(fn('syncVoiceAssistantDisplayName')+';syncVoiceAssistantDisplayName()',naming);
 assert.equal(lockRole.textContent,'Assistant');
 assert.equal(chatTitle.textContent,'小绵');
 assert.equal(naming.assistantEntrySession.title,'小绵');
 const stack=[], listeners=new Map();let notify;
 class Dialog {
  open=false; hidden=true;
  classList={toggle:(key,value)=>{this[key]=value;}};
  showModal(){assert.equal(this.open,false);this.open=true;stack.push(this);}
  close(){assert.equal(this.open,true);this.open=false;stack.splice(stack.indexOf(this),1);}
  addEventListener(type,listener){listeners.set(type,listener);}
 }
 class Observer {
  constructor(callback){notify=callback;}
  observe(){}
 }
 const lock=new Dialog(),login=new Dialog(),editor=new Dialog(),reminder=new Dialog();
 const nodes={'lock-screen':lock,'login-dialog':login};
 const context={HTMLDialogElement:Dialog,MutationObserver:Observer,document:{body:{}},$:id=>nodes[id]};
 const start=app.indexOf('function showLockModal('),end=app.indexOf('function applyLockVisual(',start);
 assert.ok(start>=0&&end>start);
 vm.runInNewContext(app.slice(start,end),context);
 editor.showModal();context.showLockModal(true);context.showLockModal(false);
 assert.equal(lock.hidden,false);assert.equal(lock.joining,false);assert.deepEqual(stack,[editor,lock]);
 let canceled=false;listeners.get('cancel')({preventDefault:()=>{canceled=true;}});assert.equal(canceled,true);
 reminder.showModal();notify([{target:reminder}]);assert.deepEqual(stack,[editor,reminder,lock]);
 login.showModal();notify([{target:login}]);assert.equal(stack.at(-1),login);
 reminder.close();reminder.showModal();notify([{target:reminder}]);assert.deepEqual(stack,[editor,lock,reminder,login]);
 login.close();notify([{target:login}]);assert.deepEqual(stack,[editor,reminder,lock]);
 lock.close();notify([{target:editor}]);assert.deepEqual(stack,[editor,reminder]);
}
function fixture(){
 const timers=new Map(),reads=[],renders=[],previews=[],saves=[],polls=[],pluginPages=[],suspendedReviews=[];let next=100;
 const nodes=new Map(),node=id=>{if(!nodes.has(id)){const classes=new Set();nodes.set(id,{hidden:false,open:true,close(){this.open=false;},value:'<robot name="unsaved draft"/>',classList:{contains:k=>classes.has(k),remove:k=>classes.delete(k)}});}return nodes.get(id);};
 const file={path:'robot.urdf',root:'workspace',content:'saved XML'},stream={close(){throw Error('lock must not discard the live delta connection');}};
 const context={window:{AideMarkdownReview:{suspend:host=>suspendedReviews.push(host)}},openRoutedPluginWorkbench:()=>pluginPages.push(true),state:{token:'fixture-token',config:{workspaceId:'A'},session:{id:'one',runs:[{id:'run',status:'running'}]},file,stream,pendingSessionId:'',poll:10,live:{run:'partial'},liveStable:{},liveRound:{},liveTool:{},liveReasoning:{},runPhase:{run:{done:true}}},lockScreen:{locked:true,wasVoiceListening:false,unlocking:false},fileView:{spec:null},sessionSeq:{value:1},$:node,recoveryScope:()=>context.state.config.workspaceId,isUrdfPath:p=>p.endsWith('.urdf'),setupUrdfPreview:(...args)=>previews.push(args),editorAutoSave:{resume:()=>saves.push('editor')},fileViewAutoSave:{resume:()=>saves.push('file')},getSessionWindow:id=>new Promise((resolve,reject)=>reads.push({id,resolve,reject})),adoptSessionIfChanged:s=>{context.state.session=s;return true;},renderSession:()=>renders.push(context.state.session),schedulePoll:()=>polls.push(true),paintSessionSelection(){},scheduleContextPreview(){},Date,console,setTimeout:(f,ms)=>{const id=next++;timers.set(id,{f,ms});return id;},clearTimeout:id=>timers.delete(id)};
 vm.createContext(context);vm.runInContext('let chatRecoveryEpoch=1,refreshSoonTimer=11,titleSyncTimers=[12];\n'+['suspendLockedReads','resumeUnlockedViews','reconcileAfterUnlock','resumeUnlockedState','releaseLockVisual'].map(fn).join('\n'),context);
 return {context,timers,reads,renders,previews,saves,polls,pluginPages,suspendedReviews,node,epoch:()=>vm.runInContext('chatRecoveryEpoch++',context)};
}
(async()=>{
 verifyLockModal();
 const a=fixture(),c=a.context;c.suspendLockedReads();const stream=c.state.stream;c.releaseLockVisual();c.releaseLockVisual();
 assert.deepEqual(a.suspendedReviews,[a.node('editor-preview'),a.node('file-view-preview')]);
 assert.equal(c.state.stream,stream);assert.equal(a.reads.length,1,'one reconciliation per unlock transition');assert.equal(a.pluginPages.length,1,'plugin page resumes once after unlock, never on repeated release');assert.equal(a.previews.length,1);assert.equal(a.previews[0].at(-1),'<robot name="unsaved draft"/>','fresh URDF preview uses unsaved XML');assert.equal(c.state.file.content,'saved XML');assert.deepEqual(a.saves,['editor','file']);
 a.reads[0].resolve({id:'one',runs:[{id:'run',status:'completed'}]});await new Promise(setImmediate);assert.equal(a.renders.length,1);assert.equal(c.state.live.run,undefined);assert.equal(c.state.runPhase.run,undefined);assert.equal(a.polls.length,1);
 for(const change of ['relock','workspace','session','sequence','token']){
  const b=fixture(),v=b.context;v.suspendLockedReads();v.releaseLockVisual();if(change==='relock'){v.lockScreen.locked=true;b.epoch();v.suspendLockedReads();}if(change==='workspace')v.state.config.workspaceId='B';if(change==='session')v.state.session={id:'two'};if(change==='sequence')v.sessionSeq.value++;if(change==='token')v.state.token='other';
  b.reads[0].resolve({id:'one',runs:[]});await new Promise(setImmediate);assert.equal(b.renders.length,0,'stale unlock read must not paint after '+change);
 }
 const active=fixture();active.context.suspendLockedReads();active.context.releaseLockVisual();active.reads[0].resolve({id:'one',runs:[{id:'run',status:'running'}]});await new Promise(setImmediate);assert.equal(active.context.state.runPhase.run.done,false,'fresh running authority revokes a stale terminal UI flag');
 const retry=fixture();retry.context.suspendLockedReads();retry.context.releaseLockVisual();retry.reads[0].reject(Error('offline'));await new Promise(setImmediate);assert.equal(retry.timers.size,1);retry.context.lockScreen.locked=true;retry.epoch();retry.context.suspendLockedReads();assert.equal(retry.timers.size,0,'relock cancels queued unlock retry');
 const closed=fixture();closed.context.suspendLockedReads();closed.node('editor-dialog').open=false;closed.context.releaseLockVisual();assert.equal(closed.previews.length,0,'unlock does not reopen a closed preview');closed.reads[0].resolve({id:'one',runs:[]});
 // Real delayed session-refresh callback: a response spanning a lock/unlock cannot win.
 const r=fixture();vm.runInContext(fn('refreshSessionSoon'),r.context);r.context.action=f=>f;r.context.lockScreen.locked=false;r.context.refreshSessionSoon();const scheduled=[...r.timers.values()][0];const pending=scheduled.f();r.epoch();r.context.lockScreen.locked=false;r.reads[0].resolve({id:'one',runs:[]});await pending;assert.equal(r.renders.length,0);
 // Initial readiness is historical: delayed config initialization must apply CURRENT lock state.
 const initStart=app.indexOf('async function initialize()'),initEnd=app.indexOf('  if (window.syncVirtualAvatarSettings)',initStart),initCode=app.slice(initStart,initEnd)+'}';let veilLocks=0,veilReleases=0;const authority={onReady:async()=>false,effectiveLocked:()=>true};const initial={state:{config:{hasPassword:true}},window:{LockCluster:authority},LockCluster:authority,syncPanelButtons(){},refreshConfig:async()=>{},showJoiningVeil(){},applyLockVisual(){veilLocks++;},releaseLockVisual(){veilReleases++;}};vm.createContext(initial);vm.runInContext(initCode,initial);await initial.initialize();assert.equal(veilLocks,1);assert.equal(veilReleases,0,'old ready result cannot dismiss a current lock');
 console.log('PASS lock recovery: transition-only reconciliation, preserved live connection/draft, fresh viewer scope, active-run repair, relock/workspace/session/token guards and canceled retries');
})().catch(error=>{console.error(error);process.exitCode=1;});
