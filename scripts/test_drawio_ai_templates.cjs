'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/server/web/vendor/drawio/js/diagramly/Dialogs.js','utf8');
const bundle=fs.readFileSync('internal/server/web/vendor/drawio/js/app.min.js','utf8');
function templates(){
 const start=source.indexOf('var populateTemplate = function('),end=source.indexOf('// Prepends the recorded conversation',start);
 assert.ok(start>=0&&end>start);
 const bs=bundle.indexOf('Aa=function(ca,da){'),be=bundle.indexOf(',ua=function(',bs);assert.ok(bs>=0&&be>bs,'locate actual shipped template copier');
 return [['source',source.slice(start,end)+'\nglobalThis.copy=populateTemplate;'],['bundle','var '+bundle.slice(bs,be)+';globalThis.copy=Aa;']];
}
function main(){
 for(const [name,code]of templates()){
  const context={Editor:{replacePlaceholders:(text,resolver)=>text.replace(/\{(\w+)\}/g,(all,key)=>resolver(key)??all)}};
  vm.createContext(context);vm.runInContext(code,context);
  const input=JSON.parse('{"messages":[{"content":"Ask {prompt}","optional":null},null,0,false,""],"temperature":null,"constructor":null,"__proto__":{"value":null}}');
  const before=JSON.stringify(input),out=context.copy(input,key=>key==='prompt'?'owned prompt':null);
  assert.equal(JSON.stringify(input),before,'template must not be mutated');
  assert.equal(JSON.stringify(out),before.replace('Ask {prompt}','Ask owned prompt'),name+' preserves literal JSON fields');
  assert.notEqual(out,input);assert.notEqual(out.messages,input.messages);assert.notEqual(out.messages[0],input.messages[0]);
  assert.ok(Object.hasOwn(out,'__proto__'));assert.equal(Object.getPrototypeOf(out).value,undefined,'literal key cannot alter result prototype');
  for(const primitive of [null,0,false,true,'',42])assert.equal(context.copy(primitive,()=>null),primitive,name+' root JSON primitive');
  assert.equal(context.copy('{prompt}',()=> 'owned replacement'),'owned replacement');
  assert.equal(JSON.stringify(context.copy([null,[null,{x:'{prompt}'}]],()=> 'nested')),'[null,[null,{"x":"nested"}]]');
  console.log('PASS draw.io JSON template copy:',name);
 }
}
module.exports={templates};if(require.main===module)main();
