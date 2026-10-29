'use strict';
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require('./browser-runtime/node_modules/playwright');
const web=path.resolve('internal/server/web'),app=fs.readFileSync(web+'/app.js','utf8');
const setup=app.slice(app.indexOf('async function setupPdfPreview('),app.indexOf('/* ── DXF 矢量渲染器'));
const ensure=app.slice(app.indexOf('let _pdfjsPromise = null;'),app.indexOf('async function setupPdfPreview('));
const {fixture}=require('./pdf-font-attachment-review.cjs');
async function main(){
 const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert(out);fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({executablePath:'/usr/bin/chromium',args:['--no-sandbox','--disable-dev-shm-usage']});const rows=[];
 try{
  const context=await browser.newContext();
  for(const mode of (process.env.AIDE_PDF_BROWSER_CASES?.split(',')||['font-normal','font-large','font-zero','attachment-normal','attachment-invalid'])){
   const page=await context.newPage(),errors=[],messages=[];page.setDefaultTimeout(15000);page.on('pageerror',e=>errors.push(e.message));page.on('console',m=>messages.push(m.text()));
   try{
    await page.route('**/*',route=>{
     const u=new URL(route.request().url());if(u.origin!=='https://aide-pdf-budgets.test')return route.abort();
     if(u.pathname==='/')return route.fulfill({contentType:'text/html; charset=utf-8',body:'<!doctype html><html data-theme="light"><meta charset="utf-8"><link rel="stylesheet" href="/themes/dark/tokens.css"><link rel="stylesheet" href="/themes/light/tokens.css"><link rel="stylesheet" href="/style.css"><style>dialog{margin:20px;background:white;color:#20242b}main{width:900px;height:650px}.pdf-viewer{height:600px}.pdf-scroll{height:500px;overflow:auto}</style><dialog open><main id="preview"></main></dialog>'});
     if(u.pathname==='/owned.pdf')return route.fulfill({contentType:'application/pdf',body:fixture(mode)});
     const p=path.resolve(web,'.'+u.pathname);if(!p.startsWith(web+path.sep)||!fs.existsSync(p)||!fs.statSync(p).isFile())return route.fulfill({status:404,body:''});
                return route.fulfill({contentType:p.endsWith('.css')?'text/css':'application/javascript',body:fs.readFileSync(p)});
    });
    await page.goto('https://aide-pdf-budgets.test/');await page.evaluate(mode=>window.fixtureMode=mode,mode);
    await page.evaluate(()=>{
     window.el=(tag,cls,text)=>{const n=document.createElement(tag);n.className=cls||'';if(text!=null)n.textContent=text;return n;};window.t=s=>s;window.escapeHtml=s=>s;window.fileRawUrl=()=>'/owned.pdf';window.passwordPrompt=async()=>null;
     window.bindViewerTeardown=(host,fn)=>{window.ownedTeardown=fn;host.closest('dialog').addEventListener('close',fn);};
    });
    await page.addScriptTag({content:ensure});
    await page.evaluate(()=>{const actual=ensurePdfJs;window.ensurePdfJs=async()=>{const native=await actual();window.nativePdf=native;return{getDocument(options){window.recordedOptions={disableEmbeddedFiles:options.disableEmbeddedFiles,maxImageSize:options.maxImageSize,canvasMaxAreaInBytes:options.canvasMaxAreaInBytes};const task=native.getDocument({...options,disableFontFace:window.fixtureMode.startsWith('font-')});window.ownedTask=task;task.promise.then(doc=>window.ownedDoc=doc,()=>{});return task;}};};});
    await page.addScriptTag({content:setup});await page.evaluate(()=>setupPdfPreview(document.querySelector('main'),'owned.pdf'));
    const info=await page.evaluate(async()=>{
     const pdfPage=await ownedDoc.getPage(1),op=await pdfPage.getOperatorList(),names=Object.fromEntries(Object.entries(nativePdf.OPS).map(([k,v])=>[v,k]));
     return{options:recordedOptions,operations:op.fnArray.length,operationNames:op.fnArray.map(n=>names[n]),annotations:(await pdfPage.getAnnotations()).map(a=>({rect:a.rect,file:a.file?{filename:a.file.filename,content:a.file.content}:undefined})),forcedFontFallback:window.fixtureMode.startsWith('font-'),pages:document.querySelectorAll('.pdf-page').length,note:document.querySelector('.pdf-info').title};
    });
    assert.equal(info.pages,1);assert(!messages.some(message=>/RangeError|Maximum call stack/.test(message)),messages.join('\n'));
    assert(info.operations<3000,JSON.stringify(info));
    assert.equal(info.options.disableEmbeddedFiles,true);
    assert(!messages.some(m=>/translateFont failed|ignoring.*glyph|RangeError|Invalid header in flate stream/.test(m)),messages.join('\n'));
    const expected=[0,0,255,255];
    await page.waitForFunction(expected=>{const c=document.querySelector('.pdf-canvas');if(!c?.width)return false;const p=c.getContext('2d').getImageData(Math.floor(c.width/2),Math.floor(c.height/2),1,1).data;return expected.every((v,i)=>Math.abs(v-p[i])<5);},expected);
    info.expectedPixel=expected;
    if(mode.startsWith('font-'))await page.waitForFunction(()=>{const c=document.querySelector('.pdf-canvas'),p=c.getContext('2d').getImageData(Math.floor(c.width*.2),Math.floor(c.height*.55),1,1).data;return p[0]<10&&p[1]<10&&p[2]<10&&p[3]===255;});
    else{assert.equal(info.annotations[0].file.filename,'owned.txt');assert.equal(info.annotations[0].file.content,null);}
    if(mode==='attachment-normal'){
     const extraction=await page.evaluate(async()=>{const bytes=new Uint8Array(await(await fetch('/owned.pdf')).arrayBuffer()),task=nativePdf.getDocument({data:bytes});try{const doc=await task.promise,p=await doc.getPage(1);await p.getOperatorList();const a=(await p.getAnnotations())[0],files=await doc.getAttachments(),explicit=await ownedDoc.getAttachments();return{defaultAnnotation:new TextDecoder().decode(a.file.content),defaultCatalog:new TextDecoder().decode(files.owned.content),explicitPreview:new TextDecoder().decode(explicit.owned.content)};}finally{await task.destroy();}});
     assert.deepEqual(extraction,{defaultAnnotation:'owned attachment bytes',defaultCatalog:'owned attachment bytes',explicitPreview:'owned attachment bytes'});info.extraction=extraction;
    }
    if(!mode.endsWith('-large'))await page.screenshot({path:path.join(out,mode+'.png')});
    await page.getByTitle('关闭',{exact:true}).click();await page.waitForFunction(()=>ownedTask.destroyed);assert.deepEqual(errors,[]);rows.push({mode,status:'PASS',...info,messages:messages.filter(m=>/Length1|Type1|RangeError/.test(m))});
   }finally{await page.close();}
  }
  await context.close();
 }finally{await browser.close();fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(rows,null,2));}
 console.log(JSON.stringify(rows));
}
if(require.main===module)main().catch(e=>{console.error(e);process.exitCode=1;});
module.exports={fixture};
