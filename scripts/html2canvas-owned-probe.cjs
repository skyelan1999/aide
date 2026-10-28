'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
function instrument(source){const marker='function XA(){';assert.equal(source.split(marker).length,2);const scan='if((c=e[Q])===R)';assert.equal(source.split(scan).length,3);source=source.split(scan).join('if((globalThis.__ownedLB25Reads=(globalThis.__ownedLB25Reads||0)+1,c=e[Q])===R)');return source.replace(marker,'globalThis.__aideCanvasProbe=function(){return {Tokenizer:XA,LineBreaker:wA,LI:aB,OL:UB,listValues:ds,get numericSteps(){return globalThis.__ownedLB25Reads||0}}};'+marker);}
function load(file=process.env.AIDE_CANVAS_BASELINE||'internal/server/web/vendor/markdown-export/html2canvas.min.js'){const context=vm.createContext({console});vm.runInContext(instrument(fs.readFileSync(file,'utf8')),context);return context.__aideCanvasProbe();}
function tokens(api,input){const t=new api.Tokenizer();t.write(input);return JSON.parse(JSON.stringify(t.read()));}
function breaks(api,input,options){const iter=api.LineBreaker(input,options),out=[];for(let n=iter.next();!n.done;n=iter.next())out.push(JSON.parse(JSON.stringify(n.value)));return out;}
module.exports={instrument,load,tokens,breaks};
