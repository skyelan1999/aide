'use strict';
// Actual product exporter in Chromium. Inputs are generated disposable fixtures.
const fs=require('node:fs'),path=require('node:path'),crypto=require('node:crypto'),assert=require('node:assert/strict'),zlib=require('node:zlib');
function png(){
 const crc=b=>{let n=0xffffffff;for(const v of b){n^=v;for(let i=0;i<8;i++)n=(n>>>1)^((n&1)?0xedb88320:0);}return(n^0xffffffff)>>>0;};
 const chunk=(type,data)=>{const b=Buffer.concat([Buffer.from(type),data]),head=Buffer.alloc(4),tail=Buffer.alloc(4);head.writeUInt32BE(data.length);tail.writeUInt32BE(crc(b));return Buffer.concat([head,b,tail]);};
 const header=Buffer.alloc(13);header.writeUInt32BE(1024);header.writeUInt32BE(512,4);header[8]=8;header[9]=2;
 const raw=Buffer.alloc((1024*3+1)*512);for(let y=0;y<512;y++)for(let x=0;x<1024;x++){let p=y*3073+1+x*3;raw[p]=x>>2;raw[p+1]=y>>1;raw[p+2]=128;}
 return Buffer.concat([Buffer.from('89504e470d0a1a0a','hex'),chunk('IHDR',header),chunk('IDAT',zlib.deflateSync(raw)),chunk('IEND',Buffer.alloc(0))]);
}
async function configure(page,out,large=false,root=process.cwd()){
 const web=path.resolve(root,'internal/server/web'),asset=png(),inputs=new Map(large?[['a.bin',crypto.randomBytes(50<<20)],['b.bin',crypto.randomBytes(50<<20)]]:[['a.bin',crypto.randomBytes(65536)]]);
 const gif=Buffer.from('R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7','base64');
 const svg='<svg xmlns="http://www.w3.org/2000/svg" width="96" height="48" viewBox="0 0 96 48"><rect width="96" height="48" fill="#ffa620"/><circle cx="48" cy="24" r="15" fill="#cc2244"/></svg>';
 const hashes=Object.fromEntries([...inputs].map(([name,b])=>[name,{bytes:b.length,sha256:crypto.createHash('sha256').update(b).digest('hex')}]));
 await page.route('**/*',route=>{
  const u=new URL(route.request().url());if(u.origin!=='https://aide-vector-pdf.test')return route.abort();
  if(u.pathname==='/')return route.fulfill({contentType:'text/html',headers:{'Content-Security-Policy':"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data: blob:; worker-src 'self'; object-src 'none'"},body:'<!doctype html><meta charset="utf-8"><title>Aide vector PDF acceptance</title><main><h1>Markdown PDF 验收</h1><span><button id="export">导出</button></span></main>'});
  if(u.pathname==='/image.png')return route.fulfill({contentType:'image/png',body:asset});
  if(u.pathname==='/image.gif')return route.fulfill({contentType:'image/gif',body:gif});
  if(u.pathname==='/image.svg')return route.fulfill({contentType:'image/svg+xml',body:svg});
  if(u.pathname.startsWith('/assets/'))return route.fulfill({contentType:'application/octet-stream',body:inputs.get(u.pathname.slice(8))});
  const file=path.resolve(web,'.'+u.pathname);if(!file.startsWith(web+path.sep)||!fs.existsSync(file))return route.fulfill({status:404,body:''});
  return route.fulfill({contentType:file.endsWith('.ttf')?'font/ttf':file.endsWith('.css')?'text/css':'application/javascript',body:fs.readFileSync(file)});
 });
 await page.goto('https://aide-vector-pdf.test/');await page.addScriptTag({url:'/vendor/marked.min.js'});await page.addScriptTag({url:'/markdown-export.js'});
 await page.evaluate(names=>{
  const markdown='# 矢量 PDF 验收 Vector QA\n\n可选择与搜索的正文 Text stays searchable.\n\n|序号|项目名称|核验说明|\n|---|---|---|\n'+Array.from({length:100},(_,i)=>`|${i+1}|项目 ${i+1}|跨页列宽保持一致，边框清晰。|`).join('\n')+'\n\n![1024 × 512 独立图片](/image.png)\n\n![GIF 图片](/image.gif)\n\n![链接 SVG 图片](/image.svg)\n\n<svg xmlns="http://www.w3.org/2000/svg" width="320" height="80" viewBox="0 0 320 80"><path d="M10 40 L150 10 L310 60" fill="none" stroke="#0055aa" stroke-width="4"/></svg>\n\n'+names.map(n=>`[内嵌附件 ${n}](${n})`).join('\n\n');
  AideMarkdownExport.bind({button:document.querySelector('#export'),options:()=>({markdown,title:'vector-acceptance.md',valid:()=>true,renderMarkdown:m=>marked.parse(m),resolve:p=>p,fetchResource:(p,signal)=>fetch('/assets/'+p,{signal}),load:async(url,test)=>{if(test())return;await new Promise((resolve,reject)=>{const s=document.createElement('script');s.src=url;s.onload=resolve;s.onerror=reject;document.head.append(s)})}}),error:error=>{window.exportFailure=String(error.stack||error)}});
 },[...inputs.keys()]);
 const download=page.waitForEvent('download',{timeout:300000});await page.getByRole('button',{name:'导出',exact:true}).click();await page.getByRole('menuitem').filter({hasText:'分页文档'}).click();
 const failure=page.waitForFunction(()=>window.exportFailure,{},{timeout:300000}).then(handle=>handle.jsonValue()).then(message=>{throw Error(message);});
 const result=await Promise.race([download,failure]),dest=path.join(out,large?'mixed-100MiB.pdf':'normal.pdf');
 await result.saveAs(dest);assert.equal(fs.readFileSync(dest).toString('ascii',0,5),'%PDF-');return hashes;
}
async function main(){const {chromium}=require('./browser-runtime/node_modules/playwright');const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert(out);fs.mkdirSync(out,{recursive:true});const browser=await chromium.launch({executablePath:process.env.AIDE_CHROMIUM_PATH||'/usr/bin/chromium',args:['--no-sandbox','--disable-dev-shm-usage']});const errors=[];try{const page=await browser.newPage();page.on('pageerror',e=>errors.push(e.message));const hashes=await configure(page,out,process.env.AIDE_VECTOR_LARGE==='1');assert.deepEqual(errors,[]);fs.writeFileSync(path.join(out,'input-hashes.json'),JSON.stringify(hashes,null,2));console.log(JSON.stringify({status:'PASS',browser:browser.version(),hashes}));await page.close();}finally{await browser.close()}}
if(require.main===module)main().catch(e=>{console.error(e);process.exitCode=1});module.exports={configure};
