/* Debounced saves share one queue with the manual button. Never bypass file hashes. */
(() => {
  'use strict';
  function bind({editor,button,manual,getContext,save,onSaved,blocked,t,error}) {
    let activeKey='', generation=0, baseline=editor.value, timer=0, inFlight=null, suspended=false, composing=false;
    const label=document.createElement('span'); label.textContent=t('自动保存');
    const rail=document.createElement('span');rail.className='file-autosave-rail';rail.setAttribute('aria-hidden','true');rail.append(document.createElement('i'));
    const status=document.createElement('span');status.className='file-autosave-status';status.setAttribute('role','status');status.setAttribute('aria-live','polite');
    button.replaceChildren(label,rail); button.after(status);button.setAttribute('role','switch');
    const enabled=()=>window.aideUI?.get('editorAutoSave')===true;
    const identity=c=>JSON.stringify([c?.root,c?.source,c?.path,c?.wsId]);
    const writable=()=>identity(getContext())===activeKey && !!getContext()?.path && !editor.readOnly && !manual.disabled && !manual.classList.contains('hidden');
    function paint() {button.setAttribute('aria-checked',String(enabled()));button.disabled=!writable() || getContext()?.autoEligible === false;button.classList.toggle('hidden',manual.classList.contains('hidden') || getContext()?.autoEligible === false);status.hidden=button.classList.contains('hidden');}
    function schedule() {clearTimeout(timer);if(enabled() && getContext()?.autoEligible !== false && writable() && !suspended && !composing && editor.value!==baseline)timer=setTimeout(()=>flush(false),1200);}
    function contextAutoDisabled(){return getContext()?.autoEligible===false;}
    async function flush(manualRequest=false) {
      clearTimeout(timer); const requestedGeneration=generation;
      while(inFlight){await inFlight; if(requestedGeneration!==generation)return;}
      if(!writable() || blocked?.() || composing || (!manualRequest && (!enabled() || suspended || contextAutoDisabled())))return;
      const content=editor.value; if(!manualRequest && content===baseline)return;
      const context={...getContext()},key=identity(context),gen=generation;
      status.textContent=t('正在保存…'); button.classList.add('saving');
      inFlight=(async()=>{
        try {
          const result=await save(context,content);
          if(gen!==generation || key!==identity(getContext()))return;
          baseline=content;suspended=false;onSaved?.(context,content,result,manualRequest);
          status.textContent=t('已保存');
        } catch(e) {
          if(gen!==generation)return;
          suspended=true;status.textContent=t('保存失败，自动保存已暂停');error?.(e.message || String(e));
        } finally {button.classList.remove('saving');}
      })();
      await inFlight;inFlight=null; if(gen===generation)schedule();
    }
    function activate(){generation++;activeKey=identity(getContext());clearTimeout(timer);baseline=editor.value;suspended=false;composing=false;status.textContent='';paint();}
    button.onclick=()=>{window.aideUI?.set('editorAutoSave',!enabled());suspended=false;paint();schedule();};
    manual.onclick=()=>flush(true);
    editor.addEventListener('input',schedule);
    editor.addEventListener('compositionstart',()=>{composing=true;clearTimeout(timer);});
    editor.addEventListener('compositionend',()=>{composing=false;schedule();});
    window.aideUI?.subscribe(()=>{paint();schedule();});
    document.addEventListener('visibilitychange',()=>{if(!document.hidden)schedule();});
    window.addEventListener('beforeunload',event=>{if(writable() && editor.value!==baseline){event.preventDefault();event.returnValue='';}});
    return {activate,flush,resume:schedule,dirty:()=>writable() && editor.value!==baseline};
  }
  window.AideFileAutoSave={bind};
})();
