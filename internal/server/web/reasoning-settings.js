(function () {
 'use strict';
 const choices=['auto','off','low','medium','high'];
 function install({select,button,api,refresh,config,t,toast}) {
  if(!select||!button)return {sync(){}};
  let saving=false;
  function sync(){
   const labels={auto:t('自动'),off:t('关闭'),low:t('低'),medium:t('中'),high:t('高')};
   select.replaceChildren(...choices.map(value=>new Option(labels[value],value)));
   select.value=choices.includes(config()?.reasoningEffort)?config().reasoningEffort:'auto';
  }
  async function save(){
   if(saving)return;
   const value=select.value;
   if(!choices.includes(value)){toast(t('无效推理强度'));return;}
   saving=true;button.disabled=true;
   try{
    // Deliberately exclude model/provider/key drafts from this independent write.
    await api('/settings',{method:'PUT',body:JSON.stringify({reasoningEffort:value})});
    await refresh();
    if(config()?.reasoningEffort!==value)throw Error(t('推理强度保存后读回不一致，请重试'));
    toast(t('默认推理强度已保存'));
   }catch(error){toast(t('推理强度保存失败：{0}',error.message));}
   finally{saving=false;button.disabled=false;}
  }
  button.onclick=save;
  return {sync};
 }
 window.AideReasoningSettings={install};
})();
