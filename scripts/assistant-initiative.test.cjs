'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const tick=async()=>{for(let n=0;n<40;n++)await Promise.resolve();};
function fixture(){
 let now=100000,valid=true,eligible=true,calls=0,spoken=[],shown=[],timers=[],signal;
 const context={window:{},AbortController,Date:{now:()=>now},setTimeout,clearTimeout};vm.createContext(context);vm.runInContext(fs.readFileSync('internal/server/web/assistant-live.js','utf8'),context);
 const hooks={valid:()=>valid,canInitiate:()=>eligible,watch:async()=>[],setTimeout:fn=>{timers.push(fn);return fn;},clearTimeout:fn=>{timers=timers.filter(x=>x!==fn);},initiative:async s=>{signal=s;calls++;return {reply:'要不要先从小步骤开始？'};},proactiveReply:async t=>shown.push(t),speak:async t=>spoken.push(t)};
 const live=context.window.AideAssistantLive.create(hooks);
 return {live,hooks,spoken,shown,get calls(){return calls;},get signal(){return signal;},setEligible:v=>eligible=v,setValid:v=>valid=v,async advance(ms=30001){now+=ms;const next=timers.shift();assert(next);next();await tick();}};
}
(async()=>{
 const f=fixture();await f.live.start();await tick();assert.equal(f.calls,0);await f.advance();assert.equal(f.calls,1);assert.equal(f.spoken.length,1);assert.equal(f.shown.length,1);assert.equal(f.live.snapshot().microphone,false);f.live.stop();
 const blocked=fixture();blocked.setEligible(false);await blocked.live.start();await tick();await blocked.advance();assert.equal(blocked.calls,0);blocked.setEligible(true);blocked.live.activity();await blocked.advance(1000);assert.equal(blocked.calls,0);await blocked.advance();assert.equal(blocked.calls,1);blocked.live.stop();
 for(const interrupt of ['activity','stop','pause','lock']){
  const f=fixture();let resolve;f.hooks.initiative=s=>{f.requestSignal=s;return new Promise(r=>resolve=r);};await f.live.start();await tick();await f.advance();assert(resolve);
  if(interrupt==='lock')f.setValid(false);else f.live[interrupt]();resolve({reply:'不应该播放'});await tick();assert.equal(f.spoken.length,0,interrupt);assert.equal(f.shown.length,0,interrupt);if(interrupt!=='lock')assert.equal(f.requestSignal.aborted,true);f.live.stop();
 }
 const rendering=fixture();rendering.hooks.proactiveReply=async()=>rendering.live.activity();await rendering.live.start();await tick();await rendering.advance();assert.equal(rendering.spoken.length,0,'activity during UI refresh prevents playback');rendering.live.stop();
 const playing=fixture();let endAudio,cancelled=0;playing.hooks.speak=()=>new Promise(r=>endAudio=r);playing.hooks.cancelSpeech=()=>{cancelled++;endAudio?.();};await playing.live.start();await tick();await playing.advance();playing.live.activity();await tick();assert.equal(cancelled,1,'typing interrupts autonomous playback');playing.live.stop();
 console.log('PASS initiative: visible opportunity, inactivity, no microphone, activity/pause/stop/lock and late-render cancellation');
})().catch(e=>{console.error(e);process.exitCode=1});
