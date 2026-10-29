'use strict';
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require('./browser-runtime/node_modules/playwright');
(async()=>{
 const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert(out);fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({executablePath:'/usr/bin/chromium',args:['--no-sandbox','--disable-dev-shm-usage']});
 const rows=[],errors=[];
 try{
  const page=await browser.newPage();page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/*',route=>{
   const u=new URL(route.request().url());if(u.origin!=='https://aide-export-cancel-owned.test')return route.abort();
   if(u.pathname==='/')return route.fulfill({contentType:'text/html',headers:{'Content-Security-Policy':"default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; object-src 'none'"},body:'<!doctype html><meta charset="utf-8"><title>Owned export cancellation</title><main>Actual local Markdown export regression</main>'});
   if(u.pathname==='/owned-red.png')return route.fulfill({contentType:'image/png',body:Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==','base64')});
   const file=u.pathname==='/canvas.js'?(process.env.AIDE_CANVAS_BASELINE||'internal/server/web/vendor/markdown-export/html2canvas.min.js'):u.pathname==='/export.js'?(process.env.AIDE_EXPORT_BASELINE||'internal/server/web/markdown-export.js'):u.pathname==='/pdf.js'?'internal/server/web/vendor/markdown-export/jspdf.umd.min.js':null;
   return file?route.fulfill({contentType:'application/javascript',body:fs.readFileSync(file)}):route.fulfill({status:404,body:''});
  });
  for(const format of ['png','pdf'])for(const cause of ['close','lock','invalid','timeout']){
   await page.goto('https://aide-export-cancel-owned.test/');
   await page.evaluate(()=>{window.ownedLockHandlers=[];window.LockCluster={on:(name,fn)=>ownedLockHandlers.push(fn)};});
   for(const url of ['/canvas.js','/pdf.js','/export.js'])await page.addScriptTag({url});
   const result=await page.evaluate(async({format,cause})=>{
    const nativeTimeout=window.setTimeout,delay=ms=>new Promise(r=>nativeTimeout(r,ms));
    const waitUntil=async fn=>{for(let i=0;i<200;i++){if(fn())return;await delay(10)}throw Error('actual font wait was not reached')};
    const actual=window.html2canvas;let releaseFont,hitFonts=0,onclone=0,rawResult,renderOptions;
    const fontReady=new Promise(r=>releaseFont=r),clones=[],observers=[];let downloads=0,valid=true;
    const createURL=URL.createObjectURL;URL.createObjectURL=function(...args){downloads++;return createURL.apply(this,args)};
    window.setTimeout=(fn,ms,...args)=>nativeTimeout(fn,cause==='timeout'&&ms>=120000?1000:ms,...args);
    window.html2canvas=(element,options)=>{
     renderOptions=options;
     const doc=element.ownerDocument,observer=new MutationObserver(records=>{
      for(const record of records)for(const node of record.addedNodes)if(node.nodeName==='IFRAME'&&node.classList.contains('html2canvas-container')){
       clones.push(node);Object.defineProperty(node.contentDocument.fonts,'ready',{configurable:true,get(){hitFonts++;return fontReady}});
      }
     });observer.observe(doc.body,{childList:true});observers.push(observer);
     const p=actual(element,{...options,onclone:()=>{onclone++}});
     p.then(()=>rawResult='resolved',e=>rawResult=e.name||String(e));return p;
    };
    let outcome;
    const job=AideMarkdownExport.run(format,{markdown:'# Owned',html:'<h1>Owned fixture</h1><p>Cancellation must stop actual rendering.</p>',title:'owned.md',valid:()=>valid,load:async()=>{}});
    job.then(()=>outcome='resolved',e=>outcome=e.name==='AbortError'?'AbortError':e.message);
    try{
     await waitUntil(()=>hitFonts>0);const captured=clones.length;
     if(cause==='close')AideMarkdownExport.close();
     else if(cause==='lock')ownedLockHandlers.forEach(fn=>fn(true));
     else if(cause==='invalid')valid=false;
     await Promise.race([job.catch(()=>{}),delay(cause==='timeout'?1600:800)]);
     const beforeRelease={outcome:outcome||'pending',cloneParents:clones.filter(n=>n.parentNode).length,outerFrames:document.querySelectorAll('.markdown-print-frame').length,rawResult:rawResult||'pending'};
     releaseFont();await delay(250);
     await Promise.race([job.catch(()=>{}),delay(500)]);
     return{format,cause,captured,hitFonts,beforeRelease,oncloneAfterRelease:onclone,rawAfterRelease:rawResult||'pending',downloads,signalPassed:!!renderOptions.signal,signalAborted:!!renderOptions.signal?.aborted,cloneParentsAfterRelease:clones.filter(n=>n.parentNode).length};
    }finally{releaseFont();AideMarkdownExport.close();observers.forEach(o=>o.disconnect());URL.createObjectURL=createURL;window.setTimeout=nativeTimeout;}
   },{format,cause});
   const expected=cause==='timeout'?/导出超时/.test(result.beforeRelease.outcome):result.beforeRelease.outcome==='AbortError';
   rows.push({...result,pass:expected&&result.captured===1&&result.beforeRelease.cloneParents===0&&result.beforeRelease.outerFrames===0&&result.oncloneAfterRelease===0&&result.rawAfterRelease==='AbortError'&&result.downloads===0&&result.signalPassed&&result.signalAborted});
  }
  // Normal exports must still produce real PNG and multi-page PDF files.
  await page.goto('https://aide-export-cancel-owned.test/');for(const url of ['/canvas.js','/pdf.js','/export.js'])await page.addScriptTag({url});
  const controls=await page.evaluate(async()=>{
   const actual=URL.createObjectURL,blobs=[];URL.createObjectURL=blob=>{blobs.push(blob);return actual.call(URL,blob)};
   const names=[],click=HTMLAnchorElement.prototype.click;HTMLAnchorElement.prototype.click=function(){names.push(this.download)};
   try{
    for(const format of ['png','pdf'])await AideMarkdownExport.run(format,{markdown:'normal',title:'normal.md',html:'<h1>Owned normal export</h1><ol><li>first</li><li value="0">zero</li><li>next</li></ol><img width="24" height="24" src="https://aide-export-cancel-owned.test/owned-red.png">'+('<p>Normal page content remains readable.</p>'.repeat(format==='pdf'?80:1)),valid:()=>true,load:async()=>{}});
    const signatures=[];for(const blob of blobs)signatures.push(Array.from(new Uint8Array(await blob.slice(0,8).arrayBuffer())));
    const image=new Image(),url=actual.call(URL,blobs[0]);image.src=url;await image.decode();const probe=document.createElement('canvas');probe.width=image.naturalWidth;probe.height=image.naturalHeight;const ctx=probe.getContext('2d');ctx.drawImage(image,0,0);const pixels=ctx.getImageData(0,0,probe.width,probe.height).data;let redPixels=0;for(let i=0;i<pixels.length;i+=4)if(pixels[i]>200&&pixels[i+1]<30&&pixels[i+2]<30)redPixels++;probe.width=probe.height=0;URL.revokeObjectURL(url);
    return{names,sizes:blobs.map(b=>b.size),signatures,redPixels,frames:document.querySelectorAll('iframe').length};
   }finally{URL.createObjectURL=actual;HTMLAnchorElement.prototype.click=click;AideMarkdownExport.close();}
  });
  assert.deepEqual(controls.names,['normal.png','normal.pdf']);assert(controls.sizes.every(x=>x>1000));assert.deepEqual(controls.signatures[0],[137,80,78,71,13,10,26,10]);assert.deepEqual(controls.signatures[1].slice(0,4),[37,80,68,70]);assert.equal(controls.frames,0);assert(controls.redPixels>100,'normal owned image is rasterized');
  const resources=await page.evaluate(async()=>{
   const delay=ms=>new Promise(r=>setTimeout(r,ms)),results=[];
   const host=document.createElement('section');host.style.width='220px';host.style.height='80px';document.body.append(host);
   const sibling=document.createElement('iframe');document.body.append(sibling);
   const pre=new AbortController();pre.abort();let preResult;
   try{await html2canvas(host,{signal:pre.signal,logging:false,width:220,height:80,scale:1});preResult='resolved'}catch(e){preResult=e.name}
   results.push({case:'already aborted signal',pass:preResult==='AbortError',result:preResult});
   const image=new Image();image.src='data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==';await image.decode();
   for(const provided of [false,true]){
    const input=document.createElement('img');input.src=image.src;input.style.width='40px';input.style.height='40px';host.replaceChildren(input);
    const canvas=document.createElement('canvas');canvas.width=220;canvas.height=80;
    const create=document.createElement.bind(document),created=[],draw=CanvasRenderingContext2D.prototype.drawImage;let draws=0,matched=0,releaseImage,result;
    document.createElement=(tag,...args)=>{const node=create(tag,...args);if(tag==='canvas')created.push(node);return node};
    CanvasRenderingContext2D.prototype.drawImage=function(...args){draws++;return draw.apply(this,args)};
    const waiting=new Promise(r=>releaseImage=r),controller=new AbortController();
    const promise=html2canvas(host,{signal:controller.signal,logging:false,width:220,height:80,scale:1,...(provided?{canvas}:{}),cache:{addImage(){return Promise.resolve()},match(){matched++;return waiting}}});
    promise.then(()=>result='resolved',e=>result=e.name);
    try{
     for(let i=0;i<200&&!matched;i++)await delay(10);if(!matched)throw Error('actual renderer never awaited image cache');
     const owned=provided?canvas:created.find(c=>c.width===220&&c.height===80);if(!owned)throw Error('actual renderer canvas not captured');
     controller.abort();await Promise.race([promise.catch(()=>{}),delay(500)]);
     const before={result:result||'pending',width:owned.width,height:owned.height},beforeDraws=draws;
     releaseImage(image);await delay(150);
     const dimensions=provided?before.width===220&&before.height===80:before.width===0&&before.height===0;
     results.push({case:provided?'caller canvas retained':'owned canvas released',before,drawsAfterAbort:draws-beforeDraws,siblingPreserved:!!sibling.parentNode,pass:before.result==='AbortError'&&dimensions&&draws===beforeDraws&&!!sibling.parentNode});
    }finally{releaseImage(image);controller.abort();document.createElement=create;CanvasRenderingContext2D.prototype.drawImage=draw;canvas.width=canvas.height=0;}
   }
   sibling.remove();host.remove();return results;
  });
  fs.writeFileSync(path.join(out,'result.json'),JSON.stringify({rows,controls,resources,errors},null,2)+'\n');console.log(JSON.stringify({checks:rows.length,failed:rows.filter(r=>!r.pass),controls,resources,errors}));
  if(!process.env.AIDE_EXPECT_CANCEL_FAILURES){assert(rows.every(r=>r.pass));assert(resources.every(r=>r.pass));assert.deepEqual(errors,[])}
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
