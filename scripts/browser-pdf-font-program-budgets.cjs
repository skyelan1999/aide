'use strict';
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require('./browser-runtime/node_modules/playwright'),{cffProgram,cffCharset,cidCff,type1Font}=require('./pdf-font-program-budget-review.cjs');
const web=path.resolve('internal/server/web'),app=fs.readFileSync(web+'/app.js','utf8');
const setup=app.slice(app.indexOf('async function setupPdfPreview('),app.indexOf('/* ── DXF 矢量渲染器'));
const ensure=app.slice(app.indexOf('let _pdfjsPromise = null;'),app.indexOf('async function setupPdfPreview('));
function fixture(mode){
 const inline=mode.startsWith('inline-'),cid=mode.startsWith('fdselect-'),ttf=mode.startsWith('ttf-'),type1=mode.startsWith('type1-');
 const stream=(props,data)=>{const bytes=Buffer.isBuffer(data)?data:Buffer.from(data);return Buffer.concat([Buffer.from(`<< ${props} /Length ${bytes.length} >>\nstream\n`),bytes,Buffer.from('\nendstream')]);};
 const content=inline?Buffer.concat([Buffer.from('q 100 0 0 100 0 0 cm BI /W 1 /H 1 /CS /RGB /BPC 8 %'+'x'.repeat(mode==='inline-long'?256*1024:0)+'\nID '),Buffer.from([255,0,0]),Buffer.from(' EI Q\n')]):Buffer.from('0 0 1 rg 0 0 100 100 re f 0 0 0 rg BT /F0 40 Tf 10 40 Td '+(cid?'<0001>':'(A)')+' Tj ET');
 const objects=[Buffer.from('<< /Type /Catalog /Pages 2 0 R >>'),Buffer.from('<< /Type /Pages /Count 1 /Kids [3 0 R] >>'),Buffer.from(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << ${inline?'':'/Font << /F0 5 0 R >>'} >> /Contents 4 0 R >>`),stream('',content)];
 if(!inline){
  let bytes,subtype='Type1',file='FontFile3',fileProps='/Subtype /Type1C',name='Test';
  if(mode.startsWith('charset-'))bytes=cffCharset(mode==='charset-normal');
  else if(cid){bytes=cidCff(mode==='fdselect-normal');name='CIDTest';fileProps='/Subtype /CIDFontType0C';}
  else if(mode.startsWith('type2-'))bytes=cffProgram([...Array(mode==='type2-stack'?49:48).fill(139),1,14]);
  else if(ttf){bytes=Buffer.from(JSON.parse(fs.readFileSync('scripts/fixtures/pdf-ttf-glyphs.json','utf8'))[mode==='ttf-cycle'?'cyclic':'normal'],'base64');subtype='TrueType';file='FontFile2';fileProps='';name='AideSyntheticGlyph';}
  else{const font=type1Font(mode==='type1-cycle');bytes=font.bytes;file='FontFile';fileProps=`/Length1 ${font.length1} /Length2 ${font.length2}`;name='TinyType1';}
  if(cid)objects.push(Buffer.from('<< /Type /Font /Subtype /Type0 /BaseFont /CIDTest /Encoding /Identity-H /DescendantFonts [6 0 R] >>'),Buffer.from('<< /Type /Font /Subtype /CIDFontType0 /BaseFont /CIDTest /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /DW 500 /FontDescriptor 7 0 R >>'),Buffer.from('<< /Type /FontDescriptor /FontName /CIDTest /Flags 4 /FontBBox [0 -200 1000 800] /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 /FontFile3 8 0 R >>'),stream(fileProps,bytes));
  else objects.push(Buffer.from(`<< /Type /Font /Subtype /${subtype} /BaseFont /${name} /FirstChar 65 /LastChar 65 /Widths [500] /Encoding ${ttf||type1?'/WinAnsiEncoding':'<< /Type /Encoding /Differences [65 /space] >>'} /FontDescriptor 6 0 R >>`),Buffer.from(`<< /Type /FontDescriptor /FontName /${name} /Flags 32 /FontBBox [0 -200 1000 800] /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 /${file} 7 0 R >>`),stream(fileProps,bytes));
 }
 const parts=[Buffer.from('%PDF-1.4\n')],offsets=[0];let offset=parts[0].length;objects.forEach((body,i)=>{offsets.push(offset);const part=Buffer.concat([Buffer.from(`${i+1} 0 obj\n`),body,Buffer.from('\nendobj\n')]);parts.push(part);offset+=part.length;});parts.push(Buffer.from(`xref\n0 ${objects.length+1}\n0000000000 65535 f \n`+offsets.slice(1).map(n=>String(n).padStart(10,'0')+' 00000 n \n').join('')+`trailer\n<< /Size ${objects.length+1} /Root 1 0 R >>\nstartxref\n${offset}\n%%EOF`));return Buffer.concat(parts);
}
async function main(){
 const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert(out);fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({executablePath:'/usr/bin/chromium',args:['--no-sandbox','--disable-dev-shm-usage']});const rows=[];
 try{
  const context=await browser.newContext();
  for(const mode of (process.env.AIDE_PDF_BROWSER_CASES?.split(',')||['inline-normal','inline-long','charset-normal','charset-truncated','fdselect-normal','fdselect-invalid','type2-normal','type2-stack','type1-normal','type1-cycle','ttf-normal','ttf-cycle'])){
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
    await page.evaluate(()=>{const actual=ensurePdfJs;window.ensurePdfJs=async()=>{const native=await actual();window.nativePdf=native;return{getDocument(options){window.recordedOptions={maxImageSize:options.maxImageSize,canvasMaxAreaInBytes:options.canvasMaxAreaInBytes};const task=native.getDocument({...options,disableFontFace:!window.fixtureMode.startsWith('inline-')});window.ownedTask=task;task.promise.then(doc=>window.ownedDoc=doc,()=>{});return task;}};};});
    await page.addScriptTag({content:setup});await page.evaluate(()=>setupPdfPreview(document.querySelector('main'),'owned.pdf'));
    const info=await page.evaluate(async()=>{
     const pdfPage=await ownedDoc.getPage(1),op=await pdfPage.getOperatorList(),names=Object.fromEntries(Object.entries(nativePdf.OPS).map(([k,v])=>[v,k]));
     return{options:recordedOptions,operations:op.fnArray.length,operationNames:op.fnArray.map(n=>names[n]),forcedFontFallback:!window.fixtureMode.startsWith('inline-'),pages:document.querySelectorAll('.pdf-page').length,note:document.querySelector('.pdf-info').title};
    });
    assert.equal(info.pages,1);assert(!messages.some(message=>/RangeError|Maximum call stack/.test(message)),messages.join('\n'));
    assert(info.operations<3000,JSON.stringify(info));
    const expectedWarning={'charset-truncated':/Truncated CFF charset/,'fdselect-invalid':/parseFDSelect/,'type2-stack':/Type2 CharString exceeds operand/,'type1-cycle':/Type1 CharString recursive/,'ttf-cycle':/Recursive or excessive TrueType/}[mode];
    if(expectedWarning)assert(messages.some(m=>expectedWarning.test(m)),messages.join('\n'));
    else assert(!messages.some(m=>/translateFont failed|Unable to compile|ignoring.*glyph|RangeError/.test(m)),messages.join('\n'));
    const expected=mode.startsWith('inline-')?[255,0,0,255]:[0,0,255,255];
    await page.waitForFunction(expected=>{const c=document.querySelector('.pdf-canvas');if(!c?.width)return false;const p=c.getContext('2d').getImageData(Math.floor(c.width/2),Math.floor(c.height/2),1,1).data;return expected.every((v,i)=>Math.abs(v-p[i])<5);},expected);
    info.expectedBackground=expected;
    if(mode==='ttf-normal'||mode==='type1-normal')await page.waitForFunction(()=>{const c=document.querySelector('.pdf-canvas'),p=c.getContext('2d').getImageData(Math.floor(c.width*.2),Math.floor(c.height*.55),1,1).data;return p[0]<10&&p[1]<10&&p[2]<10&&p[3]===255;});
    if(mode.endsWith('-normal')||mode==='inline-long')await page.screenshot({path:path.join(out,mode+'.png')});
    await page.getByTitle('关闭',{exact:true}).click();await page.waitForFunction(()=>ownedTask.destroyed);assert.deepEqual(errors,[]);rows.push({mode,status:'PASS',...info,messages:messages.filter(m=>/CFF charset|parseFDSelect|Type2|Type1|TrueType|RangeError/.test(m))});
   }finally{await page.close();}
  }
  await context.close();
 }finally{await browser.close();fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(rows,null,2));}
 console.log(JSON.stringify(rows));
}
if(require.main===module)main().catch(e=>{console.error(e);process.exitCode=1;});
module.exports={fixture};
