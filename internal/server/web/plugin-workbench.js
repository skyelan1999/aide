'use strict';
(function(){
 let active=null;
 function close(){if(!active)return;const item=active;active=null;clearInterval(item.timer);item.abort.abort();if(item.port){try{item.port.postMessage({protocol:1,event:'scope.revoked'});}catch(_){}item.port.close();}if(item.bridge){item.services.api('/plugins/'+encodeURIComponent(item.plugin.id)+'/workbench/'+item.bridge.instanceId,{method:'DELETE'}).catch(()=>{});}if(item.listener)window.removeEventListener('message',item.listener);item.frame.src='about:blank';item.host.remove();item.focus?.focus();}
 async function open(plugin,services){
  close();if(services.blocked()||!plugin.enabled)return;
  const focus=document.activeElement,scope=services.scope(),abort=new AbortController();
  const host=document.createElement('section');host.className='plugin-workbench';host.setAttribute('role','dialog');host.setAttribute('aria-modal','true');host.setAttribute('aria-label',plugin.workbench.title);
  const header=document.createElement('header'),title=document.createElement('strong'),notice=document.createElement('span'),button=document.createElement('button'),frame=document.createElement('iframe');
  title.textContent=plugin.workbench.title;notice.textContent=services.t('独立插件工作台 · 本地计算');button.textContent=services.t('关闭工作台');button.type='button';button.onclick=close;
  frame.title=plugin.workbench.title;frame.setAttribute('sandbox','allow-scripts allow-downloads');frame.referrerPolicy='no-referrer';
  header.append(title,notice,button);host.append(header,frame);document.body.append(host);
  const review=document.createElement('button');review.type='button';review.hidden=true;review.textContent=services.t('查看待审批提案');header.append(review);
  const item={host,frame,abort,focus,timer:null,services,plugin,port:null,bridge:null,listener:null};active=item;button.focus();
  const invalid=()=>active!==item||services.blocked()||services.scope()!==scope||!services.enabled(plugin.id);
  item.timer=setInterval(async()=>{if(invalid()){if(active===item)close();return;}if(!item.proposal||item.polling||Date.now()-(item.lastPoll||0)<1500)return;item.polling=true;item.lastPoll=Date.now();try{const response=await services.api('/plugins/'+encodeURIComponent(plugin.id)+'/workbench/'+item.bridge.instanceId+'/call',{method:'POST',body:JSON.stringify({protocol:1,requestId:'proposal_poll',method:'proposal.status',proposalId:item.proposal}),signal:abort.signal});const signature=JSON.stringify(response.result);if(!invalid()&&signature!==item.proposalSignature){item.proposalSignature=signature;item.port?.postMessage({protocol:1,event:'proposal.changed',proposalId:item.proposal,result:response.result});}}catch(_){/* No automatic tool retry or approval on failed observation. */}finally{item.polling=false;}},300);
  try{const result=await services.api('/plugins/'+encodeURIComponent(plugin.id)+'/workbench/open',{method:'POST',body:'{}',signal:abort.signal});
   if(invalid()){if(active===item)close();return;}
   if(result.protocol!==1||!/^\/plugin-workbench\/[a-f0-9]{64}$/.test(result.url))throw Error('Invalid workbench route');
   if(result.bridge){
    const b=result.bridge;if(b.protocol!==1||!(/^[a-f0-9]{64}$/.test(b.instanceId))||!Array.isArray(b.tools)||b.tools.length>16||b.tools.some(x=>typeof x!=='string'||!plugin.workbench.tools?.includes(x)))throw Error('Invalid workbench bridge');item.bridge=b;
    item.listener=e=>{if(invalid()||item.port||e.source!==frame.contentWindow||e.data?.type!=='aide.workbench.ready'||e.data?.protocol!==1)return;
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
    };window.addEventListener('message',item.listener);
   }
   frame.src=result.url;
  }catch(error){if(active===item){close();if(error.name!=='AbortError')services.toast(error.message);}}
 }
 document.addEventListener('keydown',e=>{if(active&&e.key==='Escape'){e.preventDefault();close();}});
 window.AidePluginWorkbench={open,close};
})();
