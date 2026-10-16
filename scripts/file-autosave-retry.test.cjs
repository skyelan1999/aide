const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');

class Element {
  constructor(tag='div') { this.tagName=tag;this.children=[];this.attributes={};this.listeners={};this.classList={values:new Set(),add:(v)=>this.classList.values.add(v),remove:(v)=>this.classList.values.delete(v),contains:(v)=>this.classList.values.has(v),toggle:(v,on)=>on?this.classList.values.add(v):this.classList.values.delete(v)}; }
  append(...nodes) { this.children.push(...nodes); }
  replaceChildren(...nodes) { this.children=[...nodes]; }
  after(node) { this.next=node; }
  setAttribute(k,v) { this.attributes[k]=v; }
  getAttribute(k) { return this.attributes[k]??null; }
  addEventListener(k,fn) { (this.listeners[k] ||= []).push(fn); }
  dispatchEvent(e) { for(const fn of this.listeners[e.type]||[])fn(e); }
  remove() { this.removed=true; }
  showModal() { this.open=true; }
  close() { this.open=false;for(const fn of this.listeners.close||[])fn(); }
}

function fixture({save,readLatest=async()=>({content:'server current'}),onConflict=()=>{},onSaved=()=>{},blocked=()=>false,viewVisible=()=>true,recovery=false,autoSave=true}) {
  const body=new Element('body'),document={body,activeElement:null,createElement:(tag)=>new Element(tag),addEventListener(){}};
  const windowListeners={},timers=new Map(),snapshots=[],frames=[];let timer=0;
  const window={aideUI:{get:key=>key==='editorAutoSave'?autoSave:true,subscribe(){}},LockCluster:{on(){}},addEventListener(k,fn){(windowListeners[k] ||= []).push(fn);}};
  if(recovery)window.AideContinuity={enabled:()=>true,cacheTab:(scope,kind,id,value)=>{snapshots.push({method:'cacheTab',scope,kind,id,value});return true;},writeTab:(scope,kind,id,value)=>{snapshots.push({method:'writeTab',scope,kind,id,value});return Promise.resolve(true);},writeFileDraft:(scope,id,value)=>{snapshots.push({method:'writeFileDraft',scope,id,value});return Promise.resolve(true);},savedFileDraft:async()=>{snapshots.push({method:'savedFileDraft'});},listFileDrafts:async()=>[],readTab:async()=>null};
  const editor=new Element('textarea');Object.assign(editor,{value:'base',readOnly:false,selectionStart:0,selectionEnd:0,scrollTop:0,scrollLeft:0,focus(){document.activeElement=this;},setSelectionRange(a,b){this.selectionStart=a;this.selectionEnd=b;}});
  const button=new Element('button'),manual=new Element('button'),context={root:'workspace',source:'',path:'retry-fixture.md',wsId:'test'};
  const errors=[];
  vm.runInNewContext(fs.readFileSync('internal/server/web/file-autosave.js','utf8'),{window,document,AideContinuity:window.AideContinuity,Event:class{constructor(type){this.type=type;}},requestAnimationFrame:fn=>frames.push(fn),setTimeout:(fn,ms)=>{const id=++timer;timers.set(id,{fn,ms});return id;},clearTimeout:id=>timers.delete(id)});
  const controller=window.AideFileAutoSave.bind({editor,button,manual,getContext:()=>context,save,onSaved,readLatest,onConflict,blocked,viewVisible,t:(s)=>s,error:(e)=>errors.push(e)});
  controller.activate();
  const runTimers=ms=>{for(const [id,scheduled]of [...timers])if(scheduled.ms===ms){timers.delete(id);scheduled.fn();}};
  return {body,editor,button,manual,context,errors,controller,retry:()=>button.next.next,runTimers,timers,snapshots,windowListeners,continuity:window.AideContinuity,runFrames:()=>{while(frames.length)frames.shift()();}};
}

