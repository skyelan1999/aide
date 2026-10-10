const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const records=new Map();
const database={transaction(){const tx={};tx.objectStore=()=>({get(key){const req={result:records.get(key)};setTimeout(()=>{req.onsuccess?.();tx.oncomplete?.();},0);return req;},put(record){records.set(record.key,structuredClone(record));const req={};setTimeout(()=>tx.oncomplete?.(),0);return req;},delete(key){records.delete(key);const req={};setTimeout(()=>tx.oncomplete?.(),0);return req;}});return tx;}};
const context={window:{aideUI:{get:()=>true}},Blob,Date,JSON,Promise,Error,setTimeout,indexedDB:{open(){const req={result:database};setTimeout(()=>req.onsuccess?.(),0);return req;}}};
vm.runInNewContext(fs.readFileSync('internal/server/web/continuity.js','utf8'),context);
(async()=>{const api=context.window.AideContinuity;
const scopes=['local||','local|recovery-isolation-b|'];
for(const kind of ['chat','scene','file','file-view']){
 await api.write(scopes[0],kind,'same-id',{text:'workspace A',start:12,scrollTop:1800});
 assert.equal(await api.read(scopes[1],kind,'same-id'),null);
 await api.write(scopes[1],kind,'same-id',{text:'workspace B',start:4,scrollTop:80});
 assert.equal((await api.read(scopes[0],kind,'same-id')).text,'workspace A');
 assert.equal((await api.read(scopes[1],kind,'same-id')).text,'workspace B');
 await api.removeMatching(scopes[0],kind,'same-id','stale text');
 assert.equal((await api.read(scopes[0],kind,'same-id')).text,'workspace A');
 await api.removeMatching(scopes[0],kind,'same-id','workspace A');
 assert.equal(await api.read(scopes[0],kind,'same-id'),null);
 assert.equal((await api.read(scopes[1],kind,'same-id')).text,'workspace B');
}
assert.equal(await api.read('', 'file','same-id'),null);
function tab(storage=new Map(),on=true){
 const ctx={...context,window:{aideUI:{get:()=>on}},sessionStorage:{
  getItem:key=>storage.has(key)?storage.get(key):null,
  setItem:(key,value)=>storage.set(key,String(value))
 }};
 vm.runInNewContext(fs.readFileSync('internal/server/web/continuity.js','utf8'),ctx);
 return {api:ctx.window.AideContinuity,storage};
}
const a=tab(),b=tab(),scope='tab-workspace';
const draftA={text:'A unsent',attachments:[{path:'a.md'}],start:3,scrollTop:18};
await a.api.writeTab(scope,'scene','workbench',{session:'A',dir:'alpha'});
await a.api.writeTab(scope,'chat','A',draftA);
// New tabs get the latest shared fallback, then own that snapshot.
assert.equal((await b.api.readTab(scope,'scene','workbench')).session,'A');
await b.api.writeTab(scope,'scene','workbench',{session:'B',dir:'beta'});
await b.api.writeTab(scope,'chat','A',{text:'B divergent draft'});
assert.equal((await tab(a.storage).api.readTab(scope,'scene','workbench')).session,'A');
assert.deepEqual(await tab(a.storage).api.readTab(scope,'chat','A'),draftA);
assert.equal((await tab(b.storage).api.readTab(scope,'scene','workbench')).session,'B');
const clone=tab(new Map(a.storage));
await clone.api.writeTab(scope,'scene','workbench',{session:'clone'});
assert.equal((await a.api.readTab(scope,'scene','workbench')).session,'A');
assert.equal(await a.api.readTab('other-workspace','scene','workbench'),null);
assert.equal(await a.api.readTab(scope,'chat','initial-empty'),null);
await b.api.writeTab(scope,'chat','initial-empty',{text:'other tab'});
assert.equal(await tab(a.storage).api.readTab(scope,'chat','initial-empty'),null);
// A fallback still loading must not overwrite an edit made in this tab.
const racing=tab();
const loading=racing.api.readTab(scope,'chat','race');
const writing=racing.api.writeTab(scope,'chat','race',{text:'new edit'});
assert.equal((await loading).text,'new edit');await writing;
assert.equal((await tab(racing.storage).api.readTab(scope,'chat','race')).text,'new edit');
const off=tab(new Map(),false);
assert.equal(await off.api.writeTab(scope,'chat','A',draftA),false);
assert.equal(await off.api.readTab(scope,'scene','workbench'),null);
assert.equal(off.storage.size,0);
await assert.rejects(a.api.writeTab(scope,'chat','oversize',{text:'x'.repeat(2*1024*1024)}),/2 MiB/);
assert.equal(await a.api.readTab(scope,'chat','oversize'),null);
console.log('Tab snapshots, reload, opener copy, empty scene, fallback race and size guard PASS');
console.log('Recovery workspace isolation and matching deletion PASS (in-memory IndexedDB fixture)');
})().catch(e=>{console.error(e);process.exitCode=1;});
