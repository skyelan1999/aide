'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
function parsers(){
 const source=fs.readFileSync('internal/server/web/vendor/drawio/js/diagramly/Editor.js','utf8');
 const marker='/* AIDE animation validation #168 */';
 let a=source.indexOf(marker);if(a<0)a=source.indexOf('Editor.parseAnimationData = function');
 const p=source.indexOf('Editor.parseAnimationData = function',a),b=source.indexOf('\n\t};',p)+5;
 assert.ok(a>=0&&b>a);
 return [['source',source.slice(a,b)],...['app.min.js','integrate.min.js'].map(file=>{
  const bundle=fs.readFileSync('internal/server/web/vendor/drawio/js/'+file,'utf8');
  let c=bundle.indexOf(marker);if(c<0)c=bundle.indexOf('Editor.parseAnimationData=function');
  const d=bundle.indexOf('Editor.AnimationPlayer=function',c);assert.ok(c>=0&&d>c);
  return [file,bundle.slice(c,d)];
 })];
}
function main(){
 for(const [name,code]of parsers()){
  const context={Editor:{},mxUtils:{trim:s=>s.trim()},mxResources:{get:key=>key}};vm.createContext(context);vm.runInContext(code,context);
  const parse=context.Editor.parseAnimationData;
  for(const step of [null,false,1,'bad',[],{fadeTo:null},{flow:null},{show:[]},{wait:null},{wait:-1},{open:5},{animation:{steps:[null]}}]){
   assert.throws(()=>parse(JSON.stringify({animation:{steps:[step]}})),e=>e.animationData&&JSON.stringify(e.animationData.steps)===JSON.stringify([step]),name+' rejects unsafe step without losing data: '+JSON.stringify(step));
  }
  const valid=[{wait:0},{wait:'10'},{fadeTo:{cells:['x'],value:0}},{flow:{start:false}},{open:'data:page/owned'},{futureAction:{value:null}}];
  const out=parse(JSON.stringify({animation:{steps:valid,loop:false}}));assert.equal(JSON.stringify(out.steps),JSON.stringify(valid));assert.equal(out.loop,false);
  assert.equal(parse(null).steps.length,0);assert.throws(()=>parse('{'));
  let nested={steps:[{wait:1}]};for(let i=0;i<129;i++)nested={steps:[{animation:nested}]};
  assert.throws(()=>parse(nested),name+' nested validation must be bounded');
  nested={steps:[{wait:1}]};for(let i=0;i<128;i++)nested={steps:[{animation:nested}]};assert.ok(parse(nested));
  const atLimit={steps:Array.from({length:100000},()=>({wait:0}))};assert.equal(parse(atLimit).steps.length,100000);
  atLimit.steps.push({wait:0});assert.throws(()=>parse(atLimit),e=>e.animationData===atLimit,name+' total validation work is bounded');

  console.log('PASS draw.io animation validation and retained invalid data:',name);
 }
}
module.exports={parsers};if(require.main===module)main();
