/* Read-only provider balances; no inference calls or credential storage in the browser. */
(function(root){
  'use strict';
  function amounts(response) {
    return (response?.balance_infos || []).flatMap(info => {
      const raw=info.total_balance;
      const value=typeof raw==='number'?raw:typeof raw==='string'&&raw.trim()!==''?Number(raw):NaN;
      return Number.isFinite(value)&&['CNY','USD'].includes(info.currency)?[{currency:info.currency,value}]:[];
    });
  }
  function gauge(value, threshold, ceiling) {
    const limit=Math.max(1,ceiling,threshold,value);
    return {level:Math.max(0,Math.min(100,value/limit*100)),line:Math.max(0,Math.min(100,threshold/limit*100)),low:value<=threshold,empty:value<=0};
  }
  function preference(value,currency) {
    const valid=n=>typeof n==='number'&&Number.isFinite(n)&&n>=0&&n<=1e9;
    return {threshold:valid(value?.threshold)?value.threshold:currency==='USD'?2:10,ceiling:valid(value?.ceiling)&&value.ceiling>0?value.ceiling:currency==='USD'?20:100};
  }
  function create({api,t,config,scope=()=>'',active=()=>true,storage}) {
    if(!storage){try{storage=root.localStorage;}catch{storage={getItem:()=>null,setItem:()=>{throw new Error('storage unavailable');}};}}
    const doc=root.document, initialScope=scope();
    const node=(tag,cls,text)=>{const n=doc.createElement(tag);n.className=cls;if(text!==undefined)n.textContent=text;return n;};
    const button=(text,fn)=>{const b=node('button','balance-pool-action',text);b.type='button';b.onclick=fn;return b;};
    const pool=node('section','account-balance-pool');pool.setAttribute('aria-label',t('账户余额'));
    const header=node('div','balance-pool-header'), copy=node('div','');
    copy.append(node('h4','',t('账户余额')),node('p','balance-pool-caption',t('API Key 账户池 · 每分钟自动刷新')));
    const summary=node('div','balance-pool-summary');summary.setAttribute('role','status');summary.setAttribute('aria-live','polite');
    const refresh=button(t('刷新余额'),()=>run());header.append(copy,refresh);
    const cards=node('div','balance-pool-grid');
    pool.append(header,summary,cards,node('p','balance-pool-caption',t('余额由服务商返回，可能为同账户共享额度；不同 Key 不重复合计，不同币种分开显示。报警线仅提醒，不会自动充值或停止任务。')));
    const accounts=[];
    for(const provider of config.modelProviders||[]) for(const key of provider.keys||[]) {
      const card=node('article','balance-account-card');
      const top=node('div','balance-account-head'), title=node('div','');
      title.append(node('strong','',provider.name||provider.id),node('span','balance-account-key',key.label||key.id));
      const badge=node('span','balance-account-badge',t('等待刷新'));top.append(title,badge);
      const body=node('div','balance-account-body'), footer=node('div','balance-account-updated',t('尚未更新'));
      const credentials=node('details','balance-credentials');credentials.hidden=true;
      credentials.append(node('summary','',t('配置 RAM 费用凭证')));
      credentials.append(node('p','balance-pool-caption',t('仅绑定此 API Key。使用可查询账户余额的 RAM AccessKey，与模型 API Key 分开保存。')));
      const help=node('p','balance-pool-caption');
      for(const [label,url] of [['RAM 控制台','https://ram.console.aliyun.com/'],['创建 AccessKey 说明','https://help.aliyun.com/zh/ram/user-guide/create-an-accesskey-pair']]) {
        const link=node('a','',t(label)+' ↗');link.href=url;link.target='_blank';link.rel='noopener noreferrer';link.style.marginRight='12px';help.append(link);
      }
      credentials.append(help);
      const form=node('div','balance-credentials-form'), ak=node('input',''),sk=node('input','');
      for(const [input,label] of [[ak,'AccessKey ID'],[sk,'AccessKey Secret']]) {input.type='password';input.placeholder=label;input.setAttribute('aria-label',(provider.name||provider.id)+' / '+(key.label||key.id)+' / '+label);input.autocomplete='new-password';input.spellcheck=false;}
      const error=node('p','balance-pool-error','');error.setAttribute('role','status');
      const account={provider,key,card,badge,body,footer,credentials,error,last:null,updated:0,stale:false,gauges:[],preferences:{}};accounts.push(account);
      const write=async clear=>{
        if(!live())return;
        if(!clear&&(!ak.value.trim()||!sk.value.trim())) {error.textContent=t('请填写完整费用凭证');return;}
        const payload={providerId:provider.id,keyId:key.id,...(clear?{clear:true}:{accessKeyId:ak.value.trim(),accessKeySecret:sk.value.trim()})};
        ak.value='';sk.value='';save.disabled=remove.disabled=true;error.textContent='';
        try {await api('/provider-billing-credentials',{method:'PUT',body:JSON.stringify(payload),signal:controller.signal});if(live())await load(account);}
        catch(e){if(live())error.textContent=t('费用凭证保存失败，请检查权限或解锁状态');}
        finally{save.disabled=remove.disabled=false;}
      };
      const save=button(t('保存费用凭证'),()=>write(false)),remove=button(t('清除费用凭证'),()=>write(true));
      form.append(ak,sk,save,remove);credentials.append(form);card.append(top,body,footer,credentials,error);cards.append(card);
    }
    if(!accounts.length) cards.append(node('p','balance-pool-caption',t('尚无 API Key 账户，请先在模型设置中添加。')));
    const controller=new AbortController();let disposed=false,busy=false,timer=null;
    const live=()=>!disposed&&pool.isConnected&&scope()===initialScope;
    const available=()=>live()&&active()&&doc.visibilityState!=='hidden';
    const prefKey=(a,c)=>'aide.balance-alarm.v1:'+JSON.stringify([initialScope,a.provider.id,a.key.id,c]);
    const readPref=(a,c)=>{if(a.preferences[c])return a.preferences[c];try{return preference(JSON.parse(storage.getItem(prefKey(a,c))),c);}catch{return preference(null,c);}};
    const report=()=>{
      if(!live())return;
      const low=accounts.filter(a=>!a.stale&&a.gauges.some(g=>g.low)).length;
      const ready=accounts.filter(a=>!a.stale&&a.last).length;
      summary.textContent=t('{0} 个 Key 账户 · {1} 个已连接 · {2} 个低余额',accounts.length,ready,low);
      summary.dataset.warning=String(low>0);
    };
    function render(account) {
      account.body.replaceChildren();account.gauges=[];
      for(const amount of amounts(account.last)) {
        const prefs=readPref(account,amount.currency), g=gauge(amount.value,prefs.threshold,prefs.ceiling);account.gauges.push(g);
        const row=node('div','balance-water-row'),tank=node('div','balance-water-tank');tank.setAttribute('aria-hidden','true');
        const water=node('div','balance-water');water.style.height=g.level+'%';water.hidden=g.level===0;
        const alarm=node('div','balance-alarm-line');alarm.style.bottom=g.line+'%';alarm.title=t('报警线 {0}',prefs.threshold.toFixed(2)+' '+amount.currency);
        tank.append(water,alarm);const detail=node('div','balance-water-detail');
        detail.append(node('div','balance-amount',amount.value.toFixed(2)),node('span','balance-currency',amount.currency+' · '+t('账户可用额度')));
        const controls=node('div','balance-alarm-controls');
        for(const [field,label] of [['threshold',t('报警线')],['ceiling',t('水位刻度')]]) {
          const lab=node('label','',label),input=node('input','');input.type='number';input.min=field==='ceiling'?'0.01':'0';input.max='1000000000';input.step='0.01';input.value=String(prefs[field]);input.setAttribute('aria-label',(account.provider.name||account.provider.id)+' / '+(account.key.label||account.key.id)+' / '+amount.currency+' / '+label);
          lab.append(input);controls.append(lab);
        }
        const apply=button(t('应用'),()=>{
          const values=Array.from(controls.querySelectorAll('input'),input=>input.value.trim()===''?NaN:Number(input.value));
          if(values.length!==2||values.some(n=>!Number.isFinite(n)||n<0||n>1e9)||values[1]===0){account.error.textContent=t('报警线和刻度必须是有效数字');return;}
          prefs.threshold=values[0];prefs.ceiling=values[1];account.preferences[amount.currency]={...prefs};
          try{storage.setItem(prefKey(account,amount.currency),JSON.stringify(prefs));account.error.textContent='';}catch{account.error.textContent=t('无法保存报警线，当前设置将在关闭后失效');}
          render(account);report();
        });
        apply.setAttribute('aria-label',(account.provider.name||account.provider.id)+' / '+(account.key.label||account.key.id)+' / '+amount.currency+' / '+t('应用报警设置'));
        controls.append(apply);
        row.append(tank,detail);account.body.append(row,controls);
      }
      const critical=account.gauges.some(g=>g.empty),low=account.gauges.some(g=>g.low);
      account.card.dataset.state=account.stale?'stale':critical?'empty':low?'low':'ready';
      account.badge.textContent=account.stale?t('数据已过期'):critical?t('余额耗尽'):low?t('低于报警线'):t('余额正常');
    }
    async function load(account) {
      if(!available())return;
      account.badge.textContent=t('正在更新');account.error.textContent='';
      const query=new URLSearchParams({providerId:account.provider.id,keyId:account.key.id});
      try {
        const result=await api('/balance?'+query,{signal:controller.signal});if(!available())return;
        // A response for another key must never be displayed in this card.
        if(result.supported!==false&&(result.providerId!==account.provider.id||result.keyId!==account.key.id))throw new Error('identity mismatch');
        account.credentials.hidden=!result.billingCredentialsRequired&&result.balanceAuth!=='aliyun-rpc';
        account.updated=Date.now();account.stale=false;
        if(result.supported===false||result.billingCredentialsRequired||!amounts(result).length) {
          account.last=null;account.gauges=[];account.card.dataset.state='pending';account.badge.textContent=result.supported===false?t('无支持插件'):result.billingCredentialsRequired?t('待配置 RAM'):t('余额不可查');
          account.body.replaceChildren(node('p','balance-pool-caption',result.billingCredentialsRequired?t('展开下方配置，绑定此 Key 的 RAM 费用凭证。'):t('此账户暂时无法返回余额，不计作零余额。')));
        } else {account.last=result;render(account);}
        account.footer.textContent=t('更新于 {0}',new Date(account.updated).toLocaleTimeString());
      }catch(e){if(!live()||controller.signal.aborted)return;account.stale=true;if(account.last)render(account);else{account.card.dataset.state='stale';account.badge.textContent=t('更新失败');account.body.replaceChildren(node('p','balance-pool-caption',t('查询失败，稍后重试；未将未知余额记为零。')));}account.footer.textContent=account.updated?t('上次更新 {0}',new Date(account.updated).toLocaleTimeString()):t('尚未更新');}
      report();
    }
    async function run(){
      if(busy||!available())return;busy=true;refresh.disabled=true;pool.classList.add('is-refreshing');
      let cursor=0;
      const worker=async()=>{while(cursor<accounts.length&&available()){const a=accounts[cursor++];await load(a);}};
      try{await Promise.all([worker(),worker()]);}finally{busy=false;refresh.disabled=false;pool.classList.remove('is-refreshing');report();}
    }
    function tick(){if(scope()!==initialScope){dispose();return;}if(!live()){if(pool.isConnected===false&&everConnected){dispose();return;}}else{everConnected=true;if(available())run();}if(!disposed)timer=setTimeout(tick,60000);}
    let everConnected=false;
    function dispose(){if(disposed)return;disposed=true;controller.abort();clearTimeout(timer);observer.disconnect();visibilityObserver?.disconnect();for(const a of accounts)for(const input of a.credentials.querySelectorAll('input'))input.value='';}
    const observer=new MutationObserver(()=>{if(scope()!==initialScope){dispose();return;}if(pool.isConnected)everConnected=true;else if(everConnected)dispose();});observer.observe(doc.documentElement,{childList:true,subtree:true});
    const dialog=doc.getElementById?.('settings-sheet');
    const clearSecrets=()=>{for(const a of accounts)for(const input of a.credentials.querySelectorAll('input'))input.value='';};
    const visibilityObserver=dialog?new MutationObserver(()=>{if(!dialog.classList.contains('open'))clearSecrets();}):null;
    if(dialog)visibilityObserver.observe(dialog,{attributes:true,attributeFilter:['class']});
    timer=setTimeout(tick,300);return pool;
  }
  const api={amounts,gauge,preference,create};
  if(typeof module==='object'&&module.exports)module.exports=api;else root.AideAccountBalancePool=api;
})(typeof window==='object'?window:globalThis);
