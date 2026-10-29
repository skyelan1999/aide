'use strict';
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require('./browser-runtime/node_modules/playwright');
const web=path.resolve('internal/server/web'),app=fs.readFileSync(web+'/app.js','utf8');
const setup=app.slice(app.indexOf('async function setupPdfPreview('),app.indexOf('/* ── DXF 矢量渲染器'));
const ensure=app.slice(app.indexOf('let _pdfjsPromise = null;'),app.indexOf('async function setupPdfPreview('));
function fixture(mode){
 const font=!mode.startsWith('squiggle-'),cmap=mode.startsWith('cmap-'),bad=mode.endsWith('-large'),vertical=mode==='cid-vertical-large';
 const stream=(props,data)=>Buffer.from(`<< ${props} /Length ${Buffer.byteLength(data)} >>\nstream\n${data}\nendstream`);
 const text=cmap?'<0000004100000042>':'<00410042>';
 const content='0 0 1 rg 0 0 100 100 re f'+(font?` BT /F0 20 Tf 10 40 Td ${text} Tj ET`:'');
 const objects=[Buffer.from('<< /Type /Catalog /Pages 2 0 R >>'),Buffer.from('<< /Type /Pages /Count 1 /Kids [3 0 R] >>'),Buffer.from(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << ${font?'/Font << /F0 5 0 R >>':''} >> ${font?'':'/Annots [5 0 R]'} /Contents 4 0 R >>`),stream('',content)];
 if(!font){const q=bad?'100000000000000000000 30 200000000000000000000 30 100000000000000000000 10 200000000000000000000 10':'10 30 90 30 10 10 90 10';objects.push(Buffer.from(`<< /Type /Annot /Subtype /Squiggly /Rect [10 10 90 30] /QuadPoints [${q}] /C [1 0 0] /F 4 >>`));}
 else{
  const range=bad?10000000:65535;
  objects.push(Buffer.from(`<< /Type /Font /Subtype /Type0 /BaseFont /Helvetica /Encoding ${cmap?'8 0 R':vertical?'/Identity-V':'/Identity-H'} /DescendantFonts [6 0 R] >>`),Buffer.from(`<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Helvetica /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 7 0 R /CIDToGIDMap /Identity /DW 500 ${vertical?'/W2 [0 '+range+' -1000 250 880]':'/W [0 '+(cmap?65535:range)+' 500]'} >>`),Buffer.from('<< /Type /FontDescriptor /FontName /Helvetica /Flags 4 /FontBBox [0 0 1000 1000] /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >>'));
  if(cmap)objects.push(stream('',`begincmap /CMapName /Owned def /CMapType 1 def 1 begincodespacerange <00000000> <00FFFFFF> endcodespacerange 1 begincidrange <00000000> <${bad?'00FFFFFF':'0000FFFF'}> 0 endcidrange endcmap`));
 }
 const parts=[Buffer.from('%PDF-1.4\n')],offsets=[0];let offset=parts[0].length;objects.forEach((body,i)=>{offsets.push(offset);const part=Buffer.concat([Buffer.from(`${i+1} 0 obj\n`),body,Buffer.from('\nendobj\n')]);parts.push(part);offset+=part.length;});parts.push(Buffer.from(`xref\n0 ${objects.length+1}\n0000000000 65535 f \n`+offsets.slice(1).map(n=>String(n).padStart(10,'0')+' 00000 n \n').join('')+`trailer\n<< /Size ${objects.length+1} /Root 1 0 R >>\nstartxref\n${offset}\n%%EOF`));return Buffer.concat(parts);
}
async function main(){
 const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert(out);fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({executablePath:'/usr/bin/chromium',args:['--no-sandbox','--disable-dev-shm-usage']});const rows=[];
 try{
  const context=await browser.newContext();
  for(const mode of (process.env.AIDE_PDF_BROWSER_CASES?.split(',')||['cid-normal','cid-large','cid-vertical-large','cmap-normal','cmap-large','squiggle-normal','squiggle-large'])){
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
    await page.evaluate(()=>{
     window.el=(tag,cls,text)=>{const n=document.createElement(tag);n.className=cls||'';if(text!=null)n.textContent=text;return n;};window.t=s=>s;window.escapeHtml=s=>s;window.fileRawUrl=()=>'/owned.pdf';window.passwordPrompt=async()=>null;
     window.bindViewerTeardown=(host,fn)=>{window.ownedTeardown=fn;host.closest('dialog').addEventListener('close',fn);};
    });
    await page.addScriptTag({content:ensure});
    await page.evaluate(()=>{const actual=ensurePdfJs;window.ensurePdfJs=async()=>{const native=await actual();window.nativePdf=native;return{getDocument(options){window.recordedOptions={maxImageSize:options.maxImageSize,canvasMaxAreaInBytes:options.canvasMaxAreaInBytes};const task=native.getDocument(options);window.ownedTask=task;task.promise.then(doc=>window.ownedDoc=doc,()=>{});return task;}};};});
    await page.addScriptTag({content:setup});await page.evaluate(()=>setupPdfPreview(document.querySelector('main'),'owned.pdf'));
    const info=await page.evaluate(async()=>{
     const pdfPage=await ownedDoc.getPage(1),op=await pdfPage.getOperatorList(),names=Object.fromEntries(Object.entries(nativePdf.OPS).map(([k,v])=>[v,k]));
     return{options:recordedOptions,operations:op.fnArray.length,operationNames:op.fnArray.map(n=>names[n]),pages:document.querySelectorAll('.pdf-page').length,note:document.querySelector('.pdf-info').title};
    });
    assert.equal(info.pages,1);assert(!messages.some(message=>/RangeError|Maximum call stack/.test(message)),messages.join('\n'));
    assert(info.operations<3000,JSON.stringify(info));
    if(mode.endsWith('-large')){
     const message=mode.startsWith('cid-')?/CID width expansion/:mode.startsWith('cmap-')?/CMap expansion/:/Squiggly annotation exceeds/;assert(messages.some(m=>message.test(m)),messages.join('\n'));
    }
    await page.waitForFunction(()=>{const c=document.querySelector('.pdf-canvas');if(!c?.width)return false;const p=c.getContext('2d').getImageData(Math.floor(c.width/2),Math.floor(c.height/2),1,1).data;return p[0]<10&&p[1]<10&&p[2]>245&&p[3]===255;});
    info.expectedBackground=[0,0,255,255];
    if(mode==='squiggle-normal')assert(info.operationNames.includes('beginAnnotation'));
    if(mode.endsWith('-normal'))await page.screenshot({path:path.join(out,mode+'.png')});
    await page.getByTitle('关闭',{exact:true}).click();await page.waitForFunction(()=>ownedTask.destroyed);assert.deepEqual(errors,[]);rows.push({mode,status:'PASS',...info,messages:messages.filter(m=>/CID width|CMap expansion|Squiggly|RangeError/.test(m))});
   }finally{await page.close();}
  }
  await context.close();
 }finally{await browser.close();fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(rows,null,2));}
 console.log(JSON.stringify(rows));
}
if(require.main===module)main().catch(e=>{console.error(e);process.exitCode=1;});
module.exports={fixture};
