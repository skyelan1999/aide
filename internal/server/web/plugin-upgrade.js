/* Online plugin replacement; all installation and validation stays server-side. */
(() => {
 'use strict';
 let busy=false;
 function setBusy(value,t){
  busy=value;
  for(const button of document.querySelectorAll('[data-plugin-upgrade]')){
   button.disabled=value;button.textContent=t(value?'升级中…':'升级');
  }
 }
 async function upgrade(plugin,services,options={}){
  if(busy)return false;
  const {api,refresh,toast,t}=services;
  setBusy(true,t);
  try{
   const result=await api('/plugins/'+encodeURIComponent(plugin.id)+'/upgrade',{method:'POST',body:JSON.stringify(options)});
   try{await refresh();}catch(_){toast(t('插件已升级，列表刷新失败，请手动刷新'));return true;}
   toast(result.unchanged?t('插件已是最新版本'):result.version===plugin.version?t('插件内容已更新，版本 {0}',result.version):t('插件已升级：{0} → {1}',plugin.version||'—',result.version||'—'));
   return true;
  }catch(error){
   if(!error.status)error.message=t('升级请求未完成，请刷新列表核对状态');
   toast(error.message||t('插件升级失败'));throw error;
  }finally{setBusy(false,t);}
 }
 function openSource(plugin,services){
  if(busy)return;
  const {el,t}=services;
  let existing=document.getElementById('plugin-upgrade-dialog');if(existing){existing.close();existing.remove();}
  const dialog=el('dialog','plugin-settings-dialog');dialog.id='plugin-upgrade-dialog';
  const form=el('form'),heading=el('div','dialog-heading'),title=el('h2','',t('插件升级：{0}',plugin.name));title.id='plugin-upgrade-title';dialog.setAttribute('aria-labelledby',title.id);
  const close=el('button','icon-button','×');close.type='button';close.setAttribute('aria-label',t('关闭'));close.onclick=()=>dialog.close();heading.append(title,close);
  const hint=el('p','muted',t('填写插件的 HTTPS ZIP 地址，成功升级后会记住。内置插件默认使用 AIDE 官方来源。'));
  const label=el('label','',t('升级来源')),input=el('input');input.type='url';input.required=true;input.value=plugin.updateURL||'';input.placeholder='https://…/plugin.zip';input.autocomplete='off';input.maxLength=2048;label.append(input);
  const details=el('details'),summary=el('summary','',t('SHA-256 校验（可选）')),hashLabel=el('label','',t('发布方提供的 SHA-256')),hash=el('input');hash.type='text';hash.pattern='[a-fA-F0-9]{64}';hash.maxLength=64;hash.autocomplete='off';hashLabel.append(hash);details.append(summary,hashLabel);
  const status=el('p','muted');status.setAttribute('role','status');status.setAttribute('aria-live','polite');
  const actions=el('div','dialog-actions'),cancel=el('button','quiet',t('取消')),submit=el('button','primary',t('保存来源并升级'));cancel.type='button';cancel.onclick=()=>dialog.close();submit.type='submit';actions.append(cancel,submit);
  form.append(heading,hint,label,details,status,actions);dialog.append(form);document.body.append(dialog);
  form.onsubmit=async event=>{
   event.preventDefault();if(busy)return;submit.disabled=true;input.disabled=true;hash.disabled=true;
   status.textContent=t('正在下载、校验并替换插件…');
   try{if(await upgrade(plugin,services,{url:input.value.trim(),sha256:hash.value.trim()}))dialog.close();}
   catch(error){status.textContent=error.message||t('插件升级失败');}
   finally{submit.disabled=false;input.disabled=false;hash.disabled=false;}
  };
  dialog.addEventListener('close',()=>dialog.remove(),{once:true});dialog.showModal();input.focus();
 }
 function button(plugin,services){
  const b=services.el('button','plugin-upgrade-button',services.t(busy?'升级中…':'升级'));b.type='button';b.dataset.pluginUpgrade=plugin.id;b.disabled=busy;
  b.title=plugin.updateURL?services.t('从已配置来源在线升级'):services.t('设置来源并在线升级');
  b.onclick=()=>{if(!plugin.updateURL){openSource(plugin,services);return;}upgrade(plugin,services).catch(()=>{});};return b;
 }
 window.AidePluginUpgrade=Object.freeze({button,openSource,upgrade});
})();
