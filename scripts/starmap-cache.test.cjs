'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const records=new Map(),db={objectStoreNames:{contains:()=>true},transaction(){
 const tx={objectStore(){return {get(key){const req={result:records.get(key)};queueMicrotask(()=>tx.oncomplete?.());return req;},put(value,key){records.set(key,structuredClone(value));const req={result:key};queueMicrotask(()=>tx.oncomplete?.());return req;}};}};return tx;
}};
const indexedDB={open(){const req={result:db};queueMicrotask(()=>{req.onsuccess?.();});return req;}};
const window={indexedDB};const context=vm.createContext({window,indexedDB,Date,JSON,Promise,Map,Array});
vm.runInContext(fs.readFileSync('internal/server/web/starmap-cache.js','utf8'),context);
(async()=>{
 const identity={workspace:'workspace-a',epoch:'4',policy:'hash-a'},graph={nodes:[{id:'F1',text:'cached'}],edges:[],sources:[],warnings:[],truncated:false};
 assert.equal(await window.AideStarmapCache.write('knowledge',identity,graph,'R1'),true);
 const hit=await window.AideStarmapCache.read('knowledge',identity);assert.equal(hit.revision,'R1');assert.equal(hit.graph.nodes[0].text,'cached');
 assert.equal(await window.AideStarmapCache.read('knowledge',{...identity,workspace:'workspace-b'}),null,'workspace change rejects stale snapshot');
 assert.equal(await window.AideStarmapCache.read('knowledge',{...identity,policy:'hash-b'}),null,'index policy change rejects stale snapshot');
 assert.equal(await window.AideStarmapCache.read('code',identity),null,'knowledge and code snapshots are isolated');
 assert.equal(await window.AideStarmapCache.write('knowledge',identity,{nodes:[{text:'x'.repeat(5*1024*1024)}],edges:[]},'R2'),false,'oversized graphs fall back without persistent storage');
 console.log('PASS: persistent star map cache write/read and workspace/policy/view isolation');
})().catch(e=>{console.error(e);process.exitCode=1;});
