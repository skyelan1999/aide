'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/starmap.js','utf8');
const timers=[],handlers={},calls=[];
const c=vm.createContext({document:{hidden:true},locked:false,pageLeaving:false,loading:false,drag:null,cosmicFlight:null,
 liveTimer:0,liveEpoch:1,liveAbort:null,liveCursor:'old',liveFailures:0,codeView:false,recoveryScopeLoaded:'w',
 graph:{workspace:'w',nodes:[{bornAt:100}],sources:[]},performance:{now:()=>10000},
 setTimeout:(fn,delay)=>{timers.push({fn,delay});return timers.length;},clearTimeout:()=>{},
 AbortController,api:async url=>{calls.push(url);return {workspace:'w',revision:'new'};},
 applyLiveUpdate:()=>calls.push('apply'),knowledgeTimeline:{sync:()=>{}},scheduleGraphCache:()=>{},
 progressiveDelay:()=>350,stellarLogbook:{interrupted:()=>{}},say:()=>{},load:()=>calls.push('load'),
 frame:9,pausedAt:100,cameraTween:null,graphFade:null,wheelZoom:null,cosmicGate:0,lastPaint:0,
 liveGhosts:[{start:100}],liveAnimationUntil:780,persistSkyView:()=>calls.push('persist'),settlePanels:()=>{},
 cancelAnimationFrame:()=>calls.push('cancel-paint'),redraw:()=>calls.push('redraw'),
 addEventListener:(name,fn)=>{handlers[name]=fn;}});
function evaluate(start,end){vm.runInContext(source.slice(source.indexOf(start),source.indexOf(end,source.indexOf(start))),c);}
evaluate('function liveReady()','// Continue unfinished catalogues');
evaluate('function scheduleLive(','function scheduleGraphCache(');
evaluate('async function pollLive()','const retrievalMode=');
evaluate("addEventListener('visibilitychange'","$('search-form').onsubmit");
(async()=>{
 assert.equal(c.liveReady(),true,'hidden is a paint state, not a scan gate');
 c.cosmicFlight={start:5};assert.equal(c.liveReady(),true,'hidden flight cannot starve updates');c.cosmicFlight=null;
 c.scheduleLive(350);assert.equal(timers.at(-1).delay,2000,'background UI sync is bounded');
 await c.pollLive();assert.ok(calls.some(x=>typeof x==='string'&&x.includes('/updates?background=1&code=0')));
 assert.ok(!calls.includes('apply'),'hidden sync must not rebuild a large scene');assert.equal(c.liveCursor,'old');
 let aborted=false;c.liveAbort={abort:()=>{aborted=true;}};const epoch=c.liveEpoch;
 handlers.visibilitychange();assert.equal(aborted,false,'hiding must not abort a running request');assert.equal(c.liveEpoch,epoch);
 assert.ok(calls.includes('cancel-paint'));assert.equal(timers.at(-1).delay,2000);
 c.document.hidden=false;handlers.visibilitychange();assert.equal(timers.at(-1).delay,0,'foreground immediately catches up');
 c.liveAbort=null;await c.pollLive();assert.ok(calls.includes('apply'));assert.equal(c.liveCursor,'new');
 assert.equal(c.graph.nodes[0].bornAt,100,'do not move births into the future after background progress');
 assert.equal(c.liveGhosts[0].start,100);assert.equal(c.liveAnimationUntil,780);
 c.locked=true;c.scheduleLive(0);assert.equal(c.liveTimer,0);assert.equal(c.liveReady(),false);
 c.locked=false;c.pageLeaving=true;assert.equal(c.liveReady(),false);
 assert.match(source,/const update=await api\('\/updates\?background=1&code='/,'initial load opts into server continuation too');
 console.log('PASS: hidden live requests/merges, app continuation opt-in, no abort on hide, bounded cadence, resume births, lock/leave guards');
})().catch(e=>{console.error(e);process.exitCode=1;});
