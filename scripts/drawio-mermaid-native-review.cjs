'use strict';
const fs=require('fs'),vm=require('vm');const standalone='internal/server/web/vendor/drawio/js/mermaid/drawio-mermaid.min.js',extensions='internal/server/web/vendor/drawio/js/extensions.min.js',marker='/*! @drawio/mermaid ',end='globalThis.mxMermaidToDrawio=Mermaid.mxMermaidToDrawio;';
function moduleSource(copy='standalone'){if(process.env.AIDE_NATIVE_REVIEW_BASELINE)return fs.readFileSync(process.env.AIDE_NATIVE_REVIEW_BASELINE,'utf8');if(copy==='standalone')return fs.readFileSync(standalone,'utf8');const text=fs.readFileSync(extensions,'utf8'),start=text.indexOf(marker),stop=text.indexOf(end,start);if(start<0||stop<0)throw Error('native module boundary missing');return text.slice(start,stop+end.length);}
// This upstream bundle generates bootstrap functions during initialization.
function context(copy){const c={};vm.createContext(c,{codeGeneration:{strings:true,wasm:false}});vm.runInContext(moduleSource(copy),c);return c;}
module.exports={standalone,extensions,marker,end,moduleSource,context};
