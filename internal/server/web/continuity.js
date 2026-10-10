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
  // Each loaded document owns a branch. Reloads may consolidate identical
  // content, but divergent documents never replace each other's branch.
  const fileBranch=crypto.randomUUID();
  async function fileTransaction(scope,id,change){
    const db=await open();return new Promise((resolve,reject)=>{
      const tx=db.transaction('records','readwrite'),store=tx.objectStore('records');
      const request=store.get(key(scope,'file-branches',id));let failure;
      request.onsuccess=()=>{try{change(store,request.result?.value||[]);}catch(e){failure=e;tx.abort();}};
      tx.oncomplete=()=>resolve(true);tx.onerror=()=>reject(failure||tx.error);tx.onabort=()=>reject(failure||tx.error);
    });
  }
  async function writeFileDraft(scope,id,value){
    if(!enabled()||!scope)return false;
    const serialized=JSON.stringify(value);
    if(new Blob([serialized]).size>2*1024*1024)throw new Error('本地恢复内容超过 2 MiB，尚未保存草稿');
    sessionStorage.setItem(tabKey(scope,'file',id),serialized);
    return fileTransaction(scope,id,(store,branches)=>{
      const next=branches.filter(b=>b.branch!==fileBranch&&!(b.text===value.text&&b.hash===value.hash));
      next.push({...value,branch:fileBranch,updated:Date.now()});
      if(next.length>32||new Blob([JSON.stringify(next)]).size>8*1024*1024)
        throw new Error('此文件恢复草稿已达上限；请保存或逐项忽略旧草稿，当前编辑仅保留在本标签页');
      store.put({key:key(scope,'file-branches',id),value:next,updated:Date.now()});
      store.put({key:key(scope,'file',id),value,updated:Date.now()});
    });
  }
  async function listFileDrafts(scope,id){
    if(!enabled()||!scope)return [];
    const branches=await read(scope,'file-branches',id)||[],legacy=await readTab(scope,'file',id);
    if(legacy&&typeof legacy.text==='string'&&!branches.some(b=>b.text===legacy.text&&b.hash===legacy.hash))branches.push({...legacy,branch:'legacy',updated:0});
    return branches.sort((a,b)=>{
      const own=d=>legacy&&d.text===legacy.text&&d.hash===legacy.hash;
      return Number(own(b))-Number(own(a))||b.updated-a.updated;
    });
  }
  async function dismissFileDraft(scope,id,draft){
    await fileTransaction(scope,id,(store,branches)=>{
      store.put({key:key(scope,'file-branches',id),value:branches.filter(b=>b.branch!==draft.branch),updated:Date.now()});
    });
    await removeMatchingTab(scope,'file',id,draft.text);
  }
  async function savedFileDraft(scope,id,text){
    await fileTransaction(scope,id,(store,branches)=>{
      store.put({key:key(scope,'file-branches',id),value:branches.filter(b=>b.text!==text),updated:Date.now()});
    });
    await removeMatchingTab(scope,'file',id,text);
  }
  async function read(scope,kind,id){if(!enabled()||!scope)return null;const record=await run('readonly',store=>store.get(key(scope,kind,id)));return record?.value||null;}
  async function write(scope,kind,id,value){
    if(!enabled()||!scope)return false;
    if(new Blob([JSON.stringify(value)]).size>2*1024*1024)throw new Error('本地恢复内容超过 2 MiB，尚未保存草稿');
    await run('readwrite',store=>store.put({key:key(scope,kind,id),value,updated:Date.now()}));return true;
  }
  // sessionStorage belongs to a browsing context: even an opener's initial
  // copy becomes independent on the first edit. IndexedDB remains the shared
  // fallback for a newly opened tab; it must not overwrite a live tab on reload.
  const tabKey=(scope,kind,id)=>'aide-recovery-tab:'+key(scope,kind,id);
  async function readTab(scope,kind,id){
    if(!enabled()||!scope)return null;
    const cached=sessionStorage.getItem(tabKey(scope,kind,id));
    if(cached!==null)return JSON.parse(cached);
    const value=await read(scope,kind,id);
    // An edit or another restoration may finish while IndexedDB is loading.
    // Never replace that newer tab snapshot with the shared fallback.
    const latest=sessionStorage.getItem(tabKey(scope,kind,id));
    if(latest!==null)return JSON.parse(latest);
    // Cache null as well: another tab creating a draft later must not change
    // this tab's initial empty scene or composer.
    sessionStorage.setItem(tabKey(scope,kind,id),JSON.stringify(value));
    return value;
  }
  async function writeTab(scope,kind,id,value){
    if(!enabled()||!scope)return false;
    const serialized=JSON.stringify(value);
    if(new Blob([serialized]).size>2*1024*1024)throw new Error('本地恢复内容超过 2 MiB，尚未保存草稿');
    // The synchronous tab snapshot survives pagehide even if its IndexedDB
    // transaction has not completed when the browser unloads the document.
    sessionStorage.setItem(tabKey(scope,kind,id),serialized);
    return write(scope,kind,id,value);
  }
  async function remove(scope,kind,id){if(scope)await run('readwrite',store=>store.delete(key(scope,kind,id)));}
  async function removeMatching(scope,kind,id,text){
    const db=await open();return new Promise((resolve,reject)=>{
      const tx=db.transaction('records','readwrite'),store=tx.objectStore('records'),request=store.get(key(scope,kind,id));
      request.onsuccess=()=>{if(request.result?.value?.text===text)store.delete(key(scope,kind,id));};
      tx.oncomplete=()=>resolve();tx.onerror=()=>reject(tx.error);tx.onabort=()=>reject(tx.error);
    });
  }
  async function removeMatchingTab(scope,kind,id,text){
    // Clear only this browsing context's matching draft. A different tab's
    // shared fallback must survive when its content differs from the save.
    const cached=sessionStorage.getItem(tabKey(scope,kind,id));
    if(cached!==null&&JSON.parse(cached)?.text===text)
      sessionStorage.setItem(tabKey(scope,kind,id),'null');
    await removeMatching(scope,kind,id,text);
  }
  window.AideContinuity={enabled,read,write,readTab,writeTab,remove,removeMatching,removeMatchingTab,writeFileDraft,listFileDrafts,dismissFileDraft,savedFileDraft};
})();
