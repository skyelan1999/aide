const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const app = fs.readFileSync(path.join(__dirname, '../internal/server/web/app.js'), 'utf8');

async function checkTerminal(status, includeStatusEvent = true) {
  const feedback = [], cues = [];
  const run = {id:'current',mode:'chat',status:'running'};
  const session = {id:'session',runs:[{id:'old',status:'completed'},run],messages:[]};
  const expectedCue = {scene:'thinking',variant:2,emotion:'curious',intensity:1};
  const doneSession = {...session,runs:[session.runs[0],{...run,status,avatarCue:expectedCue}]};
  const state = {session,live:{},liveStable:{},liveRound:{},liveTool:{},liveReasoning:{},runPhase:{},stream:null};
  const context = {state,window:{aideAvatarFeedback:(...v)=>feedback.push(v),aideAvatarCue:v=>cues.push(v)},
    EventSource:class { constructor(){this.handlers={};} addEventListener(name,cb){this.handlers[name]=cb;} close(){} },
    ensureRunPhase:id=>state.runPhase[id]={tools:[]},closeStream:()=>state.stream=null,
    getSessionWindow:async()=>doneSession,adoptSessionIfChanged:s=>{state.session=s;return true;},
    renderSession(){},refreshSessionSoon(){},loadSessions:async()=>{},loadFiles:async()=>{},
    schedulePoll(){},scheduleTitleSync(){},maybeAutoNarrate(){},api:async()=>{},
    voice:{awaitingReply:false},touchRunActivity(){},renderRunStatus(){},scheduleLiveRender(){},
    action:fn=>(...args)=>{context.pending=Promise.resolve(fn(...args));return context.pending;},
    Map,Date,Math,JSON,encodeURIComponent};
  vm.createContext(context);
  const start=app.indexOf('const avatarRunCues = new Map();');
  const end=app.indexOf('// 流式渲染节流：',start);
  vm.runInContext(app.slice(start,end),context);
  context.openStream(run);
  const stream=state.stream;
  if (includeStatusEvent) stream.handlers.avatar({data:JSON.stringify({avatarCue:expectedCue})});
  assert.equal(cues.length,includeStatusEvent?1:0);
  if(includeStatusEvent) stream.handlers.status({data:JSON.stringify({status})});
  stream.handlers.done({data:includeStatusEvent?JSON.stringify({status}):'{}'});
  await context.pending;
  assert.equal(feedback.length,1,'terminal feedback deduplicated across status/done/GET');
  assert.equal(feedback[0][0],status,'exact run status is authoritative');
  assert.equal(feedback[0][1].runId,'current');
  assert.equal(state.runPhase.current,undefined);
  assert.equal(cues.length,1,'GET restores a missed cue and deduplicates SSE');
  context.reconcileAvatarRun(run.id);
  assert.equal(cues.length,1,'fast-run reconciliation does not replay a cue');
  const nextStream = {id:'new-session-stream'};
  state.stream = nextStream;
  stream.handlers.avatar({data:JSON.stringify({avatarCue:{scene:'error'}})});
  stream.handlers.tool({data:JSON.stringify({ok:false,callId:'late-failure'})});
  stream.handlers.clarification({data:'{}'});
  stream.onerror();
  assert.equal(cues.length,1,'closed stream cannot animate another task');
  assert.equal(feedback.length,1,'late tool/clarification events cannot affect another task');
  assert.equal(state.stream,nextStream,'late connection error cannot close a newer stream');
}

async function checkActualSpeech() {
  const signals=[]; let audio;
  const context={window:{aideAvatarSignal:(...v)=>signals.push(v)},ttsPlayer:{},
    Audio:class {constructor(){audio=this;}play(){return Promise.resolve();}pause(){}},
    URL:{revokeObjectURL(){}},setInterval:()=>1,clearInterval(){},Promise};
  vm.createContext(context);
  const start=app.indexOf('function ttsPlayOne(url) {');
  const end=app.indexOf('// 朗读入口：',start);
  vm.runInContext(app.slice(start,end),context);
  const played=context.ttsPlayOne('blob:test');
  assert.deepEqual(signals,[],'pending play is not speech');
  audio.onplaying(); assert.deepEqual(signals.at(-1),['speaking',true]);
  audio.onpause(); assert.deepEqual(signals.at(-1),['speaking',false]);
  audio.onplaying(); audio.onended(); await played;
  assert.deepEqual(signals.at(-1),['speaking',false]);
}

(async()=>{
  for(const status of ['completed','failed','cancelled','interrupted','paused','awaiting_approval','awaiting_clarification']) {
    await checkTerminal(status,true);
    await checkTerminal(status,false);
  }
  await checkActualSpeech();
  assert(!app.includes("window.aideAvatarFeedback?.('done');"),'generic done must never announce success');
  console.log('Avatar feedback: 14 terminal/reconciliation paths, stale SSE, and actual speech playback passed.');
})().catch(err=>{console.error(err);process.exitCode=1;});
