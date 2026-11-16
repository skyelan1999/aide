'use strict';
// A document capability travels only in a POST form to the bundled local editor.
// The editor never sees Aide credentials. Messages require origin AND window.
window.AideOfficeCollabora = (() => {
  async function open(container, spec, services, fallback) {
    const {api, el, t, teardown, bindTeardown, attach, selection} = services;
    teardown(container);container.replaceChildren(el('p','office-collabora-note',t('正在启动本地 Office 编辑器…')));
    let pendingCancelled=false;bindTeardown(container,()=>{pendingCancelled=true;});
    let status;
    try { status = await api('/office/collabora'); } catch { if(pendingCancelled)return;teardown(container);return fallback(); }
    if (!container.isConnected||pendingCancelled) return;
    if (!status.enabled || !/\.(docx?|xlsx?|pptx?|odt|ods|odp)$/i.test(spec.path) || ['local'].includes(spec.root) || String(spec.root).startsWith('mount:') || /\.bak$/i.test(spec.path)) {
      teardown(container);await fallback();
      if (container.isConnected && status.message) { container.prepend(el('div','office-collabora-note',status.message)); }
      return;
    }
    teardown(container);container.replaceChildren();
    const wrap=el('div','office-collabora'),bar=el('div','office-collabora-toolbar'),message=el('span','muted');message.setAttribute('role','status');
    const view=el('button','quiet quiet-sm',t('预览')),edit=el('button','quiet quiet-sm',t('编辑'));
    const save=el('button','quiet quiet-sm',t('保存')),toAI=el('button','quiet quiet-sm',t('文档交给 AI'));
    const selected=el('button','quiet quiet-sm',t('选区交给 AI')),offline=el('button','quiet quiet-sm',t('离线预览'));
    const stage=el('div','office-collabora-stage');bar.append(view,edit,save,toAI,selected,offline,message);wrap.append(bar,stage);container.append(wrap);
    const forcedReadOnly=spec.readOnly===true||!!spec.knowledgeOrigin;
    edit.disabled=forcedReadOnly;save.disabled=true;
    let disposed=false,session=null,frame=null,dirty=false,active=false,loaded=false,opening=0,saving=false,saveWaiters=[],selectionPending=false,refreshing=false;
    const post=(MessageId,Values={})=>{if(frame&&session)frame.contentWindow?.postMessage(JSON.stringify({MessageId,SendTime:Date.now(),Values}),session.origin);};
    const heartbeat=(reload=true)=>session?api('/office/collabora/sessions/'+session.id,{method:'PUT',body:JSON.stringify({active:active&&!session.readOnly,dirty:dirty&&!session.readOnly})}).then(result=>{if(!disposed&&result.reloadEditor){message.textContent=t('文档已由 AI 更新，请刷新预览');if(reload&&!dirty&&!active&&!saving&&!refreshing){refreshing=true;start(session.readOnly).finally(()=>{refreshing=false;});}}}).catch(error=>{if(!disposed){message.textContent=error.message;save.disabled=true;}}):Promise.resolve();
    const saveDocument=()=>new Promise((resolve,reject)=>{
      if(!session||session.readOnly){resolve();return;}
      const waiter={resolve,reject,timer:setTimeout(()=>{saveWaiters=saveWaiters.filter(w=>w!==waiter);saving=false;reject(Error(t('保存尚未确认，请重试')));},30000)};saveWaiters.push(waiter);
      if(!saving){saving=true;post('Action_Save',{DontTerminateEdit:false,DontSaveIfUnmodified:false,Notify:true});message.textContent=t('正在保存…');}
    });
    const settleSave=(success)=>{saving=false;for(const waiter of saveWaiters){clearTimeout(waiter.timer);success?waiter.resolve():waiter.reject(Error(t('保存失败，请保留编辑器并重试')));}saveWaiters=[];};
    async function start(readOnly) {
      if(session&&!session.readOnly){try{await saveDocument();}catch(error){message.textContent=error.message;return;}}
      const gen=++opening;message.textContent=t('正在打开本地 Office 编辑器…');
      try {
        const next=await api('/office/collabora/sessions',{method:'POST',body:JSON.stringify({...spec,readOnly:forcedReadOnly||readOnly})});
        if(disposed||gen!==opening)return;
        if(session){active=false;await heartbeat(false);}
        session=next;loaded=false;dirty=false;active=false;stage.replaceChildren();
        frame=el('iframe','office-collabora-frame');frame.name='aide-office-'+session.id;frame.title=t('Office 文档编辑器');frame.setAttribute('allow','clipboard-read; clipboard-write; fullscreen');stage.append(frame);
        const form=document.createElement('form');form.method='POST';form.action=session.url;form.target=frame.name;form.hidden=true;
        for(const [name,value] of Object.entries({access_token:session.accessToken,access_token_ttl:session.accessTokenTtl})) {const input=document.createElement('input');input.type='hidden';input.name=name;input.value=String(value);form.append(input);}
        stage.append(form);form.submit();form.remove();delete session.accessToken;
        services.onMode?.(session.readOnly);save.disabled=session.readOnly;edit.disabled=forcedReadOnly||session.canWrite!==true||!session.readOnly;view.disabled=session.readOnly;
        message.textContent=session.readOnly?t('预览中 · 不占用 AI 编辑'):t('编辑中 · 空闲保存后允许 AI 编辑');
      }catch(error){if(!disposed&&gen===opening){message.textContent=error.message;stage.replaceChildren(el('p','office-preview-error',error.message));}}
    }
    const onMessage=async event=>{
      if(disposed||!session||event.origin!==session.origin||event.source!==frame?.contentWindow)return;
      let data=event.data;try{if(typeof data==='string')data=JSON.parse(data);}catch{return;}
      if(!data||typeof data!=='object')return;const values=data.Values||{};
      switch(data.MessageId){
        case 'App_LoadingStatus':
          if(values.Status==='Frame_Ready'){post('Host_PostmessageReady');}
          if(values.Status==='Document_Loaded'||values.Status==='Initialized'){loaded=true;post('Host_PostmessageReady');post('Get_User_State');}
          if(values.Status==='Failed')message.textContent=t('Office 文档打开失败');break;
        case 'Doc_ModifiedStatus':dirty=values.Modified===true;active=dirty||active;await heartbeat();break;
        case 'User_Active':if(!session.readOnly){active=true;await heartbeat();}break;
        case 'User_Idle':active=false;if(dirty){try{await saveDocument();}catch(error){message.textContent=error.message;}}await heartbeat();break;
        case 'Get_User_State_Resp':
          // Collabora reports elapsed activity time in seconds. Merely
          // keeping a preview open must never reserve a write lease.
          if(!session.readOnly){active=values.State!=='idle'&&Number(values.Elapsed)<30;
            if(!active&&dirty&&!saving){try{await saveDocument();}catch(error){message.textContent=error.message;}}
            await heartbeat();}break;
        case 'Action_Save_Resp':
          if(values.success===true){dirty=false;message.textContent=t('已保存');await heartbeat();settleSave(true);}else{message.textContent=t('保存失败，请保留编辑器并重试');settleSave(false);}break;
        case 'Action_Copy_Resp':
          if(selectionPending){selectionPending=false;let content=values.content;if(content&&typeof content==='object')content=content['text/plain;charset=utf-8']||content['text/plain']||'';
            if(typeof content==='string'&&content.length<=60000&&content.trim())selection(content);else message.textContent=t('请先在文档中选择文字');}break;
        case 'UI_Close':case 'Session_Closed':active=false;await heartbeat();break;
      }
    };
    window.addEventListener('message',onMessage);
    const timer=setInterval(()=>{if(loaded){post('Get_User_State');heartbeat();}},10000);
    bindTeardown(container,()=>{disposed=true;opening++;clearInterval(timer);window.removeEventListener('message',onMessage);active=false;heartbeat(false);settleSave(false);});
    const run=fn=>async()=>{try{await fn();}catch(error){message.textContent=error.message;}};
    view.onclick=()=>start(true);edit.onclick=()=>start(false);save.onclick=run(saveDocument);
    toAI.onclick=run(async()=>{await saveDocument();attach();});
    selected.onclick=()=>{selectionPending=true;post('Action_Copy',{Mimetype:'text/plain;charset=utf-8'});};
    offline.onclick=run(async()=>{await saveDocument();teardown(container);await fallback();});
    await start(true);
  }
  return {open};
})();
