'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/server/web/markdown-export.js','utf8');
const context={window:{addEventListener(){}},setTimeout,clearTimeout,setInterval,clearInterval,AbortController,DOMException};vm.createContext(context);vm.runInContext(source,context);
const refs=context.window.AideMarkdownExport.references;
const md='![inline](a.png)\n[x][ref]\n[ref]: <assets/file name.txt>\n<img src="graph.drawio.svg">\n```md\n![ignored](private.png)\n```\n`[ignored](secret.txt)`\n![paren](folder/a(b).png)';
assert.deepEqual(Array.from(refs(md),r=>r.value),['a.png','assets/file name.txt','graph.drawio.svg','folder/a(b).png']);
for(const ref of refs(md))assert.equal(md.slice(ref.start,ref.start+ref.length),ref.value);
assert(!source.includes('.print('));assert(source.includes("a.download=name"));assert(source.includes('signal:job.controller.signal'));
const app=fs.readFileSync('internal/server/web/app.js','utf8');assert(app.includes('fetch(fileRawUrl(path,spec.root,spec.source,spec),{signal})'));assert(app.includes("panelId==='editor-dialog'?$(panelId).open"));
const index=fs.readFileSync('internal/server/web/index.html','utf8');assert(index.includes('/markdown-export.js?v=3'),'changed export code must use a fresh browser asset version');assert(!index.includes('data-i18n="导出 PDF"'));for(const id of ['editor-export-pdf','file-view-export-pdf']){const match=index.match(new RegExp('<button[^>]*id="'+id+'"[^>]*>'));assert(match,'missing '+id);assert.match(match[0],/\bhidden\b/);assert.match(match[0],/导出/);}
function fakeElement(tagName,ownerDocument){
 const e={tagName:tagName.toUpperCase(),ownerDocument,children:[],attributes:[],style:{},parentNode:null,append(child){this.children.push(child);child.parentNode=this;},replaceChildren(...children){for(const child of this.children)child.parentNode=null;this.children=[];for(const child of children)this.append(child);},remove(){if(this.parentNode)this.parentNode.children=this.parentNode.children.filter(child=>child!==this);},setAttribute(){},querySelectorAll(){return[];},cloneNode(){const copy=fakeElement(this.tagName,ownerDocument);copy.rect=this.rect;return copy;},getBoundingClientRect(){return this.rect||{top:0,bottom:0,left:0,right:0,width:0,height:0};},click(){}};
 Object.defineProperty(e,'scrollHeight',{get(){if(ownerDocument?.isSnapshot&&this.tagName==='MAIN')return 2700;if(ownerDocument?.isSnapshot&&this.tagName==='BODY')return this.children[0]?.scrollHeight||0;return 0;}});
 Object.defineProperty(e,'innerHTML',{set(){if(ownerDocument?.isSnapshot&&this.tagName==='MAIN'){this.rect={top:48,bottom:2748,left:60,right:734,width:674,height:2700};for(const [top,height] of [[0,120],[900,150],[1800,150],[2600,150]]){const block=fakeElement('p',ownerDocument);block.rect={top:48+top,bottom:48+top+height,left:60,right:734,width:674,height};this.append(block);}}}});
 return e;
}
function fakeDocument(isSnapshot=false){
 const doc={isSnapshot,defaultView:{scrollY:0},fonts:{ready:Promise.resolve()},open(){},write(){},close(){}};
 doc.head=fakeElement('head',doc);doc.body=fakeElement('body',doc);doc.createElement=tag=>{if(tag==='iframe'){const frame=fakeElement(tag,doc);frame.contentDocument=fakeDocument();return frame;}return fakeElement(tag,doc);};
 if(isSnapshot)doc.body.rect={top:0,bottom:1200,left:0,right:794,width:794,height:1200};return doc;
}
function within(promise,label){let timer;return Promise.race([promise,new Promise((_,reject)=>{timer=setTimeout(()=>reject(Error(label+' timed out')),5000);})]).finally(()=>clearTimeout(timer));}
async function verifyPagedPdf(){
 const renderDocs=[],rendered=[],progress=[],pdfInstances=[];let downloaded='',downloadedBlob=null,frameNumber=0;
 const hostDocument=fakeDocument();const testWindow={addEventListener(){}};
 class FakePDF{constructor(){this.pages=1;this.images=0;this.imageArgs=[];pdfInstances.push(this);}setProperties(){}addPage(){this.pages++;}addImage(...args){this.images++;this.imageArgs.push(args);}output(){return new Blob(['%PDF-fake']);}}
 const testContext={window:testWindow,document:hostDocument,URL:{createObjectURL:blob=>{downloadedBlob=blob;return 'blob:fixture';},revokeObjectURL(){}},Blob,AbortController,DOMException,setTimeout:()=>1,clearTimeout(){},setInterval:()=>1,clearInterval(){},requestAnimationFrame:cb=>queueMicrotask(cb)};
 vm.createContext(testContext);vm.runInContext(source,testContext);
 let hangRender=false,startedResolve;const renderStarted=new Promise(resolve=>{startedResolve=resolve;});
 const render=async(page)=>{rendered.push(page);if(hangRender){startedResolve();return new Promise(()=>{});}return{width:1191,height:Math.ceil(page.style.height||0),toDataURL:(type,quality)=>`data:${type};quality=${quality}`};};
 testWindow.html2canvas=render;
 testWindow.jspdf={jsPDF:FakePDF};
 hostDocument.createElement=tag=>{if(tag==='iframe'){const frame=fakeElement(tag,hostDocument);frame.contentDocument=fakeDocument(frameNumber++%2===0);renderDocs.push(frame.contentDocument);return frame;}const e=fakeElement(tag,hostDocument);if(tag==='a')e.click=()=>{downloaded=e.download;};return e;};
 const result=await testWindow.AideMarkdownExport.run('pdf',{markdown:'long fixture',html:'fixture',title:'long.md',valid:()=>true,load:async(_src,ready)=>{if(!ready())throw Error('renderer missing');},resolve:()=>null,fetchResource:()=>Promise.reject(Error('unused'))},(page,total)=>progress.push([page,total]));
 assert.equal(downloaded,'long.pdf');assert.equal(rendered.length,4);assert.equal(renderDocs.length,2,'one source frame and one bounded page-render frame');
 assert.deepEqual(Array.from(rendered,n=>n.children.length),[1,1,1,1],'each canvas contains only blocks intersecting its page');
 assert.deepEqual(Array.from(progress,p=>p[0]),[1,2,3,4]);assert.deepEqual(Array.from(progress,p=>p[1]),[4,4,4,4],'page total includes keep-together breaks instead of undercounting pages');assert(downloadedBlob instanceof Blob);assert.equal(pdfInstances[0].images,4);assert(pdfInstances[0].imageArgs.every(args=>args[0]==='data:image/jpeg;quality=0.82'&&args[1]==='JPEG'),'PDF pages are encoded as compressed JPEG data');
 hangRender=true;const stalled=testWindow.AideMarkdownExport.run('pdf',{markdown:'long fixture',html:'fixture',title:'stalled.md',valid:()=>true,load:async(_src,ready)=>{if(!ready())throw Error('renderer missing');}},()=>{});try{await within(renderStarted,'page render start');testWindow.AideMarkdownExport.close();await within(assert.rejects(stalled,e=>e.name==='AbortError','closing the export must settle a stalled html2canvas render'),'cancelled render');}finally{testWindow.AideMarkdownExport.close();}
}
(async()=>{const pending=context.window.AideMarkdownExport.run('pdf',{valid:()=>true,title:'a.md',load:()=>new Promise(()=>{})});context.window.AideMarkdownExport.close();await assert.rejects(pending,e=>e.name==='AbortError');await verifyPagedPdf();console.log('PASS: direct downloads, resource offsets/code exclusion, stale editor guard, cancellation during library loading and stalled page rendering, and page-bounded long-PDF rendering');})().catch(e=>{console.error(e);process.exitCode=1;});
