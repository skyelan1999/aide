/* Explicit history metadata management; never writes the current source file. */
(() => {
 'use strict';
 function render({spec,api,t,error,isActive,onChanged}) {
  const node=(tag,text)=>{const n=document.createElement(tag);if(text)n.textContent=text;return n;};
  const section=node('details'),summary=node('summary',t('历史备份与保留管理'));section.append(summary);
  const status=node('p');status.className='muted';status.setAttribute('role','status');
  const controls=node('div');controls.className='recovery-manager-toolbar';
  const importButton=node('button',t('导入历史 ZIP')),retention=node('button',t('预览保留策略')),gc=node('button',t('预览未引用对象'));
  for(const b of [importButton,retention,gc]){b.type='button';b.className='quiet';controls.append(b);}
  const fields=node('div');fields.className='recovery-manager-toolbar';
  const latest=node('input'),days=node('input');for(const input of [latest,days]){input.type='number';input.min='0';input.step='1';input.value='0';}latest.max='10000';days.max='36500';
  for(const [label,input] of [[t('最多保留版本数（0 不限）'),latest],[t('最多保留天数（0 不限）'),days]]) {const row=node('label',label);row.append(input);fields.append(row);}
  const file=node('input');file.type='file';file.accept='.zip,application/zip';file.hidden=true;
  const preview=node('div');section.append(node('p',t('先下载备份。导入仅合并历史；保留策略会移除旧版本索引，至少保留最新版本。对象清理覆盖同一来源的历史库，并保护其他文档和恢复记录。')),fields,controls,status,preview,file);
  let epoch=0,busy=false;
  const active=n=>section.isConnected&&isActive()&&epoch===n;
  const request=(action,extra={})=>api('/file/history/manage',{method:'POST',body:JSON.stringify({action,path:spec.path,source:spec.source||'',workspaceId:spec.wsId||'',...extra})});
  async function run(work){if(busy||!isActive())return;busy=true;const n=++epoch;for(const b of [importButton,retention,gc])b.disabled=true;try{await work(n);}catch(e){if(active(n))error(e);}finally{busy=false;for(const b of [importButton,retention,gc])b.disabled=false;}}
  function confirmation(n,text,action,extra,result){if(!active(n))return;preview.replaceChildren(node('p',text));const confirm=node('button',t('确认执行')),cancel=node('button',t('取消'));confirm.type=cancel.type='button';confirm.className=cancel.className='quiet';preview.append(confirm,cancel);cancel.onclick=()=>{epoch++;preview.replaceChildren();};confirm.onclick=()=>{if(!active(n))return;run(async current=>{const response=await request(action,{...extra,identity:result.identity,revision:result.revision});if(!active(current))return;preview.replaceChildren();status.textContent=t('历史管理操作已完成；重新打开历史查看最新列表');onChanged?.(response);});};}
  retention.onclick=()=>run(async n=>{const policy={keepLatest:Number(latest.value),keepDays:Number(days.value)};if(!Number.isInteger(policy.keepLatest)||!Number.isInteger(policy.keepDays))throw Error(t('保留设置必须为整数'));const result=await request('retention-preview',{policy});confirmation(n,t('将移除旧版本索引')+' '+result.removed+' · '+t('保留')+' '+result.retained+' · '+t('此操作不能撤销，建议先下载历史备份。'),'retention',{policy},result);});
  gc.onclick=()=>run(async n=>{const result=await request('gc-preview');confirmation(n,t('将永久清理未引用对象')+' '+result.gc.objectCount+' · '+(result.gc.reclaimBytes/1048576).toFixed(2)+' MiB','gc',{archiveDigest:result.gc.revision},result);});
  importButton.onclick=()=>{if(!busy&&isActive())file.click();};file.onchange=()=>run(async n=>{const selected=file.files?.[0];file.value='';if(!selected)return;if(selected.size>64*1024*1024)throw Error(t('历史备份超过 64 MiB'));const bytes=new Uint8Array(await selected.arrayBuffer());let binary='';for(let i=0;i<bytes.length;i+=32768)binary+=String.fromCharCode(...bytes.subarray(i,i+32768));const archive=btoa(binary);const result=await request('import-preview',{archive});confirmation(n,t('将合并历史版本')+' '+result.added+' · '+t('当前文档和资源不会修改'),'import',{archive,archiveDigest:result.archiveDigest},result);});
  section.addEventListener('toggle',()=>{if(!section.open)return;run(async n=>{const result=await request('status');if(!active(n))return;latest.value=String(result.policy.keepLatest);days.value=String(result.policy.keepDays);status.textContent=t('已归档版本')+' '+result.versions;});});
  return section;
 }
 window.AideMarkdownHistoryManager={render};
})();
