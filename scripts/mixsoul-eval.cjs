#!/usr/bin/env node
'use strict';
// Reproducible retrieval/continuity experiment, no model calls by default.
const fs=require('node:fs'),path=require('node:path'),os=require('node:os'),crypto=require('node:crypto'),{performance}=require('node:perf_hooks');
const {Store,vector,cosine}=require('../internal/server/mixsoul/store.cjs');
const cases=[
 {name:'paraphrase',memory:'希望回答简洁',query:'回复精简',answer:'希望回答简洁'},
 {name:'expiry',memory:'合成会议已到期',query:'合成会议',answer:null,extra:{validTo:'2026-10-25T01:00:00Z'}},
 {name:'suppression',memory:'合成青铜往事',query:'合成青铜往事',answer:null,block:true},
 {name:'correction',memory:'合成项目旧方案',query:'合成项目旧方案',answer:'合成项目新方案',correct:true},
 {name:'dormant',memory:'合成过期见闻',query:'合成过期见闻',answer:null,kind:'episode',age:true},
 {name:'promise',memory:'合成采购承诺',query:'合成采购承诺',answer:'合成采购承诺',kind:'promise'}
];
function evaluate({onCase}={}){const folder=fs.mkdtempSync(path.join(os.tmpdir(),'mixsoul-eval-'));try{
 const results=[];for(const c of cases){const s=new Store({directory:path.join(folder,c.name),scope:'a'.repeat(32),key:crypto.randomBytes(32).toString('base64')});const T='2026-10-25T00:00:00Z',now=c.age?'2026-12-25T00:00:00Z':'2026-10-26T00:00:00Z',row=s.add(c.kind||'fact',c.memory,T,c.extra||{});
  if(c.block)s.begin('block','别再提'+c.memory,T);if(c.correct){const next=s.add('fact',c.answer,T);s.supersede(row.id,next.id,T);}s.save();
  const start=performance.now(),mix=s.context(c.query,now).references.map(r=>r.text),latencyMs=performance.now()-start;
  const rag=s.data.records.map(r=>({text:r.text,score:cosine(vector(c.query),r.vector)})).filter(r=>r.score>.04).sort((a,b)=>b.score-a.score).slice(0,8).map(r=>r.text);
  const pass=values=>c.answer?values.includes(c.answer)&&(!c.correct||!values.includes(c.memory)):!values.includes(c.memory);
  onCase?.({name:c.name,query:c.query,answer:c.answer,contexts:{noMemory:[],plainLexicalRag:rag,mixsoul:mix}});
  results.push({case:c.name,expected:c.answer?'retrieve current evidence':'abstain from excluded memory',noMemory:pass([]),plainLexicalRag:pass(rag),mixsoul:pass(mix),mixsoulLatencyMs:Number(latencyMs.toFixed(3)),contextChars:JSON.stringify(mix).length});s.close();
 }
 return {protocol:'mixsoul-system-v3-fixture-v1',kind:'deterministic retrieval/policy comparison; NOT a model-quality experiment',modelCalls:0,cases:results,summary:Object.fromEntries(['noMemory','plainLexicalRag','mixsoul'].map(k=>[k,{pass:results.filter(r=>r[k]).length,total:results.length}])),smallModel:{status:'NOT_RUN',reason:'No fixed local small model was found; do not equate fixture retrieval with naturalness or small-model capability.'}};
}finally{fs.rmSync(folder,{recursive:true,force:true});}}
if(require.main===module)process.stdout.write(JSON.stringify(evaluate(),null,2)+'\n');
module.exports={evaluate};
