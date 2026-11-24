'use strict';
// Actual KV Cache settings renderer and shipped CSS with a deterministic API fixture.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),http=require('node:http');
const {chromium}=require(process.env.AIDE_PLAYWRIGHT_MODULE||'./browser-runtime/node_modules/playwright');
const app=fs.readFileSync('internal/server/web/app.js','utf8');
const renderer=app.slice(app.indexOf('function renderKVCacheControl()'),app.indexOf('function renderAboutProject',app.indexOf('function renderKVCacheControl()')));
const modelStart=app.indexOf('function renderModelList()');
const modelRenderer=app.slice(modelStart,app.indexOf('/* ── 上下文统计',modelStart));
const submitStart=app.indexOf("$('settings-form').onsubmit");
const modelSubmit=app.slice(submitStart,app.indexOf('// 虚拟形象',submitStart));
const english=fs.readFileSync('internal/server/web/locales/en.js','utf8');
const css=['themes/dark/tokens.css','themes/light/tokens.css','style.css','macos.css','workbench.css'].map(p=>fs.readFileSync('internal/server/web/'+p,'utf8')).join('\n');
const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert(out);fs.mkdirSync(out,{recursive:true});
(async()=>{
  const server=http.createServer((req,res)=>{res.setHeader('Content-Type','text/html');res.end('<!doctype html><title>KV Cache fixture</title>');});
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const browser=await chromium.launch({headless:true,executablePath:process.env.AIDE_CHROMIUM_PATH||undefined,args:['--no-sandbox','--disable-dev-shm-usage']});const errors=[],results=[];
  try{
    const page=await browser.newPage();page.on('pageerror',error=>errors.push(error.message));
    for(const [theme,width] of [['light',1440],['dark',390]]){
      await page.goto('http://127.0.0.1:'+server.address().port);await page.setViewportSize({width,height:1100});
      await page.setContent(`<html data-theme="${theme}"><head><meta charset="utf-8"></head><body style="display:block"><main style="padding:16px;max-width:820px;margin:auto"><div id="fixture"></div></main></body></html>`);
      await page.addStyleTag({content:css});await page.addScriptTag({content:english});
      await page.evaluate(renderer=>{
        window.el=(tag,cls='',text='')=>{const node=document.createElement(tag);node.className=cls;node.textContent=text;return node;};
        window.lang='zh';window.t=(s,...args)=>(lang==='en'?(window.aideEnglish?.[s]||s):s).replace(/\{(\d+)\}/g,(_,i)=>args[i]??'');
        window.action=fn=>fn;window.toast=message=>{window.lastToast=message;};window.confirm=()=>true;window.state={config:{}};
        window.cacheConfig={enabled:false,backend:'memory',capacityMiB:1024,ttlMinutes:1440,cloudPrefix:true,llamaCpp:true,vllm:true,diskPath:''};
        window.cacheStats={capacityBytes:1024*1048576,usedBytes:0,entryCount:0,activeLeases:0,generation:1};
        window.requests=[];window.cacheStatus=()=>({config:cacheConfig,stats:cacheStats,experimental:true,fixedCapacity:true,engineRestartRequired:cacheConfig.enabled,llamaLaunchArgs:cacheConfig.backend==='memory'?['--cache-ram',String(cacheConfig.capacityMiB)]:['--slot-save-path',cacheConfig.diskPath+'/llamacpp'],lmcacheConfigPath:'/data/config/lmcache.yaml',notes:[]});
        window.api=async(url,options={})=>{const method=options.method||'GET',body=options.body?JSON.parse(options.body):null;requests.push({url,method,body});if(url!=='/kv-cache')throw Error('Unexpected API '+url);
          if(method==='PUT'){if(!Number.isInteger(body.capacityMiB)||body.capacityMiB<16)throw Error('invalid fixed capacity');cacheConfig={...body};cacheStats={...cacheStats,capacityBytes:body.capacityMiB*1048576,usedBytes:16384,entryCount:1,generation:cacheStats.generation+1,restores:2,restoredTokens:256,cachedTokens:512};}
          if(method==='DELETE')cacheStats={...cacheStats,usedBytes:0,entryCount:0,activeLeases:0,generation:cacheStats.generation+1};
          return cacheStatus();
        };
        (0,eval)(renderer);document.getElementById('fixture').replaceChildren(renderKVCacheControl());
      },renderer);
      await page.waitForFunction(()=>[...document.querySelectorAll('.kv-cache-control .section-desc')].some(node=>node.textContent.includes('固定总预算'))).catch(async error=>{throw new Error('initial KV status timeout: '+JSON.stringify(errors)+' content='+await page.locator('#fixture').textContent()+' cause='+error.message);});
      assert.equal(await page.getByLabel('启用实验性 KV Cache 统一代理').count(),1);
      await page.getByLabel('启用实验性 KV Cache 统一代理').check();
      await page.getByLabel('缓存存储',{exact:true}).selectOption('disk');
      await page.getByLabel('固定磁盘路径',{exact:true}).fill('/tmp/aide-fixed-kv-cache');
      await page.getByLabel('固定容量（MiB）',{exact:true}).fill('16');
      await page.getByLabel('缓存生命周期（分钟）',{exact:true}).fill('60');
      await page.getByRole('button',{name:'保存固定缓存配置',exact:true}).click();
      await page.waitForFunction(()=>requests.some(r=>r.method==='PUT')&&lastToast==='KV Cache 固定配置已保存');
      const put=await page.evaluate(()=>requests.find(r=>r.method==='PUT').body);
      assert.deepEqual({enabled:put.enabled,backend:put.backend,capacityMiB:put.capacityMiB,ttlMinutes:put.ttlMinutes,diskPath:put.diskPath},{enabled:true,backend:'disk',capacityMiB:16,ttlMinutes:60,diskPath:'/tmp/aide-fixed-kv-cache'});
      await page.getByRole('button',{name:'清空并释放缓存',exact:true}).click();
      await page.waitForFunction(()=>requests.some(r=>r.method==='DELETE')&&lastToast==='KV Cache 已清空并释放');
      assert.equal(await page.evaluate(()=>cacheStats.usedBytes),0);
      assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'narrow settings overflow');
      await page.screenshot({path:path.join(out,`${theme}-${width}-kv-cache.png`),fullPage:true});
      await page.evaluate(()=>{lang='en';document.getElementById('fixture').replaceChildren(renderKVCacheControl());});
      await page.waitForFunction(()=>[...document.querySelectorAll('.kv-cache-control .section-desc')].some(node=>node.textContent.includes('Fixed total budget')));
      assert.equal(await page.getByLabel('Enable experimental unified KV Cache proxy').count(),1);
      assert.equal(await page.getByRole('button',{name:'Save fixed cache configuration',exact:true}).count(),1);
      assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'English settings overflow');
      await page.screenshot({path:path.join(out,`${theme}-${width}-kv-cache-en.png`),fullPage:true});
      results.push({theme,width,languages:['zh-CN','en'],save:true,clear:true,fixedCapacity:true});
    }
    const adapterBody=await page.evaluate(async({modelRenderer,modelSubmit})=>{
      document.body.insertAdjacentHTML('beforeend','<div id="model-list"></div><form id="settings-form"></form><div id="settings-dialog"></div>');
      window.$=id=>document.getElementById(id);$('settings-dialog').close=()=>{};window.refreshConfig=async()=>{};
      const baseAPI=api;api=async(url,options={})=>{if(url==='/settings'){const body=JSON.parse(options.body);requests.push({url,method:options.method||'GET',body});return{ok:true};}return baseAPI(url,options);};
      state.config={baseURL:'http://127.0.0.1:8000/v1'};state.modelDraft={providers:[{id:'local',name:'Local',baseURL:'http://127.0.0.1:8000/v1',keys:[],activeKeyId:''}],models:[{id:'m',name:'m',providerId:'local',contextWindow:65536}],activeModel:'m',expandedProviders:new Set(['local']),providerSecrets:new Map()};
      (0,eval)(modelRenderer);(0,eval)(modelSubmit);renderModelList();
      const label=[...document.querySelectorAll('#model-list label')].find(node=>node.textContent.includes('KV Cache'));
      if(!label)throw Error('KV adapter select missing');const select=label.querySelector('select');select.value='vllm';select.dispatchEvent(new Event('change'));
      await $('settings-form').onsubmit({preventDefault(){}});
      return requests.find(request=>request.url==='/settings')?.body;
    },{modelRenderer,modelSubmit});
    assert.equal(adapterBody?.modelProviders?.[0]?.kvCacheEngine,'vllm');
    assert.deepEqual(errors,[]);
    fs.writeFileSync(path.join(out,'report.json'),JSON.stringify({status:'PASS',results,adapterSave:true,errors,liveEngine:false},null,2));
    console.log('PASS KV Cache settings browser: fixed config/save/clear/stats, provider adapter persistence, desktop/mobile, zh-CN/en');
  }finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
})().catch(error=>{console.error(error);process.exitCode=1;});
