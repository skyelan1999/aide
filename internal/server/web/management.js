/* Management proposals are server-owned. No generated code runs in this view. */
(() => {
  'use strict';
  let busy=false, last='', generation=0, renderedScope='';
  const reviewed=new Set();
  const text=(tag,cls,value)=>{const n=document.createElement(tag);n.className=cls;n.textContent=value;return n;};
  const tr=(zh,en)=>window.aideI18n?.language?.() === 'en' ? en : t(zh);
  const available=()=>!!state.config&&!!state.token&&!lockScreen.locked;
  function host(){let n=document.getElementById('management-proposals');if(!n){n=text('section','management-proposals','');n.id='management-proposals';n.setAttribute('aria-label',tr('管理提案','Management proposals'));document.querySelector('.composer-wrap')?.prepend(n);}return n;}
  function operation(p){return ({prepare:tr('安装 / 升级插件','Install / upgrade plugin'),enable:tr('启用插件','Enable plugin'),disable:tr('停用插件','Disable plugin'),remove:p.kind==='plugins'?tr('删除插件','Remove plugin'):tr('删除引用','Remove reference'),upsert:tr('配置引用','Configure reference')})[p.action]||p.action;}
  function button(label,fn){const b=text('button','quiet',label);b.type='button';b.onclick=fn;return b;}
  async function decide(p,action,button){
    if(!available())return;button.disabled=true;
    const scope=state.config.workspaceId;
    try{await api('/management-proposals/'+encodeURIComponent(p.id)+'/'+action,{method:'POST'});if(scope!==state.config?.workspaceId)return;last='';await refreshConfig();await refresh();}
    catch(e){if(scope===state.config?.workspaceId)toast(e.message);}
    finally{button.disabled=false;}
  }
  function card(p,epoch,scope){
    const item=text('article','management-proposal',''),head=text('div','management-proposal-head','');
    head.append(text('strong','',operation(p)+' · '+p.target),text('span','management-actor',p.actor==='assistant'?tr('助理','Assistant'):'aide'));
    item.append(head);
    if(p.reason)item.append(text('p','management-reason',p.reason));
    const detail=document.createElement('details'),summary=text('summary','',tr('审阅变更','Review changes')),body=text('div','management-preview','');detail.append(summary,body);item.append(detail);
    const controls=text('div','management-proposal-actions','');item.append(controls);
    let apply;
    if(p.status==='pending'){
      apply=button(tr('确认执行','Confirm'),()=>decide(p,'apply',apply));apply.classList.add('primary');apply.disabled=!reviewed.has(p.id);controls.append(apply);
    }
    if(p.status!=='running'){const reject=button(p.status==='failed'?tr('关闭','Close'):tr('拒绝','Reject'),()=>decide(p,'reject',reject));controls.append(reject);}
    if(p.status==='running')controls.append(text('span','muted',tr('正在执行，请勿重复操作','Applying; do not repeat')));
    if(p.result)item.append(text('p','management-result',p.result));
    let loading=false,loaded=false;
    detail.ontoggle=async()=>{
      if(!detail.open||loaded||loading)return;loading=true;body.replaceChildren();
      try{
        const data=await api('/management-proposals/'+encodeURIComponent(p.id));
        if(epoch!==generation||scope!==state.config?.workspaceId||!available())return;
        if(data.source)body.append(text('pre','',JSON.stringify(data.source,null,2)));
        if(data.manifest)body.append(text('pre','',JSON.stringify(data.manifest,null,2)));
        for(const [name,content] of Object.entries(data.files||{})){body.append(text('strong','',name),text('pre','',content));}
        if(!body.childNodes.length)body.append(text('p','muted',operation(p)+' · '+p.target));
        if(data.sha256){body.append(text('small','management-digest','SHA-256 '+data.sha256));const download=button(tr('下载 ZIP','Download ZIP'),async()=>{
          if(!available()||scope!==state.config?.workspaceId)return;
          try{const r=await fetch('/api/management-proposals/'+encodeURIComponent(p.id)+'/package',{headers:{Authorization:'Bearer '+state.token}});if(!r.ok)throw new Error(tr('下载失败','Download failed'));const blob=await r.blob();if(!available()||scope!==state.config?.workspaceId)return;const url=URL.createObjectURL(blob),a=document.createElement('a');a.href=url;a.download=p.target+'.zip';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);}catch(e){toast(e.message);}
        });body.append(download);}
        loaded=true;reviewed.add(p.id);if(apply)apply.disabled=false;
      }catch(e){body.append(text('p','management-result',e.message));}finally{loading=false;}
    };
    return item;
  }
  async function refresh(){
    if(!available()){generation++;last='';reviewed.clear();document.getElementById('management-proposals')?.replaceChildren();return;}
    const scope=state.config.workspaceId;
    if(scope!==renderedScope){renderedScope=scope;generation++;last='';reviewed.clear();document.getElementById('management-proposals')?.replaceChildren();}
    if(busy)return;busy=true;const epoch=generation;
    try{const response=await api('/management-proposals');if(!available()||epoch!==generation||scope!==state.config.workspaceId)return;const fingerprint=scope+JSON.stringify(response.proposals);if(fingerprint===last)return;last=fingerprint;generation++;const node=host();node.replaceChildren(...(response.proposals||[]).map(p=>card(p,generation,scope)));node.hidden=!(response.proposals||[]).length;}
    catch(_){/* Preserve the last receipt during transient network loss. */}finally{busy=false;}
  }
  window.AideManagement={refresh};
  setInterval(()=>{if(!document.hidden)refresh();},5000);
  document.addEventListener('visibilitychange',refresh);
  refresh();
})();
