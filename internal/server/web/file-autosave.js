/* Debounced saves share one queue with the manual button. Never bypass file hashes. */
(() => {
  'use strict';
  function bind({editor,button,manual,getContext,save,onSaved,readLatest,onConflict,blocked,t,error}) {
    let activeKey='', generation=0, baseline=editor.value, timer=0, inFlight=null, suspended=false, composing=false,recoveryTimer=0,recoveryBanner=null,activeDialog=null,viewChanges=0;
    const label=document.createElement('span'); label.textContent=t('自动保存');
    const rail=document.createElement('span');rail.className='file-autosave-rail';rail.setAttribute('aria-hidden','true');rail.append(document.createElement('i'));
    const status=document.createElement('span');status.className='file-autosave-status';status.setAttribute('role','status');status.setAttribute('aria-live','polite');
    const retry=document.createElement('button');retry.type='button';retry.className='file-autosave-retry hidden';retry.textContent=t('重试保存');retry.hidden=true;
    button.replaceChildren(label,rail); button.after(status);status.after(retry);button.setAttribute('role','switch');
    let retryMode='',retryContext=null,retryDraft=null;
    const enabled=()=>window.aideUI?.get('editorAutoSave')===true;
    const identity=c=>JSON.stringify([c?.root,c?.source,c?.path,c?.wsId]);
    const scope=c=>c?.recoveryScope||c?.wsId||'';
    const current=(gen,key)=>gen===generation&&key===activeKey&&key===identity(getContext());
    const writable=()=>identity(getContext())===activeKey && !!getContext()?.path && !editor.readOnly && !manual.disabled && !manual.classList.contains('hidden');
    function paint() {button.setAttribute('aria-checked',String(enabled()));button.disabled=!writable() || getContext()?.autoEligible === false;button.classList.toggle('hidden',manual.classList.contains('hidden') || getContext()?.autoEligible === false);status.hidden=button.classList.contains('hidden');retry.hidden=button.classList.contains('hidden')||!retryMode||!writable();retry.classList.toggle('hidden',retry.hidden);}
    function setRetry(mode,context=null,draft=null){retryMode=mode;retryContext=context;retryDraft=draft;retry.textContent=t(mode==='latest'?'重试读取服务器版本':'重试保存');paint();}
    function storeRecovery(){
      clearTimeout(recoveryTimer);const c={...getContext()},gen=generation,key=identity(c);
      if(identity(c)!==activeKey||!c.path||blocked?.()||!window.AideContinuity?.enabled()||!scope(c))return;
      AideContinuity.writeTab(scope(c),'file-view',identity(c),{hash:c.hash,start:editor.selectionStart,end:editor.selectionEnd,scrollTop:editor.scrollTop,scrollLeft:editor.scrollLeft}).catch(e=>{if(current(gen,key))error?.(e.message);});
      if(!writable())return;
      const dirty=editor.value!==baseline;
      if(!dirty)return;
      const operation=AideContinuity.writeFileDraft(scope(c),identity(c),{text:editor.value,hash:c.hash,start:editor.selectionStart,end:editor.selectionEnd,scrollTop:editor.scrollTop,scrollLeft:editor.scrollLeft});
      operation.catch(e=>{if(!current(gen,key))return;status.textContent=t('本地草稿保存失败');error?.(e.message);});
    }
    async function restoreView(){
      const c={...getContext()},gen=generation,key=identity(c);
      if(!window.AideContinuity||!scope(c)||!c.path||key!==activeKey)return;
      try{
        const view=await AideContinuity.readTab(scope(c),'file-view',key);
        if(!view||blocked?.()||gen!==generation||key!==identity(getContext())||view.hash!==c.hash)return;
        const stamp=viewChanges;
        const apply=()=>{
          if(blocked?.()||gen!==generation||key!==identity(getContext())||stamp!==viewChanges||editor.classList.contains('hidden')||editor.parentElement?.classList.contains('hidden'))return;
          const number=(v,max)=>Number.isFinite(v)?Math.max(0,Math.min(max,v)):0;
          const start=number(view.start,editor.value.length),end=Math.max(start,number(view.end,editor.value.length));
          editor.focus({preventScroll:true});editor.setSelectionRange(start,end);editor.scrollTop=number(view.scrollTop,10000000);editor.scrollLeft=number(view.scrollLeft,10000000);
        };
        requestAnimationFrame(()=>requestAnimationFrame(apply));
      }catch(e){if(current(gen,key))status.textContent=t('本地恢复暂不可用');}
    }
    function queueRecovery(){clearTimeout(recoveryTimer);recoveryTimer=setTimeout(storeRecovery,180);}
    function applyRecovery(draft,text){
      editor.value=text;editor.focus({preventScroll:true});
      editor.setSelectionRange(Math.min(draft.start||0,text.length),Math.min(draft.end||0,text.length));
      recoveryBanner?.remove();recoveryBanner=null;suspended=false;editor.dispatchEvent(new Event('input',{bubbles:true}));
      editor.scrollTop=draft.scrollTop||0;editor.scrollLeft=draft.scrollLeft||0;
      editor.dispatchEvent(new Event('scroll'));
    }
    function compareRecovery(draft,gen,latest=null){
      if(blocked?.()||gen!==generation||activeKey!==identity(getContext()))return;
      activeDialog?.close();const dialog=document.createElement('dialog');activeDialog=dialog;dialog.className='recovery-dialog';
      const title=document.createElement('h2');title.textContent=t('保存冲突对比');
      const columns=document.createElement('div');columns.className='recovery-columns';
      for(const [label,text]of [[t('服务器当前正文'),latest?.content??baseline],[t('本地恢复草稿'),draft.text]]){
        const section=document.createElement('section'),heading=document.createElement('h3'),pre=document.createElement('pre');heading.textContent=label;pre.textContent=text;section.append(heading,pre);columns.append(section);
      }
      const label=document.createElement('label'),merge=document.createElement('textarea');label.textContent=t('合并后的编辑内容');merge.value=draft.text;merge.setAttribute('aria-label',label.textContent);
      const accept=document.createElement('button'),cancel=document.createElement('button');accept.textContent=t('采用合并内容');cancel.textContent=t('取消');
      accept.onclick=()=>{if(gen===generation&&!blocked?.()){if(latest){baseline=latest.content;onConflict?.(latest);}applyRecovery(draft,merge.value);}dialog.close();};cancel.onclick=()=>dialog.close();
      dialog.append(title,columns,label,merge,accept,cancel);dialog.addEventListener('close',()=>{dialog.remove();if(activeDialog===dialog)activeDialog=null;},{once:true});document.body.append(dialog);dialog.showModal();
    }
    async function offerRecovery(gen){
      const c={...getContext()},stamp=viewChanges;if(!window.AideContinuity||!scope(c)||!c.path||identity(c)!==activeKey)return;
      try{
        const [storedDrafts,view]=await Promise.all([AideContinuity.listFileDrafts(scope(c),identity(c)),AideContinuity.readTab(scope(c),'file-view',identity(c))]);
        const drafts=storedDrafts.filter(d=>typeof d.text==='string'&&d.text!==baseline);let draft=drafts[0];
        if(blocked?.()||gen!==generation||identity(c)!==activeKey||editor.value!==baseline)return;
        if(stamp===viewChanges&&view&&typeof c.hash==='string'&&c.hash&&view.hash===c.hash&&!editor.classList.contains('hidden'))restoreView();
        if(!writable())return;
        if(blocked?.()||gen!==generation||identity(c)!==activeKey||!draft||typeof draft.text!=='string'||draft.text===baseline||editor.value!==baseline)return;
        recoveryBanner=document.createElement('div');recoveryBanner.className='file-recovery-banner';recoveryBanner.setAttribute('role','status');
        const text=document.createElement('span'),restore=document.createElement('button'),discard=document.createElement('button');
        const picker=document.createElement('select');picker.setAttribute('aria-label',t('选择恢复草稿'));
        drafts.forEach((d,i)=>{const option=document.createElement('option');option.value=String(i);option.textContent=`${i+1} · ${d.updated?new Date(d.updated).toLocaleString():t('旧版草稿')} · ${d.text.length} ${t('字符')}`;picker.append(option);});
        const paintDraft=()=>{const changed=draft.hash!==c.hash;text.textContent=t(changed?'服务器已更新，本地草稿等待合并':'发现未保存的本地草稿');restore.textContent=t(changed?'对比并合并':'恢复草稿');};
        picker.onchange=()=>{draft=drafts[Number(picker.value)];paintDraft();};paintDraft();discard.textContent=t('忽略此草稿');
        restore.onclick=()=>{if(!current(gen,identity(c))||blocked?.())return;if(draft.hash!==c.hash)compareRecovery(draft,gen);else applyRecovery(draft,draft.text);};
        discard.onclick=async()=>{if(!current(gen,identity(c))||blocked?.())return;discard.disabled=true;try{await AideContinuity.dismissFileDraft(scope(c),identity(c),draft);if(!current(gen,identity(c)))return;recoveryBanner?.remove();recoveryBanner=null;offerRecovery(gen);}catch(e){if(current(gen,identity(c))){error?.(e.message);discard.disabled=false;}}};
        recoveryBanner.append(text);if(drafts.length>1)recoveryBanner.append(picker);recoveryBanner.append(restore,discard);manual.parentElement.after(recoveryBanner);
      }catch(e){if(current(gen,identity(c)))status.textContent=t('本地恢复暂不可用');}
    }
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
          if(!current(gen,key))return;
          baseline=content;suspended=false;setRetry('');onSaved?.(context,content,result,manualRequest);
          status.textContent=t('服务器已保存');
          if(window.AideContinuity?.enabled()&&scope(context))await AideContinuity.savedFileDraft(scope(context),identity(context),content).catch(e=>{if(current(gen,key))error?.(e.message);});
          if(current(gen,key))storeRecovery();
        } catch(e) {
          if(!current(gen,key))return;
          suspended=true;storeRecovery();status.textContent=t(e.status===409?'服务器内容已变化，请对比后保存':'保存失败，自动保存已暂停');
          if(e.status===409){const draft={text:editor.value,start:editor.selectionStart,end:editor.selectionEnd};setRetry('');const latest=await Promise.resolve().then(()=>readLatest?.(context)).catch(()=>null);if(latest&&current(gen,key)){setRetry('');compareRecovery(draft,gen,latest);}else if(current(gen,key)){status.textContent=t('读取服务器版本失败，可重试');setRetry('latest',context,draft);}}
          else setRetry('save',context,{text:editor.value});
          if(current(gen,key))error?.(e.message || String(e));
        } finally {button.classList.remove('saving');}
      })();
      const pending=inFlight;
      try{await pending;}finally{if(inFlight===pending)inFlight=null;if(current(gen,key))schedule();}
    }
    function activate(){activeDialog?.close();generation++;activeKey=identity(getContext());clearTimeout(timer);clearTimeout(recoveryTimer);recoveryBanner?.remove();recoveryBanner=null;baseline=editor.value;suspended=false;composing=false;status.textContent='';setRetry('');button.classList.remove('saving');paint();offerRecovery(generation);}
    button.onclick=()=>{window.aideUI?.set('editorAutoSave',!enabled());suspended=false;paint();schedule();};
    manual.onclick=()=>flush(true);
    retry.onclick=async()=>{if(!retryMode||!writable()||blocked?.())return;const mode=retryMode,context=retryContext,draft=retryDraft,gen=generation,key=identity(context);if(!current(gen,key))return;retry.disabled=true;try{if(mode==='latest'){const latest=await readLatest?.(context);if(!latest)throw new Error(t('服务器未返回当前版本'));if(!current(gen,key)||blocked?.())return;setRetry('');status.textContent=t('服务器版本已读取，请比较后选择');compareRecovery(draft,gen,latest);}else{setRetry('');await flush(true);}}catch(e){if(current(gen,key)){status.textContent=t(mode==='latest'?'读取服务器版本失败，可重试':'保存重试失败，可再次重试');setRetry(mode,context,draft);error?.(e.message||String(e));}}finally{retry.disabled=false;paint();}};
    editor.addEventListener('input',()=>{recoveryBanner?.remove();recoveryBanner=null;schedule();});for(const event of ['input','scroll','select','keyup','click','blur'])editor.addEventListener(event,()=>{viewChanges++;queueRecovery();},{passive:true});document.addEventListener('selectionchange',()=>{if(document.activeElement===editor){viewChanges++;queueRecovery();}});window.addEventListener('pagehide',storeRecovery);
    editor.addEventListener('compositionstart',()=>{composing=true;clearTimeout(timer);});
    editor.addEventListener('compositionend',()=>{composing=false;schedule();});
    window.aideUI?.subscribe(()=>{paint();schedule();});
    window.LockCluster?.on('effective',value=>{if(value)activeDialog?.close();});
    document.addEventListener('visibilitychange',()=>{if(!document.hidden)schedule();});
    window.addEventListener('beforeunload',event=>{if(writable() && editor.value!==baseline){event.preventDefault();event.returnValue='';}});
    return {activate,flush,restoreView,resume:schedule,busy:()=>!!inFlight,dirty:()=>writable() && editor.value!==baseline};
  }
  window.AideFileAutoSave={bind};
})();
