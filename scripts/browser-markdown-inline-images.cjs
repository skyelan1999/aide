'use strict';
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict'),{chromium}=require('./browser-runtime/node_modules/playwright');
(async()=>{const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert(out);fs.mkdirSync(out,{recursive:true});const browser=await chromium.launch({executablePath:'/usr/bin/chromium',args:['--no-sandbox','--disable-dev-shm-usage']});try{
 const page=await browser.newPage(),errors=[],connections=[];page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>{if(r.url().startsWith('data:'))connections.push(r.url().slice(0,30))});
 await page.route('**/*',r=>{const u=new URL(r.request().url());if(u.origin!=='https://aide-inline-owned.test')return r.abort();if(u.pathname==='/')return r.fulfill({contentType:'text/html',headers:{'Content-Security-Policy':"default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data: blob:; object-src 'none'"},body:'<!doctype html><meta charset="utf-8"><title>Owned inline-image regression</title>'});const web=path.resolve('internal/server/web'),file=u.pathname==='/export.js'?(process.env.AIDE_EXPORT_BASELINE||web+'/markdown-export.js'):u.pathname==='/canvas.js'?web+'/vendor/markdown-export/html2canvas.min.js':u.pathname==='/pdf.js'?web+'/vendor/markdown-export/jspdf.umd.min.js':null;return file?r.fulfill({contentType:'application/javascript',body:fs.readFileSync(file)}):r.fulfill({status:404,body:''})});
 await page.goto('https://aide-inline-owned.test/');for(const url of ['/canvas.js','/pdf.js','/export.js'])await page.addScriptTag({url});
 const rows=await page.evaluate(async()=>{
  const png='iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==',svg='<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24"><rect width="24" height="24" fill="red"/><desc>你好 ♥ ?</desc></svg>';
  const cases=[['base64-png','data:image/png;base64,'+png],['escaped-base64-png','data:image/png;base64,'+png.replaceAll('=','%3D')],['percent-svg','data:image/svg+xml;charset=utf-8,'+encodeURIComponent(svg)],['raw-svg','data:image/svg+xml,'+svg]];
  const results=[],createURL=URL.createObjectURL,click=HTMLAnchorElement.prototype.click;let lastBlob,downloads;
  URL.createObjectURL=blob=>{lastBlob=blob;return createURL.call(URL,blob)};HTMLAnchorElement.prototype.click=function(){downloads.push(this.download)};
  try{
   for(const [name,source]of cases)for(const format of ['png','pdf']){
    downloads=[];lastBlob=null;const host=document.createElement('div'),img=document.createElement('img');img.src=source;img.width=24;img.height=24;host.append(img);let result;
    try{await AideMarkdownExport.run(format,{html:'<h1>Owned inline image</h1>'+host.innerHTML,markdown:'owned',title:'inline.md',valid:()=>true,load:async()=>{}});
     const blob=lastBlob,bytes=new Uint8Array(await blob.slice(0,8).arrayBuffer());let redPixels=null;
     if(format==='png'){const image=new Image(),url=createURL.call(URL,blob);image.src=url;await image.decode();const canvas=document.createElement('canvas');canvas.width=image.naturalWidth;canvas.height=image.naturalHeight;const ctx=canvas.getContext('2d');ctx.drawImage(image,0,0);const pixels=ctx.getImageData(0,0,canvas.width,canvas.height).data;redPixels=0;for(let i=0;i<pixels.length;i+=4)if(pixels[i]>200&&pixels[i+1]<30&&pixels[i+2]<30)redPixels++;canvas.width=canvas.height=0;URL.revokeObjectURL(url);}
     const signature=format==='png'?bytes[0]===137&&bytes[1]===80:bytes[0]===37&&bytes[1]===80;
     result={name,format,pass:signature&&downloads.length===1&&(format!=='png'||redPixels>100),size:blob.size,redPixels,downloads};
    }catch(e){result={name,format,pass:false,error:e.message}}
    results.push(result);
   }
   // Invalid data must fail without a download or surviving temporary frames.
   for(const value of ['data:text/html,owned','data:image/png;base64,@@@@']){downloads=[];let rejected=false;try{await AideMarkdownExport.run('png',{html:'<img src="'+value+'">',markdown:'owned',title:'bad.md',valid:()=>true,load:async()=>{}})}catch(_){rejected=true}results.push({name:'invalid-inline',format:'png',pass:rejected&&downloads.length===0&&document.querySelectorAll('iframe').length===0});}
   return results;
  }finally{URL.createObjectURL=createURL;HTMLAnchorElement.prototype.click=click;AideMarkdownExport.close();}
 });
 fs.writeFileSync(path.join(out,'result.json'),JSON.stringify({rows,errors},null,2)+'\n');console.log(JSON.stringify({rows,errors}));if(!process.env.AIDE_EXPECT_INLINE_FAILURES){assert(rows.every(r=>r.pass));assert.deepEqual(errors,[])}
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exitCode=1});
