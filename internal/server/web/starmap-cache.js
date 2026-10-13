'use strict';
// Local graph snapshots speed up repeat visits. The server identity is checked
// before a snapshot is painted, so another workspace or indexing policy cannot
// flash stale private content into the current sky.
(()=>{
 const dbName='aide-starmap-cache-v1',storeName='graphs',maxChars=24*1024*1024;
 let opening;
 function database(){
  if(!('indexedDB'in window))return Promise.resolve(null);
  if(!opening)opening=new Promise(resolve=>{
   let request;try{request=indexedDB.open(dbName,1);}catch{return resolve(null);}
   request.onupgradeneeded=()=>{if(!request.result.objectStoreNames.contains(storeName))request.result.createObjectStore(storeName);};
   request.onsuccess=()=>resolve(request.result);request.onerror=()=>resolve(null);request.onblocked=()=>resolve(null);
  });
  return opening;
 }
 function transaction(db,mode,run){return new Promise(resolve=>{let result=null;try{const tx=db.transaction(storeName,mode),store=tx.objectStore(storeName);result=run(store);tx.oncomplete=()=>resolve(result?.result??result);tx.onerror=tx.onabort=()=>resolve(null);}catch{resolve(null);}});}
 async function read(mode,identity){const db=await database();if(!db)return null;const value=await transaction(db,'readonly',store=>store.get(mode));return value?.schema===1&&value.mode===mode&&value.workspace===identity.workspace&&value.epoch===identity.epoch&&value.policy===identity.policy?value:null;}
 function estimate(value){if(typeof value==='string')return value.length*6+2;if(typeof value==='number')return 20;if(typeof value==='boolean')return 6;if(Array.isArray(value))return 2+value.reduce((sum,item)=>sum+estimate(item),0);if(value&&typeof value==='object')return 2+Object.entries(value).reduce((sum,[key,item])=>sum+key.length*6+3+estimate(item),0);return 8;}
 async function write(mode,identity,graph,revision){
  if(!revision||!Array.isArray(graph?.nodes)||!Array.isArray(graph?.edges))return false;
  const chars=estimate(graph);if(chars>maxChars)return false;
  const db=await database();if(!db)return false;
  const value={schema:1,mode,workspace:identity.workspace,epoch:identity.epoch,policy:identity.policy,revision,savedAt:Date.now(),chars,graph:{nodes:graph.nodes,edges:graph.edges,sources:graph.sources||[],warnings:graph.warnings||[],truncated:!!graph.truncated,code:graph.code||null}};
  return (await transaction(db,'readwrite',store=>store.put(value,mode)))!==null;
 }
 window.AideStarmapCache={read,write};
})();
