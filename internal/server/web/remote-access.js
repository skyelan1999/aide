'use strict';
// Account setting and user-activity lease. No credential is placed in a QR URL.
window.AideLANAccess = (() => {
 let lastActivity=0,checking=false;
 setInterval(()=>{if(!checking&&typeof state!=='undefined'&&state.config?.remoteSession&&state.token){checking=true;api('/remote-access/session').catch(()=>{}).finally(()=>{checking=false;});}},2000);
 function activity(){
  if(!state.config?.remoteSession||!state.token||lockScreen.locked)return;
  const gap=Math.max(100,Math.min(15000,(state.config.lockTimeoutSec||300)*1000/3));
  if(Date.now()-lastActivity<gap)return;lastActivity=Date.now();
  api('/remote-access/activity',{method:'POST',body:'{}'}).catch(()=>{});
 }
 function render(cfg,onUpdate){
  const box=el('section','lan-access');box.append(el('h3','',t('局域网访问授权')));
  if(cfg.remoteSession){box.append(el('p','muted',t('请在本机管理访问授权；当前设备使用密码会话登录。')));const logout=el('button','quiet',t('退出当前设备'));logout.type='button';logout.onclick=action(async()=>{await api('/remote-access/logout',{method:'POST',body:'{}'});localStorage.removeItem('aide-token');location.replace('/lan-login.html');});box.append(logout);return box;}
  const row=el('label','checkbox'),toggle=el('input');toggle.type='checkbox';toggle.checked=!!cfg.remoteAccessEnabled;toggle.setAttribute('aria-label',t('允许局域网设备访问'));toggle.disabled=!cfg.hasPassword;row.append(toggle,document.createTextNode(t('允许局域网设备访问')));box.append(row);
  box.append(el('p','muted',t('默认关闭。开启后，手机扫码或其他设备打开 HTTPS 地址均须输入账户密码。锁屏时间不能为 0；原为 0 时自动设为 5 分钟。')));
  if(!cfg.hasPassword)box.append(el('p','muted lan-access-prerequisite',t('请先在上方设置账户密码。')));
  const details=el('div','lan-access-details');box.append(details);
  let timer=null;
  async function refresh(){if(!box.isConnected)return;try{const data=await api('/remote-access');details.replaceChildren();toggle.checked=!!data.enabled;if(!data.enabled)return;
   if(data.problem||!data.ready)details.append(el('p','muted',data.problem||t('正在准备局域网加密入口…')));
   if(data.ready){details.append(el('p','muted',t('请在同一局域网访问。首次使用需在访问设备上信任本机证书，并核对下方 SHA-256 指纹。')));
    for(const entry of data.entries){const item=el('div','lan-access-entry'),a=el('a','',entry.url);a.href=entry.url;a.target='_blank';a.rel='noopener noreferrer';const qr=el('img','lan-access-qr');qr.src=entry.qr;qr.alt=t('手机扫码登录');qr.width=192;qr.height=192;item.append(a,qr);details.append(item);}
    const fingerprint=el('code','lan-access-fingerprint',data.fingerprint);details.append(fingerprint);
   }
   clearTimeout(timer);timer=setTimeout(refresh,data.ready?10000:2000);
  }catch(e){details.replaceChildren(el('p','error',e.message));}}
  toggle.onchange=async()=>{const enabled=toggle.checked;toggle.disabled=true;try{let password='';if(enabled){password=await passwordPrompt(t('开启局域网访问请输入账户密码'));if(!password){toggle.checked=false;return;}}
   await api('/remote-access',{method:'PUT',body:JSON.stringify({enabled,password})});await refreshConfig();onUpdate?.();resetIdleTimer();await refresh();
  }catch(e){toggle.checked=!!state.config?.remoteAccessEnabled;toast(e.message);}finally{toggle.disabled=!state.config?.hasPassword;}};
  setTimeout(refresh,0);return box;
 }
 function update(wrap,cfg){const box=wrap.querySelector('.lan-access'),input=box?.querySelector('input[type=checkbox]');if(input){input.disabled=!cfg.hasPassword;input.checked=!!cfg.remoteAccessEnabled;}const hint=box?.querySelector('.lan-access-prerequisite');if(hint)hint.hidden=!!cfg.hasPassword;}
 return {render,activity,update};
})();
