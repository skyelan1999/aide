'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const records=new Map(),db={objectStoreNames:{contains:()=>true},transaction(){
 const tx={objectStore(){return {get(key){const req={result:records.get(key)};queueMicrotask(()=>tx.oncomplete?.());return req;},put(value,key){records.set(key,structuredClone(value));const req={result:key};queueMicrotask(()=>tx.oncomplete?.());return req;}};}};return tx;
}};
const indexedDB={open(){const req={result:db};queueMicrotask(()=>{req.onsuccess?.();});return req;}};
const window={indexedDB};const context=vm.createContext({window,indexedDB,Date,JSON,Promise,Map,Array,setTimeout,clearTimeout});
vm.runInContext(fs.readFileSync('internal/server/web/starmap-cache.js','utf8'),context);
(async()=>{
 const identity={workspace:'workspace-a',epoch:'4',policy:'hash-a'},graph={nodes:[{id:'F1',text:'cached',readState:'unavailable'}],edges:[],sources:[],warnings:[],truncated:false};
 assert.equal(await window.AideStarmapCache.write('knowledge',identity,graph,'R1'),true);
 const hit=await window.AideStarmapCache.read('knowledge',identity);assert.equal(hit.revision,'R1');assert.equal(hit.graph.nodes[0].text,'cached');assert.equal(hit.graph.nodes[0].readState,'unavailable','unreadable metadata remains explicit in cached first paint');
 assert.equal(await window.AideStarmapCache.read('knowledge',{...identity,workspace:'workspace-b'}),null,'workspace change rejects stale snapshot');
 assert.equal(await window.AideStarmapCache.read('knowledge',{...identity,policy:'hash-b'}),null,'index policy change rejects stale snapshot');
 assert.equal(await window.AideStarmapCache.read('code',identity),null,'knowledge and code snapshots are isolated');
 assert.equal(await window.AideStarmapCache.write('knowledge',identity,{nodes:[{text:'x'.repeat(25*1024*1024)}],edges:[]},'R2'),false,'oversized graphs fall back without persistent storage');
 const node={id:'cycle',name:'real node',text:'x'.repeat(6*1024*1024)},edge={from:'cycle',to:'cycle',kind:'mention'};
 node.cluster={members:[node]};node.projected={n:node};edge.a=node.projected;
 assert.equal(await window.AideStarmapCache.write('knowledge',identity,{nodes:[node],edges:[edge]},'cycle-R'),true,'real layout references must not break cache writes');
 const restored=await window.AideStarmapCache.read('knowledge',identity);assert.equal(restored.graph.nodes[0].id,'cycle');assert.equal(restored.graph.nodes[0].cluster,undefined);assert.equal(restored.graph.edges[0].a,undefined);
 const hungWindow={indexedDB:{open(){return {};}}},hungContext=vm.createContext({window:hungWindow,indexedDB:hungWindow.indexedDB,Date,JSON,Promise,Map,Array,setTimeout,clearTimeout});
 vm.runInContext(fs.readFileSync('internal/server/web/starmap-cache.js','utf8'),hungContext);
 const started=Date.now();assert.equal(await hungWindow.AideStarmapCache.read('knowledge',identity),null);assert.ok(Date.now()-started<1000,'blocked cache must yield to network');
 console.log('PASS: persistent star map cache write/read and workspace/policy/view isolation');
})().catch(e=>{console.error(e);process.exitCode=1;});

// Exercise the actual scheduler under continuous scan and delayed idle callbacks.
{
'use strict';
const source=fs.readFileSync('internal/server/web/starmap.js','utf8');
function harness(idleDelay=0){
 let now=0,next=0;const timers=new Map(),writes=[];
 const setTimeout=(fn,delay=0)=>{const id=++next;timers.set(id,{at:now+delay,fn});return id;};
 const clearTimeout=id=>timers.delete(id);
 const identity={workspace:'A',epoch:'1',policy:'p'};
 const c=vm.createContext({cacheWriteTimer:0,locked:false,cacheIdentity:identity,liveCursor:'R0',graph:{workspace:'A',nodes:[]},codeView:false,liveEpoch:1,setTimeout,clearTimeout,window:{AideStarmapCache:{write(mode,identity,graph,revision){writes.push({mode,workspace:identity.workspace,revision,at:now});return Promise.resolve(true);}},requestIdleCallback:true},requestIdleCallback:fn=>setTimeout(fn,idleDelay)});
 vm.runInContext(source.slice(source.indexOf('function scheduleGraphCache('),source.indexOf('async function pollLive(')),c);
 function advance(end){while(true){const row=[...timers].filter(([,t])=>t.at<=end).sort((a,b)=>a[1].at-b[1].at)[0];if(!row)break;now=row[1].at;timers.delete(row[0]);row[1].fn();}now=end;}
 return {c,writes,advance,clearTimeout};
}
{
 const h=harness();for(let i=0;i<12;i++){h.advance(i*350);h.c.liveCursor='R'+i;h.c.scheduleGraphCache();}
 assert.ok(h.writes.length>=2,'continuous350ms updates must persist usable snapshots before scanning stops');
 assert.equal(h.writes[0].revision,'R3','first write coalesces latest revision without postponing its deadline');
}
{
 const h=harness(1800);h.c.scheduleGraphCache();h.advance(1200);
 h.clearTimeout(h.c.cacheWriteTimer);h.c.cacheWriteTimer=0;h.c.cacheIdentity={workspace:'B',epoch:'2',policy:'q'};h.c.graph={workspace:'B',nodes:[]};h.c.liveEpoch++;h.c.liveCursor='B1';h.c.scheduleGraphCache();
 const newer=h.c.cacheWriteTimer;h.advance(3000);assert.equal(h.writes.length,0,'cancelled idle callback cannot write another workspace');assert.equal(h.c.cacheWriteTimer,newer,'cancelled callback cannot clear newer task');h.advance(4200);assert.equal(h.writes.length,1);assert.equal(h.writes[0].workspace,'B');
}
{
 const h=harness(100);h.c.scheduleGraphCache();h.advance(1200);h.c.locked=true;h.advance(1300);assert.equal(h.writes.length,0,'locked view must not persist a pending idle snapshot');
}
console.log('PASS star-map cache makes bounded progress during fast scans and rejects cancelled/locked owner callbacks');

}
