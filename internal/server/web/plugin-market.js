/* Official catalog data is untrusted text. Mutations use existing server install transactions. */
(() => {
 'use strict';
 const names={all:'全部',creation:'创作与专业',workspace:'工作空间',communication:'连接与通信',planning:'规划与协作',utilities:'日常工具'};
 const paths={creation:'M12 3v18M3 12h18M5 5l14 14M19 5L5 19',workspace:'M3 7h7l2 2h9v11H3ZM3 7V4h7l2 3',communication:'M8 3v5m8-5v5M6 8h12v3a6 6 0 0 1-12 0ZM12 17v4',planning:'M8 5h13M8 12h13M8 19h13M3 5h1M3 12h1M3 19h1',utilities:'M12 3v3m0 12v3M3 12h3m12 0h3M8 8l8 8m0-8-8 8'};
 function icon(category,node){const box=node('span','market-icon');box.innerHTML='<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="'+(paths[category]||paths.creation)+'"/></svg>';return box;}
 function stateOf(p){if(p.distribution==='private')return p.commerce?.status==='available'?'授权安装':'付费筹备中';return !p.compatible?'不兼容':p.newerInstalled?'已安装较新版本':p.updateAvailable?'更新':p.installedVersion?'已安装':'安装';}
 function mount(services){
  const {api,raw,t,el:node,refreshInstalled,blocked}=services,host=document.getElementById('plugin-market'),panel=document.getElementById('plugin-panel');
  if(!host||host.dataset.mounted)return;host.dataset.mounted='true';
  const controls=node('div','market-controls'),search=node('input'),select=node('select'),status=node('p','market-status'),syncStatus=node('small','market-status'),list=node('div','market-list'),more=node('button','quiet',t('显示更多'));
  search.type='search';search.placeholder=t('搜索官方插件');search.setAttribute('aria-label',t('搜索官方插件'));select.setAttribute('aria-label',t('插件分类'));
  for(const[id,label]of Object.entries(names)){const o=node('option','',t(label));o.value=id;select.append(o);}
  const routing=node('section','market-routing'),routingTitle=node('strong','',t('插件路由')),routingLabel=node('label','market-routing-toggle'),autoInstall=node('input'),routingStatus=node('p','market-status'),policyStatus=node('p','market-status'),routeButton=node('button','quiet',t('按需求匹配'));
  autoInstall.type='checkbox';autoInstall.disabled=true;autoInstall.setAttribute('aria-label',t('允许 Aide 自动安装所需插件'));routingLabel.append(autoInstall,node('span','',t('允许 Aide 自动安装所需插件')));policyStatus.setAttribute('role','status');routing.append(routingTitle,routingLabel,node('p','muted',t('路由索引默认更新；仅加载匹配工具。此开关对当前 Aide 实例生效。仅自动安装官方公开兼容插件，不覆盖已安装或停用插件，执行权限保持原有规则。')),policyStatus);routeButton.type='button';
  let routingPolicy=null,routingLoading=false,routedIds=null;
  async function initRouting(){if(blocked()||routingLoading)return;routingLoading=true;try{const data=await api('/plugin-routing');if(blocked())return;routingPolicy=data;autoInstall.checked=data.autoInstall;autoInstall.disabled=false;policyStatus.textContent=t('索引按需本地匹配，不向模型发送完整目录');}catch(e){policyStatus.textContent=e.message;}finally{routingLoading=false;}}
  autoInstall.onchange=async()=>{if(!routingPolicy||blocked())return;const previous=routingPolicy.autoInstall;autoInstall.disabled=true;try{const data=await api('/plugin-routing',{method:'PUT',body:JSON.stringify({autoInstall:autoInstall.checked,generation:routingPolicy.generation})});if(blocked())return;routingPolicy=data;autoInstall.checked=data.autoInstall;policyStatus.textContent=t(data.autoInstall?'已允许 Aide 自动安装匹配的公开插件':'已关闭自动安装');}catch(e){autoInstall.checked=previous;const message=e.message;await initRouting();policyStatus.textContent=message;}finally{autoInstall.disabled=blocked();}};
  routeButton.onclick=async()=>{if(blocked()||!search.value.trim())return;routeButton.disabled=true;const query=search.value.trim();try{const result=await api('/plugin-routing/search?query='+encodeURIComponent(query));if(blocked()||search.value.trim()!==query)return;if(!snapshot)await load(false);routedIds=new Set(result.matches.map(m=>m.id));paint();routingStatus.textContent=result.matches.length?result.matches.map(m=>m.name+' · '+m.reasons.join(' / ')+' · '+t(m.installed?(m.enabled?'已启用':'已停用'):result.autoInstall&&m.autoInstallable?'可由 Aide 自动安装':'尚未安装')).join('；'):t('没有匹配的插件');}catch(e){routingStatus.textContent=e.message;}finally{routeButton.disabled=false;}};
  window.AidePluginMarket.initRouting=initRouting;
  window.AidePluginMarket.renderSettings=()=>{void initRouting();return routing;};
  more.hidden=true;status.setAttribute('role','status');status.setAttribute('aria-live','polite');controls.append(search,select,routeButton);host.append(controls,routingStatus,status,syncStatus,list,more);
  let snapshot=null,category='all',limit=24,busy=false,loading=false,epoch=0,dialog=null;
  const installedTab=document.getElementById('plugin-mode-installed'),marketTab=document.getElementById('plugin-mode-market');
  function tab(market){panel.dataset.marketView=market?'market':'installed';host.hidden=!market;marketTab.setAttribute('aria-selected',String(market));installedTab.setAttribute('aria-selected',String(!market));if(market){void initRouting();if(snapshot)paint();void load(false);}else{document.getElementById('plugin-count').textContent='';services.paintInstalled();}}
  installedTab.onclick=()=>tab(false);marketTab.onclick=()=>tab(true);
  function paint(){if(!snapshot)return;list.replaceChildren();const q=search.value.trim().toLowerCase(),items=snapshot.plugins.filter(p=>(category==='all'||category===p.category)&&(routedIds?routedIds.has(p.id):(!q||(p.name+' '+p.id+' '+p.description+' '+(p.routing?.keywords||[]).join(' ')).toLowerCase().includes(q))));
   if(panel.dataset.marketView==='market')document.getElementById('plugin-count').textContent=items.length+' / '+snapshot.plugins.length;
   if(!items.length)list.append(node('p','muted',t('没有匹配的插件')));
   for(const p of items.slice(0,limit)){const card=node('article','market-card'),head=node('div','market-card-head'),title=node('div','market-card-title');title.append(node('strong','',p.name),node('small','',t('Aide 官方')+' · v'+p.version));head.append(icon(p.category,node),title);const foot=node('div','market-card-foot'),open=node('button','quiet',t('查看详情'));open.type='button';open.onclick=()=>details(p);foot.append(node('small','',t(stateOf(p))),open);card.append(head,node('p','market-description',p.description),foot);list.append(card);}
   more.hidden=items.length<=limit;
  }
  search.oninput=()=>{routedIds=null;limit=24;paint();};select.onchange=()=>{category=select.value;limit=24;paint();};more.onclick=()=>{limit+=24;paint();};
  function showSync(data){
   const stamp=Number(data.fetched);syncStatus.textContent=Number.isFinite(stamp)&&stamp>0?t('上次成功同步：')+new Date(stamp*1000).toLocaleString():t('尚未成功同步，当前显示随 Aide 提供的离线目录。');
   syncStatus.title=t('市场目录独立更新，无需升级 Aide。');
   status.textContent=data.refreshError?t('刷新失败，保留可用目录：')+data.refreshError:data.stale?t('当前使用缓存目录；正在检查更新。'):t('官方目录 · 下载前核对权限与兼容性');
  }
  async function load(force){if(loading||blocked())return;loading=true;let refreshNext=false;const captured=++epoch;if(!snapshot||force)status.textContent=t('正在读取官方目录…');try{const data=await api('/plugin-market'+(force?'?refresh=1':''));if(captured!==epoch||blocked())return;snapshot=data;refreshNext=data.stale&&!force&&!(Number(data.retryAfter)>0);paint();showSync(data);}catch(e){if(captured!==epoch||blocked())return;status.textContent=snapshot?t('刷新失败，保留可用目录：')+(e.message||t('目录加载失败，请刷新重试')):(e.message||t('目录加载失败，请刷新重试'));}finally{loading=false;if(refreshNext&&!blocked())void load(true);}}
  // Reopening and visible-page checks ask the server about freshness. Its bounded
  // cache avoids a GitHub request per tab click; offline failures back off.
  const checkVisible=()=>{if(panel.getClientRects&&!panel.getClientRects().length)return;if(panel.dataset.marketView==='market'&&!document.hidden&&!blocked())void load(false);};
  setInterval(checkVisible,60000);
  document.addEventListener?.('visibilitychange',checkVisible);
  window.addEventListener?.('online',checkVisible);
  function details(p){if(dialog?.open)dialog.close();const focus=document.activeElement,reviewed=snapshot;const modal=node('dialog','market-dialog');dialog=modal;modal.setAttribute('role','dialog');modal.setAttribute('aria-modal','true');modal.setAttribute('aria-labelledby','market-detail-title');const heading=node('div','market-detail-head'),title=node('h2','',p.name),close=node('button','icon-button','×');title.id='market-detail-title';close.type='button';close.setAttribute('aria-label',t('关闭'));close.onclick=()=>modal.close();heading.append(icon(p.category,node),title,close);modal.append(heading,node('p','',p.description));const facts=node('dl','market-facts');
   for(const[k,v]of [[t('版本'),p.version],[t('最低 Aide 版本'),p.minAideVersion],[t('运行依赖'),(p.dependencies||[]).join('；')||t('使用 Aide 内置运行环境')],[t('适用平台'),p.platforms.join(' / ')],[t('许可证'),p.license],[t('权限说明'),p.permissions],[t(p.distribution==='private'?'交付方式':'SHA-256'),p.distribution==='private'?t('私有交付 · 安装包不公开'):p.sha256]])facts.append(node('dt','',k),node('dd','',v));modal.append(facts,node('p','market-trust',p.trustNotice));
   if(p.readme){const d=node('details','market-readme');d.append(node('summary','',t('使用说明')),node('pre','',p.readme));modal.append(d);}
   modal.append(node('p','muted',p.changelog));
   if(!p.compatible)modal.append(node('p','task-error',t(p.incompatibility)));if(p.installedVersion)modal.append(node('p','muted',t('已安装版本')+' '+p.installedVersion+' · '+t('更新保留设置、启用状态及数据；同版本内容变化不自动覆盖。')));
   if(p.distribution==='private'){
    const commerce=p.commerce||{},notice=node('p','market-status'),actions=node('div','dialog-actions');notice.setAttribute('role','status');
    modal.append(node('p','',t(commerce.priceLabel||'价格待公布')),node('p','muted',t('一次购买可永久使用已购版本；大版本升级单独授权。')));
    if(commerce.status==='available'&&commerce.purchaseURL&&/^https:\/\//.test(commerce.purchaseURL)){const link=node('a','primary',t('前往购买'));link.href=commerce.purchaseURL;link.target='_blank';link.rel='noopener noreferrer';actions.append(link);}
    if(commerce.status==='available'){
     const device=node('button','quiet',t('显示设备授权码')),code=node('code','market-device'),bundle=node('input'),license=node('input'),install=node('button','primary',t('核对授权并安装'));
     install.disabled=!p.compatible||!!p.newerInstalled;device.type=install.type='button';bundle.type=license.type='file';bundle.accept='.zip';license.accept='.json';
     const bundleLabel=node('label','',t('私有安装包 ZIP')),licenseLabel=node('label','',t('签名许可证 JSON'));bundleLabel.append(bundle);licenseLabel.append(license);
     device.onclick=async()=>{try{const d=await api('/plugin-license/device',{method:'POST',body:'{}'});if(!blocked()&&modal.open)code.textContent=d.deviceId;}catch(e){notice.textContent=e.message;}};
     install.onclick=async()=>{if(busy||blocked())return;const pack=bundle.files?.[0],permit=license.files?.[0];if(!pack||!permit||pack.size>24*1024*1024||permit.size>16384){notice.textContent=t('请选择有效私有安装包与许可证');return;}
      busy=true;install.disabled=true;notice.textContent=t('正在核对签名、设备与安装包…');
      const controller=new AbortController(),poll=setInterval(()=>{if(blocked())controller.abort();},300);
      try{const signed=JSON.parse(await permit.text());if(blocked())throw Error(t('工作台已锁定'));const form=new FormData();form.append('review',JSON.stringify({revision:reviewed.revision,version:p.version,sha256:p.sha256||'',workspace:reviewed.workspace,workspaceRevision:reviewed.workspaceRevision,license:signed}));form.append('bundle',pack);
       const response=await raw('/plugin-market/'+encodeURIComponent(p.id)+'/install',{method:'POST',body:form,signal:controller.signal});if(!response.ok){const result=await response.json();throw Error(result.error||t('安装失败'));}await refreshInstalled();await load(false);modal.close();
      }catch(e){notice.textContent=e.message;}finally{clearInterval(poll);busy=false;install.disabled=!p.compatible||!!p.newerInstalled;}
     };
     modal.append(bundleLabel,licenseLabel,code);actions.append(device,install);
    }else{notice.textContent=t('尚未开放销售或授权安装，价格与购买入口稍后公布。');}
    modal.append(notice,actions);document.body.append(modal);modal.addEventListener('close',()=>{modal.remove();focus?.focus();},{once:true});modal.showModal();return;
   }
   const actions=node('div','dialog-actions'),download=node('button','quiet',t('下载 ZIP')),install=node('button','primary',t(stateOf(p))),notice=node('p','market-status');notice.setAttribute('role','status');notice.setAttribute('aria-live','polite');
   const source=node('a','market-source',t('查看源码 ↗'));if(/^https:\/\/github\.com\/skyelan1999\//.test(p.source)){source.href=p.source;source.target='_blank';source.rel='noopener noreferrer';}modal.append(source);
   install.disabled=!p.compatible||!!p.installedVersion&&!p.updateAvailable;install.type=download.type='button';
   install.onclick=async()=>{if(busy||blocked())return;busy=true;install.disabled=download.disabled=true;notice.textContent=t('正在下载、校验并安装…');const controller=new AbortController(),poll=setInterval(()=>{if(blocked())controller.abort();},300);try{await api('/plugin-market/'+encodeURIComponent(p.id)+'/install',{method:'POST',signal:controller.signal,body:JSON.stringify({revision:reviewed.revision,version:p.version,sha256:p.sha256,workspace:reviewed.workspace,workspaceRevision:reviewed.workspaceRevision})});notice.textContent=t('安装完成');try{await refreshInstalled();await load(false);if(modal.open)modal.close();}catch(_){notice.textContent=t('插件已安装，列表刷新失败，请手动刷新');}}catch(e){notice.textContent=e.name==='AbortError'?t('请求已中断，请刷新核对安装状态'):e.message;install.disabled=false;}finally{clearInterval(poll);busy=false;download.disabled=false;}};
   download.onclick=async()=>{if(busy||blocked())return;busy=true;download.disabled=true;notice.textContent=t('正在下载并校验…');try{const response=await raw('/plugin-market/'+encodeURIComponent(p.id)+'/download?revision='+encodeURIComponent(reviewed.revision));if(!response.ok){const d=await response.json();throw Error(d.error||t('下载失败'));}const bytes=await response.arrayBuffer();if(bytes.byteLength!==p.bytes)throw Error(t('下载不完整'));const digest=Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',bytes))).map(b=>b.toString(16).padStart(2,'0')).join('');if(digest!==p.sha256)throw Error(t('下载校验失败'));if(blocked())throw Error(t('工作台已锁定'));const url=URL.createObjectURL(new Blob([bytes],{type:'application/zip'})),a=node('a');a.href=url;a.download='aide-pkg-'+p.id+'-v'+p.version+'.zip';document.body.append(a);a.click();a.remove();setTimeout(()=>URL.revokeObjectURL(url),60000);notice.textContent=t('校验通过，已开始下载');}catch(e){notice.textContent=e.message;}finally{busy=false;download.disabled=false;}};
   actions.append(download,install);modal.append(notice,actions);document.body.append(modal);modal.addEventListener('close',()=>{modal.remove();focus?.focus();},{once:true});modal.showModal();
  }
  window.AidePluginMarket.close=()=>dialog?.close();
  window.AidePluginMarket.refresh=()=>panel.dataset.marketView==='market'?load(true):refreshInstalled();
  window.AidePluginMarket.syncInstalled=items=>{if(!snapshot)return;const byID=new Map(items.map(p=>[p.id,p]));for(const p of snapshot.plugins){const local=byID.get(p.id);p.installedVersion=local?.version||'';p.enabled=local?.enabled;p.updateAvailable=!!local&&versionOlder(local.version,p.version);p.newerInstalled=!!local&&versionOlder(p.version,local.version);}paint();};
 }
 function versionOlder(a,b){
  const parse=v=>{const [core,...suffix]=String(v||'').trim().replace(/^v/,'').replace(/ /g,'-').split('-');const numbers=core.split('.').map(Number);if(numbers.length<2||numbers.length>4||numbers.some(n=>!Number.isSafeInteger(n)||n<0))return null;return [numbers,suffix.join('-').toLowerCase().match(/[a-z]+|[0-9]+/g)||[]];};
  const x=parse(a),y=parse(b);if(!x||!y)return false;
  for(let i=0;i<4;i++){if((x[0][i]||0)!==(y[0][i]||0))return(x[0][i]||0)<(y[0][i]||0);}
  const xp=x[1],yp=y[1];if(!xp.length)return false;if(!yp.length)return true;
  for(let i=0;i<Math.min(xp.length,yp.length);i++){if(xp[i]===yp[i])continue;const xn=/^\d+$/.test(xp[i]),yn=/^\d+$/.test(yp[i]);if(xn&&yn)return Number(xp[i])<Number(yp[i]);if(xn!==yn)return xn;return xp[i]<yp[i];}return xp.length<yp.length;
 }

 window.AidePluginMarket={mount,stateOf,versionOlder};
})();
