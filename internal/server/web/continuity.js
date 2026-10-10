/* Local recovery records are separate from server saves and do not read configuration credentials. */
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
  // Each loaded document owns a branch. Identical restoration keeps the
  // original owner so the next edit cannot overwrite another draft.
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
      const next=branches.filter(b=>b.branch!==fileBranch);
      if(!next.some(b=>b.text===value.text&&b.hash===value.hash))next.push({...value,branch:fileBranch,updated:Date.now()});
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
  // Chat identity includes attachments: equal text with different task sources
  // is still a divergent draft. Positions travel with the selected branch.
  const chatBranches=new Map();
  const chatBranchFor=(scope,id)=>{const k=key(scope,'chat',id);if(!chatBranches.has(k))chatBranches.set(k,crypto.randomUUID());return chatBranches.get(k);};
  const sameChat=(a,b)=>a?.text===b?.text&&JSON.stringify(a?.attachments||[])===JSON.stringify(b?.attachments||[]);
  async function chatTransaction(scope,id,change){
    const db=await open();return new Promise((resolve,reject)=>{
      const tx=db.transaction('records','readwrite'),store=tx.objectStore('records');
      const request=store.get(key(scope,'chat-branches',id));let failure;
      request.onsuccess=()=>{try{change(store,request.result?.value||[]);}catch(e){failure=e;tx.abort();}};
      tx.oncomplete=()=>resolve(true);tx.onerror=()=>reject(failure||tx.error);tx.onabort=()=>reject(failure||tx.error);
    });
  }
  async function writeChatDraft(scope,id,value){
    if(!enabled()||!scope)return false;
    const serialized=JSON.stringify(value);
    if(new Blob([serialized]).size>2*1024*1024)throw new Error('本地恢复内容超过 2 MiB，尚未保存草稿');
    sessionStorage.setItem(tabKey(scope,'chat',id),serialized);
    return chatTransaction(scope,id,(store,branches)=>{
      const chatBranch=chatBranchFor(scope,id),next=branches.filter(b=>b.branch!==chatBranch);
      // Restoring identical content must not transfer another document's
      // branch ownership: a later edit in this tab must preserve that draft.
      if((value.text||value.attachments?.length)&&!next.some(b=>sameChat(b,value)))next.push({...value,branch:chatBranch,updated:Date.now()});
      if(next.length>32||new Blob([JSON.stringify(next)]).size>8*1024*1024)
        throw new Error('此会话恢复草稿已达上限；当前编辑仅保留在本标签页');
      store.put({key:key(scope,'chat-branches',id),value:next,updated:Date.now()});
      store.put({key:key(scope,'chat',id),value,updated:Date.now()});
    });
  }
  async function adoptChatDraft(scope,id,draft){
    const k=key(scope,'chat',id),prior=chatBranches.get(k),cacheKey=tabKey(scope,'chat',id),priorCache=sessionStorage.getItem(cacheKey),adopted=JSON.stringify(draft);
    chatBranches.set(k,crypto.randomUUID());
    try{return await writeChatDraft(scope,id,draft);}catch(e){
      if(prior)chatBranches.set(k,prior);else chatBranches.delete(k);
      // Roll back only our attempted cache write; retain later user edits.
      if(sessionStorage.getItem(cacheKey)===adopted){if(priorCache===null)sessionStorage.removeItem(cacheKey);else sessionStorage.setItem(cacheKey,priorCache);}
      throw e;
    }
  }
  async function listChatDrafts(scope,id){
    if(!enabled()||!scope)return [];
    const branches=await read(scope,'chat-branches',id)||[],own=await readTab(scope,'chat',id);
    if((own?.text||own?.attachments?.length)&&!branches.some(b=>sameChat(b,own)))branches.push({...own,branch:'legacy',updated:0});
    return branches.sort((a,b)=>Number(sameChat(b,own))-Number(sameChat(a,own))||b.updated-a.updated);
  }
  async function sentChatDraft(scope,id,draft){
    if(!enabled()||!scope)return false;
    await chatTransaction(scope,id,(store,branches)=>{
      store.put({key:key(scope,'chat-branches',id),value:branches.filter(b=>!sameChat(b,draft)),updated:Date.now()});
      const request=store.get(key(scope,'chat',id));
      request.onsuccess=()=>{if(sameChat(request.result?.value,draft))store.delete(key(scope,'chat',id));};
    });
    const cached=sessionStorage.getItem(tabKey(scope,'chat',id));
    if(cached!==null&&sameChat(JSON.parse(cached),draft))sessionStorage.setItem(tabKey(scope,'chat',id),'null');
    return true;
  }
  async function dismissChatDraft(scope,id,draft){
    if(!enabled()||!scope)return false;
    await chatTransaction(scope,id,(store,branches)=>{
      store.put({key:key(scope,'chat-branches',id),value:branches.filter(b=>b.branch!==draft.branch),updated:Date.now()});
      const request=store.get(key(scope,'chat',id));
      request.onsuccess=()=>{if(sameChat(request.result?.value,draft))store.delete(key(scope,'chat',id));};
    });
    const cached=sessionStorage.getItem(tabKey(scope,'chat',id));
    if(cached!==null&&sameChat(JSON.parse(cached),draft))sessionStorage.setItem(tabKey(scope,'chat',id),'null');
    return true;
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
    if(kind==='chat')return writeChatDraft(scope,id,value);
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
  // Management reads remain available when recovery is disabled. A read-only
  // cursor takes one transaction snapshot; no draft is evicted or rewritten.
  async function recoverySnapshot(includeValues=false){
    const db=await open();return new Promise((resolve,reject)=>{
      const tx=db.transaction('records','readonly'),store=tx.objectStore('records'),request=store.openCursor();
      const records=[],groups=new Map();let bytes=0,failure;
      request.onsuccess=()=>{const cursor=request.result;if(!cursor)return;
        try {
          const record=cursor.value,parts=JSON.parse(record.key);
          if(['recovery-operation','recovery-policy'].includes(parts?.[1])){cursor.continue();return;}
          if(!Array.isArray(parts)||parts.length!==3)throw Error('恢复记录键无效');
          const size=new Blob([JSON.stringify(record)]).size;bytes+=size;
          if(includeValues&&bytes>64*1024*1024)throw Error('恢复备份超过 64 MiB，请保留现有记录并分批处理');
          const scope=String(parts[0]),kind=String(parts[1]);
          const group=groups.get(scope)||{scope,count:0,bytes:0,kinds:{}};
          group.count++;group.bytes+=size;group.kinds[kind]=(group.kinds[kind]||0)+1;groups.set(scope,group);
          if(includeValues)records.push(record);
          cursor.continue();
        }catch(e){failure=e;tx.abort();}
      };
      tx.oncomplete=()=>resolve({count:[...groups.values()].reduce((n,g)=>n+g.count,0),bytes,groups:[...groups.values()],...(includeValues?{records}: {})});
      tx.onerror=()=>reject(failure||tx.error);tx.onabort=()=>reject(failure||tx.error);
    });
  }
  async function inventory(){
    const result=await recoverySnapshot();let originStorage=null;
    try{const estimate=await navigator.storage?.estimate();if(estimate)originStorage={usage:estimate.usage,quota:estimate.quota};}catch(_e){}
    return {...result,originStorage};
  }
  async function exportRecovery(){
    const snapshot=await recoverySnapshot(true);
    const backup={format:'aide-local-recovery',schema:1,created:new Date().toISOString(),...snapshot};
    if(new Blob([JSON.stringify(backup)]).size>64*1024*1024)throw Error('恢复备份超过 64 MiB，请保留现有记录并分批处理');
    return backup;
  }
  window.AideContinuity={...window.AideRecoveryTransfer?.create({open}),inventory,exportRecovery,enabled,read,write,readTab,writeTab,writeChatDraft,adoptChatDraft,listChatDrafts,dismissChatDraft,sentChatDraft,remove,removeMatching,removeMatchingTab,writeFileDraft,listFileDrafts,dismissFileDraft,savedFileDraft};
  // Default retention is disabled. Only maintain confirmed undo-backup policy;
  // hidden or locked pages do not purge, and live tab snapshots are untouched.
  if(typeof window.setInterval==='function')window.setInterval(()=>{
    const active=()=>document.visibilityState!=='hidden'&&window.LockCluster?.snapshot()?.settled&&!window.LockCluster.snapshot().locked;
    if(active())window.AideContinuity.pruneExpiredRecoveryOperations({active}).then(()=>{window.aideRecoveryMaintenanceError='';}).catch(e=>{window.aideRecoveryMaintenanceError=String(e.message||e);});
  },60000);
})();
