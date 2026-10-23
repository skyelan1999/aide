const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
class Element{constructor(tag){this.tag=tag;this.children=[];this.dataset={};this.attributes={};}append(...children){this.children.push(...children);}setAttribute(k,v){this.attributes[k]=v;}addEventListener(){}focus(){this.focused=true;}}
const tick=async()=>{for(let i=0;i<30;i++)await Promise.resolve();};
function fixture(){
 let valid=true,supported=true,clock=1000,timer,stops=0,errors=[],recognizers=[],freed=0,permission;
 const ctx={window:{},document:{createElement:tag=>new Element(tag)},AbortController,Date,setTimeout:()=>1,clearTimeout(){}};vm.createContext(ctx);
 for(const file of ['assistant-live','assistant-call'])vm.runInContext(fs.readFileSync(`internal/server/web/${file}.js`,'utf8'),ctx);
 let call;const live=ctx.window.AideAssistantLive.create({valid:()=>valid,state:value=>call?.paint(value),watch:async()=>[],setTimeout:()=>1,clearTimeout(){},cancelSpeech:()=>stops++,openMicrophone:async()=>{
 if(permission)return permission;
 const recognition={start(){this.onstart?.();},abort(){this.aborted=true;}};recognizers.push(recognition);return {recognition,stream:{getTracks:()=>[{stop(){freed++;}}]}};},error:message=>errors.push(message)});
 call=ctx.window.AideAssistantCall.create({valid:()=>valid,supported:()=>supported,live:()=>live,now:()=>clock,setInterval:fn=>{timer=fn;return 1;},clearInterval:()=>{timer=null},unlockAudio(){},cancelSpeech:()=>stops++,error:message=>errors.push(message)});
 const panel=call.element.children[1],mute=panel.children[3].children[0],hangup=panel.children[3].children[1];
 return {call,live,panel,mute,hangup,errors,recognizers,freed:()=>freed,stops:()=>stops,setValid:v=>valid=v,setSupported:v=>supported=v,setPermission:v=>permission=v,advance:()=>{clock+=61000;timer?.();},timer:()=>timer};
}
(async()=>{
 const f=fixture();await f.call.start();assert(f.call.active());assert.equal(f.panel.hidden,false);assert.equal(f.live.snapshot().phase,'listening');f.advance();assert.equal(f.panel.children[2].textContent,'01:01');
 await f.call.start();assert.equal(f.recognizers.length,1,'duplicate start does not acquire another mic');
 f.mute.onclick();assert.equal(f.live.snapshot().microphone,false);assert.equal(f.call.active(),true);assert.equal(f.freed(),1);assert.equal(f.mute.attributes['aria-pressed'],'true');
 f.mute.onclick();await tick();assert.equal(f.live.snapshot().microphone,true);assert.equal(f.recognizers.length,2);
 f.hangup.onclick();assert.equal(f.call.active(),false);assert.equal(f.live.snapshot().enabled,false);assert.equal(f.freed(),2);assert.equal(f.timer(),null);assert(f.stops()>0);
 const denied=fixture();denied.setPermission(Promise.reject(new Error('denied')));await denied.call.start();assert.equal(denied.call.active(),false);assert.equal(denied.timer(),null);
 const late=fixture();let resolve;late.setPermission(new Promise(r=>resolve=r));const start=late.call.start();late.call.end();let released=0;resolve({recognition:{start(){throw Error('late permission must not start');}},stream:{getTracks:()=>[{stop(){released++;}}]}});await start;assert.equal(released,1);assert.equal(late.call.active(),false);
 const timeout=fixture();timeout.setPermission(new Promise(()=>{}));void timeout.call.start();await tick();timeout.advance();assert.equal(timeout.call.active(),false);assert.equal(timeout.errors.length,1);
 const unsupported=fixture();unsupported.setSupported(false);await unsupported.call.start();assert.equal(unsupported.call.active(),false);assert.equal(unsupported.errors.length,1);
 const locked=fixture();await locked.call.start();locked.setValid(false);locked.call.paint(locked.live.snapshot());assert.equal(locked.freed(),1);assert.equal(locked.call.active(),false);assert.equal(locked.call.element.hidden,true);
 const paused=fixture();await paused.call.start();paused.live.pause();assert.equal(paused.call.active(),false);
 const app=fs.readFileSync('internal/server/web/app.js','utf8');assert(app.includes('if(id!==state.session?.id)window.aideAssistantCall?.end()'));assert(app.includes("if(document.visibilityState==='hidden'){window.aideAssistantCall?.end()"));
 console.log('PASS call: lifecycle, clock, mute/resume, hangup, late permission, permission denial, unsupported browser, lock, pause, page/session boundaries');
})().catch(error=>{console.error(error);process.exitCode=1});
