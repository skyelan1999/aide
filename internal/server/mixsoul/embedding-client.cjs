'use strict';
async function boundedBody(response,maxBytes){let size=0;const parts=[];for await(const chunk of response.body){size+=chunk.length;if(size>maxBytes)throw Error('Local response budget reached');parts.push(Buffer.from(chunk));}return Buffer.concat(parts).toString('utf8');}
async function embedding(endpoint,model,texts,token){
 const u=new URL(endpoint);if(u.protocol!=='http:'||!['127.0.0.1','[::1]'].includes(u.hostname)||u.username||u.password||u.search||u.hash)throw Error('Only literal loopback HTTP embedding endpoints are allowed');
 const headers={'Content-Type':'application/json'};
 if(token!==undefined){if(typeof token!=='string'||!/^[a-f0-9]{64}$/.test(token))throw Error('Invalid local service token');headers.Authorization='Bearer '+token;}
 const res=await fetch(u,{method:'POST',redirect:'error',signal:AbortSignal.timeout(5000),headers,body:JSON.stringify({model,input:texts})});if(!res.ok)throw Error('Local embedding request failed');const raw=await boundedBody(res,2*1024*1024);const body=JSON.parse(raw);if(!Array.isArray(body.data)||body.data.length!==texts.length)throw Error('Embedding count mismatch');const ordered=[...body.data].sort((a,b)=>a.index-b.index);if(ordered.some((v,i)=>v.index!==i||!Array.isArray(v.embedding)))throw Error('Invalid embedding order');return ordered.map(x=>x.embedding);
}
async function prepareLocalEmbedding(store,config,query){
 store.semanticState={enabled:true,status:'unavailable'};
 try{
  const {endpoint,model,revision,dimensions}=config;
  if(typeof model!=='string'||!model||model.length>200||typeof revision!=='string'||!revision||revision.length>100||!Number.isInteger(dimensions))throw Error('Invalid local model identity');
  const space='mixsoul-local-'+require('node:crypto').createHash('sha256').update(model+'\0'+revision).digest('hex').slice(0,20);
  store.registerSpace(space,{dimensions,model,version:revision,metric:'cosine'});
  // Recent memories become searchable promptly; subsequent turns drain older rows.
  const rows=store.data.records.filter(r=>!r.vectors?.[space]).slice(-15).reverse(),vectors=await embedding(endpoint,model,[query,...rows.map(r=>r.text)],config.token);
  if(vectors.some(v=>v.length!==dimensions||v.some(x=>!Number.isFinite(x))||!Number.isFinite(Math.hypot(...v))||Math.hypot(...v)===0))throw Error('Invalid local vectors');
  rows.forEach((r,i)=>store.setVector(r.id,space,vectors[i+1]));if(rows.length)store.save();
  const norm=Math.hypot(...vectors[0]);store.semanticQuery={query,space,vector:vectors[0].map(x=>x/norm)};
  store.semanticState={enabled:true,status:'ready',space,indexed:rows.length,pending:store.data.records.filter(r=>!r.vectors?.[space]).length};
 }catch{store.semanticState={enabled:true,status:'unavailable',fallback:'lexical-concept-only'};}
}
module.exports={embedding,boundedBody,prepareLocalEmbedding};
