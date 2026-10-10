/* Local recovery inventory is deliberately separate from configuration backups. */
(() => {
  'use strict';
  function render({t,error,isActive=()=>true}) {
    const node=(tag,cls,text)=>{const n=document.createElement(tag);if(cls)n.className=cls;if(text)n.textContent=text;return n;};
    const wrap=node('div','settings-control recovery-manager');
    wrap.append(node('h3','',t('本地恢复存储')),
      node('p','muted',t('仅统计当前浏览器、当前地址的共享恢复记录。备份可能包含未发送文字和文件草稿，请妥善保管；不读取模型配置或凭据；草稿中的敏感文字仍可能被导出。')));
    const toolbar=node('div','recovery-manager-toolbar'),inspect=node('button','quiet',t('查看恢复占用')),download=node('button','quiet',t('导出本地恢复记录'));
    const upload=node('button','quiet',t('导入恢复备份')),input=node('input');input.type='file';input.accept='.json,application/json';input.hidden=true;
    inspect.type=download.type=upload.type='button';toolbar.append(inspect,download,upload);
    const status=node('p','muted'),list=node('div','recovery-manager-list');status.setAttribute('role','status');status.setAttribute('aria-live','polite');
    const transfer=node('div','recovery-transfer-preview'),operations=node('div','recovery-manager-list recovery-import-operations');
    wrap.append(toolbar,status,transfer,operations,list,node('p','muted',t('浏览器估算包含同地址的其他存储，并非恢复记录的磁盘大小。导出不含其他已打开标签页独有的即时快照；导入不会修改正在编辑的标签页，首次打开的新标签页才能使用导入记录；撤销记录单独保留，不包含在导出中。当前不自动清理。')));
    wrap.append(input);
    const blocked=()=>{const s=window.LockCluster?.snapshot();return !!s&&(!s.settled||s.locked);};
    let epoch=0;
    const valid=e=>wrap.isConnected&&isActive()&&!blocked()&&epoch===e;
    const size=n=>(Number(n||0)/1048576).toFixed(2)+' MiB';
    async function execute(operation){if(blocked())return;const e=++epoch;inspect.disabled=download.disabled=upload.disabled=true;
      try{await operation(e);}catch(err){if(valid(e))error(err);}finally{inspect.disabled=download.disabled=upload.disabled=false;}}
    async function showOperations(e){
      const items=await window.AideContinuity.listRecoveryOperations?.()||[];if(!valid(e))return;operations.replaceChildren();
      for(const item of items){const row=node('div','recovery-manager-row'),undo=node('button','quiet',t('撤销此导入'));undo.type='button';row.append(node('span','',new Date(item.updated).toLocaleString()+' · '+item.count+' '+t('条记录')),undo);
        undo.onclick=()=>execute(async n=>{await window.AideContinuity.undoRecovery(item.id,{active:()=>valid(n)});if(!valid(n))return;status.textContent=t('已撤销导入；现有标签页快照保持不变');await showOperations(n);});operations.append(row);}
    }
    upload.onclick=()=>{if(!blocked())input.click();};
    input.onchange=()=>execute(async e=>{
      const file=input.files?.[0];input.value='';if(!file)return;if(file.size>64*1024*1024)throw Error(t('恢复备份超过 64 MiB'));
      const plan=await window.AideContinuity.previewImport(JSON.parse(await file.text()));if(!valid(e))return;
      transfer.replaceChildren();transfer.append(node('p','',t('导入预览')+' · '+t('新增')+' '+plan.added+' · '+t('相同')+' '+plan.identical+' · '+t('冲突')+' '+plan.conflicts.length));
      const label=node('label','settings-check'),replace=node('input');replace.type='checkbox';label.append(replace,node('span','',t('替换冲突记录，保留旧记录以便撤销')));if(plan.conflicts.length)transfer.append(label);
      for(const k of plan.conflicts)transfer.append(node('code','recovery-conflict-key',k));
      const apply=node('button','quiet',t('确认导入')),cancel=node('button','quiet',t('取消'));apply.type=cancel.type='button';transfer.append(apply,cancel);cancel.onclick=()=>{epoch++;transfer.replaceChildren();};
      apply.onclick=()=>execute(async n=>{const result=await window.AideContinuity.importRecovery(plan,{replace:replace.checked,active:()=>valid(n)});if(!valid(n))return;transfer.replaceChildren();status.textContent=t('已导入共享记录')+' '+result.count+' · '+t('现有标签页快照保持不变');await showOperations(n);});
    });
    inspect.onclick=()=>execute(async e=>{
      const data=await window.AideContinuity.inventory();if(!valid(e))return;
      status.textContent=t('共享记录')+' '+data.count+' · '+t('序列化大小')+' '+size(data.bytes);
      if(data.originStorage)status.textContent+=' · '+t('浏览器使用／配额')+' '+size(data.originStorage.usage)+' / '+size(data.originStorage.quota);
      list.replaceChildren();for(const group of data.groups){const row=node('div','recovery-manager-row');row.append(node('code','',group.scope),node('span','',group.count+' · '+size(group.bytes)),node('small','muted',Object.entries(group.kinds).map(([kind,count])=>kind+': '+count).join(' · ')));list.append(row);}
      await showOperations(e);
    });
    download.onclick=()=>execute(async e=>{
      const data=await window.AideContinuity.exportRecovery();if(!valid(e))return;
      const blob=new Blob([JSON.stringify(data)],{type:'application/json'}),url=URL.createObjectURL(blob),a=node('a');a.href=url;a.download='aide-local-recovery-'+data.created.replace(/[:.]/g,'-')+'.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
      status.textContent=t('已导出共享恢复记录')+' '+data.count+' · '+size(blob.size);
    });
    return wrap;
  }
  window.AideRecoveryManager={render};
})();
