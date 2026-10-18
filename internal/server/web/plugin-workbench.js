'use strict';
(function(){
 let active=null;
 function close(){if(!active)return;const item=active;active=null;clearInterval(item.timer);item.abort.abort();if(item.port){try{item.port.postMessage({protocol:1,event:'scope.revoked'});}catch(_){}item.port.close();}if(item.editorId)item.services.api('/plugins/brdplan/editor/'+item.editorId,{method:'DELETE'}).catch(()=>{});if(item.bridge){item.services.api('/plugins/'+encodeURIComponent(item.plugin.id)+'/workbench/'+item.bridge.instanceId,{method:'DELETE'}).catch(()=>{});}if(item.download)URL.revokeObjectURL(item.download.url);if(item.listener)window.removeEventListener('message',item.listener);item.frame.src='about:blank';item.host.remove();item.focus?.focus();}
 async function open(plugin,services){
  close();if(services.blocked()||!plugin.enabled)return;
  const focus=document.activeElement,scope=services.scope(),abort=new AbortController();
  const host=document.createElement('section');host.className='plugin-workbench';host.setAttribute('role','dialog');host.setAttribute('aria-modal','true');host.setAttribute('aria-label',plugin.workbench.title);
  const header=document.createElement('header'),title=document.createElement('strong'),notice=document.createElement('span'),button=document.createElement('button'),frame=document.createElement('iframe');
  title.textContent=plugin.workbench.title;notice.textContent=services.t('独立插件工作台 · 本地计算');button.textContent=services.t('关闭工作台');button.type='button';button.onclick=close;
  frame.title=plugin.workbench.title;frame.setAttribute('sandbox','allow-scripts allow-downloads');frame.referrerPolicy='no-referrer';
  header.append(title,notice,button);host.append(header,frame);document.body.append(host);
  const review=document.createElement('button');review.type='button';review.hidden=true;review.textContent=services.t('查看待审批提案');header.append(review);
  const save=document.createElement('button');save.type='button';save.className='plugin-workbench-export';save.hidden=true;header.append(save);
  const editor=document.createElement('button');editor.type='button';editor.hidden=true;header.append(editor);
  const item={host,frame,abort,focus,timer:null,services,plugin,port:null,bridge:null,listener:null,download:null,toolReady:null};active=item;button.focus();
  const invalid=()=>active!==item||services.blocked()||services.scope()!==scope||!services.enabled(plugin.id);
  item.listener=e=>{
   if(invalid()||e.source!==frame.contentWindow)return;
   const m=e.data;
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
   if(item.download&&!item.download.requested){respond('error','Save the pending export first');return;}
   if(item.download)URL.revokeObjectURL(item.download.url);
   const url=URL.createObjectURL(new Blob([m.bytes],{type:'application/octet-stream'}));item.download={url,name:m.name,requested:false};save.hidden=false;save.title=m.name;save.textContent=services.t('保存文件')+' · '+m.name;
   save.onclick=()=>{if(invalid()||item.download?.url!==url)return;const a=document.createElement('a');a.href=url;a.download=m.name;document.body.append(a);a.click();a.remove();item.download.requested=true;save.textContent=services.t('再次保存')+' · '+m.name;respond('requested');};
   respond('ready');
  };window.addEventListener('message',item.listener);
  item.timer=setInterval(async()=>{if(invalid()){if(active===item)close();return;}if(!item.proposal||item.polling||Date.now()-(item.lastPoll||0)<1500)return;item.polling=true;item.lastPoll=Date.now();try{const response=await services.api('/plugins/'+encodeURIComponent(plugin.id)+'/workbench/'+item.bridge.instanceId+'/call',{method:'POST',body:JSON.stringify({protocol:1,requestId:'proposal_poll',method:'proposal.status',proposalId:item.proposal}),signal:abort.signal});const signature=JSON.stringify(response.result);if(!invalid()&&signature!==item.proposalSignature){item.proposalSignature=signature;item.port?.postMessage({protocol:1,event:'proposal.changed',proposalId:item.proposal,result:response.result});}}catch(_){/* No automatic tool retry or approval on failed observation. */}finally{item.polling=false;}},300);
  try{const result=await services.api('/plugins/'+encodeURIComponent(plugin.id)+'/workbench/open',{method:'POST',body:'{}',signal:abort.signal});
   if(invalid()){if(active===item)close();return;}
   if(result.protocol!==1||!/^\/plugin-workbench\/[a-f0-9]{64}$/.test(result.url))throw Error('Invalid workbench route');
   if(result.bridge){
    const b=result.bridge;if(b.protocol!==1||!(/^[a-f0-9]{64}$/.test(b.instanceId))||!Array.isArray(b.tools)||b.tools.length>16||b.tools.some(x=>typeof x!=='string'||!plugin.workbench.tools?.includes(x)))throw Error('Invalid workbench bridge');item.bridge=b;
    item.toolReady=e=>{if(invalid()||item.port||e.source!==frame.contentWindow||e.data?.type!=='aide.workbench.ready'||e.data?.protocol!==1)return;
     const channel=new MessageChannel();item.port=channel.port1;const seen=new Set();let busy=false;
     item.port.onmessage=async event=>{const m=event.data;if(invalid()){close();return;}if(!m||m.protocol!==1||typeof m.requestId!=='string'||!(/^[A-Za-z_][A-Za-z0-9_]{0,63}$/.test(m.requestId||'')))return;
      const send=value=>{if(!invalid())item.port?.postMessage(value);};
      if(seen.has(m.requestId)||seen.size>=128||busy){send({requestId:m.requestId,status:'error',error:'Busy or duplicate request'});return;}seen.add(m.requestId);
      if(!['tool.call','proposal.status'].includes(m.method)||(m.method==='tool.call'&&!b.tools.includes(m.tool))){send({requestId:m.requestId,status:'error',error:'Method/tool not permitted'});return;}
      busy=true;try{const body=JSON.stringify({protocol:1,requestId:m.requestId,method:m.method,tool:m.tool,arguments:m.arguments||{},attachments:m.attachments||[],proposalId:m.proposalId});if(body.length>262144)throw Error('Bridge request too large');
       const value=await services.api('/plugins/'+encodeURIComponent(plugin.id)+'/workbench/'+b.instanceId+'/call',{method:'POST',body,signal:abort.signal});if(invalid())return;
       send(value);if(value.status==='proposal_pending'){item.proposal=value.proposalId;item.proposalSignature='';review.hidden=false;review.onclick=()=>{close();services.reviewProposal?.(value.sessionId);};}
      }catch(error){send({requestId:m.requestId,status:'error',error:error.message});}finally{busy=false;}
     };item.port.start();frame.contentWindow.postMessage({type:'aide.workbench.connect',protocol:1,pluginId:plugin.id,tools:b.tools},'*',[channel.port2]);
    };
   }
   frame.src=result.url;
  }catch(error){if(active===item){close();if(error.name!=='AbortError')services.toast(error.message);}}
 }
 document.addEventListener('keydown',e=>{if(active&&e.key==='Escape'){e.preventDefault();close();}});
 window.AidePluginWorkbench={open,close};
})();
