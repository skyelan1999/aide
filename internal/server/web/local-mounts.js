/* Host directory configuration. This module never receives Docker credentials. */
(() => {
 'use strict';
 function render({api,t,el,blocked}) {
  const box=el('section','local-mount-settings'),intro=el('p','muted',t('添加 Windows 不同盘符或 macOS 外置硬盘中的目录。每个目录独立设置权限，默认只读。工作区与资料可使用这些目录，缓存保留在 Aide 数据区。'));
  const notice=el('p','local-mount-notice'),list=el('div','local-mount-list'),actions=el('div','local-mount-actions');notice.setAttribute('role','status');
  const button=(text,fn,cls='quiet')=>{const b=el('button',cls,t(text));b.type='button';b.onclick=fn;return b;};
  let snapshot=null,entries=[],busy=false,serial=0;
  const live=()=>box.isConnected&&!blocked();
  const refresh=button('刷新',()=>run(load)),add=button('添加目录',()=>{entries.push({id:'dir-'+Date.now().toString(36)+'-'+(++serial),name:t('本地目录'),path:'',readOnly:true});paint();});
  const save=button('保存配置',()=>run(async()=>{const data=await api('/local-mounts',{method:'PUT',body:JSON.stringify({revision:snapshot.revision,config:{version:1,entries}})});if(!live())return;snapshot=data;entries=data.config.entries;paint();notice.textContent=t('配置已保存，应用并重启后生效。');}),'primary');
  const apply=button('应用并重启',()=>run(async()=>{if(!snapshot||!window.confirm(t('请先保存当前工作。应用目录配置会重新创建 Aide 容器，中断正在运行的会话和手机连接；还需在本机窗口确认。')))return;await request('apply');}));
  actions.append(add,refresh,save,apply);box.append(intro,notice,list,actions);
  async function run(fn){if(busy||blocked())return;busy=true;toggle();try{await fn();}catch(e){if(live())notice.textContent=e.message;}finally{busy=false;if(live())toggle();}}
  const unsaved=()=>snapshot&&JSON.stringify(entries)!==JSON.stringify(snapshot.config.entries);
  function changed(){toggle();notice.textContent=t('有未保存的目录配置，请先保存。');}
  function toggle(){for(const b of box.querySelectorAll('button,input,select'))b.disabled=busy||(b.dataset.native==='true'&&!snapshot?.hostReady);save.disabled=busy||!snapshot;add.disabled=busy||entries.length>=16;apply.disabled=busy||!snapshot?.hostReady||!snapshot?.pending||unsaved();}
  async function load(){const data=await api('/local-mounts');if(!live())return;snapshot=data;entries=data.config.entries.map(e=>({...e}));paint();}
  async function request(action,row){
   const req=await api('/local-mounts/action',{method:'POST',body:JSON.stringify({action,revision:snapshot.revision})});
   notice.textContent=t(action==='choose'?'请在本机窗口选择目录…':'正在应用目录配置，请等待重新连接…');
   const until=Date.now()+190000;
   while(live()&&Date.now()<until){
    await new Promise(r=>setTimeout(r,1000));if(!live())return;
    let d;try{d=await api('/local-mounts');}catch(e){if(action==='apply')continue;throw e;}
    const reply=d.response;if(reply?.id!==req.id||reply.state==='running')continue;
    if(reply.error)throw Error(reply.error);
    if(row&&entries.includes(row)){row.path=reply.path;paint();notice.textContent=t('目录已选择，请保存配置。');}
    else await load();return;
   }
   if(live())throw Error(t('等待本机操作超时，请刷新核对状态。'));
  }
  function paint(){
   list.replaceChildren();
   notice.textContent=t(!snapshot?.hostReady?'本机目录服务未连接；配置可保存，下次使用新版启动器启动时应用。':snapshot.pending?'配置与实际挂载不同，保存后请应用并重启。':'当前目录配置已生效。');
   if(!entries.length)list.append(el('p','muted',t('尚未添加额外目录。原有工作区和引用不受影响。')));
   for(const row of entries){
    const card=el('article','local-mount-card'),name=el('input'),path=el('input'),access=el('select'),top=el('div','local-mount-row'),location=el('div','local-mount-row');
    name.value=row.name;name.maxLength=120;name.setAttribute('aria-label',t('目录名称'));name.oninput=()=>{row.name=name.value;changed();};
    path.value=row.path;path.placeholder='E:/Projects / /Volumes/Archive/Projects';path.setAttribute('aria-label',t('宿主机目录'));path.oninput=()=>{row.path=path.value;changed();};
    for(const [value,label] of [['ro','只读'],['rw','读写']]){const o=el('option','',t(label));o.value=value;access.append(o);}access.value=row.readOnly?'ro':'rw';access.setAttribute('aria-label',t('目录权限'));access.onchange=()=>{row.readOnly=access.value==='ro';changed();};
    const remove=button('移除',()=>{entries=entries.filter(e=>e!==row);paint();}),pick=button('选择目录…',()=>run(()=>request('choose',row)));pick.dataset.native='true';pick.disabled=!snapshot?.hostReady;
    top.append(name,access,remove);location.append(path,pick);card.append(top,location);
    const active=snapshot?.active.entries.find(e=>e.id===row.id),status=el('small','muted',active?.error?t('目录不可用：')+active.error:active?t('已挂载 · ')+t(active.readOnly?'只读':'读写'):t('待应用'));card.append(status);list.append(card);
   }
   for(const active of snapshot?.active.entries||[])if(!entries.some(e=>e.id===active.id))list.append(el('p','muted',t('待卸载：')+active.name+' · '+active.path));
   toggle();
  }
  // Render is called before the returned element enters the document.
  queueMicrotask(()=>run(load));return box;
 }
 window.AideLocalMounts={render};
})();
