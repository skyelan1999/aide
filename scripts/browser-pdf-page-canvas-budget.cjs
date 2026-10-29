'use strict';
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict'),{chromium}=require('./browser-runtime/node_modules/playwright');
const web=path.resolve('internal/server/web'),app=fs.readFileSync(process.env.AIDE_PDF_PAGE_APP_BASELINE||web+'/app.js','utf8');
const ensure=app.slice(app.indexOf('let _pdfjsPromise = null;'),app.indexOf('async function setupPdfPreview(')),setup=app.slice(app.indexOf('async function setupPdfPreview('),app.indexOf('/* ── DXF 矢量渲染器'));
function fixture(width,height,invalidBox){
 const content=`0.2 0.4 0.8 rg 0 0 ${width} ${height} re f\n`,huge='1'+'0'.repeat(308),box=invalidBox?`-${huge} -${huge} ${huge} ${huge}`:`0 0 ${width} ${height}`;
 const objects=['<< /Type /Catalog /Pages 2 0 R >>','<< /Type /Pages /Count 1 /Kids [3 0 R] >>',`<< /Type /Page /Parent 2 0 R /MediaBox [${box}] /Resources << >> /Contents 4 0 R >>`,`<< /Length ${content.length} >>\nstream\n${content}endstream`];
 let text='%PDF-1.6\n',offsets=[0];objects.forEach((value,i)=>{offsets.push(text.length);text+=`${i+1} 0 obj\n${value}\nendobj\n`;});const x=text.length;text+=`xref\n0 ${objects.length+1}\n0000000000 65535 f \n`+offsets.slice(1).map(n=>String(n).padStart(10,'0')+' 00000 n \n').join('')+`trailer\n<< /Size ${objects.length+1} /Root 1 0 R >>\nstartxref\n${x}\n%%EOF`;return Buffer.from(text);
}
async function main(){
 const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert(out);fs.mkdirSync(out,{recursive:true});const browser=await chromium.launch({executablePath:'/usr/bin/chromium',args:['--no-sandbox','--disable-dev-shm-usage']}),rows=[];
 try{
  const cases=[['wide',100000,200000,1,1280,'light'],['tall',200,200000,2,1280,'light'],['high-dpr',200,200,20,390,'dark'],['invalid-box',200,200,1,1280,'light'],['normal',200,200,1,1280,'light']];
  for(const [name,width,height,dpr,size,theme]of cases){
   const context=await browser.newContext({deviceScaleFactor:dpr,viewport:{width:size,height:900}}),page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
   try{
    await page.route('**/*',route=>{
     const u=new URL(route.request().url());if(u.origin!=='https://aide-pdf-page-budget.test')return route.abort();
     if(u.pathname==='/')return route.fulfill({contentType:'text/html; charset=utf-8',body:'<!doctype html><html data-theme="'+theme+'"><meta charset="utf-8"><link rel="stylesheet" href="/themes/dark/tokens.css"><link rel="stylesheet" href="/themes/light/tokens.css"><link rel="stylesheet" href="/style.css"><style>body{display:block}dialog{margin:20px;max-width:none;padding:0}main{width:min(900px,calc(100vw - 40px));height:650px}.pdf-viewer{height:600px}.pdf-scroll{height:500px;overflow:auto}</style><dialog open><main></main></dialog>'});
     if(u.pathname==='/owned.pdf')return route.fulfill({contentType:'application/pdf',body:fixture(width,height,name==='invalid-box')});
     const file=path.resolve(web,'.'+u.pathname);if(!file.startsWith(web+path.sep)||!fs.existsSync(file)||!fs.statSync(file).isFile())return route.fulfill({status:404,body:''});return route.fulfill({contentType:file.endsWith('.css')?'text/css':'application/javascript',body:fs.readFileSync(file)});
    });
    await page.goto('https://aide-pdf-page-budget.test/');
    await page.evaluate(()=>{
     window.ownedAttempts=[];window.ownedSizes=[];window.ownedRenders=0;
     for(const key of ['width','height']){const descriptor=Object.getOwnPropertyDescriptor(HTMLCanvasElement.prototype,key);Object.defineProperty(HTMLCanvasElement.prototype,key,{...descriptor,set(value){if(this.classList.contains('pdf-canvas')){const width=key==='width'?Number(value):this.width,height=key==='height'?Number(value):this.height;if(!Number.isFinite(width)||!Number.isFinite(height)||width>8192||height>8192||width*height>16777216){ownedAttempts.push({key,width,height});throw Error('owned canvas allocation trap');}ownedSizes.push({width,height});}descriptor.set.call(this,value);}});}
     window.el=(tag,cls,text)=>{const node=document.createElement(tag);node.className=cls||'';if(text!=null)node.textContent=text;return node;};window.t=s=>s;window.escapeHtml=s=>s;window.fileRawUrl=()=>'/owned.pdf';window.passwordPrompt=async()=>null;window.bindViewerTeardown=(host,fn)=>host.closest('dialog').addEventListener('close',fn);
    });
    await page.addScriptTag({content:ensure});
    await page.evaluate(()=>{const actual=ensurePdfJs;window.ensurePdfJs=async()=>{const native=await actual();return{getDocument(options){const base=native.getDocument(options);window.ownedTask=base;return{promise:base.promise.then(doc=>new Proxy(doc,{get(target,key){if(key==='getPage')return async n=>{const p=await target.getPage(n);return new Proxy(p,{get(page,key){const value=page[key];if(key==='render')return function(...args){const task=value.apply(page,args);task.promise.then(()=>ownedRenders++,()=>{});return task;};return typeof value==='function'?value.bind(page):value;}});};const value=target[key];return typeof value==='function'?value.bind(target):value;}})),destroy:()=>base.destroy(),set onPassword(callback){base.onPassword=callback;}};}};};});
    await page.addScriptTag({content:setup});await page.evaluate(()=>setupPdfPreview(document.querySelector('main'),'owned.pdf'));
    await page.waitForFunction(()=>ownedAttempts.length||ownedRenders||document.querySelector('.pdf-error'));
    const attempts=await page.evaluate(()=>ownedAttempts);assert.deepEqual(attempts,[],'no oversized native canvas assignment');
    if(name==='invalid-box'){
     assert(await page.locator('.pdf-error').count(),'invalid page geometry reports a controlled error');
    }else{
     const info=await page.evaluate(()=>{const c=document.querySelector('.pdf-canvas'),pixel=Array.from(c.getContext('2d').getImageData(Math.floor(c.width/2),Math.floor(c.height/2),1,1).data);return{width:c.width,height:c.height,pixel,meta:document.querySelector('.pdf-meta').textContent,sizes:ownedSizes.length};});
     assert(info.width>0&&info.height>0&&info.width<=8192&&info.height<=8192&&info.width*info.height<=16777216);assert(Math.abs(info.pixel[2]-204)<=2&&info.pixel[3]===255,'actual bounded page pixels');
     if(name==='tall'||name==='high-dpr')assert.match(info.meta,/预览尺寸已受限/);
     if(name==='wide'){const before=await page.evaluate(()=>ownedRenders);await page.getByTitle('放大',{exact:true}).click();await page.waitForFunction(before=>ownedRenders>before,before);const after=await page.evaluate(()=>ownedRenders);await page.getByTitle('缩小',{exact:true}).click();await page.waitForFunction(before=>ownedRenders>before,after);await page.getByTitle('适应宽度',{exact:true}).click();}
     rows.push({name,status:'PASS',dpr,size,...info});
    }
    await page.getByTitle('关闭',{exact:true}).click();await page.waitForFunction(()=>ownedTask.destroyed);assert(await page.evaluate(()=>[...document.querySelectorAll('.pdf-canvas')].every(c=>c.width===0&&c.height===0)));assert.deepEqual(errors,[]);
    if(name==='invalid-box')rows.push({name,status:'PASS',controlledError:true});
   }finally{await context.close();}
  }
 }finally{await browser.close();fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(rows,null,2));}
 console.log(JSON.stringify(rows));
}
if(require.main===module)main().catch(e=>{console.error(e);process.exitCode=1;});
module.exports={fixture};
