'use strict';
// File choice and native fallback are owned by the parent, never by the iframe.
(function(){
 let generation=0;
 async function open(spec,services){
  if(!spec?.path||services.blocked()||!services.current())return false;
  const query=new URLSearchParams({path:spec.path,root:spec.root||'workspace'});for(const key of ['source','origin','knowledgeWorkspace'])if(spec[key])query.set(key,spec[key]);if(spec.readOnly)query.set('readOnly','true');
  let response;try{response=await services.api('/plugins/file-handlers?'+query);}catch(_){return false;}
  if(!services.current()||services.blocked())return true;
  const handlers=response.handlers||[];if(!handlers.length)return false;
  const serial=++generation;let stopped=false,chosen=handlers[0],timer=null;
  const document={path:spec.path,root:spec.root||'workspace',source:spec.source||'',readOnly:spec.readOnly===true,origin:spec.origin||'',knowledgeWorkspace:spec.knowledgeWorkspace||''};
  function fallback(reason,dirty){if(stopped)return;stopped=true;clearTimeout(timer);if(!services.current()||services.blocked())return;if(dirty)services.toast(services.t('插件已停止，未保存的修改不会写入原文件'));if(reason==='error')services.toast(services.t('插件预览不可用，已切换原生预览'));void services.native();}
  async function launch(plugin){
   const launched=await window.AidePluginWorkbench.open(plugin,{...services,document,handlers,enabled:()=>!stopped&&serial===generation,
    onSelect:id=>{if(id==='native'){window.AidePluginWorkbench.close();return;}const next=handlers.find(p=>p.id===id);if(next)void launch(next);},
    onClose:(reason,dirty)=>{if(reason==='unload'){stopped=true;clearTimeout(timer);return;}if(reason!=='replace')fallback(reason,dirty);}
   });
   if(launched)chosen=plugin;
  }
  async function watch(){if(stopped||serial!==generation)return;if(!services.current()||services.blocked()){stopped=true;window.AidePluginWorkbench.close(true);return;}
   try{const live=await services.api('/plugins/file-handlers?'+query);if(stopped||serial!==generation)return;const match=live.handlers?.find(p=>p.id===chosen.id&&p.version===chosen.version&&JSON.stringify(p.workbench)===JSON.stringify(chosen.workbench));if(!match){window.AidePluginWorkbench.close(true);return;}}
   catch(_){/* A transient failed read does not destroy a user's draft. Calls still enforce server scope. */}
   if(!stopped)timer=setTimeout(watch,2500);
  }
  await launch(chosen);if(!stopped)timer=setTimeout(watch,2500);return true;
 }
 window.AidePluginFilePreview=Object.freeze({open});
})();
