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
    const retentionRow=node('div','recovery-manager-toolbar'),retentionDays=node('input'),retentionLabel=node('label','',t('撤销备份保留天数（0 永久保留）')),retentionButton=node('button','quiet',t('预览撤销备份保留期'));
    retentionDays.type='number';retentionDays.min='0';retentionDays.max='3650';retentionDays.step='1';retentionDays.value='0';retentionLabel.append(retentionDays);retentionButton.type='button';retentionRow.append(retentionLabel,retentionButton);toolbar.append(retentionRow);

    const blocked=()=>{const s=window.LockCluster?.snapshot();return !!s&&(!s.settled||s.locked);};
    let epoch=0;
    const valid=e=>wrap.isConnected&&isActive()&&!blocked()&&epoch===e;
    const size=n=>(Number(n||0)/1048576).toFixed(2)+' MiB';
    async function execute(operation){if(blocked())return;const e=++epoch;inspect.disabled=download.disabled=upload.disabled=true;
      try{await operation(e);}catch(err){if(valid(e))error(err);}finally{inspect.disabled=download.disabled=upload.disabled=false;}}
    retentionButton.onclick=()=>execute(async e=>{
      const plan=await window.AideContinuity.previewRecoveryRetention(Number(retentionDays.value));if(!valid(e))return;
      transfer.replaceChildren();transfer.append(node('p','',t('仅清理过期的撤销备份，不删除草稿、现有标签页快照或服务器文件。启用后每分钟在可见且已解锁页面检查；过期备份永久删除，不能撤销。')),node('p','',plan.expired.length+' · '+size(plan.bytes)));
      const confirm=node('button','quiet',t('确认保留策略')),cancel=node('button','quiet',t('取消'));confirm.type=cancel.type='button';transfer.append(confirm,cancel);cancel.onclick=()=>{epoch++;transfer.replaceChildren();};
      confirm.onclick=()=>execute(async n=>{const result=await window.AideContinuity.applyRecoveryRetention(plan,{active:()=>valid(n)});if(!valid(n))return;transfer.replaceChildren();status.textContent=t('保留策略已保存')+' · '+result.count;await showOperations(n);});
    });
    async function showOperations(e){
      const policy=await window.AideContinuity.recoveryRetention?.()||{days:0};if(!valid(e))return;retentionDays.value=String(policy.days);

      const items=await window.AideContinuity.listRecoveryOperations?.()||[];if(!valid(e))return;operations.replaceChildren();
      if(window.aideRecoveryMaintenanceError)operations.append(node('p','muted',t('上次撤销备份维护失败：')+window.aideRecoveryMaintenanceError));
      for(const item of items){const row=node('div','recovery-manager-row'),undo=node('button','quiet',t(item.type==='archive'?'恢复此归档':'撤销此导入'));undo.type='button';row.append(node('span','',new Date(item.updated).toLocaleString()+' · '+item.count+' '+t('条记录')),node('small','muted',t('可撤销备份')+' · '+size(item.bytes)+(item.scope?' · '+item.scope:'')),undo);
        undo.onclick=()=>execute(async n=>{await window.AideContinuity.undoRecovery(item.id,{active:()=>valid(n)});if(!valid(n))return;status.textContent=t(item.type==='archive'?'已恢复归档；现有标签页快照保持不变':'已撤销导入；现有标签页快照保持不变');await showOperations(n);});
        const discard=node('button','quiet',t('清除撤销备份'));discard.type='button';row.append(discard);
        discard.onclick=()=>{if(blocked())return;transfer.replaceChildren();transfer.append(node('p','',t('永久删除此操作的撤销备份以释放空间；此操作不能撤销，不修改当前草稿或服务器文件。')),node('code','',item.id));const confirm=node('button','quiet',t('确认永久清除')),cancel=node('button','quiet',t('取消'));confirm.type=cancel.type='button';transfer.append(confirm,cancel);cancel.onclick=()=>{epoch++;transfer.replaceChildren();};confirm.onclick=()=>execute(async n=>{await window.AideContinuity.discardRecoveryOperation(item.id,item.updated,{active:()=>valid(n)});if(!valid(n))return;transfer.replaceChildren();status.textContent=t('已清除撤销备份');await showOperations(n);});};operations.append(row);}
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
      list.replaceChildren();for(const group of data.groups){const row=node('div','recovery-manager-row');row.append(node('code','',group.scope),node('span','',group.count+' · '+size(group.bytes)),node('small','muted',Object.entries(group.kinds).map(([kind,count])=>kind+': '+count).join(' · ')));const archive=node('button','quiet',t('归档此范围'));archive.type='button';row.append(archive);
        archive.onclick=()=>execute(async n=>{
          const plan=await window.AideContinuity.previewArchive(group.scope);if(!valid(n))return;
          transfer.replaceChildren();transfer.append(node('h4','',t('恢复记录归档预览')),node('code','',group.scope),node('p','',t('共享记录')+' '+plan.count+' · '+size(plan.bytes)),node('p','muted',t('仅归档此范围的共享恢复记录，保留可撤销备份；不会修改服务器文件或现有标签页。备份仍占空间，此操作不等于释放配额。现有标签页继续编辑可重新生成记录。')));
          const apply=node('button','quiet',t('确认归档')),cancel=node('button','quiet',t('取消'));apply.type=cancel.type='button';apply.disabled=!plan.count;transfer.append(apply,cancel);
          cancel.onclick=()=>{epoch++;transfer.replaceChildren();};
          apply.onclick=()=>execute(async current=>{const result=await window.AideContinuity.archiveRecovery(plan,{active:()=>valid(current)});if(!valid(current))return;transfer.replaceChildren();list.replaceChildren();status.textContent=t('已归档共享恢复记录')+' '+result.count;await showOperations(current);});
        });list.append(row);}
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
