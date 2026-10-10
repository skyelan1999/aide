const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/starmap.js','utf8');
const elements=new Map(),el=id=>{if(!elements.has(id))elements.set(id,{value:'',hidden:true,disabled:false,textContent:''});return elements.get(id)};
let response,resolve,body,rendered;
const c=vm.createContext({$:el,documentBatch:null,documentAbort:null,documentGeneration:0,graphGeneration:1,locked:false,loading:false,region:'workspace',graph:{workspace:'w'},retrievalMode:()=> 'original',AbortController,renderDocumentResults:d=>rendered=d,say(){},api:async(_,o)=>{body=JSON.parse(o.body);return response}});
vm.runInContext(source.slice(source.indexOf('function resetDocumentBatch()'),source.indexOf('// Data revisions')),c);
vm.runInContext(source.slice(source.indexOf('async function searchDocuments(')),c);
(async()=>{
 el('search').value='needle';response={nextCursor:'next',scanned:32,total:60,hits:[{id:'a'}],warnings:[],files:32,chunks:32};
 await vm.runInContext('searchDocuments()',c);assert.equal(body.cursor,'');assert.equal(el('document-next').hidden,false);assert.match(el('document-coverage').textContent,/32 \/ 60/);
 response={scanned:60,total:60,hits:[{id:'a'},{id:'b'}],warnings:[],files:28,chunks:28};
 await el('document-next').onclick();assert.equal(body.cursor,'next');assert.equal(rendered.hits.length,2);assert.equal(el('document-next').hidden,true);
 response={nextCursor:'next',scanned:32,total:60,hits:[],warnings:[],files:32,chunks:32};await vm.runInContext('searchDocuments()',c);
 el('search').value='changed';await el('document-next').onclick();assert.equal(body.query,'needle','changed query must not submit old cursor');
 el('search').value='needle';c.api=()=>new Promise(r=>resolve=r);const pending=vm.runInContext('searchDocuments(true)',c);c.locked=true;vm.runInContext('documentGeneration++;resetDocumentBatch()',c);resolve(response);await pending;assert.equal(el('document-progress').hidden,true);
 console.log('PASS: continuation, coverage, accumulation, deduplication, query mismatch and locked stale response');
})().catch(e=>{console.error(e);process.exitCode=1});
