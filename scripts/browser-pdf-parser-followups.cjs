'use strict';
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require('./browser-runtime/node_modules/playwright'),{ttcHeader,meshRecords,meshPatch}=require('./pdf-parser-followup-review.cjs'),zlib=require('node:zlib');
const web=path.resolve('internal/server/web'),app=fs.readFileSync(web+'/app.js','utf8');
const setup=app.slice(app.indexOf('async function setupPdfPreview('),app.indexOf('/* ── DXF 矢量渲染器'));
const ensure=app.slice(app.indexOf('let _pdfjsPromise = null;'),app.indexOf('async function setupPdfPreview('));
function corruptCmap(bytes){const b=Buffer.from(bytes),tables=b.readUInt16BE(4);let cmap,head;
 for(let i=0;i<tables;i++){const p=12+i*16,tag=b.toString('ascii',p,p+4);if(tag==='cmap')cmap=b.readUInt32BE(p+8);if(tag==='head')head=b.readUInt32BE(p+8);}
 assert(cmap!==undefined&&head!==undefined);const group=cmap+b.readUInt32BE(cmap+8)+16;b.writeUInt32BE(65536,group);b.writeUInt32BE(1114111,group+4);b.writeUInt32BE(1,group+8);b.writeUInt32BE(0,head+8);
 function checksum(offset,length){let sum=0;for(let i=0;i<length;i+=4){let word=0;for(let k=0;k<4;k++)word=word*256+(i+k<length?b[offset+i+k]:0);sum=(sum+word)>>>0;}return sum;}
 for(let i=0;i<tables;i++){const p=12+i*16;b.writeUInt32BE(checksum(b.readUInt32BE(p+8),b.readUInt32BE(p+12)),p+4);}b.writeUInt32BE((0xb1b0afba-checksum(0,b.length))>>>0,head+8);return b;
}
function fixture(mode){
 const font=mode.startsWith('ttc-')||mode.startsWith('cmap-'),mesh=/^mesh(?:[67])?-/.test(mode),ps=mode.startsWith('ps-'),widget=mode.startsWith('widget-')||mode==='xfa-entities',negative=mode==='annotation-negative';
 const stream=(props,data)=>{const bytes=Buffer.isBuffer(data)?data:Buffer.from(data);return Buffer.concat([Buffer.from(`<< ${props} /Length ${bytes.length} >>\nstream\n`),bytes,Buffer.from('\nendstream')]);};
 const background=negative?'0 0 1 rg -200 -200 200 200 re f':'0 0 1 rg 0 0 100 100 re f';const content=background+(font?' 0 0 0 rg BT /F0 40 Tf 10 40 Td (A) Tj ET':mesh||ps?' /S sh':'');
 const objects=[Buffer.from(`<< /Type /Catalog /Pages 2 0 R ${widget?'/AcroForm 6 0 R':''} >>`),Buffer.from('<< /Type /Pages /Count 1 /Kids [3 0 R] >>'),Buffer.from(`<< /Type /Page /Parent 2 0 R /MediaBox ${negative?'[-200 -200 0 0]':'[0 0 100 100]'} /Resources << ${font?'/Font << /F0 5 0 R >>':mesh||ps?'/Shading << /S 5 0 R >>':''} >> ${widget||negative?'/Annots [5 0 R]':''} /Contents 4 0 R >>`),stream('',content)];
 if(font){const data=JSON.parse(fs.readFileSync('scripts/fixtures/pdf-ttf-glyphs.json','utf8'));let bytes=mode.startsWith('ttc-')?mode==='ttc-large'?ttcHeader(1000000,1,false):Buffer.from(data.collection,'base64'):Buffer.from(data.format12,'base64');if(mode==='cmap-large')bytes=corruptCmap(bytes);
  objects.push(Buffer.from('<< /Type /Font /Subtype /TrueType /BaseFont /AideSyntheticGlyph /FirstChar 65 /LastChar 65 /Widths [500] /Encoding /WinAnsiEncoding /FontDescriptor 6 0 R >>'),Buffer.from('<< /Type /FontDescriptor /FontName /AideSyntheticGlyph /Flags 32 /FontBBox [0 -200 1000 800] /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 /FontFile2 7 0 R >>'),stream('',bytes));
 }else if(mesh){const type=mode.startsWith('mesh6-')?6:mode.startsWith('mesh7-')?7:4,bytes=zlib.deflateSync(type===4?meshRecords(mode==='mesh-large'?65537:6):meshPatch(type));objects.push(stream('/ShadingType '+type+' /ColorSpace /DeviceRGB /BitsPerCoordinate 8 /BitsPerComponent 8 /BitsPerFlag 8 /Decode [0 100 0 100 0 1 0 1 0 1] /Filter /FlateDecode',bytes));}
 else if(ps){const count=mode==='ps-large'?12000:mode==='ps-deep'?500:0,program=count?'{ '+'1 add '.repeat(count)+count+' sub }':'{ dup mul }';objects.push(Buffer.from('<< /ShadingType 2 /ColorSpace /DeviceGray /Coords [0 0 100 0] /Function 6 0 R /Extend [true true] >>'),stream('/FunctionType 4 /Domain [0 1] /Range [0 1]',program));}
 else if(negative)objects.push(Buffer.from('<< /Type /Annot /Subtype /Square /Rect [-120 -120 -20 -20] /C [1 0 0] /Border [0 0 1] /F 4 >>'));
 else if(widget){const height=mode==='widget-tiny'?1:mode==='widget-normal'?3:30,xml=mode==='xfa-entities';objects.push(Buffer.from(`<< /Type /Annot /Subtype /Widget /FT /Tx /Ff 4096 /F 4 /T (visible) ${xml?'':'/V (A)'} /DA (/Helv 0 Tf 0 g) /Rect [10 10 90 ${10+height}] >>`),Buffer.from(`<< /Fields [5 0 R] /NeedAppearances true /DR << /Font << /Helv 7 0 R >> >> ${xml?'/XFA [(datasets) 8 0 R]':''} >>`),Buffer.from('<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>'));if(xml)objects.push(stream('',`<xfa:datasets xmlns:xfa="http://www.xfa.org/schema/xfa-data/1.0/"><xfa:data><unrelated>${'&'.repeat(220000)}</unrelated><visible>A</visible></xfa:data></xfa:datasets>`));}
 const parts=[Buffer.from('%PDF-1.4\n')],offsets=[0];let offset=parts[0].length;objects.forEach((body,i)=>{offsets.push(offset);const part=Buffer.concat([Buffer.from(`${i+1} 0 obj\n`),body,Buffer.from('\nendobj\n')]);parts.push(part);offset+=part.length;});parts.push(Buffer.from(`xref\n0 ${objects.length+1}\n0000000000 65535 f \n`+offsets.slice(1).map(n=>String(n).padStart(10,'0')+' 00000 n \n').join('')+`trailer\n<< /Size ${objects.length+1} /Root 1 0 R >>\nstartxref\n${offset}\n%%EOF`));return Buffer.concat(parts);
}
async function main(){
 const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert(out);fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({executablePath:'/usr/bin/chromium',args:['--no-sandbox','--disable-dev-shm-usage']});const rows=[];
 try{
  const context=await browser.newContext();
  for(const mode of (process.env.AIDE_PDF_BROWSER_CASES?.split(',')||['ttc-normal','ttc-large','cmap-normal','cmap-large','mesh-normal','mesh6-normal','mesh7-normal','mesh-large','ps-normal','ps-deep','ps-large','xfa-entities','annotation-negative','widget-normal','widget-tiny'])){
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
    await page.evaluate(()=>{const actual=ensurePdfJs;window.ensurePdfJs=async()=>{const native=await actual();window.nativePdf=native;return{getDocument(options){window.recordedOptions={maxImageSize:options.maxImageSize,canvasMaxAreaInBytes:options.canvasMaxAreaInBytes};const task=native.getDocument({...options,disableFontFace:window.fixtureMode.startsWith('ttc-')||window.fixtureMode.startsWith('cmap-')});window.ownedTask=task;task.promise.then(doc=>window.ownedDoc=doc,()=>{});return task;}};};});
    await page.addScriptTag({content:setup});await page.evaluate(()=>setupPdfPreview(document.querySelector('main'),'owned.pdf'));
    const info=await page.evaluate(async()=>{
     const pdfPage=await ownedDoc.getPage(1),op=await pdfPage.getOperatorList(),names=Object.fromEntries(Object.entries(nativePdf.OPS).map(([k,v])=>[v,k]));
     return{options:recordedOptions,operations:op.fnArray.length,operationNames:op.fnArray.map(n=>names[n]),annotations:(await pdfPage.getAnnotations()).map(a=>({rect:a.rect,value:a.fieldValue})),forcedFontFallback:window.fixtureMode.startsWith('ttc-')||window.fixtureMode.startsWith('cmap-'),pages:document.querySelectorAll('.pdf-page').length,note:document.querySelector('.pdf-info').title};
    });
    assert.equal(info.pages,1);assert(!messages.some(message=>/RangeError|Maximum call stack/.test(message)),messages.join('\n'));
    assert(info.operations<3000,JSON.stringify(info));
    const warning={'ttc-large':/TrueType Collection/,'cmap-large':/TrueType format12/,'mesh-large':/mesh geometry exceeds/,'ps-deep':/PostScript expression exceeds/,'ps-large':/PostScript function exceeds/}[mode];
    if(warning)assert(messages.some(m=>warning.test(m)),messages.join('\n'));
    else assert(!messages.some(m=>/translateFont failed|ignoring.*glyph|RangeError/.test(m)),messages.join('\n'));
    const expected=mode==='mesh-large'||mode==='ps-large'?[255,105,180,255]:mode.startsWith('mesh')&&mode.endsWith('-normal')?[255,0,0,255]:mode==='ps-normal'?[64,64,64,255]:mode==='ps-deep'?[128,128,128,255]:[0,0,255,255];
    await page.waitForFunction(expected=>{const c=document.querySelector('.pdf-canvas');if(!c?.width)return false;const p=c.getContext('2d').getImageData(Math.floor(c.width/2),Math.floor(c.height/2),1,1).data;return expected.every((v,i)=>Math.abs(v-p[i])<5);},expected);
    info.expectedPixel=expected;if(mode==='mesh-large'||mode==='ps-large')info.nativeDummyShading=true;
    if(mode==='ttc-normal'||mode==='cmap-normal')await page.waitForFunction(()=>{const c=document.querySelector('.pdf-canvas'),p=c.getContext('2d').getImageData(Math.floor(c.width*.2),Math.floor(c.height*.55),1,1).data;return p[0]<10&&p[1]<10&&p[2]<10&&p[3]===255;});
    if(mode==='annotation-negative')assert.deepEqual(info.annotations[0].rect,[-120,-120,-20,-20]);
    if(mode==='xfa-entities')assert.equal(info.annotations[0].value,'A');
    if(mode==='widget-tiny')assert(!info.operationNames.includes('showText'));if(mode==='widget-normal')assert(info.operationNames.includes('showText'));
    if(!mode.endsWith('-large'))await page.screenshot({path:path.join(out,mode+'.png')});
    await page.getByTitle('关闭',{exact:true}).click();await page.waitForFunction(()=>ownedTask.destroyed);assert.deepEqual(errors,[]);rows.push({mode,status:'PASS',...info,messages:messages.filter(m=>/TrueType|mesh geometry|PostScript|RangeError/.test(m))});
   }finally{await page.close();}
  }
  await context.close();
 }finally{await browser.close();fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(rows,null,2));}
 console.log(JSON.stringify(rows));
}
if(require.main===module)main().catch(e=>{console.error(e);process.exitCode=1;});
module.exports={fixture};
