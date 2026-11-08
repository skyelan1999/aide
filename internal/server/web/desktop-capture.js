'use strict';
(function(){
 let client=null,ticket=null;
 const native=()=>window.AideNativeCapture;
 const request=async body=>{if(!native())throw Error('native-unavailable');const value=await native()(body);if(value.error)throw Error(value.error);return value;};
 function prepare(){
  ticket=null;if(!client?.available())return null;
  ticket={id:crypto.randomUUID(),scope:client.scope(),at:Date.now()};return ticket.id;
 }
 async function receive(payload){
  const saved=ticket;ticket=null;
  if(!saved||payload?.ticket!==saved.id||Date.now()-saved.at>30000||!client.available()||client.scope()!==saved.scope)return false;
  const active=()=>client.available()&&client.scope()===saved.scope;
  try{
   if(typeof payload.imageBase64!=='string'||payload.imageBase64.length>24*1024*1024)throw Error(client.t('截图数据无效'));
   const bytes=Uint8Array.from(atob(payload.imageBase64),c=>c.charCodeAt(0));
   if(bytes.length<8||[137,80,78,71,13,10,26,10].some((v,i)=>bytes[i]!==v))throw Error(client.t('截图数据无效'));
   const safe=value=>String(value||'').replace(/[\u0000-\u001f]/g,' ').slice(0,500);
   const app=safe(payload.app)||'App',stamp=new Date().toISOString().replace(/[:.]/g,'-'),name=app.replace(/[\\/:*?"<>|]/g,'_').slice(0,60);
   const context={type:'aide-app-capture',app,bundleId:safe(payload.bundleId),windowTitle:safe(payload.windowTitle),capturedAt:safe(payload.capturedAt),pixelWidth:payload.pixelWidth,pixelHeight:payload.pixelHeight};
   const files=[new File([bytes],`${name}-${stamp}.png`,{type:'image/png'}),new File([JSON.stringify(context,null,2)],`${name}-${stamp}-context.txt`,{type:'text/plain'})];
   const result=await client.attach(files,{active});
   if(active()){client.focus();client.notify(result?.added===2?client.t('已添加应用截图和上下文，输入问题后发送。'):client.t('部分截图附件未添加，请检查后重试。'));}
   return active()&&result?.added===2;
  }catch(error){client.notify(error.message);return false;}
 }
 function render({t,onCleanup}){
  const create=(tag,text)=>{const n=document.createElement(tag);if(text)n.textContent=text;return n;};
  const wrap=create('div');wrap.className='desktop-capture-settings';
  const title=create('h4',t('全局截屏到 Aide')),description=create('p',t('双击修饰键截取前台应用窗口，附带应用名、窗口标题和时间，再唤起 Aide 加入待发送附件。不会自动发送。'));description.className='section-desc';
  wrap.append(title,description);
  if(!native()){wrap.append(create('p',t('此功能需要 macOS 原生 Aide 桌面端。')));return wrap;}
  const label=create('label'),enabled=create('input');enabled.type='checkbox';enabled.setAttribute('aria-label',t('启用全局截屏'));label.append(enabled,document.createTextNode(' '+t('启用全局截屏')));
  const comboLabel=create('label',t('触发方式')),combo=create('select');combo.setAttribute('aria-label',t('截屏触发方式'));
  for(const [value,text] of [['shift','双击 Shift'],['command','双击 Command']]){const option=create('option',t(text));option.value=value;combo.append(option);}comboLabel.append(combo);
  const permission=create('button',t('授权全局按键与屏幕录制')),refresh=create('button',t('刷新权限状态'));for(const b of [permission,refresh]){b.type='button';b.className='quiet';}
  const status=create('p');status.setAttribute('role','status');status.setAttribute('aria-live','polite');
  const controls=[enabled,combo,permission,refresh];let disposed=false,current=null,busy=false;
  const show=value=>{current=value;enabled.checked=value.enabled;combo.value=value.modifier;status.textContent=value.accessibility&&value.screenRecording?t('全局截屏权限已就绪'):t('需要在系统隐私设置中允许 Aide 辅助功能和屏幕录制。');
   if(value.enabled){const result=value.lastResult==='Attached'?t('截图已加入草稿'):value.lastResult==='Capturing'?t('正在截屏…'):value.lastResult||(!value.listening?t('按键监听未启动，请关闭后重新开启。'):value.modifierEvents?t('已检测到按键，等待快速双击修饰键。'):t('尚未检测到按键，请在其他应用中双击修饰键。'));status.textContent+=' · '+result;}
  };
  const run=async body=>{if(busy)return;busy=true;controls.forEach(n=>n.disabled=true);try{const value=await request(body);if(!disposed)show(value);}catch(error){if(!disposed){if(current)show(current);status.textContent=error.message;}}finally{busy=false;if(!disposed)controls.forEach(n=>n.disabled=false);}};
  const save=()=>run({action:'configure',enabled:enabled.checked,modifier:combo.value});enabled.onchange=save;combo.onchange=save;
  permission.onclick=()=>run({action:'permissions'});refresh.onclick=()=>run({action:'status'});
  wrap.append(label,comboLabel,permission,refresh,status);onCleanup?.(()=>{disposed=true;});void run({action:'status'});return wrap;
 }
 window.AideDesktopCapture={install:options=>{client=options;},prepare,receive,error:message=>{ticket=null;client?.notify(message);},render};
})();
