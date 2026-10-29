'use strict';
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require('./browser-runtime/node_modules/playwright'),{controls}=require('./highlight-work-budget-review.cjs');
async function main(){
 const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert(out);fs.mkdirSync(out,{recursive:true});
 const web=path.resolve('internal/server/web'),html=fs.readFileSync(web+'/index.html','utf8');
 const scripts=[...html.matchAll(/<script[^>]*src="([^"?]+)(?:\?[^" ]*)?"/g)].map(m=>m[1]).filter(p=>p.startsWith('/vendor/highlight.js/'));
 const browser=await chromium.launch({executablePath:'/usr/bin/chromium',args:['--no-sandbox','--disable-dev-shm-usage']});const rows=[],errors=[];
 try{
  const context=await browser.newContext(),page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/*',route=>{
   const u=new URL(route.request().url());if(u.origin!=='https://aide-highlight-budget.test')return route.abort();
   if(u.pathname==='/')return route.fulfill({contentType:'text/html',body:'<!doctype html><html data-theme="light"><meta charset="utf-8"><link rel="stylesheet" href="/themes/dark/tokens.css"><link rel="stylesheet" href="/themes/light/tokens.css"><link rel="stylesheet" href="/style.css"><style>body{display:block;overflow:auto}main,section{padding:16px}textarea{height:120px;width:95%}.code-wrapper{height:150px}pre{white-space:pre-wrap}</style><main></main><section></section>'});
   const file=path.resolve(web,'.'+u.pathname);if(!file.startsWith(web+path.sep)||!fs.existsSync(file))return route.fulfill({status:404,body:''});
   let data=fs.readFileSync(file);if(u.searchParams.has('base'))data=data.toString().split('/* AIDE grammar repairs:')[0];return route.fulfill({contentType:file.endsWith('.css')?'text/css':'application/javascript',body:data});
  });
  for(const variant of ['source','bundle']){
   await page.goto('https://aide-highlight-budget.test/');
   for(const script of scripts){const core=script.endsWith('/highlight.min.js');await page.addScriptTag({url:script+(variant==='source'&&core?'?base=1':'')});if(variant==='source'&&core)await page.addScriptTag({url:'/highlight-grammar-fixes.js'});}
   for(const script of ['/markdown-code.js','/text-editor.js'])await page.addScriptTag({url:script});
   for(const [theme,width]of [['light',1280],['dark',390]]){
    await page.setViewportSize({width,height:900});
    const result=await page.evaluate(async({controls,theme})=>{
     document.documentElement.dataset.theme=theme;const main=document.querySelector('main'),host=document.querySelector('section'),rows=[];
     const renderMarkdown=(language,text)=>{main.replaceChildren();const pre=document.createElement('pre'),code=document.createElement('code');code.className='language-'+language;code.textContent=text;pre.append(code);main.append(pre);AideMarkdownCode.decorate(main);return code;};
     const renderEditor=async(language,text)=>{host.replaceChildren();const textarea=document.createElement('textarea');textarea.value=text;host.append(textarea);AideTextEditor.mount(textarea,language);await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));return{textarea,overlay:host.querySelector('.code-highlight-overlay code')};};
     for(const [language,text,marker,scope]of controls){
      const code=renderMarkdown(language,text),selector=scope==='title.function'?'.hljs-title.function_':'.hljs-'+scope;
      if(code.textContent!==text||![...code.querySelectorAll(selector)].some(n=>n.textContent.includes(marker)))throw Error(language+' Markdown scope/text control');
      const {textarea,overlay}=await renderEditor(language,text);if(overlay.textContent!==text+'\n'||![...overlay.querySelectorAll(selector)].some(n=>n.textContent.includes(marker)))throw Error(language+' editor scope/text control');AideTextEditor.dispose(textarea);
     }
     for(const language of ['c','cpp','csharp','ini','ruby']){
      const make=n=>language==='ruby'?Array.from({length:n},(_,i)=>'puts <<H'+String(i).padStart(5,'0')).join('\n')+'\n':language==='ini'?'a.'.repeat(n)+'!':'a '.repeat(n);
      const small=make(language==='ruby'?4000:30000),large=make(language==='ruby'?32000:249999);let start=performance.now();const code=renderMarkdown(language,small);const markdownMs=performance.now()-start;
      if(code.textContent!==small||code.dataset.highlightMode!=='explicit')throw Error(language+' Markdown pressure text/route');
      start=performance.now();const {textarea,overlay}=await renderEditor(language,large),editorMs=performance.now()-start;
      if(textarea.value!==large||overlay.textContent!==large+'\n')throw Error(language+' editor pressure text/route');AideTextEditor.dispose(textarea);rows.push({language,markdownBytes:small.length,markdownMs,editorBytes:large.length,editorMs});
     }
     // Keep the final screenshot compact, with actual normal syntax colors.
     main.replaceChildren();host.replaceChildren();for(const [language,text]of controls){const pre=document.createElement('pre'),code=document.createElement('code');code.className='language-'+language;code.textContent=text;pre.append(code);main.append(pre);}AideMarkdownCode.decorate(main);
     return{controls:controls.length,rows};
    },{controls,theme});
    await page.screenshot({path:path.join(out,theme+'-'+variant+'.png'),fullPage:true});rows.push({variant,theme,width,...result});
   }
  }
  await context.close();assert.deepEqual(errors,[]);
 }finally{await browser.close();fs.writeFileSync(path.join(out,'result.json'),JSON.stringify({rows,errors},null,2));}
 console.log(JSON.stringify({rows,errors}));
}
main().catch(e=>{console.error(e);process.exitCode=1;});
