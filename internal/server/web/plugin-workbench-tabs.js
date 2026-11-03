'use strict';
(function(){
 const validId=value=>typeof value==='string'&&/^[a-z0-9][a-z0-9_-]{0,63}$/.test(value);
 // wsID() is an opaque mode|path|host identity, not a filesystem selection or
 // slug. URLSearchParams encodes it; the destination rechecks exact equality.
 const validWorkspace=value=>typeof value==='string'&&value.length>0&&value.length<=4096&&!/[\x00-\x1f\x7f]/.test(value)&&(/^[A-Za-z0-9_-]{1,128}$/.test(value)||/^(local|ssh)\|.+\|[^|]*$/.test(value));
 function read(hash=location.hash){
  const params=new URLSearchParams(hash.slice(1)),id=params.get('pluginWorkbench');
  if(!validId(id))return null;
  const workspace=params.get('workspace');if(!validWorkspace(workspace))return null;
  let selection;const encoded=params.get('selection');
  if(encoded){if(encoded.length>10000)return null;try{selection=window.AidePluginWorkbench.selection(JSON.parse(encoded));}catch(_){return null;}if(!selection)return null;}
  return {id,workspace,selection};
 }
 function open(plugin,services){
  if(services.blocked()||!plugin.enabled||plugin.error||plugin.workbench?.protocol!==1||!validId(plugin.id))return false;
  const workspace=services.workspace();if(!validWorkspace(workspace))return false;
  const url=new URL('/',location.href),params=new URLSearchParams({pluginWorkbench:plugin.id,workspace});
  const selected=window.AidePluginWorkbench.selection(services.selection);if(services.selection&&!selected)return false;
  if(selected)params.set('selection',JSON.stringify(selected));url.hash=params.toString();
  // Open directly in the click path, before any asynchronous bridge/API work.
  const page=window.open(url.href,'_blank');
  if(!page){services.toast(services.t('请允许 Aide 打开新页面后重试'));return false;}
  page.opener=null;return true;
 }
 window.AidePluginWorkbenchTabs=Object.freeze({read,open});
})();
