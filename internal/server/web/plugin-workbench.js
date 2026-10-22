'use strict';
(function(){
 let active=null;
 function selection(value){
  if(!value||typeof value!=='object')return undefined;const out={};
  for(const side of ['left','right']){const v=value[side];if(!v)continue;
   if(typeof v.path!=='string'||!v.path||v.path.length>2048||v.path.startsWith('/')||v.path.split('/').includes('..')||/[\\\x00-\x1f]/.test(v.path)||! /^(workspace|context|source:[A-Za-z0-9_-]{1,64})$/.test(v.source||''))return undefined;
   out[side]={path:v.path,source:v.source,directory:v.directory===true};
  }return out.left?out:undefined;
 }
 function close(force=false,reason='close'){if(!active)return;const item=active;if(item.documentDirty&&!force&&!window.confirm(item.services.t('插件中有未保存修改，仍要离开吗？')))return false;active=null;clearInterval(item.timer);if(item.startupTimer)clearTimeout(item.startupTimer);item.themeObserver?.disconnect();if(item.themeFrame)cancelAnimationFrame(item.themeFrame);const release=()=>{item.abort.abort();if(item.bridge)item.services.api('/plugins/'+encodeURIComponent(item.plugin.id)+'/workbench/'+item.bridge.instanceId,{method:'DELETE',keepalive:true}).catch(()=>{});};if(!force&&item.plugin.daemon&&item.plugin.workbench.closeBehavior==='detach'&&item.inflight)item.inflight.then(release,release);else release();if(item.port){try{item.port.postMessage({protocol:1,event:'scope.revoked'});}catch(_){}item.port.close();}if(item.editorId)item.services.api('/plugins/brdplan/editor/'+item.editorId,{method:'DELETE'}).catch(()=>{});if(item.download)URL.revokeObjectURL(item.download.url);if(item.listener)window.removeEventListener('message',item.listener);item.frame.src='about:blank';item.host.remove();item.reviewBack?.remove();item.focus?.focus();item.services.onClose?.(reason,!!item.documentDirty);return true;}
 async function open(plugin,services){
  if(close(false,'replace')===false)return false;if(services.blocked()||!plugin.enabled)return false;
  const focus=document.activeElement,scope=services.scope(),abort=new AbortController();
  const host=document.createElement('section');host.className='plugin-workbench';host.setAttribute('role','dialog');host.setAttribute('aria-modal','true');host.setAttribute('aria-label',plugin.workbench.title);
  const header=document.createElement('header'),title=document.createElement('strong'),notice=document.createElement('span'),button=document.createElement('button'),frame=document.createElement('iframe');
  title.textContent=plugin.workbench.title;notice.textContent=services.t('插件工作台 · 本地计算');button.textContent=services.t('关闭工作台');button.type='button';button.onclick=()=>close();
  frame.title=plugin.workbench.title;frame.setAttribute('sandbox','allow-scripts allow-downloads');frame.referrerPolicy='no-referrer';
  header.append(title,notice,button);host.append(header,frame);document.body.append(host);
  const reviewBack=document.createElement('button');reviewBack.type='button';reviewBack.className='plugin-workbench-return';reviewBack.hidden=true;reviewBack.textContent=services.t('返回插件工作台');document.body.append(reviewBack);
  const review=document.createElement('button');review.type='button';review.hidden=true;review.textContent=services.t('查看待审批提案');header.append(review);
  const save=document.createElement('button');save.type='button';save.className='plugin-workbench-export';save.hidden=true;header.append(save);
  const editor=document.createElement('button');editor.type='button';editor.hidden=true;header.append(editor);
  const documentSave=document.createElement('button');documentSave.type='button';documentSave.hidden=true;documentSave.textContent=services.t('保存到原文件');header.append(documentSave);
  if(services.document){title.textContent=services.document.path;title.title=services.document.path;title.className='plugin-document-title';const picker=document.createElement('select');picker.setAttribute('aria-label',services.t('文件打开方式'));for(const p of [...(services.handlers||[plugin]),{id:'native',name:services.t('工作台原生预览')}]){const opt=document.createElement('option');opt.value=p.id;opt.textContent=p.name;picker.append(opt);}picker.value=plugin.id;picker.onchange=()=>{const id=picker.value;services.onSelect?.(id);picker.value=plugin.id;};header.append(picker);notice.textContent=services.t('插件文件预览')+' · '+services.document.path;}

  const item={host,frame,abort,focus,timer:null,services,plugin,port:null,bridge:null,listener:null,download:null,toolReady:null};active=item;item.reviewBack=reviewBack;const resumeReview=()=>{if(invalid())return;item.reviewing=false;host.hidden=false;reviewBack.hidden=true;};reviewBack.onclick=resumeReview;if(services.document)item.startupTimer=setTimeout(()=>{if(active===item&&!item.port)close(true,'error');},15000);button.focus();
  const invalid=()=>active!==item||services.blocked()||services.scope()!==scope||!services.enabled(plugin.id);
  // Presentation only: never expose host credentials, settings, or arbitrary CSS.
  const themeKeys=['bg','panel','surface','field','text','muted','line','field-border','brand-hi','brand-text','on-brand','chip-bg','selected','focus-ring','danger','warn','success'];
  const sendTheme=()=>{item.themeFrame=null;if(invalid()||!item.themeReady)return;const root=document.documentElement,style=getComputedStyle(root),colors={};for(const key of themeKeys)colors[key]=style.getPropertyValue('--'+key).trim();frame.contentWindow.postMessage({type:'aide.workbench.theme',protocol:1,theme:root.dataset.theme==='light'?'light':'dark',palette:['blue','comfort','green'].includes(root.dataset.palette)?root.dataset.palette:'blue',colors},'*');};
  item.listener=e=>{
   if(invalid()||e.source!==frame.contentWindow)return;
   const m=e.data;
   if(m?.type==='aide.workbench.theme.ready'&&m.protocol===1){if(!item.themeReady){item.themeReady=true;item.themeObserver=new MutationObserver(()=>{if(!item.themeFrame)item.themeFrame=requestAnimationFrame(sendTheme);});item.themeObserver.observe(document.documentElement,{attributes:true,attributeFilter:['data-theme','data-palette','style','class']});}sendTheme();return;}
   if(m?.type==='aide.workbench.editor'){
    if(plugin.id!=='brdplan'||m.pluginId!==plugin.id||m.protocol!==1||!/^editor_[0-9]{1,32}$/.test(m.requestId||'')||!['schematic','pcb'].includes(m.view)||typeof m.sessionPath!=='string'||m.sessionPath.length>500||m.sessionPath.startsWith('/')||m.sessionPath.split('/').some(p=>!p||p==='.'||p==='..')||/[\\\x00-\x1f]/.test(m.sessionPath))return;
    const request={protocol:1,requestId:m.requestId,sessionPath:m.sessionPath,view:m.view};
    const reply=(status,error)=>frame.contentWindow.postMessage({type:'aide.workbench.editor.status',protocol:1,requestId:request.requestId,status,...(error?{error}:{})},'*');
    editor.hidden=false;editor.disabled=false;editor.textContent=m.view==='pcb'?'打开 PCB 手画编辑器':'打开原理图手画编辑器';notice.textContent='手画会锁定AI写入；请在原生窗口保存并正常关闭';reply('ready');
    editor.onclick=async event=>{
     if(!event.isTrusted||invalid()||editor.disabled)return;
     editor.disabled=true;
     const popup=window.open('about:blank','_blank');if(popup)popup.opener=null;
     try{const response=await services.api('/plugins/brdplan/editor/open',{method:'POST',body:JSON.stringify(request),signal:abort.signal});
      if(invalid())throw Error('工作区已切换');
      if(response.protocol!==1||response.status!=='ready'||!/^\/plugin-editor\/[a-f0-9]{64}\/$/.test(response.url)||!(/^[a-f0-9]{64}$/.test(response.editorId)))throw Error('Invalid editor response');
      item.editorId=response.editorId;
      if(!popup)throw Error('请允许Aide打开编辑器窗口，再次点击恢复');
      popup.location=response.url;reply('opened');notice.textContent='原生窗口已打开；保存后关闭原生窗口，再刷新工程';
     }catch(error){popup?.close();reply('error',error.message);services.toast(error.message);}finally{editor.disabled=false;}
    };return;
   }
   if(m?.type!=='aide.workbench.export'){item.toolReady?.(e);return;}
   const respond=(status,error)=>frame.contentWindow.postMessage({type:'aide.workbench.export.status',protocol:1,requestId:m.requestId,status,...(error?{error}:{})},'*');
   if(m.protocol!==1||m.pluginId!==plugin.id||typeof m.requestId!=='string'||!/^export_[0-9]+$/.test(m.requestId))return;
   if(typeof m.name!=='string'||m.name.length>160||!m.name.length||m.name.startsWith('.')||/[\\/\x00-\x1f]/.test(m.name)||!(m.bytes instanceof ArrayBuffer)||m.bytes.byteLength>16*1024*1024||!m.bytes.byteLength){respond('error','Invalid or oversized export');return;}
   if(item.download)URL.revokeObjectURL(item.download.url);
   const url=URL.createObjectURL(new Blob([m.bytes],{type:'application/octet-stream'}));item.download={url,name:m.name,requested:false};save.hidden=false;save.title=m.name;save.textContent=services.t('保存文件')+' · '+m.name;
   save.onclick=()=>{if(invalid()||item.download?.url!==url)return;const a=document.createElement('a');a.href=url;a.download=m.name;document.body.append(a);a.click();a.remove();item.download.requested=true;save.textContent=services.t('再次保存')+' · '+m.name;respond('requested');};
   respond('ready');
  };window.addEventListener('message',item.listener);
  item.timer=setInterval(async()=>{if(invalid()){if(active===item)close(true);return;}if(!item.proposal||item.polling||Date.now()-(item.lastPoll||0)<1500)return;item.polling=true;item.lastPoll=Date.now();try{const response=await services.api('/plugins/'+encodeURIComponent(plugin.id)+'/workbench/'+item.bridge.instanceId+'/call',{method:'POST',body:JSON.stringify({protocol:1,requestId:'proposal_poll',method:'proposal.status',proposalId:item.proposal}),signal:abort.signal});const signature=JSON.stringify(response.result);if(!invalid()&&signature!==item.proposalSignature){item.proposalSignature=signature;if(response.result?.applied){review.hidden=true;if(item.reviewing)resumeReview();}item.port?.postMessage({protocol:1,event:'proposal.changed',proposalId:item.proposal,result:response.result});}}catch(_){/* No automatic tool retry or approval on failed observation. */}finally{item.polling=false;}},300);
  try{const result=await services.api('/plugins/'+encodeURIComponent(plugin.id)+'/workbench/open',{method:'POST',body:JSON.stringify(services.document?{document:services.document}:{}),signal:abort.signal});
   if(invalid()){if(active===item)close(true);return;}
   if(result.protocol!==1||!/^\/plugin-workbench\/[a-f0-9]{64}$/.test(result.url))throw Error('Invalid workbench route');
   if(result.bridge){
    const b=result.bridge;if(b.protocol!==1||!(/^[a-f0-9]{64}$/.test(b.instanceId))||!Array.isArray(b.tools)||b.tools.length>16||b.tools.some(x=>typeof x!=='string'||!plugin.workbench.tools?.includes(x)))throw Error('Invalid workbench bridge');item.bridge=b;
    item.toolReady=e=>{if(invalid()||item.port||e.source!==frame.contentWindow||e.data?.type!=='aide.workbench.ready'||e.data?.protocol!==1)return;
     const channel=new MessageChannel();item.port=channel.port1;const seen=new Set(),sequenced=e.data.requestIds==='monotonic';let highest=0,busy=false,requests=0,renewed=Date.now();
     item.port.onmessage=async event=>{const m=event.data;if(invalid()){close(true);return;}if(!m||m.protocol!==1||typeof m.requestId!=='string'||!(/^[A-Za-z_][A-Za-z0-9_]{0,63}$/.test(m.requestId||'')))return;
      const send=value=>{if(!invalid())item.port?.postMessage(value);};
      if(busy){send({requestId:m.requestId,status:'error',error:'A call is already pending'});return;}
      if(sequenced){const match=/^r_([1-9][0-9]{0,14})$/.exec(m.requestId),number=match?Number(match[1]):0;if(!Number.isSafeInteger(number)||number<=highest){send({requestId:m.requestId,status:'error',error:'Invalid or replayed sequence'});return;}highest=number;}
      else{if(seen.has(m.requestId)||seen.size>=128){send({requestId:m.requestId,status:'error',error:'Duplicate or exhausted request'});return;}seen.add(m.requestId);}
      const documentMethods=['document.package.info','document.package.read','document.info','document.read','document.save.begin','document.save.chunk','document.save.request','document.save.abort','document.dirty','document.error'];
      if((!['tool.call','proposal.status','file.target','file.save',...(b.mobileCapture?['capture.control','capture.network']:[]),...(b.professionalWorkflow&&plugin.id==='professional-workflow'?['workflow.control']:[])].includes(m.method)&&!(services.document&&b.document&&documentMethods.includes(m.method)))||(m.method==='tool.call'&&!b.tools.includes(m.tool))){send({requestId:m.requestId,status:'error',error:'Method/tool not permitted'});return;}
      busy=true;try{const body=JSON.stringify({protocol:1,requestId:m.requestId,method:m.method==='file.save'?'file.save.commit':m.method,tool:m.tool,arguments:m.arguments||{},attachments:m.attachments||[],proposalId:m.proposalId});if(body.length>(m.method==='document.save.chunk'?800000:262144))throw Error('Bridge request too large');
       if(m.method==='document.dirty'){item.documentDirty=m.arguments?.dirty===true;send({requestId:m.requestId,status:'result',result:{dirty:item.documentDirty}});return;}
       if(m.method==='document.error'){send({requestId:m.requestId,status:'result',result:{fallback:true}});setTimeout(()=>{if(active===item)close(true,'error');},0);return;}
       if(sequenced&&(requests>=100||Date.now()-renewed>20*60*1000)){
        item.inflight=services.api('/plugins/'+encodeURIComponent(plugin.id)+'/workbench/'+b.instanceId+'/renew',{method:'POST',body:'{}',signal:abort.signal});
        const next=await item.inflight;
        if(next.protocol!==1||!(/^[a-f0-9]{64}$/.test(next.instanceId)))throw Error('Invalid renewed bridge');
        b.instanceId=next.instanceId;requests=0;renewed=Date.now();
        if(invalid()){services.api('/plugins/'+encodeURIComponent(plugin.id)+'/workbench/'+b.instanceId,{method:'DELETE'}).catch(()=>{});return;}
       }
       requests++;
       item.inflight=services.api('/plugins/'+encodeURIComponent(plugin.id)+'/workbench/'+b.instanceId+'/call',{method:'POST',body,signal:abort.signal});
       const value=await item.inflight;if(invalid())return;
       if(m.method==='file.save'&&value.hash){value.requestId=m.requestId;value.status='result';value.result={saved:true,hash:value.hash};}
       send(value);
       if(value.status==='result'&&m.method==='document.save.begin')item.documentDirty=true;
       if(value.status==='result'&&m.method==='document.save.abort'){item.documentDirty=false;documentSave.hidden=true;}
       if(value.status==='result'&&m.method==='document.save.request'){item.documentDirty=true;documentSave.hidden=false;notice.textContent=services.t('插件已准备修改，请点击保存到原文件');}
       if(value.status==='proposal_pending'){item.proposal=value.proposalId;item.proposalSignature='';review.hidden=false;review.onclick=async()=>{if(invalid())return;item.reviewing=true;host.hidden=true;reviewBack.hidden=false;try{await services.reviewProposal?.(value.sessionId);}catch(error){resumeReview();services.toast(error.message);}};}
      }catch(error){send({requestId:m.requestId,status:'error',error:error.message});}finally{item.inflight=null;busy=false;}
     };
     documentSave.onclick=async event=>{if(!event.isTrusted||invalid()||item.inflight||documentSave.disabled)return;documentSave.disabled=true;try{
      if(requests>=100||Date.now()-renewed>20*60*1000){const next=await services.api('/plugins/'+encodeURIComponent(plugin.id)+'/workbench/'+b.instanceId+'/renew',{method:'POST',body:'{}',signal:abort.signal});if(!(/^[a-f0-9]{64}$/.test(next.instanceId)))throw Error('Invalid renewed bridge');b.instanceId=next.instanceId;requests=0;renewed=Date.now();}
      const saved=await services.api('/plugins/'+encodeURIComponent(plugin.id)+'/workbench/'+b.instanceId+'/call',{method:'POST',body:JSON.stringify({protocol:1,requestId:'user_save_'+Date.now(),method:'document.save.commit',arguments:{}}),signal:abort.signal});requests++;
      if(invalid())return;if(!saved.result?.saved)throw Error('Save did not complete');item.documentDirty=false;documentSave.hidden=true;notice.textContent=services.t('文件已保存');sendDocumentEvent(saved.result);
      if(saved.result.warning)services.toast(saved.result.warning);
     }catch(error){if(!invalid())services.toast(error.message);}finally{documentSave.disabled=false;}};
     const sendDocumentEvent=result=>{if(!invalid())item.port?.postMessage({protocol:1,event:'document.saved',result});};
     item.port.start();frame.contentWindow.postMessage({type:'aide.workbench.connect',protocol:1,pluginId:plugin.id,tools:b.tools,capabilities:['bridge.renew','daemon.detach','file.manual-save',...(b.mobileCapture?['capture.auto-network']:[]),...(b.mobileCapture?['capture',...(b.mobileCaptureVersion>=2?['capture.locations','capture.unclassified',...(b.mobileCaptureVersion>=3?['capture.person-binding']:[])]:[])]:[]),...(b.document?['document.read','document.save']:[])],selection:selection(services.selection)},'*',[channel.port2]);
    };
   }
   frame.src=result.url;return true;
  }catch(error){if(active===item){close(true,'error');if(error.name!=='AbortError')services.toast(error.message);}return false;}
 }
 document.addEventListener('keydown',e=>{if(active&&!active.reviewing&&e.key==='Escape'){e.preventDefault();close();}});
 window.addEventListener('beforeunload',event=>{if(active?.documentDirty){event.preventDefault();event.returnValue='';}});
 window.addEventListener('pagehide',()=>close(true,'unload'));
 window.AidePluginWorkbench={open,close,selection,prepareFileOpen(){if(!active?.services.document)return true;return close(false,'replace')!==false;}};
})();
