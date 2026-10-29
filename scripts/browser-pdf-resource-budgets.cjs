'use strict';
const fs=require('node:fs'),path=require('node:path'),zlib=require('node:zlib'),assert=require('node:assert/strict');
const {chromium}=require('./browser-runtime/node_modules/playwright');
const web=path.resolve('internal/server/web'),app=fs.readFileSync(web+'/app.js','utf8');
const setup=app.slice(app.indexOf('async function setupPdfPreview('),app.indexOf('/* ── DXF 矢量渲染器'));
function fixture(mode){
 const large=mode==='large-image',width=large?5000:2,height=large?4000:2;
 const encoded=zlib.deflateSync(Buffer.from(large?[0]:[0,255,0,0,0,255,0,0,0,0,255,255,0,0]));
 const predictor=large?'':` /DecodeParms << /Predictor 12 /Columns ${mode==='large-row'?33554432:2} /Colors ${mode==='large-row'?4:3} /BitsPerComponent 8 >>`;
 const content=Buffer.from('q 150 0 0 150 10 10 cm /Im0 Do Q\n');
 const objects=[Buffer.from('<< /Type /Catalog /Pages 2 0 R >>'),Buffer.from('<< /Type /Pages /Count 1 /Kids [3 0 R] >>'),Buffer.from('<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >>'),Buffer.concat([Buffer.from(`<< /Type /XObject /Subtype /Image /Width ${width} /Height ${height} /ColorSpace /${large?'DeviceGray':'DeviceRGB'} /BitsPerComponent ${large?1:8} /Filter /FlateDecode${predictor} /Length ${encoded.length} >>\nstream\n`),encoded,Buffer.from('\nendstream')]),Buffer.concat([Buffer.from(`<< /Length ${content.length} >>\nstream\n`),content,Buffer.from('endstream')])];
 const parts=[Buffer.from('%PDF-1.4\n')],offsets=[0];let offset=parts[0].length;
 objects.forEach((body,i)=>{offsets.push(offset);const part=Buffer.concat([Buffer.from(`${i+1} 0 obj\n`),body,Buffer.from('\nendobj\n')]);parts.push(part);offset+=part.length;});
 parts.push(Buffer.from(`xref\n0 ${objects.length+1}\n0000000000 65535 f \n`+offsets.slice(1).map(n=>String(n).padStart(10,'0')+' 00000 n \n').join('')+`trailer\n<< /Size ${objects.length+1} /Root 1 0 R >>\nstartxref\n${offset}\n%%EOF`));return Buffer.concat(parts);
}
async function main(){
 const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert(out);fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({executablePath:'/usr/bin/chromium',args:['--no-sandbox','--disable-dev-shm-usage']});const rows=[];
 try{
  const context=await browser.newContext();
  for(const mode of ['normal','large-row','large-image']){
   const page=await context.newPage(),errors=[],messages=[];page.on('pageerror',e=>errors.push(e.message));page.on('console',m=>messages.push(m.text()));
   try{
    await page.route('**/*',route=>{
     const u=new URL(route.request().url());if(u.origin!=='https://aide-pdf-budgets.test')return route.abort();
     if(u.pathname==='/')return route.fulfill({contentType:'text/html; charset=utf-8',body:'<!doctype html><html data-theme="light"><meta charset="utf-8"><link rel="stylesheet" href="/themes/dark/tokens.css"><link rel="stylesheet" href="/themes/light/tokens.css"><link rel="stylesheet" href="/style.css"><style>dialog{margin:20px;background:white;color:#20242b}main{width:900px;height:650px}.pdf-viewer{height:600px}.pdf-scroll{height:500px;overflow:auto}</style><dialog open><main id="preview"></main></dialog>'});
     if(u.pathname==='/owned.pdf')return route.fulfill({contentType:'application/pdf',body:fixture(mode)});
     const p=path.resolve(web,'.'+u.pathname);if(!p.startsWith(web+path.sep)||!fs.existsSync(p)||!fs.statSync(p).isFile())return route.fulfill({status:404,body:''});
     return route.fulfill({contentType:p.endsWith('.css')?'text/css':'application/javascript',body:fs.readFileSync(p)});
    });
    await page.goto('https://aide-pdf-budgets.test/');
    await page.evaluate(async()=>{
     const native=await import('/vendor/pdfjs/pdf.min.mjs?v=2');native.GlobalWorkerOptions.workerSrc='/vendor/pdfjs/pdf.worker.min.mjs?v=2';window.nativePdf=native;
     window.el=(tag,cls,text)=>{const n=document.createElement(tag);n.className=cls||'';if(text!=null)n.textContent=text;return n;};window.t=s=>s;window.escapeHtml=s=>s;window.fileRawUrl=()=>'/owned.pdf';window.passwordPrompt=async()=>null;
     window.bindViewerTeardown=(host,fn)=>{window.ownedTeardown=fn;host.closest('dialog').addEventListener('close',fn);};
     window.ensurePdfJs=async()=>({getDocument(options){window.recordedOptions={maxImageSize:options.maxImageSize,canvasMaxAreaInBytes:options.canvasMaxAreaInBytes};const task=native.getDocument(options);window.ownedTask=task;task.promise.then(doc=>window.ownedDoc=doc);return task;}});
    });
    await page.addScriptTag({content:setup});await page.evaluate(()=>setupPdfPreview(document.querySelector('main'),'owned.pdf'));
    const info=await page.evaluate(async()=>{
     const pdfPage=await ownedDoc.getPage(1),op=await pdfPage.getOperatorList(),names=Object.fromEntries(Object.entries(nativePdf.OPS).map(([k,v])=>[v,k]));
     const imageObjects=[];for(let i=0;i<op.fnArray.length;i++)if(names[op.fnArray[i]]==='paintImageXObject'){const id=op.argsArray[i][0],value=await new Promise(resolve=>pdfPage.objs.get(id,resolve));imageObjects.push({id,empty:value===null,width:value?.width,height:value?.height,bytes:value?.data?.byteLength??(value?.bitmap?value.bitmap.width*value.bitmap.height*4:0)});}
     return{options:recordedOptions,imageOps:op.fnArray.map(n=>names[n]).filter(n=>/paint.*Image/.test(n)),imageObjects,pages:document.querySelectorAll('.pdf-page').length,note:document.querySelector('.pdf-info').title};
    });
    assert.equal(info.options.maxImageSize,16*1024*1024);assert.equal(info.options.canvasMaxAreaInBytes,32*1024*1024);assert.equal(info.pages,1);assert.match(info.note,/16 Mi/);
    if(mode==='normal'){
     assert(info.imageOps.length>0);await page.waitForFunction(()=>{const c=document.querySelector('canvas');if(!c.width)return false;const d=c.getContext('2d').getImageData(0,0,c.width,c.height).data;for(let i=0;i<d.length;i+=4)if(d[i]>200&&d[i+1]<60&&d[i+2]<60)return true;return false;});
     await page.screenshot({path:path.join(out,'normal.png')});
     await page.getByTitle('放大',{exact:true}).click();await page.getByTitle('适应宽度',{exact:true}).click();
    }else{if(mode==='large-image')assert.equal(info.imageOps.length,0,'over-budget image must be skipped before painting');else assert(info.imageObjects.every(o=>o.empty||(o.width===2&&o.height===2&&o.bytes<=16)),'invalid row permits only the declared tiny image fallback');assert(messages.some(m=>mode==='large-row'?/predictor.*preview limits/.test(m):/maximum allowed size/.test(m)),messages.join('\n'));}
    await page.getByTitle('关闭',{exact:true}).click();await page.waitForFunction(()=>ownedTask.destroyed);assert.deepEqual(errors,[]);rows.push({mode,status:'PASS',...info,messages:messages.filter(m=>/predictor|maximum allowed size/.test(m))});
   }finally{await page.close();}
  }
  await context.close();
 }finally{await browser.close();fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(rows,null,2));}
 console.log(JSON.stringify(rows));
}
if(require.main===module)main().catch(e=>{console.error(e);process.exitCode=1;});
module.exports={fixture};
