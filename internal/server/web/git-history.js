/* Read-only Git comparison, sharing the existing file-history dialog. */
(() => {
 'use strict';
 const exceedsLines=text=>{let count=0;for(let i=0;i<text.length;i++)if(text[i]==='\n'&&++count>6000)return true;return false;};
 const query=spec=>{const q=new URLSearchParams({path:spec.path,root:spec.root||'workspace'});if(spec.source)q.set('source',spec.source);else if(spec.wsId)q.set('workspaceId',spec.wsId);return q;};
 const detect=({spec,api})=>{const q=query(spec);q.set('probe','1');return api('/file/git-history?'+q);};
 function mount({dialog,snapshotPanel,hasSnapshots,spec,editor,api,t,isActive,onTabChange,lineDiff,renderDiff,syncComparison}) {
  const el=(tag,cls,text)=>{const e=document.createElement(tag);if(cls)e.className=cls;if(text!=null)e.textContent=text;return e;};
  const tabs=el('div','git-history-tabs'),snap=el('button','quiet',t('Aide 快照')),git=el('button','quiet',t('Git 比较'));
  tabs.setAttribute('role','group');tabs.setAttribute('aria-label',t('历史来源'));
  for(const button of [snap,git]){button.type='button';tabs.append(button);}
  snap.hidden=!hasSnapshots;git.disabled=true;
  const panel=el('section','git-history-panel'),note=el('p','muted',t('正在读取 Git 文件历史…'));
  panel.append(note);dialog.append(tabs,snapshotPanel,panel);
  let seq=0,controller=null,loading=false;
  const alive=()=>dialog.open&&isActive();
  const choose=showGit=>{onTabChange();seq++;controller?.abort();panel.hidden=!showGit;snapshotPanel.hidden=showGit;snap.setAttribute('aria-pressed',String(!showGit));git.setAttribute('aria-pressed',String(showGit));const submit=panel.querySelector('button[type="submit"]');if(submit)submit.disabled=false;};
  snap.onclick=()=>choose(false);git.onclick=()=>choose(true);choose(!hasSnapshots);
  dialog.addEventListener('close',()=>{seq++;controller?.abort();},{once:true});
  const load=async()=>{
   if(loading)return;loading=true;
   try {
    const data=await api('/file/git-history?'+query(spec));if(!alive())return;
    if(!data.available){git.hidden=true;if(hasSnapshots)tabs.hidden=true;note.textContent=data.reason||t('当前目录没有可比较的 Git 历史。');return;}
    git.disabled=false;git.onclick=()=>choose(true);
    const form=el('form','git-history-controls'),from=el('select'),to=el('select'),compare=el('button','primary',t('比较版本'));
    const option=(select,value,label)=>{const o=el('option','',label);o.value=value;select.append(o);};
    const choices=select=>{if(data.head)option(select,'HEAD','HEAD · '+data.head.slice(0,12));option(select,'index',t('暂存区'));option(select,'worktree',t('磁盘正文'));for(const v of data.versions||[])option(select,v.id,v.short+' · '+v.subject+' · '+new Date(v.created).toLocaleDateString());};
    choices(from);option(to,'draft',t('编辑器正文（含未保存）'));choices(to);from.value=data.head?'HEAD':'index';to.value='draft';
    for(const [label,select] of [[t('比较基准'),from],[t('目标版本'),to]]){const field=el('label','',label);select.setAttribute('aria-label',label);field.append(select);form.append(field);}
    compare.type='submit';form.append(compare);
    const summary=el('p','git-history-status'),result=el('div','git-history-result');summary.setAttribute('role','status');
    note.textContent=t('只读比较，不修改工作区或暂存区。')+' '+t('提交列表按当前文件路径读取，不追踪重命名前路径。')+(data.truncated?' '+t('仅显示最近 {0} 次提交。',data.limit):'');
    panel.append(form,summary,result);
    const invalidate=()=>{seq++;controller?.abort();compare.disabled=false;summary.textContent=t('版本选择已变化，请重新比较。');result.replaceChildren();};
    from.onchange=to.onchange=invalidate;
    form.onsubmit=async event=>{
     event.preventDefault();if(!alive())return;controller?.abort();const control=new AbortController();controller=control;const pick=++seq;
     const left=from.value,right=to.value,draft=editor.value;
     if(right==='draft'&&new TextEncoder().encode(draft).length>2*1024*1024){summary.textContent=t('Git 比较仅支持不超过 2 MiB 的文本版本');return;}
     compare.disabled=true;summary.textContent=t('正在比较版本…');result.replaceChildren();
     try {
      const q=query(spec);q.set('from',left==='HEAD'?data.head:left);q.set('to',right==='draft'?'worktree':right==='HEAD'?data.head:right);
      const comparison=await api('/file/git-history?'+q,{signal:control.signal});if(!alive()||pick!==seq)return;
      if(!comparison.available)throw Error(comparison.reason||t('Git 仓库已不可用。'));
      const before=comparison.before.content,after=right==='draft'?draft:comparison.after.content,plain=exceedsLines(before)||exceedsLines(after),ops=plain?null:lineDiff(before,after);
      summary.textContent=before===after?t('两侧正文相同。'):t('两侧正文有差异。');
      if(plain)summary.textContent+=' '+t('内容较长，显示完整正文，不逐行着色。');
      if(right==='draft')summary.textContent+=' '+t('右侧是点击比较时的编辑器正文，包含未保存修改。');
      if(!comparison.before.exists)summary.textContent+=' '+t('基准版本不存在此文件。');
      if(right!=='draft'&&!comparison.after.exists)summary.textContent+=' '+t('目标版本不存在此文件。');
      const grid=el('div','md-history-comparison');const pres=[];
      for(const [label,side] of [[from.selectedOptions[0].textContent,'before'],[to.selectedOptions[0].textContent,'after']]){const column=el('div'),heading=el('strong','',label),pre=el('pre');if(plain)pre.textContent=side==='before'?before:after;else renderDiff(pre,ops,side);column.append(heading,pre);grid.append(column);pres.push(pre);}
      syncComparison(...pres);result.append(grid);
      // The patch comes from disk/Git snapshots; never label it as a draft diff.
      if(right!=='draft'){const details=el('details'),title=el('summary','',t('Unified diff')),pre=el('pre','git-history-patch',comparison.diff||t('无正文差异。'));details.append(title,pre);result.append(details);}
     }catch(e){if(alive()&&pick===seq&&e.name!=='AbortError')summary.textContent=e.message;}
     finally{if(alive()&&pick===seq)compare.disabled=false;if(controller===control)controller=null;}
    };
    if(!hasSnapshots)form.requestSubmit();
   }catch(e){if(alive()){note.textContent=t('Git 历史读取失败：')+e.message;git.disabled=false;git.onclick=()=>{choose(true);load();};}}finally{loading=false;}
  };
  load();
 }
 window.AideGitHistory={detect,mount};
})();