(async()=>{
  let manualWrites=0;
  const manualStatus=fixture({autoSave:false,save:async()=>{manualWrites++;return {hash:'saved'};}});
  manualStatus.editor.value='confirmed V1';await manualStatus.manual.onclick();
  assert.equal(manualStatus.button.next.textContent,'服务器已保存');
  manualStatus.editor.value='new local V2';manualStatus.editor.dispatchEvent({type:'input'});
  assert.equal(manualStatus.button.next.textContent,'尚未保存','new dirty input must invalidate the confirmed server label');
  manualStatus.runTimers(1200);assert.equal(manualWrites,1,'manual dirty-status paint must not enable saving');
  manualStatus.editor.value='confirmed V1';manualStatus.editor.dispatchEvent({type:'input'});
  assert.equal(manualStatus.button.next.textContent,'服务器已保存','undo to confirmed baseline is clean');
  assert.equal(manualStatus.controller.dirty(),false);assert.equal(manualWrites,1);
  manualStatus.context.hash='different-server-revision';manualStatus.editor.dispatchEvent({type:'input'});
  assert.equal(manualStatus.button.next.textContent,'尚未保存','an older text baseline cannot confirm a different content revision');
  const freshStatus=fixture({autoSave:false,save:async()=>({hash:'saved'})});
  freshStatus.editor.value='';freshStatus.controller.activate();freshStatus.editor.dispatchEvent({type:'input'});
  assert.equal(freshStatus.button.next.textContent,'尚未保存','a new empty file without a confirmed baseline is not server-saved');
  let finishStatus;
  const savingStatus=fixture({autoSave:false,save:()=>new Promise(resolve=>{finishStatus=resolve;})});
  savingStatus.editor.value='in flight';const statusPending=savingStatus.manual.onclick();
  savingStatus.editor.value='newer local';savingStatus.editor.dispatchEvent({type:'input'});
  assert.equal(savingStatus.button.next.textContent,'正在保存…','input cannot erase in-flight state');
  finishStatus({hash:'saved'});await statusPending;
  assert.equal(savingStatus.button.next.textContent,'上一版已保存，当前修改未保存');
  const failedStatus=fixture({autoSave:false,save:async()=>{throw new Error('offline');}});
  await failedStatus.manual.onclick();const failureLabel=failedStatus.button.next.textContent;
  failedStatus.editor.value='new after failure';failedStatus.editor.dispatchEvent({type:'input'});
  assert.equal(failedStatus.button.next.textContent,failureLabel,'dirty paint cannot erase failure state');
  let lockedFinish,lockedFlag=false;
  const uncertainStatus=fixture({autoSave:false,blocked:()=>lockedFlag,save:()=>new Promise(resolve=>{lockedFinish=resolve;})});
  uncertainStatus.editor.value='submitted';const uncertainPending=uncertainStatus.manual.onclick();
  lockedFlag=true;lockedFinish({hash:'saved'});await uncertainPending;lockedFlag=false;
  uncertainStatus.editor.value='new after unlock';uncertainStatus.editor.dispatchEvent({type:'input'});
  assert.equal(uncertainStatus.button.next.textContent,'保存结果待核对','editing after unlock must preserve an unacknowledged save');
  const archiveStatus=fixture({autoSave:false,save:async()=>{throw Object.assign(new Error('archive unavailable'),{status:400,data:{saved:true,hash:'a'.repeat(64),historySaved:false}});}});
  archiveStatus.editor.value='confirmed body';await archiveStatus.manual.onclick();
  archiveStatus.editor.value='new after archive failure';archiveStatus.editor.dispatchEvent({type:'input'});
  assert.equal(archiveStatus.button.next.textContent,'上一版已保存，当前修改未保存；历史归档失败');
  const conflictStatus=fixture({autoSave:false,save:async()=>{throw Object.assign(new Error('conflict'),{status:409});},readLatest:async()=>{throw new Error('read failed');}});
  await conflictStatus.manual.onclick();const conflictLabel=conflictStatus.button.next.textContent;
  conflictStatus.editor.value='new after conflict';conflictStatus.editor.dispatchEvent({type:'input'});
  assert.equal(conflictStatus.button.next.textContent,conflictLabel,'input cannot erase the conflict reread failure');
  console.log('Manual dirty/undo and in-flight/failure/pending-verification status ownership PASS');
  let writes=0;
  const saveRetry=fixture({save:async(_c,text)=>{writes++;if(writes===1)throw new Error('temporary offline');return {hash:'saved'};}});
  await saveRetry.manual.onclick();
  assert.equal(saveRetry.retry().textContent,'重试保存');
  assert.equal(saveRetry.retry().hidden,false);
  await saveRetry.retry().onclick();
  assert.equal(writes,2,'retry must issue one serialized write');
  assert.equal(saveRetry.editor.value,'base');
  assert.equal(saveRetry.retry().hidden,true,'successful retry must clear the retry control');

  const persistedHash='a'.repeat(64),partialError=()=>Object.assign(new Error('archive unavailable'),{status:400,data:{saved:true,hash:persistedHash,historySaved:false}});
  let partialWrites=0,partial;
  partial=fixture({save:async(c,text)=>{partialWrites++;if(partialWrites===1)throw partialError();assert.equal(c.hash,persistedHash,'archive retry must use confirmed persisted hash');return {hash:persistedHash};},onSaved:(_c,_text,result)=>{partial.context.hash=result.hash;}});
  partial.editor.value='submitted';await partial.manual.onclick();
  assert.equal(partial.controller.dirty(),false,'confirmed body must become baseline despite archive failure');
  assert.equal(partial.context.hash,persistedHash);
  assert.match(partial.button.next.textContent,/正文已保存/);
  assert.equal(partial.retry().textContent,'重试历史归档');
  await partial.retry().onclick();assert.equal(partialWrites,2);assert.equal(partial.retry().hidden,true);
  assert.equal(partial.button.next.textContent,'服务器已保存');
  let rejectNewer,newer;
  newer=fixture({save:()=>new Promise((_resolve,reject)=>{rejectNewer=reject;}),onSaved:(_c,_text,result)=>{newer.context.hash=result.hash;}});
  newer.editor.value='submitted';const newerPending=newer.manual.onclick();newer.editor.value='newer local draft';rejectNewer(partialError());await newerPending;
  assert.equal(newer.editor.value,'newer local draft');assert.equal(newer.controller.dirty(),true);assert.equal(newer.context.hash,persistedHash);assert.match(newer.button.next.textContent,/当前修改未保存/);assert.equal(newer.retry().textContent,'重试保存');
  let rejectStale,staleSaved=0;
  const stalePartial=fixture({save:()=>new Promise((_resolve,reject)=>{rejectStale=reject;}),onSaved:()=>staleSaved++});
  const stalePending=stalePartial.manual.onclick();stalePartial.context.path='other.md';stalePartial.editor.value='other';stalePartial.controller.activate();rejectStale(partialError());await stalePending;assert.equal(staleSaved,0,'stale partial save cannot acknowledge another file');
  let rejectLocked,isLocked=false,lockedSaved=0;
  const lockedPartial=fixture({save:()=>new Promise((_resolve,reject)=>{rejectLocked=reject;}),blocked:()=>isLocked,onSaved:()=>lockedSaved++});
  const lockedPending=lockedPartial.manual.onclick();isLocked=true;rejectLocked(partialError());await lockedPending;assert.equal(lockedSaved,0,'locked response cannot acknowledge editor state');
  for(const data of [{saved:true,hash:'bad',historySaved:false},{saved:true,hash:persistedHash},{hash:persistedHash,historySaved:false}]){
    let acknowledgements=0;const invalid=fixture({save:async()=>{throw Object.assign(new Error('unconfirmed save'),{status:400,data});},onSaved:()=>acknowledgements++});
    invalid.editor.value='unsaved';await invalid.manual.onclick();assert.equal(acknowledgements,0);assert.equal(invalid.controller.dirty(),true,'unconfirmed partial result must retain dirty draft');
  }
  console.log('Partial body-save acknowledgement, archive retry, newer draft and stale/lock ownership PASS');

  let conflictWrites=0,reads=0,conflicts=0;
  const conflict=fixture({save:async()=>{conflictWrites++;const e=new Error('stale hash');e.status=409;throw e;},readLatest:async()=>{reads++;if(reads===1)throw new Error('temporary read failure');return {content:'server version',hash:'new-hash'};},onConflict:()=>conflicts++});
  conflict.editor.value='local draft';
  await conflict.manual.onclick();
  assert.equal(conflict.retry().textContent,'重试读取服务器版本');
  assert.equal(conflict.retry().hidden,false);
  await conflict.retry().onclick();
  assert.equal(conflictWrites,1,'retrying a conflict must not issue another write');
  assert.equal(reads,2,'the retry must fetch the latest server version');
  assert.equal(conflicts,0,'reading latest content must not accept a merge');
  assert.equal(conflict.body.children.at(-1).open,true,'latest content must open the explicit compare dialog');
  const dialog=conflict.body.children.at(-1),accept=dialog.children[4],merged=dialog.children[3];
  merged.value='merged deliberately';
  accept.onclick();
  assert.equal(conflicts,1,'the conflict callback runs only after explicit acceptance');
  assert.equal(conflict.editor.value,'merged deliberately');
  assert.equal(conflict.retry().hidden,true);

  for(const chosen of ['base','remote new']){
    let adoptedWrites=0,adopted;
    adopted=fixture({autoSave:false,recovery:true,save:async()=>{adoptedWrites++;throw Object.assign(new Error('conflict'),{status:409});},readLatest:async()=>({content:'remote new',hash:'remote-hash'}),onConflict:latest=>{adopted.context.hash=latest.hash;}});
    adopted.context.hash='old-hash';adopted.controller.activate();
    adopted.editor.value='local draft';await adopted.manual.onclick();
    const comparison=adopted.body.children.at(-1);comparison.children[3].value=chosen;comparison.children[4].onclick();
    assert.equal(adopted.controller.dirty(),chosen!=='remote new','adopted server body becomes the dirty/recovery baseline');
    assert.equal(adopted.button.next.textContent,chosen==='remote new'?'服务器已保存':'尚未保存','explicit adoption confirms the latest server revision');
    adopted.runTimers(180);adopted.runTimers(1200);
    assert.equal(adoptedWrites,1,'explicit adoption with auto-save off never writes implicitly');
    if(chosen==='base')assert.ok(adopted.snapshots.some(item=>item.method==='writeFileDraft'&&item.value.text===chosen),'retaining old local text against a newer server remains recoverable');
  }
  console.log('Explicit conflict adoption confirms server content/hash and preserves divergent local recovery PASS');

  const recovery=fixture({recovery:true,save:async()=>({hash:'saved'})});
  recovery.editor.value='a long edited document';
  recovery.editor.dispatchEvent({type:'input'});
  for(let i=0;i<20;i++)recovery.editor.dispatchEvent({type:'scroll'});
  assert.equal(recovery.snapshots.length,0,'typing and scroll bursts must not synchronously serialize the recovery snapshot');
  recovery.runTimers(120);
  assert.deepEqual(recovery.snapshots.filter(item=>item.method==='cacheTab').map(item=>item.kind).sort(),['file','file-view'],'bursts coalesce into one draft and one view snapshot');
  assert.equal(recovery.snapshots.find(item=>item.method==='cacheTab'&&item.kind==='file').value.text,'a long edited document','the coalesced draft stores the newest text');
  recovery.editor.value='latest text before pagehide';
  recovery.editor.dispatchEvent({type:'input'});
  for(const fn of recovery.windowListeners.pagehide||[])fn();
  assert.ok(recovery.snapshots.some(item=>item.method==='writeFileDraft'&&item.value.text==='latest text before pagehide'),'pagehide synchronously hands the newest dirty text to tab recovery before async persistence');
  console.log('File recovery snapshots coalesce typing/scroll bursts and flush the latest state on pagehide PASS');
  let viewIsVisible=true;
  const closedView=fixture({recovery:true,viewVisible:()=>viewIsVisible,save:async()=>({hash:'saved'})});
  closedView.editor.setSelectionRange(2,2);closedView.editor.scrollTop=506;
  closedView.editor.value='unsaved visible text';closedView.editor.dispatchEvent({type:'input'});
  closedView.editor.dispatchEvent({type:'scroll'});closedView.runTimers(120);closedView.runTimers(180);
  viewIsVisible=false;closedView.editor.scrollTop=0;
  closedView.editor.dispatchEvent({type:'scroll'});closedView.runTimers(120);closedView.runTimers(180);
  for(const fn of closedView.windowListeners.pagehide||[])fn();
  const recordedViews=closedView.snapshots.filter(item=>item.kind==='file-view');
  assert.equal(recordedViews.at(-1).value.scrollTop,506,'hidden layout scroll must not replace the last visible view');
  assert.equal(closedView.snapshots.filter(item=>item.method==='writeFileDraft').at(-1).value.scrollTop,506,'closing must keep the last visible draft position');
  viewIsVisible=true;closedView.editor.scrollTop=72;
  closedView.editor.dispatchEvent({type:'scroll'});closedView.runTimers(120);
  assert.equal(closedView.snapshots.filter(item=>item.kind==='file-view').at(-1).value.scrollTop,72,'visible user scrolling must still persist');
  let restoreVisible=true;
  const closingRestore=fixture({recovery:true,viewVisible:()=>restoreVisible,save:async()=>({hash:'saved'})});
  closingRestore.context.hash='same';let finishView;
  closingRestore.continuity.readTab=()=>new Promise(resolve=>{finishView=resolve;});
  const pendingView=closingRestore.controller.restoreView();restoreVisible=false;
  finishView({hash:'same',start:2,end:2,scrollTop:506});await pendingView;closingRestore.runFrames();
  assert.equal(closingRestore.editor.scrollTop,0,'pending restoration cannot scroll a closed editor');
  assert.equal(closingRestore.editor.selectionStart,0,'pending restoration cannot move a closed caret');
  console.log('Closed editor layout scroll and pending restoration ownership PASS');
  for(const phase of ['read','animation']){
    const view=fixture({recovery:true,save:async()=>({hash:'saved'})});
    view.context.hash='same-content';
    let resolveView;
    view.continuity.readTab=()=>new Promise(resolve=>{resolveView=resolve;});
    const restoring=view.controller.restoreView();
    if(phase==='animation'){resolveView({hash:'same-content',start:0,end:0,scrollTop:999});await restoring;}
    view.editor.setSelectionRange(2,2);view.editor.scrollTop=44;view.editor.dispatchEvent({type:'select'});
    if(phase==='read'){resolveView({hash:'same-content',start:0,end:0,scrollTop:999});await restoring;}
    view.runFrames();
    assert.equal(view.editor.selectionStart,2,'new cursor must win during '+phase);
    assert.equal(view.editor.scrollTop,44,'new scroll must win during '+phase);
  }
  for(const phase of ['read','animation','unchanged']){
    const view=fixture({recovery:true,save:async()=>({hash:'saved'})});
    view.context.hash='old-content';let resolveView;
    view.continuity.readTab=()=>new Promise(resolve=>{resolveView=resolve;});
    const restoring=view.controller.restoreView();
    if(phase==='read')view.context.hash='new-content';
    resolveView({hash:'old-content',start:1,end:3,scrollTop:55});await restoring;
    if(phase==='animation')view.context.hash='new-content';
    view.runFrames();
    assert.equal(view.editor.scrollTop,phase==='unchanged'?55:0,'position must match the still-current content during '+phase);
    if(phase==='unchanged')assert.equal(view.editor.selectionStart,1,'unchanged current content must restore normally');
  }
  let resolveLocked,successLocked=false,successAcknowledgements=0;
  const lockedSuccess=fixture({recovery:true,blocked:()=>successLocked,save:()=>new Promise(resolve=>{resolveLocked=resolve;}),onSaved:()=>successAcknowledgements++});
  lockedSuccess.editor.value='unsaved draft';const successfulSave=lockedSuccess.manual.onclick();successLocked=true;resolveLocked({hash:'saved'});await successfulSave;
  assert.equal(successAcknowledgements,0,'locked successful response cannot acknowledge editor state');
  assert.equal(lockedSuccess.controller.dirty(),true,'locked successful response must retain the recovery draft');
  assert.equal(lockedSuccess.button.next.textContent,'保存结果待核对','completed locked response cannot leave a saving indicator');
  assert.equal(lockedSuccess.snapshots.filter(x=>x.method==='savedFileDraft').length,0,'locked response cannot clear draft persistence');
  console.log('Async position ownership, content revision and locked success acknowledgement PASS');
  console.log('File autosave retry, conflict reread and explicit merge acceptance PASS');
})().catch(e=>{console.error(e);process.exitCode=1;});
