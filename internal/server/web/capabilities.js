/* Explicit read and write probes. Never treat configured permissions as tested capabilities. */
(()=>{'use strict';
 const node=(tag,cls='',text)=>{const e=document.createElement(tag);e.className=cls;if(text!=null)e.textContent=text;return e;};
 window.aideCapabilities={render({api,t=x=>x,blocked=()=>false,active=()=>true}){
  const wrap=node('div','settings-control capability-dashboard');
  const toolbar=node('div','capability-toolbar'),refresh=node('button','quiet',t('重读能力状态')),status=node('p','muted');refresh.type='button';
  const list=node('div','capability-list'),failure=node('p','task-error');failure.setAttribute('role','alert');failure.hidden=true;toolbar.append(refresh);wrap.append(toolbar,status,failure,list);
  let data=null,busy=false,epoch=0,loading=false,lastRead=0,wasAvailable=false;
  const valid=()=>wrap.isConnected&&!blocked()&&active();
  const states={not_run:'未检查',pass:'本项检查通过',failed:'检查失败',stale:'检查已失效'};
  const kinds={workspace:'工作空间',source:'引用来源',plugin:'插件',parser:'文档依赖',model:'模型'};
  async function load(){if(loading||busy||!valid())return;loading=true;lastRead=Date.now();const current=++epoch;refresh.disabled=true;status.textContent=t('正在读取能力状态…');try{const next=await api('/capabilities');if(!valid()||current!==epoch)return;data=next;status.textContent=t(next.note);render();}catch(e){if(valid()&&current===epoch)status.textContent=e.message;}finally{loading=false;if(valid()&&current===epoch)refresh.disabled=busy;else wasAvailable=false;}}
  function render(){list.replaceChildren();for(const item of data.items||[]){
   const row=node('section','capability-card'),head=node('div','capability-card-head'),title=node('div');title.append(node('small','muted',t(kinds[item.kind]||item.kind)),node('h4','',item.name));
   const badge=node('span','capability-state '+item.check.state,t(states[item.check.state]||'未检查'));head.append(title,badge);row.append(head,node('p','muted',t(item.detail)));
   const metadata=[];metadata.push(t(item.enabled?'已启用':'未启用'));if(item.writableConfigured)metadata.push(t('配置允许写入，尚未验证写权限'));if(item.kind==='plugin'||(item.kind==='source'&&item.detail.startsWith('mcp')))metadata.push(t('已登记工具：{0}',item.toolCount||0));if(item.hostState)metadata.push(t('最近宿主记录：{0}',t(({registered:'已登记',error:'加载有错误',not_registered:'未登记'})[item.hostState]||'未登记')));if(item.daemonState)metadata.push(t('进程状态：{0}',item.daemonState));row.append(node('p','capability-meta',metadata.join(' · ')));
   row.append(node('p','capability-result',t(item.check.message)));if(item.check.at)row.append(node('small','muted',new Date(item.check.at).toLocaleString()+' · '+item.check.durationMs+' ms'));
   const actions=node('div','capability-actions'),check=node('button','quiet',t('检查此项'));check.type='button';check.disabled=busy||!!data.checking||!item.enabled;check.setAttribute('aria-label',t('检查此项')+' · '+item.name);
   check.onclick=()=>runCheck(item,'read',badge);
   actions.append(check);
   if(item.writableConfigured&&(item.kind==='workspace'||item.kind==='source')&&!(item.kind==='source'&&item.detail.startsWith('mcp'))){
    const result=node('p','capability-result capability-write-result',t(item.writeCheck?.message||'尚未实测写入'));
    const writeState=node('span','capability-state '+(item.writeCheck?.state||'not_run'),t(item.writeCheck?.state==='not_run'?'尚未实测写入':states[item.writeCheck?.state]||'尚未实测写入'));
    if(item.writeCheck?.at)row.append(node('small','muted',new Date(item.writeCheck.at).toLocaleString()+' · '+item.writeCheck.durationMs+' ms'));
    row.append(result);
    const write=node('button','quiet',t('实测写权限'));write.type='button';write.disabled=busy||!!data.checking||!item.enabled;
    write.setAttribute('aria-label',t('实测写权限')+' · '+item.name);
    write.onclick=()=>runCheck(item,'write',badge);
    actions.append(writeState,write);
   }
   row.append(actions);list.append(row);
  }}
  async function runCheck(item,mode,badge){
   if(!valid()||busy)return;
   if(mode==='write'&&!confirm(t('写权限实测会在该目录创建一个随机命名的临时文件并立即删除；若清理失败会明确报告。不会覆盖现有文件。继续？')))return;
   if(mode==='read'&&item.kind==='source'&&item.detail.startsWith('mcp')&&!confirm(t('检查将启动已配置的 MCP 程序并发现工具，不调用工具。继续？')))return;
   failure.hidden=true;busy=true;refresh.disabled=true;list.querySelectorAll('button').forEach(b=>b.disabled=true);badge.textContent=t('检查中…');badge.className='capability-state checking';status.textContent=t('检查中…');
   try{await api('/capabilities/check',{method:'POST',body:JSON.stringify({key:item.key,fingerprint:item.fingerprint,workspaceId:data.workspaceId,mode})});}catch(e){if(valid()){failure.textContent=e.message;failure.hidden=false;}}finally{busy=false;if(valid())await load();else wasAvailable=false;}
  }
  refresh.onclick=()=>{if(!busy&&!blocked())load();};
  // Initial request is read-only; no network probes start on opening settings.
  queueMicrotask(()=>{if(valid())load();});
  // Keep a cheap lifecycle timer while this panel is mounted. Locking pauses
  // reads, rather than destroying the only route back to a fresh unlocked UI.
  const poll=()=>setTimeout(()=>{
   if(!wrap.isConnected||!active())return;
   const available=valid()&&!document.hidden;
   if(available&&!busy&&(!wasAvailable||Date.now()-lastRead>=30000))load();
   wasAvailable=available;poll();
  },1000);poll();return wrap;
 }};
})();
