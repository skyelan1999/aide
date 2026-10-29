'use strict';
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {controls,uml,nested}=require('./plantuml-resource-budget-review.cjs');
const regexSource=(input,pattern)=>uml('!$parts = %splitstr_regex('+JSON.stringify(input)+', '+JSON.stringify(pattern).replaceAll('\\\\','\\')+')\nAlice -> Bob: $parts');
module.exports=async(frame,out,theme)=>{
 const rows=[];
 const normal=[...controls,nested('state',23),uml('map M {\n'+Array.from({length:200},(_,i)=>`k${i} *-> City${i}`).join('\n')+'\n}'),regexSource('a'.repeat(32)+'!','(a+)+$')];
 for(let index=0;index<normal.length;index++){
  const result=await frame.evaluate(async source=>{
   const start=performance.now(),xml=await new Promise((resolve,reject)=>ownedUi.parsePlantUmlDiagram(source,{},resolve,reject)),conversionMs=performance.now()-start,doc=mxUtils.parseXml(xml);
   if(doc.getElementsByTagName('parsererror').length)throw Error('invalid native XML');
   const box=document.createElement('div');box.style.cssText='position:fixed;left:0;top:160px;width:100vw;max-width:1100px;height:600px;z-index:9999;background:#fff';document.body.append(box);const graph=new Graph(box);graph.setEnabled(false);
   try{
    new mxCodec(doc).decode(doc.documentElement,graph.model);graph.view.validate();graph.fit(15);const all=Object.values(graph.model.cells);
    for(const cell of all){const g=cell.geometry;if(g&&!['x','y','width','height'].every(k=>Number.isFinite(g[k])))throw Error('nonfinite imported geometry');if(cell.edge)for(const end of [cell.source,cell.target])if(end&&!graph.model.contains(end))throw Error('dangling imported endpoint');}
    if(!box.querySelector('svg'))throw Error('native SVG missing');for(const p of box.querySelectorAll('path'))if(/NaN|Infinity/.test(p.getAttribute('d')||''))throw Error('nonfinite rendered path');
    return{conversionMs,cells:all.length,vertices:all.filter(c=>c.vertex).length,edges:all.filter(c=>c.edge).length,svg:true};
   }finally{graph.destroy();box.remove();}
  },normal[index]);
  assert(result.vertices>0);rows.push({theme,case:'native conversion/import '+index,status:'PASS',...result});
 }
 const rejected=[
  ['notes',uml('object A\n'+Array.from({length:3000},(_,i)=>'note right of A: Note '+i).join('\n')),'AIDE_PLANTUML_LIMIT'],
  ['package-depth',nested('package',5000),'AIDE_PLANTUML_LIMIT'],
  ['activity-depth',nested('activity',500),'AIDE_PLANTUML_LIMIT'],
  ['regex-input',regexSource('x'.repeat(16385),','),'AIDE_PLANTUML_LIMIT'],
  ['regex-backreference',regexSource('aa','(a)\\1'),'AIDE_PLANTUML_REGEX_UNSUPPORTED'],
  ['regex-final-newline',uml('!$parts = %splitstr_regex("a" + %chr(10), "$")\nAlice -> Bob: $parts'),'AIDE_PLANTUML_REGEX_UNSUPPORTED']
 ];
 for(const [name,source,code]of rejected){
  const result=await frame.evaluate(async source=>{
   const start=performance.now();let error;
   try{await new Promise((resolve,reject)=>ownedUi.parsePlantUmlDiagram(source,{},resolve,reject));}catch(e){error={code:e.code,message:e.message};}
   // The same real loader/API must remain usable after its error callback.
   const xml=await new Promise((resolve,reject)=>ownedUi.parsePlantUmlDiagram('@startuml\nAlice -> Bob: after limit\n@enduml',{},resolve,reject));
   return{error,recovered:xml.includes('after limit'),ms:performance.now()-start};
  },source);
  assert.equal(result.error?.code,code,name);assert(result.recovered);assert(!/RangeError|call stack/.test(result.error.message));rows.push({theme,case:name,status:'PASS',...result});
 }
 fs.writeFileSync(path.join(out,theme+'-plantuml-resources.json'),JSON.stringify(rows,null,2));return rows;
};
