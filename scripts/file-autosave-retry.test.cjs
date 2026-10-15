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

function fixture({save,readLatest=async()=>({content:'server current'}),onConflict=()=>{},onSaved=()=>{},blocked=()=>false,recovery=false}) {
  const body=new Element('body'),document={body,activeElement:null,createElement:(tag)=>new Element(tag),addEventListener(){}};
  const windowListeners={},timers=new Map(),snapshots=[];let timer=0;
  const window={aideUI:{get:()=>true,subscribe(){}},LockCluster:{on(){}},addEventListener(k,fn){(windowListeners[k] ||= []).push(fn);}};
  if(recovery)window.AideContinuity={enabled:()=>true,cacheTab:(scope,kind,id,value)=>{snapshots.push({method:'cacheTab',scope,kind,id,value});return true;},writeTab:(scope,kind,id,value)=>{snapshots.push({method:'writeTab',scope,kind,id,value});return Promise.resolve(true);},writeFileDraft:(scope,id,value)=>{snapshots.push({method:'writeFileDraft',scope,id,value});return Promise.resolve(true);},listFileDrafts:async()=>[],readTab:async()=>null};
  const editor=new Element('textarea');Object.assign(editor,{value:'base',readOnly:false,selectionStart:0,selectionEnd:0,scrollTop:0,scrollLeft:0,focus(){document.activeElement=this;},setSelectionRange(a,b){this.selectionStart=a;this.selectionEnd=b;}});
  const button=new Element('button'),manual=new Element('button'),context={root:'workspace',source:'',path:'retry-fixture.md',wsId:'test'};
  const errors=[];
  vm.runInNewContext(fs.readFileSync('internal/server/web/file-autosave.js','utf8'),{window,document,AideContinuity:window.AideContinuity,Event:class{constructor(type){this.type=type;}},setTimeout:(fn,ms)=>{const id=++timer;timers.set(id,{fn,ms});return id;},clearTimeout:id=>timers.delete(id)});
  const controller=window.AideFileAutoSave.bind({editor,button,manual,getContext:()=>context,save,onSaved,readLatest,onConflict,blocked,t:(s)=>s,error:(e)=>errors.push(e)});
  controller.activate();
  const runTimers=ms=>{for(const [id,scheduled]of [...timers])if(scheduled.ms===ms){timers.delete(id);scheduled.fn();}};
  return {body,editor,button,manual,context,errors,controller,retry:()=>button.next.next,runTimers,timers,snapshots,windowListeners};
}

(async()=>{
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
  console.log('File autosave retry, conflict reread and explicit merge acceptance PASS');
})().catch(e=>{console.error(e);process.exitCode=1;});
