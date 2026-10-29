'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict'),acorn=require('../internal/server/code_analysis/acorn.cjs');
const root='internal/server/web/vendor/pdfjs/',worker=root+'pdf.worker.min.mjs',original=fs.readFileSync(root+'openjpeg/openjpeg_nowasm_fallback.js','utf8');
function payload(){
 assert.equal((original.match(/async function OpenJPEG/g)||[]).length,1);
 assert.equal((original.match(/await run\(\);/g)||[]).length,1);
 let decoder=original.replace('async function OpenJPEG','function OpenJPEG').replace('await run();','run();').replace('export default OpenJPEG;','');
 // This private factory only receives warn: no asynchronous pre/post/init or status hooks.
 const ast=acorn.parse(decoder,{sourceType:'module',ecmaVersion:'latest'}),factory=ast.body.find(n=>n.type==='FunctionDeclaration'&&n.id.name==='OpenJPEG'),run=factory.body.body.find(n=>n.type==='FunctionDeclaration'&&n.id.name==='run');assert(run);
 decoder=decoder.slice(0,run.start)+'function run(){preRun();if(ABORT)return;initRuntime();Module["onRuntimeInitialized"]?.();postRun()}'+decoder.slice(run.end);
 return `(()=>{${decoder};return function(options){const module=OpenJPEG({warn:options.warn});module.decode=function(bytes,{numComponents,isIndexedColormap=false,smaskInData=false}={}){const padComponents=numComponents!==undefined;numComponents??=4;let ptr;try{ptr=module._malloc(bytes.length);if(!ptr)throw new Error("OpenJPEG input allocation failed");module.writeArrayToMemory(bytes,ptr);const status=module._jp2_decode(ptr,bytes.length,numComponents>0?numComponents:0,!!isIndexedColormap,!!smaskInData,0,padComponents);if(status){const message=module.errorMessages||"OpenJPEG decode failed";delete module.errorMessages;module.imageData=null;return message;}const image=module.imageData;module.imageData=null;return image;}finally{if(ptr)module._free(ptr);}};return module;}})()`;
}
const source=fs.readFileSync(worker,'utf8'),ast=acorn.parse(source,{sourceType:'module',ecmaVersion:'latest'});let init;
for(const node of ast.body)if(node.type==='VariableDeclaration')for(const decl of node.declarations)if(decl.id.name==='oi')init=decl.init;assert(init);
const updated=source.slice(0,init.start)+payload()+source.slice(init.end);
if(process.argv.includes('--check'))assert.equal(updated===source,true,'embedded OpenJPEG differs from pinned source/adapter');else fs.writeFileSync(worker,updated);
