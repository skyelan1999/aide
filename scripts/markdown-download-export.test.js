'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/server/web/markdown-export.js','utf8');
const context={window:{addEventListener(){},__AIDE_MARKDOWN_EXPORT_TEST__:true},setTimeout,clearTimeout,setInterval,clearInterval,AbortController,DOMException};vm.createContext(context);vm.runInContext(source,context);
const refs=context.window.AideMarkdownExport.references;
const markdownFragments=context.window.AideMarkdownExport.fragments;
const md='![inline](a.png)\n[x][ref]\n[ref]: <assets/file name.txt>\n<img src="graph.drawio.svg">\n```md\n![ignored](private.png)\n```\n`[ignored](secret.txt)`\n![paren](folder/a(b).png)';
assert.deepEqual(Array.from(refs(md),r=>r.value),['a.png','assets/file name.txt','graph.drawio.svg','folder/a(b).png']);
for(const ref of refs(md))assert.equal(md.slice(ref.start,ref.start+ref.length),ref.value);
assert(!source.includes('.print('));assert(source.includes("a.download=name"));assert(source.includes('signal:job.controller.signal'));
function verifyHundredMiBFragmentation(){const size=100*1024*1024,fixture='x'.repeat(size);let count=0,total=0,max=0;for(const fragment of markdownFragments(fixture)){count++;total+=fragment.length;max=Math.max(max,fragment.length);}assert.equal(total,size,'fragmentation retains every Markdown character');assert(count>=500,'100 MiB documents are processed as bounded fragments');assert(max<=192*1024,'no generated Markdown fragment exceeds the per-fragment DOM budget');}
function verifyEmbeddedAttachmentPdf(){const {jsPDF}=require('../internal/server/web/vendor/markdown-export/jspdf.umd.min.js'),payload=Buffer.from('attachment-payload-integrity'),pdf=new jsPDF({unit:'mm',format:'a4'}),attachment={index:0,name:'report.txt',size:payload.length,binary:payload.toString('latin1')},shared={attachmentRects:new Map([[1,[{index:0,left:18,top:24,width:90,height:8}]]])};const found=context.window.AideMarkdownExport.test.uniquePdfAttachments('[one](docs/report.txt) [duplicate](docs/report.txt) [external](https://example.com/file.txt) ![image](chart.png)',{resolve:value=>value});assert.deepEqual(JSON.parse(JSON.stringify(found)),[{path:'docs/report.txt',name:'report.txt'}],'external URLs and image resources are excluded while duplicate local links dedupe');context.window.AideMarkdownExport.test.registerPdfAttachments(pdf,[attachment],shared);pdf.text('download report.txt',18,28);const bytes=Buffer.from(pdf.output('arraybuffer'));for(const marker of ['/EmbeddedFiles','/Type /EmbeddedFile','/Type /Filespec','/S /GoToE','attachment-payload-integrity'])assert(bytes.includes(Buffer.from(marker)),`PDF attachment marker missing: ${marker}`);}
const app=fs.readFileSync('internal/server/web/app.js','utf8');assert(app.includes('fetch(fileRawUrl(path,spec.root,spec.source,spec),{signal})'));assert(app.includes("panelId==='editor-dialog'?$(panelId).open"));
const index=fs.readFileSync('internal/server/web/index.html','utf8');assert(Number(index.match(/\/markdown-export\.js\?v=(\d+)/)?.[1]) >= 10,'changed export code must use a fresh browser asset version');assert(!index.includes('data-i18n="导出 PDF"'));for(const id of ['editor-export-pdf','file-view-export-pdf']){const match=index.match(new RegExp('<button[^>]*id="'+id+'"[^>]*>'));assert(match,'missing '+id);assert.match(match[0],/\bhidden\b/);assert.match(match[0],/导出/);}
function fakeElement(tagName,ownerDocument){
 const e={nodeType:1,tagName:tagName.toUpperCase(),ownerDocument,children:[],attributes:[],style:{},parentNode:null,append(child){this.children.push(child);child.parentNode=this;},replaceChildren(...children){for(const child of this.children)child.parentNode=null;this.children=[];for(const child of children)this.append(child);},remove(){if(this.parentNode)this.parentNode.children=this.parentNode.children.filter(child=>child!==this);},setAttribute(){},querySelectorAll(){return[];},cloneNode(){const copy=fakeElement(this.tagName,ownerDocument);copy.rect=this.rect;return copy;},getBoundingClientRect(){return this.rect||{top:0,bottom:0,left:0,right:0,width:0,height:0};},click(){}};
 Object.defineProperty(e,'childNodes',{get(){return this.children;}});
 Object.defineProperty(e,'scrollHeight',{get(){if(ownerDocument?.isSnapshot&&this.tagName==='MAIN')return 2800;if(ownerDocument?.isSnapshot&&this.tagName==='BODY')return this.children[0]?.scrollHeight||0;return 0;}});
 Object.defineProperty(e,'innerHTML',{get(){return this._html||'';},set(value){this._html=value;if(ownerDocument?.isSnapshot&&this.tagName==='MAIN'){this.rect={top:48,bottom:2848,left:60,right:734,width:674,height:2800};for(const [top,height] of [[0,120],[180,110],[900,150],[1800,150],[2600,200]]){const block=fakeElement('p',ownerDocument);block.rect={top:48+top,bottom:48+top+height,left:60,right:734,width:674,height};this.append(block);}}}});
 return e;
}
function fakeDocument(isSnapshot=false){
 const doc={isSnapshot,defaultView:{scrollY:0,getComputedStyle:()=>({display:'block'})},fonts:{ready:Promise.resolve()},createTreeWalker:()=>({nextNode:()=>false}),open(){},write(){},close(){}};
 doc.head=fakeElement('head',doc);doc.body=fakeElement('body',doc);doc.createElement=tag=>{if(tag==='iframe'){const frame=fakeElement(tag,doc);frame.contentDocument=fakeDocument();return frame;}return fakeElement(tag,doc);};
 if(isSnapshot)doc.body.rect={top:0,bottom:1200,left:0,right:794,width:794,height:1200};return doc;
}
function within(promise,label){let timer;return Promise.race([promise,new Promise((_,reject)=>{timer=setTimeout(()=>reject(Error(label+' timed out')),5000);})]).finally(()=>clearTimeout(timer));}
async function verifyVectorPdf(){
 const definitions=[],progress=[],inserted=[];let downloaded='',downloadedBlob=null,renderCount=0,stall=false,renderStarted;
 const hostDocument=fakeDocument(),testWindow={addEventListener(){}};
 const testContext={window:testWindow,document:hostDocument,URL:{createObjectURL:blob=>{downloadedBlob=blob;return 'blob:fixture';},revokeObjectURL(){}},Blob,AbortController,DOMException,btoa,fetch:async()=>({ok:true,arrayBuffer:async()=>new Uint8Array([1,2,3]).buffer}),setTimeout:()=>1,clearTimeout(){},setInterval:()=>1,clearInterval(){}};
 vm.createContext(testContext);vm.runInContext(source,testContext);
 testWindow.htmlToPdfmake=()=>[{text:'vector content'}];
 testWindow.pdfMake={addVirtualFileSystem(){},addFonts(){},createPdf(definition){definitions.push(definition);renderCount++;return{getBlob(){if(stall){renderStarted();return new Promise(()=>{});}return Promise.resolve(new Blob(['%PDF-vector-'+renderCount]));}};}};
 testWindow.PDFLib={PDFDocument:{async load(){return{getPageIndices(){return[0];},async copyPages(){return[{id:'prior'}];},insertPage(index,page){inserted.push([index,page.id]);},setTitle(value){this.title=value;},setCreator(value){this.creator=value;},async save(){return new Uint8Array([37,80,68,70]);}};}}};
 hostDocument.createElement=tag=>{if(tag==='iframe'){const frame=fakeElement(tag,hostDocument);frame.contentDocument=fakeDocument();return frame;}const e=fakeElement(tag,hostDocument);if(tag==='a')e.click=()=>{downloaded=e.download;};return e;};
 const fixture='x'.repeat(400*1024);
 await testWindow.AideMarkdownExport.run('pdf',{markdown:fixture,html:'<p>fixture</p>',title:'long.md',valid:()=>true,load:async()=>{},renderMarkdown:()=>'<p>fixture</p>',resolve:()=>null,fetchResource:()=>Promise.reject(Error('unused'))},(...args)=>progress.push(args));
 assert.equal(downloaded,'long.pdf');assert.equal(definitions.length,3,'large Markdown is converted in bounded vector chunks');
 assert.equal(inserted.length,2,'earlier chunks are merged into the final PDF');
 assert.equal(progress.length,3);assert.deepEqual(Array.from(progress,p=>p[0]),[1,2,3]);
 assert(downloadedBlob instanceof Blob);assert.equal(downloadedBlob.type,'application/pdf');
 for(const definition of definitions){assert.equal(definition.defaultStyle.font,'NotoSansSC');assert.equal(definition.pageSize,'A4');assert.deepEqual(Array.from(definition.pageMargins),[42,42,42,42]);}
 stall=true;const started=new Promise(resolve=>{renderStarted=resolve;});
 const pending=testWindow.AideMarkdownExport.run('pdf',{markdown:'fixture',html:'<p>fixture</p>',title:'stalled.md',valid:()=>true,load:async()=>{},resolve:()=>null,fetchResource:()=>Promise.reject(Error('unused'))});
 try{await within(started,'vector render start');testWindow.AideMarkdownExport.close();await within(assert.rejects(pending,e=>e.name==='AbortError'),'cancelled vector render');}finally{testWindow.AideMarkdownExport.close();}
}

(async()=>{const pending=context.window.AideMarkdownExport.run('pdf',{valid:()=>true,title:'a.md',load:()=>new Promise(()=>{})});context.window.AideMarkdownExport.close();await assert.rejects(pending,e=>e.name==='AbortError');verifyHundredMiBFragmentation();verifyEmbeddedAttachmentPdf();await verifyVectorPdf();console.log('PASS: bounded Markdown fragmentation, embedded PDF attachments, vector PDF chunking/merge, download, references and cancellation');})().catch(e=>{console.error(e);process.exitCode=1;});
