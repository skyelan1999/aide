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
 async function read(mode,identity){
  let timer;const deadline=new Promise(resolve=>{timer=setTimeout(()=>resolve(null),250);});
  const pending=(async()=>{const db=await database();if(!db)return null;const value=await transaction(db,'readonly',store=>store.get(mode));return value?.schema===1&&value.mode===mode&&value.workspace===identity.workspace&&value.epoch===identity.epoch&&value.policy===identity.policy?value:null;})();
  try{return await Promise.race([pending,deadline]);}finally{clearTimeout(timer);}
 }

 const nodeKeys=['id','name','kind','region','path','root','source','sourceName','sourceType','origin','format','session','number','text','size','modified','language','symbolKind','line','endLine','parentFile','signature','contentHash'];
 const edgeKeys=['from','to','kind','line','column','evidence','confidence'];
 const wire=(value,keys)=>Object.fromEntries(keys.filter(k=>value[k]!==undefined).map(k=>[k,value[k]]));
 async function write(mode,identity,graph,revision){
  if(!revision||!Array.isArray(graph?.nodes)||!Array.isArray(graph?.edges))return false;
  // Rendered nodes/edges contain cyclic cluster/projected references. Persist
  // only wire fields, never canvas/layout objects or duplicated scene members.
  const snapshot={nodes:graph.nodes.map(n=>wire(n,nodeKeys)),edges:graph.edges.map(e=>wire(e,edgeKeys)),sources:graph.sources||[],warnings:graph.warnings||[],truncated:!!graph.truncated,code:graph.code||null};
  let chars;try{chars=JSON.stringify(snapshot).length;}catch{return false;}
  if(chars>maxChars)return false;
  const db=await database();if(!db)return false;
  const value={schema:1,mode,workspace:identity.workspace,epoch:identity.epoch,policy:identity.policy,revision,savedAt:Date.now(),chars,graph:snapshot};
  return (await transaction(db,'readwrite',store=>store.put(value,mode)))!==null;
 }
 window.AideStarmapCache={read,write};
})();
