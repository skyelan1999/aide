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
  const guardedWrites=new Set();
  function trackGuardedWrite(tx){guardedWrites.add(tx);const done=()=>guardedWrites.delete(tx);tx.addEventListener?.('complete',done,{once:true});tx.addEventListener?.('abort',done,{once:true});}
  function abortPendingWrites(){for(const tx of guardedWrites){try{tx.abort();}catch(_e){}}guardedWrites.clear();}
  async function run(mode,operation,active){const db=await open();return new Promise((resolve,reject)=>{
    if(active&&!active()){reject(new Error('恢复写入已取消'));return;}
    const tx=db.transaction('records',mode);if(mode==='readwrite')trackGuardedWrite(tx);let request;
    try{if(active&&!active()){tx.abort();reject(new Error('恢复写入已取消'));return;}request=operation(tx.objectStore('records'));}
    catch(error){try{tx.abort();}catch(_e){}reject(error);return;}
    tx.oncomplete=()=>{guardedWrites.delete(tx);if(active&&!active())reject(new Error('恢复写入已取消'));else resolve(request.result);};
    tx.onerror=()=>{guardedWrites.delete(tx);reject(tx.error);};
    tx.onabort=()=>{guardedWrites.delete(tx);reject(tx.error||new Error('恢复存储事务中断'));};
  });}
  const key=(scope,kind,id)=>JSON.stringify([scope,kind,id]);
  // Draft text and its base identify content; the cursor and scroll identify
  // the view that must be recovered. Keep both when pruning shared snapshots
  // so dismissing an older view cannot erase a newer same-text snapshot.
  const sameFileDraft=(a,b)=>a?.text===b?.text&&a?.hash===b?.hash&&a?.start===b?.start&&a?.end===b?.end&&a?.scrollTop===b?.scrollTop&&a?.scrollLeft===b?.scrollLeft;
  // Each loaded document owns a branch. Identical restoration keeps the
  // original owner so the next edit cannot overwrite another draft.
  const fileBranch=crypto.randomUUID();
  async function fileTransaction(scope,id,change,active){
    const db=await open();return new Promise((resolve,reject)=>{
      if(active&&!active()){reject(new Error('恢复写入已取消'));return;}
      const tx=db.transaction('records','readwrite'),store=tx.objectStore('records');
      trackGuardedWrite(tx);
      const request=store.get(key(scope,'file-branches',id));let failure;
      request.onsuccess=()=>{if(active&&!active()){failure=new Error('恢复写入已取消');tx.abort();return;}try{change(store,request.result?.value||[]);}catch(e){failure=e;tx.abort();}};
      tx.oncomplete=()=>{guardedWrites.delete(tx);if(active&&!active())reject(new Error('恢复写入已取消'));else resolve(true);};
      tx.onerror=()=>{guardedWrites.delete(tx);reject(failure||tx.error);};tx.onabort=()=>{guardedWrites.delete(tx);reject(failure||tx.error||new Error('恢复存储事务中断'));};
    });
  }
  async function writeFileDraft(scope,id,value,active){
    if(!enabled()||!scope)return false;
    if(active&&!active())return false;
    const serialized=JSON.stringify(value);
    if(new Blob([serialized]).size>2*1024*1024)throw new Error('本地恢复内容超过 2 MiB，尚未保存草稿');
    writeTabCache(tabKey(scope,'file',id),serialized);
    return fileTransaction(scope,id,(store,branches)=>{
      const next=branches.filter(b=>b.branch!==fileBranch);
      if(!next.some(b=>b.text===value.text&&b.hash===value.hash))next.push({...value,branch:fileBranch,updated:Date.now()});
      if(next.length>32||new Blob([JSON.stringify(next)]).size>8*1024*1024)
        throw new Error('此文件恢复草稿已达上限；请保存或逐项忽略旧草稿，当前编辑仅保留在本标签页');
      store.put({key:key(scope,'file-branches',id),value:next,updated:Date.now()});
      store.put({key:key(scope,'file',id),value,updated:Date.now()});
    },active);
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
      // The shared last-writer fallback is also consulted by newly opened
      // tabs. Remove it in the same transaction when it is this exact draft,
      // or a dismissed branch can reappear after a reload in another tab.
      const request=store.get(key(scope,'file',id));
      request.onsuccess=()=>{const saved=request.result?.value;if(sameFileDraft(saved,draft))store.delete(key(scope,'file',id));};
    });
    await removeMatchingTab(scope,'file',id,draft.text,{draft});
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
  async function chatTransaction(scope,id,change,active){
    const db=await open();return new Promise((resolve,reject)=>{
      if(active&&!active()){reject(new Error('恢复写入已取消'));return;}
      const tx=db.transaction('records','readwrite'),store=tx.objectStore('records');trackGuardedWrite(tx);
      const request=store.get(key(scope,'chat-branches',id));let failure;
      request.onsuccess=()=>{if(active&&!active()){failure=new Error('恢复写入已取消');tx.abort();return;}try{change(store,request.result?.value||[]);}catch(e){failure=e;tx.abort();}};
      tx.oncomplete=()=>{guardedWrites.delete(tx);if(active&&!active())reject(new Error('恢复写入已取消'));else resolve(true);};
      tx.onerror=()=>{guardedWrites.delete(tx);reject(failure||tx.error);};
      tx.onabort=()=>{guardedWrites.delete(tx);reject(failure||tx.error||new Error('恢复存储事务中断'));};
    });
  }
  async function writeChatDraft(scope,id,value,active){
    if(!enabled()||!scope)return false;
    if(active&&!active())return false;
    const serialized=JSON.stringify(value);
    if(new Blob([serialized]).size>2*1024*1024)throw new Error('本地恢复内容超过 2 MiB，尚未保存草稿');
    writeTabCache(tabKey(scope,'chat',id),serialized);
    return chatTransaction(scope,id,(store,branches)=>{
      const chatBranch=chatBranchFor(scope,id),next=branches.filter(b=>b.branch!==chatBranch);
      // Restoring identical content must not transfer another document's
      // branch ownership: a later edit in this tab must preserve that draft.
      if((value.text||value.attachments?.length)&&!next.some(b=>sameChat(b,value)))next.push({...value,branch:chatBranch,updated:Date.now()});
      if(next.length>32||new Blob([JSON.stringify(next)]).size>8*1024*1024)
        throw new Error('此会话恢复草稿已达上限；当前编辑仅保留在本标签页');
      store.put({key:key(scope,'chat-branches',id),value:next,updated:Date.now()});
      store.put({key:key(scope,'chat',id),value,updated:Date.now()});
    },active);
  }
  async function adoptChatDraft(scope,id,draft){
    const k=key(scope,'chat',id),prior=chatBranches.get(k),cacheKey=tabKey(scope,'chat',id),priorCache=readTabCache(cacheKey),adopted=JSON.stringify(draft);
    chatBranches.set(k,crypto.randomUUID());
    try{return await writeChatDraft(scope,id,draft);}catch(e){
      if(prior)chatBranches.set(k,prior);else chatBranches.delete(k);
      // Roll back only our attempted cache write; retain later user edits.
      const currentCache=readTabCache(cacheKey);
      if(currentCache.available&&currentCache.value===adopted){if(priorCache.available&&priorCache.value!==null)writeTabCache(cacheKey,priorCache.value);else{try{sessionStorage.removeItem(cacheKey);}catch(_ignored){}}}
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
    const cacheKey=tabKey(scope,'chat',id),cached=readTabCache(cacheKey),parsed=cached.available&&cached.value!==null?parseTabSnapshot(cached.value):null;
    if(parsed?.ok&&sameChat(parsed.value,draft))writeTabCache(cacheKey,'null');
    return true;
  }
  async function dismissChatDraft(scope,id,draft){
    if(!enabled()||!scope)return false;
    await chatTransaction(scope,id,(store,branches)=>{
      store.put({key:key(scope,'chat-branches',id),value:branches.filter(b=>b.branch!==draft.branch),updated:Date.now()});
      const request=store.get(key(scope,'chat',id));
      request.onsuccess=()=>{if(sameChat(request.result?.value,draft))store.delete(key(scope,'chat',id));};
    });
    const cacheKey=tabKey(scope,'chat',id),cached=readTabCache(cacheKey),parsed=cached.available&&cached.value!==null?parseTabSnapshot(cached.value):null;
    if(parsed?.ok&&sameChat(parsed.value,draft))writeTabCache(cacheKey,'null');
    return true;
  }
  async function read(scope,kind,id){if(!enabled()||!scope)return null;const record=await run('readonly',store=>store.get(key(scope,kind,id)));return record?.value||null;}
  async function write(scope,kind,id,value,active){
    if(!enabled()||!scope)return false;
    if(active&&!active())return false;
    if(new Blob([JSON.stringify(value)]).size>2*1024*1024)throw new Error('本地恢复内容超过 2 MiB，尚未保存草稿');
    await run('readwrite',store=>store.put({key:key(scope,kind,id),value,updated:Date.now()}),active);return true;
  }
  // sessionStorage belongs to a browsing context: even an opener's initial
  // copy becomes independent on the first edit. IndexedDB remains the shared
  // fallback for a newly opened tab; it must not overwrite a live tab on reload.
  const tabKey=(scope,kind,id)=>'aide-recovery-tab:'+key(scope,kind,id);
  function readTabCache(cacheKey){try{return {available:true,value:sessionStorage.getItem(cacheKey)};}catch(error){return {available:false,value:null,error};}}
  function writeTabCache(cacheKey,serialized,{required=false}={}){try{sessionStorage.setItem(cacheKey,serialized);return true;}catch(error){if(required)throw error;return false;}}
  // Keep the newest editable text and view position in the tab-local
  // synchronous store. IndexedDB still owns durable, shared branches; this
  // fast path closes the debounce window if a tab is killed before its
  // transaction runs.
  function cacheTab(scope,kind,id,value,{active}={}){
    if(!enabled()||!scope||!['chat','file','file-view'].includes(kind))return false;
    if(active&&!active())return false;
    const serialized=JSON.stringify(value);
    if(new Blob([serialized]).size>2*1024*1024)throw new Error('本地恢复内容超过 2 MiB，尚未保存草稿');
    writeTabCache(tabKey(scope,kind,id),serialized,{required:true});
    return true;
  }
  function parseTabSnapshot(raw){
    try{return {ok:true,value:JSON.parse(raw)};}catch(error){return {ok:false,error};}
  }
  async function readTab(scope,kind,id){
    if(!enabled()||!scope)return null;
    const cacheKey=tabKey(scope,kind,id),initial=readTabCache(cacheKey),cached=initial.available?initial.value:null;
    let corruptCache=false;
    if(cached!==null){const parsed=parseTabSnapshot(cached);if(parsed.ok)return parsed.value;corruptCache=true;}
    const value=await read(scope,kind,id);
    // An edit or another restoration may finish while IndexedDB is loading.
    // Never replace that newer tab snapshot with the shared fallback.
    const latest=readTabCache(cacheKey);
    if(latest.available&&latest.value!==null){const parsed=parseTabSnapshot(latest.value);if(parsed.ok)return parsed.value;corruptCache=true;}
    // A malformed synchronous snapshot must not hide a still-valid IndexedDB
    // record. Heal it from the durable value when available; if there is no
    // durable value, keep the corrupt bytes for inspection instead of
    // pretending the local record was cleanly initialized.
    if(corruptCache){if(value!==null)writeTabCache(cacheKey,JSON.stringify(value));return value;}
    // Cache null as well: another tab creating a draft later must not change
    // this tab's initial empty scene or composer.
    writeTabCache(cacheKey,JSON.stringify(value));
    return value;
  }
  async function writeTab(scope,kind,id,value,{active}={}){
    if(active&&!active())return false;
    if(kind==='chat')return writeChatDraft(scope,id,value,active);
    if(!enabled()||!scope)return false;
    const serialized=JSON.stringify(value);
    if(new Blob([serialized]).size>2*1024*1024)throw new Error('本地恢复内容超过 2 MiB，尚未保存草稿');
    // The synchronous tab snapshot survives pagehide even if its IndexedDB
    // transaction has not completed when the browser unloads the document.
    writeTabCache(tabKey(scope,kind,id),serialized);
    return write(scope,kind,id,value,active);
  }
  async function remove(scope,kind,id){if(scope)await run('readwrite',store=>store.delete(key(scope,kind,id)));}
  async function removeMatching(scope,kind,id,text,expected={}){
    const db=await open();return new Promise((resolve,reject)=>{
      const tx=db.transaction('records','readwrite'),store=tx.objectStore('records'),request=store.get(key(scope,kind,id));
      request.onsuccess=()=>{const value=request.result?.value;if(value?.text===text&&(Object.hasOwn(expected,'draft')?sameFileDraft(value,expected.draft):!Object.hasOwn(expected,'hash')||value?.hash===expected.hash))store.delete(key(scope,kind,id));};
      tx.oncomplete=()=>resolve();tx.onerror=()=>reject(tx.error);tx.onabort=()=>reject(tx.error);
    });
  }
  async function removeMatchingTab(scope,kind,id,text,expected={}){
    // Clear only this browsing context's matching draft. A different tab's
    // shared fallback must survive when its content differs from the save.
    const cacheKey=tabKey(scope,kind,id),cached=readTabCache(cacheKey),parsed=cached.available&&cached.value!==null?parseTabSnapshot(cached.value):null;
    if(parsed?.ok&&parsed.value?.text===text&&(Object.hasOwn(expected,'draft')?sameFileDraft(parsed.value,expected.draft):!Object.hasOwn(expected,'hash')||parsed.value?.hash===expected.hash))writeTabCache(cacheKey,'null');
    await removeMatching(scope,kind,id,text,expected);
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
  window.AideContinuity={...window.AideRecoveryTransfer?.create({open}),inventory,exportRecovery,enabled,read,write,readTab,writeTab,cacheTab,writeChatDraft,adoptChatDraft,listChatDrafts,dismissChatDraft,sentChatDraft,remove,removeMatching,removeMatchingTab,writeFileDraft,listFileDrafts,dismissFileDraft,savedFileDraft,abortPendingWrites};
  // Default retention is disabled. Only maintain confirmed undo-backup policy;
  // hidden or locked pages do not purge, and live tab snapshots are untouched.
  if(typeof window.setInterval==='function')window.setInterval(()=>{
    const active=()=>document.visibilityState!=='hidden'&&window.LockCluster?.snapshot()?.settled&&!window.LockCluster.snapshot().locked;
    // The starmap loads continuity without the optional recovery-transfer
    // module. Do not throw every minute on pages that have no retention API.
    if(active()&&typeof window.AideContinuity?.pruneExpiredRecoveryOperations==='function')window.AideContinuity.pruneExpiredRecoveryOperations({active}).then(()=>{window.aideRecoveryMaintenanceError='';}).catch(e=>{window.aideRecoveryMaintenanceError=String(e.message||e);});
  },60000);
})();
