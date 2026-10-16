'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const {webcrypto,createHash}=require('node:crypto');
const text=fs.readFileSync('internal/server/web/file-route.js','utf8');
const hash=s=>createHash('sha256').update(s).digest('hex');
const c=vm.createContext({window:{},URL,URLSearchParams,TextEncoder,Uint8Array,crypto:webcrypto});vm.runInContext(text,c);
const match=c.window.AideFileRoute.matchesSourceLocation;
(async()=>{
 const body='\u03b1 title\r\nreference.go\r\n',bytes=Buffer.byteLength(body);
 const proof={knowledgeWorkspace:'W',indexedTextHash:hash(body),indexedTextBytes:bytes};
 assert.equal(await match(proof,{content:body}),true);
 assert.equal(await match(proof,{content:body+'unobserved tail changed'}),true,'prefix proof does not claim whole-file identity');
 assert.equal(await match(proof,{content:'inserted\n'+body}),false,'old line must not apply after prefix changes');
 assert.equal(await match(proof,{content:body.slice(1)}),false);
 for(const patch of [{indexedTextBytes:8193},{indexedTextBytes:0},{indexedTextBytes:2.5},{indexedTextHash:'bad'}])assert.equal(await match({...proof,...patch},{content:body}),false);
 assert.equal(await match({knowledgeWorkspace:'W'},{content:body}),false,'old unbound map routes fail closed');
 assert.equal(await match({},{content:body}),true,'ordinary file routes remain compatible');
 assert.equal(await match({codeHash:'full'},{hash:'full',content:body}),true);
 assert.equal(await match({codeHash:'old'},{hash:'new',content:body}),false);
 let encoded=0;const limited=vm.createContext({window:{},TextEncoder:class{encode(s){encoded=s.length;return new TextEncoder().encode(s);}},Uint8Array,crypto:webcrypto});vm.runInContext(text,limited);
 const prefix='x'.repeat(512);assert.equal(await limited.window.AideFileRoute.matchesSourceLocation({indexedTextHash:hash(prefix),indexedTextBytes:512},{content:prefix+'z'.repeat(1000000)}),true);assert.equal(encoded,512,'digest only bounded text rather than whole large document');
 const app=fs.readFileSync('internal/server/web/app.js','utf8');
 const implementation=app.slice(app.indexOf('function fileViewRouteCurrent('),app.indexOf('let filePresentationPrefs'));
 function harness(phase){
  const elements=new Map(),spec={root:'workspace',path:'report.md',readOnly:true,knowledgeWorkspace:'W',line:3};let scope='W',finishAPI,finishProof,proofCalls=0;
  const el=id=>{if(!elements.has(id))elements.set(id,{classList:{add(){},remove(){},toggle(){}},setAttribute(){}});return elements.get(id);};
  const context=vm.createContext({window:{AideFileRoute:{read:()=>spec,returnToMap:()=>null,pin(){},matchesSourceLocation(){proofCalls++;return new Promise(resolve=>{finishProof=resolve;});}}},document:{body:{classList:{add(){}}}},fileView:{},state:{token:'T'},chatRecoveryEpoch:1,lockScreen:{locked:false},recoveryScope:()=>scope,$:el,t:x=>x,URLSearchParams,addKnowledgeViewerIdentity(){},api:()=>phase==='api'?new Promise(resolve=>{finishAPI=resolve;}):Promise.resolve({content:'body',hash:'H'}),isMarkdownPath:()=>false,effectiveFileTypePath:x=>x,isImagePath:()=>false,isStlPath:()=>false,isStepPath:()=>false,isPdfPath:()=>false,isDxfPath:()=>false,isDocxPath:()=>false,isXlsxPath:()=>false,isZipPath:()=>false});
  vm.runInContext(implementation,context);
  return {context,elements,el,scope(v){scope=v;},finish(){if(phase==='api')finishAPI({content:'body',hash:'H'});else finishProof(true);},proofCalls:()=>proofCalls};
 }
 const turn=()=>new Promise(resolve=>setImmediate(resolve));
 for(const phase of ['api','digest'])for(const mutation of ['token','scope','epoch','lock','route']){
  const h=harness(phase),pending=vm.runInContext('openFileViewMode()',h.context);await turn();
  if(mutation==='token')h.context.state.token='other';
  if(mutation==='scope')h.scope('other');
  if(mutation==='epoch')h.context.chatRecoveryEpoch++;
  if(mutation==='lock')h.context.lockScreen.locked=true;
  if(mutation==='route')h.context.fileView.spec={};
  h.finish();await pending;assert.equal(h.el('file-view-editor').value,undefined,phase+' '+mutation+' must not render stale file');if(phase==='api')assert.equal(h.proofCalls(),0);
 }
 const fresh=harness('digest');Object.defineProperty(fresh.context.fileView,'hash',{set(){throw Error('fresh route reached render');}});const p=vm.runInContext('openFileViewMode()',fresh.context);await turn();fresh.finish();await assert.rejects(p,/fresh route reached render/);
 console.log('PASS indexed UTF-8/CRLF prefix and full-hash guards, bounded digest, old map routes, and actual viewer stale API/digest ownership');
})().catch(error=>{console.error(error);process.exitCode=1;});
