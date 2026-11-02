'use strict';
// Production settings renderer/CSS, mock config API and playback. No paid calls,
// personal browser, microphone, production service or listening-quality claim.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {chromium}=require(process.env.AIDE_PLAYWRIGHT_MODULE||'./browser-runtime/node_modules/playwright');
const app=fs.readFileSync('internal/server/web/app.js','utf8');
const renderer=app.slice(app.indexOf('function renderTTSEngineControl()'),app.indexOf("controlRenderers['tts-engine']"));
const css=['themes/dark/tokens.css','themes/light/tokens.css','style.css','macos.css','workbench.css'].map(p=>fs.readFileSync('internal/server/web/'+p,'utf8')).join('\n');
const en=fs.readFileSync('internal/server/web/locales/en.js','utf8');
const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;
assert(out,'Set an owned AIDE_COMPONENT_EVIDENCE_DIR');fs.mkdirSync(out,{recursive:true});
(async()=>{
 const browser=await chromium.launch({headless:true});const results=[],errors=[];
 try{
  const context=await browser.newContext();const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
  for(const [theme,width,lang] of [['light',1440,'zh'],['dark',390,'zh'],['light',390,'en'],['dark',1440,'en']]){
   await page.setViewportSize({width,height:1000});
   await page.setContent(`<html data-theme="${theme}" data-palette="blue"><head><meta charset="utf-8"></head><body style="display:block"><main style="padding:16px;max-width:720px;margin:auto"><h1>Voice settings · local mock API</h1><div id="fixture"></div></main></body></html>`);
   await page.addStyleTag({content:css});await page.addScriptTag({content:en});
   await page.evaluate(({renderer,lang})=>{
    window.el=(tag,cls='',text='')=>{const n=document.createElement(tag);n.className=cls;n.textContent=text;return n;};
    window.t=text=>lang==='en'?(window.aideEnglish[text]||text):text;
    window.action=fn=>fn;window.notices=[];window.toast=s=>notices.push(s);window.requests=[];window.previewCalls=[];
    window.ttsUnlockAudio=()=>{};window.ttsCancel=()=>{};window.ttsSpeak=async(text,opts)=>previewCalls.push({text,...opts});
    window.callEnds=0;window.aideAssistantCall={active:()=>true,end:()=>callEnds++};
    window.setTimeout=()=>0; // preview Promise.race guard must not leave 65s timers in this fixture
    window.state={config:{ttsProvider:'auto',edgeAvailable:false,sherpaAvailable:false,ttsModel:'qwen3-tts-flash',ttsVoice:'Cherry',hasTTSKey:true,qwenTTSCatalog:{models:[{id:'qwen3-tts-flash'}],voices:[{id:'Cherry'}]}}};
    window.api=async(url,options)=>{requests.push({url,body:options?JSON.parse(options.body):null});
     if(url==='/settings'){Object.assign(state.config,JSON.parse(options.body));state.config.hasOpenAITTSKey=!!state.config.openaiTTSAPIKey;if(state.config.clearOpenAITTSKey)state.config.hasOpenAITTSKey=false;delete state.config.openaiTTSAPIKey;return{};}
     if(url==='/tts/catalog')return state.config.qwenTTSCatalog;throw Error('Unexpected fixture API '+url);
    };
    window.refreshConfig=async()=>{};
    (0,eval)(renderer);document.getElementById('fixture').append(renderTTSEngineControl());
   },{renderer,lang});
   const engine=page.locator('.tts-engine-select select');await engine.selectOption('openai');
   for(const warning of await page.locator('#fixture small').filter({hasText:/^⚠/}).all())assert.equal(await warning.isVisible(),false,'OpenAI must hide the unrelated Edge warning');
   const label=text=>page.getByLabel(lang==='en'?({
    'OpenAI 语音接口':'OpenAI speech API','OpenAI 语音模型':'OpenAI speech model','OpenAI 音色':'OpenAI voice',
    'OpenAI 语音 API Key':'OpenAI speech API Key','OpenAI 语音 API Base URL':'OpenAI speech API Base URL',
    '清除已保存的 OpenAI 语音密钥':'Clear saved OpenAI speech key'}[text]||text):text,{exact:true});
   assert.equal(await label('OpenAI 语音接口').inputValue(),'realtime');assert.equal(await label('OpenAI 语音模型').inputValue(),'gpt-realtime-2.1-mini');
   assert.equal(await label('OpenAI 音色').inputValue(),'marin');assert.equal(await label('OpenAI 语音 API Base URL').inputValue(),'https://api.openai.com/v1');
   assert.equal(await label('OpenAI 语音 API Key').getAttribute('type'),'password');
   await page.getByRole('button',{name:lang==='en'?'Preview':'试听',exact:true}).click();
   assert.equal(await page.evaluate(()=>previewCalls.length),0,'unsaved preview must not synthesize');
   await label('OpenAI 语音接口').selectOption('speech');assert.equal(await label('OpenAI 语音模型').inputValue(),'gpt-4o-mini-tts');
   await label('OpenAI 语音模型').fill('tts-1-hd');await label('OpenAI 音色').selectOption('nova');
   await label('OpenAI 语音 API Key').fill('synthetic-browser-key');
   await page.getByRole('button',{name:lang==='en'?'Save':'保存',exact:true}).click();
   await label('OpenAI 语音 API Key').evaluate(n=>new Promise((resolve,reject)=>{let tries=0;const timer=setInterval(()=>{if(n.value===''){clearInterval(timer);resolve();}else if(++tries>100){clearInterval(timer);reject(Error('Saved key not cleared from input'));}},10);}));
   let saved=await page.evaluate(()=>requests.filter(r=>r.url==='/settings').at(-1).body);
   assert.equal(saved.openaiTTSModel,'tts-1-hd');assert.equal(saved.openaiTTSVoice,'nova');assert.equal(saved.openaiTTSAPIKey,'synthetic-browser-key');assert.equal(saved.ttsAPIKey,'');assert(!('ttsVoice'in saved));
   assert.equal(await label('OpenAI 语音 API Key').inputValue(),'');
   assert.equal(await page.evaluate(()=>callEnds),1,'provider/model changes must end an existing call');
   await page.getByRole('button',{name:lang==='en'?'Save':'保存',exact:true}).click();
   assert.equal(await page.evaluate(()=>callEnds),1,'saving unchanged settings must not end a call');
   await page.getByRole('button',{name:lang==='en'?'Preview':'试听',exact:true}).click();
   const preview=await page.evaluate(()=>previewCalls.at(-1));assert.equal(preview.provider,'openai');assert.equal(preview.voice,'nova');
   await label('清除已保存的 OpenAI 语音密钥').check();await page.getByRole('button',{name:lang==='en'?'Save':'保存',exact:true}).click();
   assert.equal(await page.evaluate(()=>requests.filter(r=>r.url==='/settings').at(-1).body.clearOpenAITTSKey),true);
   await engine.selectOption('qwen');assert.equal(await page.getByLabel(lang==='en'?'Voice':'音色',{exact:true}).filter({visible:true}).last().inputValue(),'Cherry');
   await engine.selectOption('openai');await label('OpenAI 语音接口').selectOption('realtime');
   assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'page overflow');
   await page.screenshot({path:path.join(out,`${theme}-${width}-${lang}.png`),fullPage:true});
   results.push({theme,width,lang,status:'pass',scope:'actual renderer/CSS with mock config and playback'});
  }
  assert.deepEqual(errors,[]);fs.writeFileSync(path.join(out,'report.json'),JSON.stringify({status:'pass',results,errors,liveProvider:false},null,2));
  console.log('PASS OpenAI settings four browser cells: modes, model/voice, independent key, save/preview/clear, Qwen return and layout');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1;});
