'use strict';
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require('./browser-runtime/node_modules/playwright');
const web=path.resolve('internal/server/web'),app=fs.readFileSync(web+'/app.js','utf8');
const setup=app.slice(app.indexOf('async function setupPdfPreview('),app.indexOf('/* ── DXF 矢量渲染器'));
const ensure=app.slice(app.indexOf('let _pdfjsPromise = null;'),app.indexOf('async function setupPdfPreview('));
function fixture(mode){
 let content,extra;
 const alias=mode.startsWith('alias-');
 if(alias){content=Buffer.concat([Buffer.from('q 100 0 0 100 10 10 cm BI /W 1 /H 1 /CS /Alias /BPC 8 ID '),Buffer.from([128]),Buffer.from(' EI Q\n')]);extra=Buffer.from('null');}
 else content=Buffer.from('/Sh1 sh\n');
 let functionData,fnProps;
 if(mode.startsWith('ps-')){const depth=mode==='ps-depth'?4000:0;functionData=Buffer.from('{ '+'true { '.repeat(depth)+'0.8 0.2 0.1 '+'} if '.repeat(depth)+'}');fnProps='/FunctionType 4';}
 else{functionData=mode==='sample32'?Buffer.from([0,0,0,0,0,0,0,0,0,0,0,0,255,255,255,255,0,0,0,0,0,0,0,0]):Buffer.from([255,0,0,0,0,255]);fnProps=`/FunctionType 0 /Size [${mode==='sample-large'?1000000:2}] /BitsPerSample ${mode==='sample32'?32:8}`;}
 const resources=alias?`/ColorSpace << /Alias /${mode==='alias-cycle'?'Alias':'DeviceGray'} >>`:'/Shading << /Sh1 5 0 R >>';
 const objects=[Buffer.from('<< /Type /Catalog /Pages 2 0 R >>'),Buffer.from('<< /Type /Pages /Count 1 /Kids [3 0 R] >>'),Buffer.from(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << ${resources} >> /Contents 4 0 R >>`),Buffer.concat([Buffer.from(`<< /Length ${content.length} >>\nstream\n`),content,Buffer.from('\nendstream')])];
 if(!alias)objects.push(Buffer.from('<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 200 0] /Function 6 0 R /Extend [true true] >>'),Buffer.concat([Buffer.from(`<< ${fnProps} /Domain [0 1] /Range [0 1 0 1 0 1] /Length ${functionData.length} >>\nstream\n`),functionData,Buffer.from('\nendstream')]));
 const parts=[Buffer.from('%PDF-1.4\n')],offsets=[0];let offset=parts[0].length;objects.forEach((body,i)=>{offsets.push(offset);const part=Buffer.concat([Buffer.from(`${i+1} 0 obj\n`),body,Buffer.from('\nendobj\n')]);parts.push(part);offset+=part.length;});parts.push(Buffer.from(`xref\n0 ${objects.length+1}\n0000000000 65535 f \n`+offsets.slice(1).map(n=>String(n).padStart(10,'0')+' 00000 n \n').join('')+`trailer\n<< /Size ${objects.length+1} /Root 1 0 R >>\nstartxref\n${offset}\n%%EOF`));return Buffer.concat(parts);
}
async function main(){
 const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert(out);fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({executablePath:'/usr/bin/chromium',args:['--no-sandbox','--disable-dev-shm-usage']});const rows=[];
 try{
  const context=await browser.newContext();
  for(const mode of (process.env.AIDE_PDF_BROWSER_CASES?.split(',')||['alias-normal','alias-cycle','sample-normal','sample-large','ps-normal','ps-depth','sample32'])){
   const page=await context.newPage(),errors=[],messages=[];page.on('pageerror',e=>errors.push(e.message));page.on('console',m=>messages.push(m.text()));
   try{
    await page.route('**/*',route=>{
     const u=new URL(route.request().url());if(u.origin!=='https://aide-pdf-budgets.test')return route.abort();
     if(u.pathname==='/')return route.fulfill({contentType:'text/html; charset=utf-8',body:'<!doctype html><html data-theme="light"><meta charset="utf-8"><link rel="stylesheet" href="/themes/dark/tokens.css"><link rel="stylesheet" href="/themes/light/tokens.css"><link rel="stylesheet" href="/style.css"><style>dialog{margin:20px;background:white;color:#20242b}main{width:900px;height:650px}.pdf-viewer{height:600px}.pdf-scroll{height:500px;overflow:auto}</style><dialog open><main id="preview"></main></dialog>'});
     if(u.pathname==='/owned.pdf')return route.fulfill({contentType:'application/pdf',body:fixture(mode)});
     const p=path.resolve(web,'.'+u.pathname);if(!p.startsWith(web+path.sep)||!fs.existsSync(p)||!fs.statSync(p).isFile())return route.fulfill({status:404,body:''});
                return route.fulfill({contentType:p.endsWith('.css')?'text/css':'application/javascript',body:fs.readFileSync(u.pathname.endsWith('/pdf.worker.min.mjs')&&process.env.AIDE_PDF_FUNCTION_BASELINE||p)});
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
    assert.equal(info.pages,1);assert(!messages.some(message=>/RangeError|Maximum call stack/.test(message)),messages.join('\n'));
    if(['alias-cycle','sample-large','ps-depth'].includes(mode)){
     const message=mode==='alias-cycle'?/Cyclic ColorSpace/:mode==='sample-large'?/Sampled function.*preview limit/:/PostScript conditional nesting.*preview limit/;assert(messages.some(m=>message.test(m)),messages.join('\n'));
    }else{
     const expected=mode==='alias-normal'?[128,128,128,255]:mode==='ps-normal'?[204,51,26,255]:mode==='sample32'?[128,0,0,255]:[127,0,128,255];
     await page.waitForFunction(({mode,expected})=>{const c=document.querySelector('.pdf-canvas');if(!c?.width)return false;const pixel=c.getContext('2d').getImageData(Math.floor(c.width*(mode==='alias-normal'?0.3:0.5)),Math.floor(c.height*(mode==='alias-normal'?0.7:0.5)),1,1).data;return expected.every((value,i)=>Math.abs(value-pixel[i])<=5);},{mode,expected});
     info.expectedPixel=expected;await page.screenshot({path:path.join(out,mode+'.png')});
    }
    await page.getByTitle('关闭',{exact:true}).click();await page.waitForFunction(()=>ownedTask.destroyed);assert.deepEqual(errors,[]);rows.push({mode,status:'PASS',...info,messages:messages.filter(m=>/Cyclic|Sampled|PostScript|RangeError/.test(m))});
   }finally{await page.close();}
  }
  await context.close();
 }finally{await browser.close();fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(rows,null,2));}
 console.log(JSON.stringify(rows));
}
if(require.main===module)main().catch(e=>{console.error(e);process.exitCode=1;});
module.exports={fixture};
