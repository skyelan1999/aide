#!/usr/bin/env node
'use strict';
// Local operator utility. Credentials are read once from stdin, never argv,
// env, output or a committed config. No HTTP route/plugin capability is added.
const {Store}=require('../internal/server/mixsoul/store.cjs');
const {embedding,boundedBody}=require('../internal/server/mixsoul/embedding-client.cjs');
async function run(request){
 const {storage,action,now=new Date().toISOString()}=request;const s=new Store(storage);
 try{
  if(action==='status')return s.storageInfo();
  if(action==='inspect')return {records:s.data.records.map(({vectors,vector,...r})=>r),mentionRules:s.data.mentionRules};
  if(action==='forget'){const result=s.forget(String(request.query||''));s.save();return result;}
  if(action==='correct'){const result=s.manage('更正记忆 '+request.id+'：'+request.text,now);return {result};}
  if(action==='undo-merge'){const restored=s.undoMerge(request.id);s.save();return {restored};}
  if(action==='maintain'){const result=s.reconcile(now);s.save();return result;}
  if(action==='embed'){
   const {space,model,dimensions,endpoint}=request;s.registerSpace(space,{model,dimensions,version:request.version||'1',metric:'cosine'});
   const rows=s.data.records.filter(r=>!r.vectors?.[space]).slice(0,16);if(rows.length){const vectors=await embedding(endpoint,model,rows.map(r=>r.text));rows.forEach((r,i)=>s.setVector(r.id,space,vectors[i]));}s.save();return {indexed:rows.length,remaining:s.data.records.filter(r=>!r.vectors?.[space]).length,space};
  }
  if(action==='search'){
   if(request.endpoint){const spec=s.data.spaces[request.space];if(!spec)throw Error('Unknown space');const [queryVector]=await embedding(request.endpoint,spec.model,[request.query]);return {references:s.retrieve(request.query,now,{space:request.space,queryVector})};}
   return {references:s.context(String(request.query||''),now).references};
  }
  throw Error('Unsupported local management action');
 }finally{s.close();}
}
if(require.main===module){let raw='';process.stdin.setEncoding('utf8');process.stdin.on('data',chunk=>{raw+=chunk;if(raw.length>65536)process.exit(2);});process.stdin.on('end',async()=>{try{const result=await run(JSON.parse(raw));process.stdout.write(JSON.stringify(result)+'\n');}catch{process.stderr.write('Local memory operation failed; no credentials or raw request logged.\n');process.exitCode=1;}});}
module.exports={run,embedding,boundedBody};
