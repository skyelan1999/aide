'use strict';
(function(){
 let active=null;
 function close(){if(!active)return;const item=active;active=null;clearInterval(item.timer);item.abort.abort();item.frame.src='about:blank';item.host.remove();item.focus?.focus();}
 async function open(plugin,services){
  close();if(services.blocked()||!plugin.enabled)return;
  const focus=document.activeElement,scope=services.scope(),abort=new AbortController();
  const host=document.createElement('section');host.className='plugin-workbench';host.setAttribute('role','dialog');host.setAttribute('aria-modal','true');host.setAttribute('aria-label',plugin.workbench.title);
  const header=document.createElement('header'),title=document.createElement('strong'),notice=document.createElement('span'),button=document.createElement('button'),frame=document.createElement('iframe');
  title.textContent=plugin.workbench.title;notice.textContent=services.t('独立插件工作台 · 本地计算');button.textContent=services.t('关闭工作台');button.type='button';button.onclick=close;
  frame.title=plugin.workbench.title;frame.setAttribute('sandbox','allow-scripts allow-downloads');frame.referrerPolicy='no-referrer';
  header.append(title,notice,button);host.append(header,frame);document.body.append(host);
  const item={host,frame,abort,focus,timer:null};active=item;button.focus();
  const invalid=()=>active!==item||services.blocked()||services.scope()!==scope||!services.enabled(plugin.id);
  item.timer=setInterval(()=>{if(invalid()&&active===item)close();},300);
  try{const result=await services.api('/plugins/'+encodeURIComponent(plugin.id)+'/workbench/open',{method:'POST',body:'{}',signal:abort.signal});
   if(invalid()){if(active===item)close();return;}
   if(result.protocol!==1||!/^\/plugin-workbench\/[a-f0-9]{64}$/.test(result.url))throw Error('Invalid workbench route');
   frame.src=result.url;
  }catch(error){if(active===item){close();if(error.name!=='AbortError')services.toast(error.message);}}
 }
 document.addEventListener('keydown',e=>{if(active&&e.key==='Escape'){e.preventDefault();close();}});
 window.AidePluginWorkbench={open,close};
})();
