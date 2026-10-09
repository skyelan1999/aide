/* Local recovery records are separate from server saves and never contain credentials. */
(() => {
  'use strict';
  let database;
  const enabled=()=>window.aideUI?.get('workRecovery')!==false;
  const open=()=>database||(database=new Promise((resolve,reject)=>{
    const request=indexedDB.open('aide-recovery',1);
    request.onupgradeneeded=()=>request.result.createObjectStore('records',{keyPath:'key'});
    request.onsuccess=()=>resolve(request.result);request.onerror=()=>reject(request.error);
    request.onblocked=()=>reject(new Error('恢复存储暂不可用'));
  }).catch(error=>{database=null;throw error;}));
  async function run(mode,operation){const db=await open();return new Promise((resolve,reject)=>{
    const tx=db.transaction('records',mode),request=operation(tx.objectStore('records'));
    tx.oncomplete=()=>resolve(request.result);tx.onerror=()=>reject(tx.error);tx.onabort=()=>reject(tx.error||new Error('恢复存储事务中断'));
  });}
  const key=(scope,kind,id)=>JSON.stringify([scope,kind,id]);
  async function read(scope,kind,id){if(!enabled()||!scope)return null;const record=await run('readonly',store=>store.get(key(scope,kind,id)));return record?.value||null;}
  async function write(scope,kind,id,value){
    if(!enabled()||!scope)return false;
    if(new Blob([JSON.stringify(value)]).size>2*1024*1024)throw new Error('本地恢复内容超过 2 MiB，尚未保存草稿');
    await run('readwrite',store=>store.put({key:key(scope,kind,id),value,updated:Date.now()}));return true;
  }
  async function remove(scope,kind,id){if(scope)await run('readwrite',store=>store.delete(key(scope,kind,id)));}
  async function removeMatching(scope,kind,id,text){
    const db=await open();return new Promise((resolve,reject)=>{
      const tx=db.transaction('records','readwrite'),store=tx.objectStore('records'),request=store.get(key(scope,kind,id));
      request.onsuccess=()=>{if(request.result?.value?.text===text)store.delete(key(scope,kind,id));};
      tx.oncomplete=()=>resolve();tx.onerror=()=>reject(tx.error);tx.onabort=()=>reject(tx.error);
    });
  }
  window.AideContinuity={enabled,read,write,remove,removeMatching};
})();
