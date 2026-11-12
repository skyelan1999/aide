'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict'),crypto=require('node:crypto'),acorn=require('../internal/server/code_analysis/acorn.cjs');
const preflightJPX=require('./pdf-jpx-preflight.cjs');
const {convertJPXColors,useJPXColorConversion}=require('./pdf-jpx-colors.cjs');
const root='internal/server/web/vendor/pdfjs/',worker=root+'pdf.worker.min.mjs',original=fs.readFileSync(root+'openjpeg/openjpeg_nowasm_fallback.js','utf8');
function payload(){
 const provenance=JSON.parse(fs.readFileSync(root+"openjpeg/provenance.json","utf8"));assert.equal(crypto.createHash("sha256").update(original).digest("hex"),provenance.sourceSHA256,"pinned OpenJPEG source digest changed");
 assert.equal((original.match(/async function OpenJPEG/g)||[]).length,1);
 assert.equal((original.match(/await run\(\);/g)||[]).length,1);
 let decoder=original.replace('async function OpenJPEG','function OpenJPEG').replace('await run();','run();').replace('export default OpenJPEG;','');
 // OpenJPEG tgt.c uses 999 for an unknown node value. The pinned generated
 // reset/create/init loops contain 15 unrolled stores to value (node+4).
 // Keep the official source unchanged; assert this exact compiled layout before
 // applying the same INT32_MAX sentinel fix to each emitted initialization.
 const sentinel=/H\[[a-z]+\+(?:4|20|36|52)>>2\]=999/g;
 assert.equal((decoder.match(sentinel)||[]).length,15,'unexpected tag-tree initialization layout');
 assert.equal((decoder.match(/(?<![0-9])999(?![0-9])/g)||[]).length,15);
 decoder=decoder.replace(sentinel,match=>match.replace('=999','=2147483647'));
 // All native malloc/calloc requests in this pinned fallback enter ib(alignment,
 // bytes). Abort at a NULL result instead of allowing C error paths to continue
 // through absent allocations. JpxImage discards the poisoned module on throw.
 const allocator='function ib(a,b){';assert.equal(decoder.split(allocator).length,2);
 decoder=decoder.replace(allocator,'function ib(a,b){Module.aideAllocationBytes=(Module.aideAllocationBytes||0)+(b>>>0);if(Module.aideAllocationBytes>268435456){Module.aideAllocationFailed=true;throw new Error("OpenJPEG allocation budget exceeded");}const pointer=aideNativeAllocate(a,b)|0;if((b>>>0)&&!pointer){Module.aideAllocationFailed=true;throw new Error("OpenJPEG native allocation failed");}return pointer|0;}function aideNativeAllocate(a,b){');


 // This private factory only receives warn: no asynchronous pre/post/init or status hooks.
 const ast=acorn.parse(decoder,{sourceType:'module',ecmaVersion:'latest'}),factory=ast.body.find(n=>n.type==='FunctionDeclaration'&&n.id.name==='OpenJPEG'),run=factory.body.body.find(n=>n.type==='FunctionDeclaration'&&n.id.name==='run');assert(run);
 decoder=decoder.slice(0,run.start)+'function run(){preRun();if(ABORT)return;initRuntime();Module["onRuntimeInitialized"]?.();postRun()}'+decoder.slice(run.end);
 return `(()=>{${decoder};const preflightJPX=${preflightJPX.toString()},convertJPXColors=${convertJPXColors.toString()},useJPXColorConversion=${useJPXColorConversion.toString()};return function(options){const module=OpenJPEG({warn:options.warn});module.decode=function(bytes,{numComponents,isIndexedColormap=false,smaskInData=false}={}){if(module.aideAllocationFailed)throw new Error("OpenJPEG decoder requires recreation after allocation failure");module.aideAllocationBytes=0;const padComponents=numComponents!==undefined;numComponents??=4;if(bytes.length<12)throw new Error("OpenJPEG input too short");const metadata={};try{bytes=preflightJPX(bytes,metadata);}catch(error){return error.message;}const convert=useJPXColorConversion(metadata,numComponents,isIndexedColormap),nativeComponents=convert?metadata.components:numComponents;let ptr;try{ptr=module._malloc(bytes.length);if(!ptr)throw new Error("OpenJPEG input allocation failed");module.writeArrayToMemory(bytes,ptr);const status=module._jp2_decode(ptr,bytes.length,nativeComponents>0?nativeComponents:0,!!isIndexedColormap,!!smaskInData,0,padComponents);if(status){const message=module.errorMessages||"OpenJPEG decode failed";delete module.errorMessages;module.imageData=null;return message;}const image=module.imageData;module.imageData=null;return convert?convertJPXColors(image,metadata,smaskInData):image;}finally{if(ptr)module._free(ptr);}};return module;}})()`;
}
const source=fs.readFileSync(worker,'utf8'),ast=acorn.parse(source,{sourceType:'module',ecmaVersion:'latest'});let init;
for(const node of ast.body)if(node.type==='VariableDeclaration')for(const decl of node.declarations)if(decl.id.name==='oi')init=decl.init;assert(init);
const updated=source.slice(0,init.start)+payload()+source.slice(init.end);
if(process.argv.includes('--check'))assert.equal(updated===source,true,'embedded OpenJPEG differs from pinned source/adapter');else fs.writeFileSync(worker,updated);
