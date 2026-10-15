'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/app.js','utf8');
function fn(name){const start=source.search(new RegExp('^function '+name+'\\(','m'));assert(start>=0,name);const eol=source.indexOf('\n',start);return source.slice(start,source[eol-1]==='}'?eol:source.indexOf('\n}',start)+2);}
const nodes=new Map();function $(id){if(!nodes.has(id))nodes.set(id,{value:'',hidden:false,dataset:{},attrs:{},classList:{toggle(){}},setAttribute(k,v){this.attrs[k]=v}});return nodes.get(id)}
const run={id:'r',status:'running',canResume:true};const state={session:{id:'s',runs:[run]},attachments:[],busy:true};
const ctx={state,$,stopRequests:new Set(),recoveryScope:()=> 'workspace',t:s=>s};vm.createContext(ctx);
vm.runInContext(['currentPauseRun','syncPauseControl','composerControlRun','composerHasDraft','setSendMode'].map(fn).join('\n'),ctx);
function sync(){ctx.setSendMode(state.busy)}
sync();assert.equal($('send').dataset.action,'stop');assert($('stop-active-run').hidden);assert(!$('run-controls').hidden);
$('prompt').value='Change direction';sync();assert.equal($('send').dataset.action,'send');assert.equal($('send').attrs['aria-label'],'发送插话');assert(!$('stop-active-run').hidden,'a draft must not remove stop capability');
state.queueMode=true;sync();assert.equal($('send').attrs['aria-label'],'排队发送');assert.equal($('queue-toggle').textContent,'排队');
state.previewOverLimit=true;sync();assert($('send').disabled);assert(!$('stop-active-run').disabled,'over-budget input cannot disable stopping');
$('prompt').value='';sync();assert(!$('send').disabled,'over-budget input cannot disable the primary stop');
state.previewOverLimit=false;run.status='awaiting_clarification';sync();assert.equal($('send').dataset.action,'stop');assert($('pause-current-run').hidden);assert.equal($('run-control-status').textContent,'等待你的回答或审批');
run.status='running';run.pauseRequested=true;sync();assert(!$('send').disabled,'stop may supersede a pending pause');assert($('run-control-more').hidden);
run.pauseRequested=false;ctx.stopRequests.add(JSON.stringify(['workspace','s','r']));sync();assert($('send').disabled);assert.match($('run-control-status').textContent,/停止/);ctx.stopRequests.clear();
run.status='paused';sync();assert.equal($('send').dataset.action,'send');assert(!$('resume-active-run').hidden);
state.session={id:'b',runs:[]};sync();assert($('run-controls').hidden);assert.equal($('send').dataset.action,'send');
state.session={id:'s',runs:[run]};run.status='running';state.pendingSessionId='b';sync();assert($('run-controls').hidden);assert.equal($('send').dataset.action,'send');

class ES{constructor(){this.events={}}addEventListener(k,v){this.events[k]=v}emit(k,v){this.events[k]({data:JSON.stringify(v)})}close(){}}
(async()=>{
 let resolveSnapshot;let polls=0;const liveRun={id:'live',status:'running',mode:'chat',steps:[{name:'chat'}]};
 const st={session:{id:'s',runs:[liveRun]},live:{},liveStable:{},liveRound:{},liveTool:{},liveReasoning:{},runPhase:{}};
 const stream={state:st,EventSource:ES,window:{},ensureRunPhase:id=>st.runPhase[id]={tools:[]},closeStream:()=>st.stream=null,avatarRunFeedback(){},touchRunActivity(){},refreshSessionSoon(){},renderRunStatus(){},scheduleLiveRender(){},schedulePoll(){polls++},reconcileAvatarRun(){},renderSession(){},loadSessions:async()=>{},loadFiles:async()=>{},api:async()=>{},scheduleTitleSync(){},maybeAutoNarrate(){},voice:{awaitingReply:false},getSessionWindow:()=>new Promise(r=>resolveSnapshot=r),adoptSessionIfChanged:s=>{st.session=s;return true},action:fn=>()=>stream.pending=fn(),Date,Math,encodeURIComponent};
 vm.createContext(stream);const start=source.indexOf('function openStream('),end=source.indexOf('// 流式渲染节流：',start);vm.runInContext(source.slice(start,end),stream);stream.openStream(liveRun);const es=st.stream;
 es.emit('intent',{callId:'one',tool:'read_file'});es.emit('intent',{callId:'one',tool:'read_file'});assert.equal(st.runPhase.live.tools.length,1,'same intent has one card');
 es.emit('tool',{callId:'unknown',ok:true});assert.equal(st.runPhase.live.tools[0].status,'running','a mismatched result cannot complete another call');
 es.emit('tool',{callId:'one',ok:true});es.emit('intent',{callId:'one',tool:'read_file'});assert.equal(st.runPhase.live.tools[0].status,'done');assert.equal(st.runPhase.live.tools.length,1);
 es.emit('delta',{round:0,text:'Visible partial output'});es.emit('done',{status:'cancelled'});
 assert.equal(st.live.live,'Visible partial output','done must not blank the answer before the snapshot');assert.equal(st.runPhase.live.done,true);assert(polls>0);
 resolveSnapshot({id:'s',runs:[{...liveRun,status:'cancelled',steps:[{name:'chat',content:'Visible partial output'}]}]});await stream.pending;
 assert.equal(st.live.live,undefined);assert.equal(st.runPhase.live,undefined,'terminal persistence replaces transient state');
 st.stream={another:true};es.emit('intent',{callId:'late',tool:'write_file'});assert.equal(st.runPhase.live,undefined,'closed stream cannot recreate a card');
 console.log('Session run controls PASS: draft send/stop, queue, waiting, pause escalation, budget, scope, intent dedupe, terminal snapshot handoff');
})().catch(e=>{console.error(e);process.exitCode=1});
