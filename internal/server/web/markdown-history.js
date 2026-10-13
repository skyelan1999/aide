/* Revision inspection is separate from explicit, hash-guarded restoration. */
(() => {
 'use strict';
 function lineDiff(before,after) {
  const a=before.split('\n'),b=after.split('\n'),n=a.length,m=b.length;
  // Keep the matrix bounded for very large Markdown files. Preserve matching
  // edges and mark only the unmatched middle as a replacement in that case.
  let prefix=0;while(prefix<n&&prefix<m&&a[prefix]===b[prefix])prefix++;
  let suffix=0;while(suffix<n-prefix&&suffix<m-prefix&&a[n-1-suffix]===b[m-1-suffix])suffix++;
  const middleA=n-prefix-suffix,middleB=m-prefix-suffix;
  let ops=Array.from({length:prefix},(_,i)=>({kind:'equal',before:a[i],after:b[i]}));
  if(middleA*middleB<=250000) {
   const width=middleB+1,dp=Array.from({length:middleA+1},()=>new Uint32Array(width));
   for(let i=middleA-1;i>=0;i--)for(let j=middleB-1;j>=0;j--)dp[i][j]=a[prefix+i]===b[prefix+j]?dp[i+1][j+1]+1:Math.max(dp[i+1][j],dp[i][j+1]);
   let i=0,j=0,mid=[];while(i<middleA||j<middleB){if(i<middleA&&j<middleB&&a[prefix+i]===b[prefix+j]){mid.push({kind:'equal',before:a[prefix+i++],after:b[prefix+j++]});}else if(j<middleB&&(i===middleA||dp[i][j+1]>=dp[i+1][j]))mid.push({kind:'added',before:'',after:b[prefix+j++]});else mid.push({kind:'removed',before:a[prefix+i++],after:''});}
   // Align replacements as a single row even when the LCS tie-break puts an
   // insertion before a deletion. This keeps both panes at the same line.
   for(let k=0;k<mid.length;){if(mid[k].kind==='equal'){ops.push(mid[k++]);continue;}let end=k;while(end<mid.length&&mid[end].kind!=='equal')end++;const removed=mid.slice(k,end).filter(x=>x.kind==='removed'),added=mid.slice(k,end).filter(x=>x.kind==='added'),paired=Math.min(removed.length,added.length);for(let p=0;p<paired;p++)ops.push({kind:'changed',before:removed[p].before,after:added[p].after});for(let p=paired;p<removed.length;p++)ops.push(removed[p]);for(let p=paired;p<added.length;p++)ops.push(added[p]);k=end;}
  } else {
   for(let i=0;i<middleA||i<middleB;i++)ops.push({kind:i<middleA&&i<middleB?'changed':i<middleA?'removed':'added',before:a[prefix+i]||'',after:b[prefix+i]||''});
  }
  for(let i=suffix;i>0;i--)ops.push({kind:'equal',before:a[n-i],after:b[m-i]});
  return ops;
 }
 function renderDiff(pre,ops,side){for(const op of ops){const line=document.createElement('span'),text=side==='before'?op.before:op.after;line.className='md-history-line'+(op.kind==='equal'?'':' md-history-line-'+(op.kind==='changed'?'changed':side==='before'?'removed':'added'));line.textContent=text||'\u00a0';pre.append(line);}}
 function syncComparison(left,right){let syncing=false;const sync=(source,target)=>{if(syncing)return;syncing=true;const from=source.scrollHeight-source.clientHeight,to=target.scrollHeight-target.clientHeight;target.scrollTop=from>0?source.scrollTop/from*Math.max(0,to):0;requestAnimationFrame(()=>{syncing=false;});};left.addEventListener('scroll',()=>sync(left,right),{passive:true});right.addEventListener('scroll',()=>sync(right,left),{passive:true});}
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
    if(data.observationChange){const change=data.observationChange,parts=[];if(change.bodyChanged)parts.push(t('正文有变化'));if(change.assetsChanged?.length)parts.push(t('资源有变化')+' '+change.assetsChanged.join('、'));if(!parts.length)parts.push(t('版本指纹有变化'));status.textContent+=' '+t('本次检查发现新版本')+' '+change.version+'：'+parts.join('；')+'。';}
    if(data.observationError)status.textContent+=' '+t('当前文件检查失败：')+data.observationError;
    const layout=document.createElement('div');layout.className='md-history-layout';const list=document.createElement('nav'),detail=document.createElement('section');layout.append(list,detail);
    let selection=0,observedItem=null;
    for(const v of [...data.versions].reverse()) {const item=document.createElement('button');item.type='button';item.className='quiet';item.textContent=v.id+' · '+new Date(v.created).toLocaleString();if(data.observationChange?.version===v.id)observedItem=item;item.onclick=async()=>{
     const pick=++selection;try{const q=query(spec);q.set('revision',v.id||v.ID);const revision=await api('/file/history?'+q);if(blocked()||pick!==selection||!dialog.open)return;
      detail.replaceChildren();const summary=document.createElement('p');summary.textContent=revision.content===currentText?t('正文相同；请查看引用资源指纹。'):t('正文与当前编辑内容不同。');detail.append(summary);
      const grids=document.createElement('div');grids.className='md-history-comparison';const diff=lineDiff(revision.content,currentText),panes=[];
      if(revision.content!==currentText){const counts=diff.reduce((n,row)=>{if(row.kind==='removed')n.removed++;if(row.kind==='added')n.added++;if(row.kind==='changed'){n.removed++;n.added++;}return n;},{removed:0,added:0});summary.textContent+=' · −'+counts.removed+' / +'+counts.added+' · '+t('红色为删除，绿色为新增，琥珀色为修改；两侧自动同步滚动。');}
      for(const [label,side] of [[t('历史正文'),'before'],[t('当前正文'),'after']]) {const column=document.createElement('div'),heading=document.createElement('strong'),pre=document.createElement('pre');heading.textContent=label;pre.setAttribute('aria-label',label+' · '+t('自动同步滚动'));renderDiff(pre,diff,side);column.append(heading,pre);grids.append(column);panes.push(pre);}detail.append(grids);syncComparison(...panes);
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
    const backupArea=document.createElement('section');backupArea.className='md-history-backup';
    const storage=document.createElement('button'),download=document.createElement('button'),storageInfo=document.createElement('p');
    storage.type=download.type='button';storage.className=download.className='quiet';storage.textContent=t('历史存储统计');download.textContent=t('下载完整历史备份');storageInfo.className='muted';
    const backupRequest=async(archive)=>{const q=query(spec);if(archive)q.set('archive','1');const result=await api('/file/history/backup?'+q);if(blocked()||!dialog.open||request!==epoch||['path','source','wsId'].some(k=>(spec[k]||'')!==(getContext()?.[k]||'')))return null;return result;};
    const describe=result=>{storageInfo.textContent=t('已归档版本')+': '+result.versions+' · '+t('去重对象')+': '+result.objects+' · '+(result.objectBytes/1024/1024).toFixed(2)+' MiB';if(result.unavailableAssets)storageInfo.textContent+=' · '+t('未归档资源记录')+': '+result.unavailableAssets;};
    storage.onclick=async()=>{storage.disabled=true;try{const result=await backupRequest(false);if(result)describe(result);}catch(e){error(e);}finally{storage.disabled=false;}};
    download.onclick=async()=>{download.disabled=true;try{const result=await backupRequest(true);if(!result)return;describe(result);const raw=Uint8Array.from(atob(result.base64),c=>c.charCodeAt(0)),url=URL.createObjectURL(new Blob([raw],{type:'application/zip'}));const link=document.createElement('a');link.href=url;link.download=result.filename;link.click();setTimeout(()=>URL.revokeObjectURL(url),1000);storageInfo.textContent+=' · '+t('已验证归档对象摘要');}catch(e){error(e);}finally{download.disabled=false;}};
    backupArea.append(storage,download,storageInfo);
    if(window.AideMarkdownHistoryManager)backupArea.append(window.AideMarkdownHistoryManager.render({spec,api,t,error,isActive:()=>!blocked()&&dialog.open&&request===epoch&&!['path','source','wsId'].some(k=>(spec[k]||'')!==(getContext()?.[k]||'')),onChanged:()=>dialog.close()}));
    dialog.append(head,status,backupArea,layout);dialog.addEventListener('close',()=>{selection++;if(activeDialog===dialog)activeDialog=null;dialog.remove();},{once:true});document.body.append(dialog);dialog.showModal();if(observedItem)observedItem.click();close.focus();
   }catch(e){error(e);}finally{button.disabled=false;}
  };
  return {refresh};
 }
 window.AideMarkdownHistory={bind};
})();
