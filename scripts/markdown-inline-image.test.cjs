'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/server/web/markdown-export.js','utf8').replace('window.AideMarkdownExport={','window.ownedInlineImage=inlineImageBlob;window.ownedImageDataURL=imageDataURL;window.AideMarkdownExport={');
const context={window:{addEventListener(){}},Blob,Uint8Array,URL,TextEncoder,atob,DOMException};vm.createContext(context);vm.runInContext(source,context);
(async()=>{
 const inputs=[
  'data:image/png;base64,AQIDBA==','DATA:image/png;base64,AQIDBA','data:image/png;base64,AQIDBA%3D%3D',
  'data:image/png;base64,AQID%20BA%3D%3D','data:image/png,%89PNG%0D%0A%1A%0A',
  'data:image/svg+xml,%3Csvg%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3C/svg%3E',
  'data:image/svg+xml,<svg>你好♥?query</svg>','data:image/svg+xml;charset=utf-8,%E2%99%A5',
  'data:image/svg+xml,<svg width="100%"></svg>','data:image/png,abc#fragment',
  'data:image/png,','data:image/png;base64,','data:image/svg+xml;charset=utf-8;BASE64,PHN2Zy8+',
 ];
 for(const input of inputs){const expected=new Uint8Array(await(await fetch(input)).arrayBuffer()),blob=context.window.ownedInlineImage(new URL(input),1024);assert.deepEqual(new Uint8Array(await blob.arrayBuffer()),expected,input);assert(blob.type.startsWith('image/'));}
 for(const input of ['data:image/png;base64,AQIDBA==','data:image/png,%01%02%03%04','data:image/png,abcd']){
  assert.equal(context.window.ownedInlineImage(new URL(input),4).size,4);
  assert.throws(()=>context.window.ownedInlineImage(new URL(input),3),/32 MiB/);
 }
 for(const input of ['data:text/html,owned','data:image/png;base64','data:image/png;base64,@@@@','data:image/png;base64,%GG','data:image/png;base64,A'])assert.throws(()=>context.window.ownedInlineImage(new URL(input),1024),/图片渲染失败/);
 assert.throws(()=>context.window.ownedInlineImage(new URL('data:image/png;'+ 'x'.repeat(1024)+',owned'),4096),/图片渲染失败/);
 const big='data:image/png;base64,'+'A'.repeat(10000);assert.throws(()=>context.window.ownedInlineImage(new URL(big),16),/32 MiB/);
 // Reader cancellation is part of snapshot resource cleanup, not just renderer cancellation.
 let reader;context.FileReader=class{constructor(){reader=this;}readAsDataURL(){this.started=true;}abort(){this.aborted=true;this.onabort?.();}};
 const controller=new AbortController(),pending=context.window.ownedImageDataURL(new Blob(['owned']),controller.signal);assert(reader.started);controller.abort();await assert.rejects(pending,e=>e.name==='AbortError');assert(reader.aborted);assert.equal(reader.onload,null);
 const aborted=new AbortController();aborted.abort();await assert.rejects(context.window.ownedImageDataURL(new Blob(['owned']),aborted.signal),e=>e.name==='AbortError');assert(!reader.started);
 console.log('PASS:13 native data-URL byte oracles, exact/over-budget base64 and binary controls, invalid inputs, and active/pre-aborted FileReader cancellation');
})().catch(e=>{console.error(e);process.exitCode=1});
