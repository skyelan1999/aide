'use strict';
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require('./browser-runtime/node_modules/playwright'),{lzw,header}=require('./pdf-decoder-followup-review.cjs');
const web=path.resolve('internal/server/web'),app=fs.readFileSync(web+'/app.js','utf8');
const setup=app.slice(app.indexOf('async function setupPdfPreview('),app.indexOf('/* ── DXF 矢量渲染器'));
const ensure=app.slice(app.indexOf('let _pdfjsPromise = null;'),app.indexOf('async function setupPdfPreview('));
function expandingLzw(){let width=9,next=258,previous=false,buffer=0,bits=0,decoded=0;const out=[];function emit(code){buffer=(buffer<<width)|code;bits+=width;while(bits>=8){bits-=8;out.push((buffer>>>bits)&255);}if(code===256){width=9;next=258;previous=false;return;}if(previous){next++;if(((next+1)&next)===0)width=Math.min(Math.log2(next+1)+1,12);}previous=true;}emit(256);emit(32);decoded++;for(let code=258;code<=4095;code++){emit(code);decoded+=code-256;}while(decoded<=67118864){emit(4095);decoded+=3839;}emit(257);if(bits)out.push((buffer<<(8-bits))&255);return Buffer.from(out);}
function fixture(mode){
 let content=Buffer.from('1 0 0 rg 10 10 100 100 re f\n'),contentFilter='',image=null;
 if(mode==='lzw-normal'){content=Buffer.from(lzw(content));contentFilter=' /Filter /LZWDecode';}
 if(mode==='lzw-large'){content=expandingLzw();contentFilter=' /Filter /LZWDecode';}
 if(mode.startsWith('indexed-')||mode==='jbig2-extended'){
  const pageInfo=Buffer.alloc(19);pageInfo.writeUInt32BE(1,0);pageInfo.writeUInt32BE(1,4);pageInfo[16]=4;
  const bytes=mode==='jbig2-extended'?Buffer.concat([Buffer.from(header(0,1,false,19,48)),pageInfo,Buffer.from(header(8,2))]):Buffer.from([0]);
  const imageProps=mode==='jbig2-extended'?'/ColorSpace /DeviceGray /BitsPerComponent 1 /Filter /JBIG2Decode':`/ColorSpace [/Indexed /DeviceRGB ${mode==='indexed-large'?100000000:0} <ff0000>] /BitsPerComponent 8`;
  image=Buffer.concat([Buffer.from(`<< /Type /XObject /Subtype /Image /Width 1 /Height 1 ${imageProps} /Length ${bytes.length} >>\nstream\n`),bytes,Buffer.from('\nendstream')]);content=Buffer.from('q 100 0 0 100 10 10 cm /Im0 Do Q\n');
 }
 if(mode==='inline-atlas')content=Buffer.concat(Array.from({length:10},(_,i)=>Buffer.concat([Buffer.from(`q 10 0 0 10 ${10+i*15} 10 cm BI /W 1 /H 1 /CS /RGB /BPC 8 ID `),Buffer.from([255,0,0]),Buffer.from(' EI Q\n')])));
 const objects=[Buffer.from('<< /Type /Catalog /Pages 2 0 R >>'),Buffer.from('<< /Type /Pages /Count 1 /Kids [3 0 R] >>'),Buffer.from(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << ${image?'/XObject << /Im0 5 0 R >>':''} >> /Contents 4 0 R >>`),Buffer.concat([Buffer.from(`<< /Length ${content.length}${contentFilter} >>\nstream\n`),content,Buffer.from('\nendstream')])];if(image)objects.push(image);
 const parts=[Buffer.from('%PDF-1.4\n')],offsets=[0];let offset=parts[0].length;objects.forEach((body,i)=>{offsets.push(offset);const part=Buffer.concat([Buffer.from(`${i+1} 0 obj\n`),body,Buffer.from('\nendobj\n')]);parts.push(part);offset+=part.length;});parts.push(Buffer.from(`xref\n0 ${objects.length+1}\n0000000000 65535 f \n`+offsets.slice(1).map(n=>String(n).padStart(10,'0')+' 00000 n \n').join('')+`trailer\n<< /Size ${objects.length+1} /Root 1 0 R >>\nstartxref\n${offset}\n%%EOF`));return Buffer.concat(parts);
}
async function assertEventually(check){for(let i=0;i<100;i++){if(check())return;await new Promise(resolve=>setTimeout(resolve,50));}assert(check(),'actual display optimizer grouped inline images');}
async function main(){
 const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert(out);fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({executablePath:'/usr/bin/chromium',args:['--no-sandbox','--disable-dev-shm-usage']});const rows=[];
 try{
  const context=await browser.newContext();
  for(const mode of (process.env.AIDE_PDF_BROWSER_CASES?.split(',')||['lzw-normal','lzw-large','jbig2-extended','indexed-normal','indexed-large','inline-atlas'])){
   const page=await context.newPage(),errors=[],messages=[];page.on('pageerror',e=>errors.push(e.message));page.on('console',m=>messages.push(m.text()));
   try{
    await page.route('**/*',route=>{
     const u=new URL(route.request().url());if(u.origin!=='https://aide-pdf-budgets.test')return route.abort();
     if(u.pathname==='/')return route.fulfill({contentType:'text/html; charset=utf-8',body:'<!doctype html><html data-theme="light"><meta charset="utf-8"><link rel="stylesheet" href="/themes/dark/tokens.css"><link rel="stylesheet" href="/themes/light/tokens.css"><link rel="stylesheet" href="/style.css"><style>dialog{margin:20px;background:white;color:#20242b}main{width:900px;height:650px}.pdf-viewer{height:600px}.pdf-scroll{height:500px;overflow:auto}</style><dialog open><main id="preview"></main></dialog>'});
     if(u.pathname==='/owned.pdf')return route.fulfill({contentType:'application/pdf',body:fixture(mode)});
     const p=path.resolve(web,'.'+u.pathname);if(!p.startsWith(web+path.sep)||!fs.existsSync(p)||!fs.statSync(p).isFile())return route.fulfill({status:404,body:''});
                let body=fs.readFileSync(p);if(u.pathname.endsWith('/pdf.worker.min.mjs')&&mode==='lzw-large')body=Buffer.concat([body,Buffer.from('\nconst ownedEnsure=LZWStream.prototype.ensureBuffer;LZWStream.prototype.ensureBuffer=function(n){try{return ownedEnsure.call(this,n)}catch(error){console.info("owned LZW limit: "+error.message);throw error}};')]);
                if(u.pathname.endsWith('/pdf.worker.min.mjs')&&mode==='inline-atlas')body=Buffer.from(body.toString().replace('if(o<10)return t-(t-r)%4;let c=0;', 'if(o<10)return t-(t-r)%4;console.info("owned atlas group: "+o);let c=0;'));
                return route.fulfill({contentType:p.endsWith('.css')?'text/css':'application/javascript',body});
    });
    await page.goto('https://aide-pdf-budgets.test/');
    await page.evaluate(()=>{
     window.el=(tag,cls,text)=>{const n=document.createElement(tag);n.className=cls||'';if(text!=null)n.textContent=text;return n;};window.t=s=>s;window.escapeHtml=s=>s;window.fileRawUrl=()=>'/owned.pdf';window.passwordPrompt=async()=>null;
     window.bindViewerTeardown=(host,fn)=>{window.ownedTeardown=fn;host.closest('dialog').addEventListener('close',fn);};
    });
    await page.addScriptTag({content:ensure});
    await page.evaluate(()=>{const actual=ensurePdfJs;window.ensurePdfJs=async()=>{const native=await actual();window.nativePdf=native;return{getDocument(options){window.recordedOptions={maxImageSize:options.maxImageSize,canvasMaxAreaInBytes:options.canvasMaxAreaInBytes};const task=native.getDocument(options);window.ownedTask=task;task.promise.then(doc=>window.ownedDoc=doc,()=>{});return task;}};};});
    await page.addScriptTag({content:setup});await page.evaluate(()=>setupPdfPreview(document.querySelector('main'),'owned.pdf'));
    const info=await page.evaluate(async()=>{
     const pdfPage=await ownedDoc.getPage(1),op=await pdfPage.getOperatorList(),names=Object.fromEntries(Object.entries(nativePdf.OPS).map(([k,v])=>[v,k]));
     const imageObjects=[];for(let i=0;i<op.fnArray.length;i++)if(names[op.fnArray[i]]==='paintImageXObject'){const id=op.argsArray[i][0],value=await new Promise(resolve=>pdfPage.objs.get(id,resolve));imageObjects.push({id,empty:value===null,width:value?.width,height:value?.height,bytes:value?.data?.byteLength??(value?.bitmap?value.bitmap.width*value.bitmap.height*4:0)});}
     return{options:recordedOptions,imageOps:op.fnArray.map(n=>names[n]).filter(n=>/paint.*Image/.test(n)),imageObjects,pages:document.querySelectorAll('.pdf-page').length,note:document.querySelector('.pdf-info').title};
    });
    assert.equal(info.pages,1);assert.equal(info.options.maxImageSize,16777216);
    if(mode==='lzw-large')assert(messages.some(m=>/LZW decoded stream exceeds preview limit/.test(m)),messages.join('\n'));
    else if(mode==='jbig2-extended'){assert(info.imageObjects.some(image=>!image.empty&&image.width===1&&image.height===1),'extended JBIG2 image decoded');assert(!messages.some(m=>/Unable to decode/.test(m)),messages.join('\n'));}
    else if(mode==='indexed-large')assert(messages.some(m=>/Indexed color-space lookup size/.test(m)),messages.join('\n'));
    else {
     await page.waitForFunction(()=>{const c=document.querySelector('canvas');if(!c?.width)return false;const data=c.getContext('2d').getImageData(0,0,c.width,c.height).data;for(let i=0;i<data.length;i+=4)if(data[i]>180&&data[i+1]<100&&data[i+2]<100)return true;return false;});
     if(mode==='inline-atlas'){await assertEventually(()=>messages.some(m=>/owned atlas group: 10/.test(m)));info.optimizedRenderGroup=10;}
     await page.screenshot({path:path.join(out,mode+'.png')});
    }
    await page.getByTitle('关闭',{exact:true}).click();await page.waitForFunction(()=>ownedTask.destroyed);assert.deepEqual(errors,[]);rows.push({mode,status:'PASS',...info,messages:messages.filter(m=>/LZW|JBIG2|Jbig2|Indexed|page information|pageInfo/.test(m))});
   }finally{await page.close();}
  }
  await context.close();
 }finally{await browser.close();fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(rows,null,2));}
 console.log(JSON.stringify(rows));
}
if(require.main===module)main().catch(e=>{console.error(e);process.exitCode=1;});
module.exports={fixture,expandingLzw};
