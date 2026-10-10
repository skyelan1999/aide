/* Revision inspection is separate from explicit, hash-guarded restoration. */
(() => {
 'use strict';
 function bind({button,editor,getContext,api,t,error,isDirty=()=>false,onRestored=()=>{}}) {
  let epoch=0,activeDialog=null;
  const blocked=()=>{const state=window.LockCluster?.snapshot();return !!state&&(!state.settled||state.locked);};
  window.LockCluster?.on('effective',value=>{if(value){epoch++;activeDialog?.close();}});
  const query=spec=>{const q=new URLSearchParams({path:spec.path});if(spec.source)q.set('source',spec.source);else if(spec.wsId)q.set('workspaceId',spec.wsId);return q;};
  function refresh(){epoch++;activeDialog?.close();button.hidden=!/\.(md|markdown)$/i.test(getContext()?.path||'');}
  button.onclick=async()=>{
   const spec={...getContext()},currentText=editor.value;if(!spec.path||blocked())return;activeDialog?.close();const request=++epoch;button.disabled=true;
   try {
    const data=await api('/file/history?'+query(spec));if(blocked()||request!==epoch||['path','source','wsId'].some(k=>(spec[k]||'')!==(getContext()?.[k]||'')))return;
    const dialog=document.createElement('dialog');activeDialog=dialog;dialog.className='md-history-dialog';
    const head=document.createElement('header'),title=document.createElement('h2'),close=document.createElement('button');title.textContent=t('历史版本')+' · '+spec.path;close.type='button';close.className='icon-button';close.textContent='×';close.setAttribute('aria-label',t('关闭'));close.onclick=()=>dialog.close();head.append(title,close);
    const status=document.createElement('p');status.className='muted';status.textContent=t(data.enabled?'已启用：只记录实际变更，直接修改原文件。':'追踪未启用；已有历史仍可查看。');
    if(data.observation==='checked')status.textContent+=' '+t('已检查当前正文与引用资源的外部变更。');
    if(data.observationError)status.textContent+=' '+t('当前文件检查失败：')+data.observationError;
    const layout=document.createElement('div');layout.className='md-history-layout';const list=document.createElement('nav'),detail=document.createElement('section');layout.append(list,detail);
    let selection=0;
    for(const v of [...data.versions].reverse()) {const item=document.createElement('button');item.type='button';item.className='quiet';item.textContent=v.id+' · '+new Date(v.created).toLocaleString();item.onclick=async()=>{
     const pick=++selection;try{const q=query(spec);q.set('revision',v.id||v.ID);const revision=await api('/file/history?'+q);if(blocked()||pick!==selection||!dialog.open)return;
      detail.replaceChildren();const summary=document.createElement('p');summary.textContent=revision.content===currentText?t('正文相同；请查看引用资源指纹。'):t('正文与当前编辑内容不同。');detail.append(summary);
      const grids=document.createElement('div');grids.className='md-history-comparison';
      for(const [label,text] of [[t('历史正文'),revision.content],[t('当前正文'),currentText]]) {const column=document.createElement('div'),heading=document.createElement('strong'),pre=document.createElement('pre');heading.textContent=label;pre.textContent=text;column.append(heading,pre);grids.append(column);}detail.append(grids);
      const assets=document.createElement('div');assets.className='md-history-assets';for(const asset of revision.revision.assets||[]){const row=document.createElement('p');row.textContent=asset.path+' · '+asset.status+' · '+(asset.digest?.slice(0,16)||'—');if(asset.status==='stored'){const download=document.createElement('button');download.type='button';download.className='quiet quiet-sm';download.textContent=t('下载资源快照');download.onclick=async()=>{download.disabled=true;try{const q=query(spec);q.set('revision',v.id);q.set('asset',asset.path);const snapshot=await api('/file/history?'+q);if(blocked()||!dialog.open)return;const raw=Uint8Array.from(atob(snapshot.base64),c=>c.charCodeAt(0));const url=URL.createObjectURL(new Blob([raw],{type:'application/octet-stream'}));const a=document.createElement('a');a.href=url;a.download=asset.path.split('/').pop();a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);}catch(e){error(e);}finally{download.disabled=false;}};row.append(download);}assets.append(row);}detail.append(assets);
      if(data.enabled && !spec.readOnly && spec.editable!==false && (spec.source || spec.root!=='context')) {
       const restoreArea=document.createElement('section');restoreArea.className='md-restore-area';
       const label=document.createElement('label'),checkbox=document.createElement('input');checkbox.type='checkbox';checkbox.checked=true;
       label.append(checkbox,document.createTextNode(t('同时恢复此版本的引用资源')));
       const preview=document.createElement('button'),changes=document.createElement('div');preview.type='button';preview.className='quiet';preview.textContent=t('对比恢复范围');
       preview.onclick=async()=>{preview.disabled=true;changes.replaceChildren();try{
        if(isDirty() || editor.value!==currentText)throw Error(t('请先保存或处理当前编辑草稿，再恢复历史。'));
        const restoreAssets=checkbox.checked;const q=query(spec);q.set('revision',v.id);q.set('assets',restoreAssets?'1':'0');
        const plan=await api('/file/history/restore?'+q);if(blocked()||!dialog.open||pick!==selection||checkbox.checked!==restoreAssets)return;
        const description=document.createElement('p');description.textContent=t('仅恢复下列文件；不删除其他资源。恢复前会记录快照。');changes.append(description);
        for(const f of plan.files){const row=document.createElement('p');row.textContent=f.path+' · '+(f.before===f.after?t('内容不变'):t('将恢复'))+' · '+f.before.slice(0,12)+' → '+f.after.slice(0,12);changes.append(row);}
        const confirm=document.createElement('button');confirm.type='button';confirm.className='primary';confirm.textContent=t('确认恢复此版本');changes.append(confirm);
        confirm.onclick=async()=>{confirm.disabled=true;checkbox.disabled=true;const wasReadOnly=editor.readOnly;let restoring=false;try{
         if(blocked()||isDirty()||editor.value!==currentText||['path','source','wsId'].some(k=>(spec[k]||'')!==(getContext()?.[k]||'')))throw Error(t('编辑状态已变化，请重新对比。'));
         editor.readOnly=true;restoring=true;const result=await api('/file/history/restore',{method:'POST',body:JSON.stringify({path:spec.path,source:spec.source||'',workspaceId:spec.wsId||'',revision:v.id,identity:plan.identity,assets:restoreAssets,expected:Object.fromEntries(plan.files.map(f=>[f.path,f.before]))})});
         if(blocked()||!dialog.open||['path','source','wsId'].some(k=>(spec[k]||'')!==(getContext()?.[k]||'')))return;
         editor.readOnly=wasReadOnly;onRestored(result);dialog.close();
        }catch(e){if(e.data?.journal){const receipt=document.createElement('p');receipt.textContent=t('恢复未完成，操作记录：')+e.data.journal+' · '+(e.data.files||[]).filter(f=>f.applied).map(f=>f.path).join(', ');changes.append(receipt);}error(e);}finally{if(restoring&&!['path','source','wsId'].some(k=>(spec[k]||'')!==(getContext()?.[k]||'')))editor.readOnly=wasReadOnly;confirm.disabled=false;checkbox.disabled=false;}};
       }catch(e){error(e);}finally{preview.disabled=false;}};
       checkbox.onchange=()=>changes.replaceChildren();restoreArea.append(label,preview,changes);detail.append(restoreArea);
      }
      list.querySelectorAll('button').forEach(b=>b.classList.toggle('active',b===item));
     }catch(e){error(e);}
    };list.append(item);}
    if(!data.versions.length)detail.textContent=t('暂无历史版本；启用后第一次保存会建立基线。');else detail.textContent=t('选择版本，对比正文与引用资源。');
    dialog.append(head,status,layout);dialog.addEventListener('close',()=>{selection++;if(activeDialog===dialog)activeDialog=null;dialog.remove();},{once:true});document.body.append(dialog);dialog.showModal();close.focus();
   }catch(e){error(e);}finally{button.disabled=false;}
  };
  return {refresh};
 }
 window.AideMarkdownHistory={bind};
})();
