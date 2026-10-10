/* Recovery transfer changes shared browser records only, never live tab snapshots. */
(() => {
  'use strict';
  const LIMIT=64*1024*1024,operationKind='recovery-operation';
  const bytes=value=>new Blob([JSON.stringify(value)]).size;
  const same=(a,b)=>JSON.stringify(a??null)===JSON.stringify(b??null);
  const operationKey=id=>JSON.stringify(['',operationKind,id]);
  function validate(backup){
    if(!backup||backup.format!=='aide-local-recovery'||backup.schema!==1||!Array.isArray(backup.records)||backup.count!==backup.records.length||bytes(backup)>LIMIT)throw Error('恢复备份格式、数量或大小无效');
    const keys=new Set(),kinds=new Set(['chat','scene','file','file-view','file-branches','chat-branches']);
    const check=(value,depth=0)=>{if(depth>64)throw Error('恢复备份内容嵌套过深');if(value&&typeof value==='object')for(const k of Object.keys(value)){if(['__proto__','constructor','prototype'].includes(k))throw Error('恢复备份包含不支持的字段');check(value[k],depth+1);}};
    for(const r of backup.records){
      const p=typeof r?.key==='string'?JSON.parse(r.key):null;
      if(!p||p.length!==3||!p.every(x=>typeof x==='string')||!p[0]||p[0].length>4096||p[2].length>16384||!kinds.has(p[1])||JSON.stringify(p)!==r.key||keys.has(r.key)||!Number.isFinite(r.updated)||!r.value||typeof r.value!=='object')throw Error('恢复备份记录无效或重复');
      keys.add(r.key);check(r.value);
      if(['file-branches','chat-branches'].includes(p[1])){
        if(!Array.isArray(r.value)||r.value.length>32||bytes(r.value)>8*1024*1024||r.value.some(b=>!b||typeof b.text!=='string'||typeof b.branch!=='string'||bytes(b)>2*1024*1024))throw Error('文件草稿分支无效或超出上限');
      }else if(Array.isArray(r.value)||bytes(r.value)>2*1024*1024)throw Error('恢复记录超出上限');
      if(['chat','file'].includes(p[1])&&typeof r.value.text!=='string')throw Error('恢复文字记录无效');
    }
    return backup.records.map(r=>({key:r.key,value:structuredClone(r.value),updated:r.updated}));
  }
  function create({open}){
    async function collect(){const db=await open();return new Promise((resolve,reject)=>{const tx=db.transaction('records','readonly'),req=tx.objectStore('records').openCursor(),all=new Map();req.onsuccess=()=>{const c=req.result;if(c){all.set(c.value.key,c.value);c.continue();}};tx.oncomplete=()=>resolve(all);tx.onerror=tx.onabort=()=>reject(tx.error||Error('恢复存储事务中断'));});}
    async function previewImport(backup){const records=validate(backup),all=await collect(),entries=records.map(record=>({record,before:all.get(record.key)||null}));return {entries,added:entries.filter(e=>!e.before).length,identical:entries.filter(e=>e.before&&same(e.record.value,e.before.value)).length,conflicts:entries.filter(e=>e.before&&!same(e.record.value,e.before.value)).map(e=>e.record.key)};}
    async function importRecovery(plan,{replace=false,active=()=>true}={}){
      const records=validate({format:'aide-local-recovery',schema:1,count:plan.entries.length,records:plan.entries.map(e=>e.record)});
      const db=await open();return new Promise((resolve,reject)=>{
        const tx=db.transaction('records','readwrite'),store=tx.objectStore('records');let failure,result,pending=records.length;const changes=[];
        const fail=e=>{failure=e;tx.abort();};
        const finish=()=>{try{if(!active())throw Error('恢复操作已取消');if(!changes.length){result={count:0,id:null};return;}
          const id=crypto.randomUUID(),operation={key:operationKey(id),updated:Date.now(),value:{id,type:'import',changes}};
          if(bytes(operation)>LIMIT)throw Error('恢复操作备份超过 64 MiB');
          for(const c of changes)store.put(c.after);store.put(operation);result={count:changes.length,id};
        }catch(e){fail(e);}};
        for(let i=0;i<records.length;i++){const record=records[i],request=store.get(record.key);request.onsuccess=()=>{if(failure)return;try{
          const current=request.result||null;if(!same(current,plan.entries[i].before))throw Error('恢复记录发生变化，请重新选择备份');
          if(!current||(replace&&!same(current.value,record.value)))changes.push({key:record.key,before:current,after:record});
          if(!--pending)finish();
        }catch(e){fail(e);}};}
        if(!pending)finish();tx.oncomplete=()=>resolve(result);tx.onerror=tx.onabort=()=>reject(failure||tx.error||Error('恢复存储事务中断'));
      });
    }
    async function previewArchive(scope){
      if(typeof scope!=='string'||!scope||scope.length>4096)throw Error('恢复范围无效');
      const records=[...(await collect()).values()].filter(r=>{try{const p=JSON.parse(r.key);return p[0]===scope&&p[1]!==operationKind;}catch(_e){return false;}});
      validate({format:'aide-local-recovery',schema:1,count:records.length,records});
      return {scope,entries:records.map(before=>({key:before.key,before})),count:records.length,bytes:bytes(records)};
    }
    async function archiveRecovery(plan,{active=()=>true}={}){
      if(!plan||!Array.isArray(plan.entries)||!plan.scope||plan.entries.some(e=>e.key!==e.before?.key||JSON.parse(e.key)[0]!==plan.scope))throw Error('恢复归档预览无效');
      validate({format:'aide-local-recovery',schema:1,count:plan.entries.length,records:plan.entries.map(e=>e.before)});
      const db=await open();return new Promise((resolve,reject)=>{
        const tx=db.transaction('records','readwrite'),store=tx.objectStore('records');let failure,result;
        const fail=e=>{failure=e;tx.abort();};
        const finish=()=>{try{
          if(!active())throw Error('恢复操作已取消');
          if(!plan.entries.length){result={count:0,id:null};return;}
          const id=crypto.randomUUID(),changes=plan.entries.map(e=>({key:e.key,before:e.before,after:null}));
          const operation={key:operationKey(id),updated:Date.now(),value:{id,type:'archive',scope:plan.scope,changes}};
          if(bytes(operation)>LIMIT)throw Error('恢复操作备份超过 64 MiB');
          for(const c of changes)store.delete(c.key);store.put(operation);result={count:changes.length,id};
        }catch(e){fail(e);}};
        // Compare the whole scope inside the same transaction, including added
        // keys, so preview cannot silently miss another tab's new draft.
        const request=store.openCursor(),expected=new Map(plan.entries.map(e=>[e.key,e.before]));
        request.onsuccess=()=>{if(failure)return;try{
          const cursor=request.result;
          if(cursor){const record=cursor.value;let parts;try{parts=JSON.parse(record.key);}catch(_e){}
            if(parts?.[0]===plan.scope&&parts[1]!==operationKind){if(!same(record,expected.get(record.key)))throw Error('恢复记录发生变化，请重新查看归档范围');expected.delete(record.key);}
            cursor.continue();
          }else{if(expected.size)throw Error('恢复记录发生变化，请重新查看归档范围');finish();}
        }catch(e){fail(e);}};
        tx.oncomplete=()=>resolve(result);tx.onerror=tx.onabort=()=>reject(failure||tx.error||Error('恢复存储事务中断'));
      });
    }
    async function listRecoveryOperations(){return [...(await collect()).values()].filter(r=>{try{return JSON.parse(r.key)[1]===operationKind;}catch(_e){return false;}}).map(r=>({id:r.value.id,type:r.value.type,scope:r.value.scope||'',updated:r.updated,count:r.value.changes.length,bytes:bytes(r)})).sort((a,b)=>b.updated-a.updated);}
    // Discard only the undo journal. Live drafts and server files are untouched.
    // Requiring the displayed timestamp prevents deleting a changed operation.
    async function discardRecoveryOperation(id,updated,{active=()=>true}={}){
      const db=await open();return new Promise((resolve,reject)=>{
        const tx=db.transaction('records','readwrite'),store=tx.objectStore('records'),req=store.get(operationKey(id));let failure;
        req.onsuccess=()=>{try{if(!active())throw Error('恢复操作已取消');const r=req.result;if(!r||r.updated!==updated||r.value.id!==id)throw Error('恢复操作记录已变化，请重新加载');store.delete(r.key);}catch(e){failure=e;tx.abort();}};
        tx.oncomplete=()=>resolve({discarded:id});tx.onerror=tx.onabort=()=>reject(failure||tx.error||Error('恢复存储事务中断'));
      });
    }
    async function undoRecovery(id,{active=()=>true}={}){
      const db=await open();return new Promise((resolve,reject)=>{
        const tx=db.transaction('records','readwrite'),store=tx.objectStore('records'),request=store.get(operationKey(id));let failure,count=0;
        const fail=e=>{failure=e;tx.abort();};
        request.onsuccess=()=>{try{const op=request.result;if(!op)throw Error('恢复操作记录不存在');const changes=op.value.changes;let pending=changes.length;
          const finish=()=>{try{if(!active())throw Error('恢复操作已取消');for(const c of changes){if(c.before)store.put(c.before);else store.delete(c.key);}store.delete(op.key);count=changes.length;}catch(e){fail(e);}};
          for(const c of changes){const r=store.get(c.key);r.onsuccess=()=>{if(failure)return;if(!same(r.result||null,c.after))return fail(Error('操作后的记录已变化，不能撤销覆盖新内容'));if(!--pending)finish();};}
          if(!pending)finish();
        }catch(e){fail(e);}};
        tx.oncomplete=()=>resolve({count});tx.onerror=tx.onabort=()=>reject(failure||tx.error||Error('恢复存储事务中断'));
      });
    }
    return {previewImport,importRecovery,previewArchive,archiveRecovery,listRecoveryOperations,undoRecovery,discardRecoveryOperation};
  }
  window.AideRecoveryTransfer={create,validate};
})();
