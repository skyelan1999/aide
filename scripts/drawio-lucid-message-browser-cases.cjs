'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const files=['diagramly/Extensions.js','extensions.min.js','integrate.min.js'];
function importer(file){
 const text=fs.readFileSync('internal/server/web/vendor/drawio/js/'+file,'utf8');
 if(file.endsWith('Extensions.js'))return text;
 const a=text.indexOf('LucidImporter={};'),b=text.indexOf('function BmpDecoder(',a);assert.ok(a>=0&&b>a);
 return text.slice(a,b);
}
function fixture(){
 const text=(t,size,color,style)=>({t,m:[{n:'s',v:size,s:0},{n:'c',v:color,s:0},{n:style,v:true,s:0}]});
 const blocks={};
 for(const [index,kind] of ['EIMessageBlock','EIMessageHistoryBlock'].entries())blocks['owned-'+index]={Class:kind,Properties:{
  BoundingBox:{x:80+index*360,y:100,w:280,h:240},Text:{t:'Owned '+kind,m:[]},Messages:2,HistoryMessages:2,
  message_0:text('Owned existing history',20,'#008040','b'),message_1:text('Owned red '+kind,24,'#d12f23','b'),message_2:text('Owned blue '+kind,16,'#1948d1','i')}};
 return {Pages:{owned:{Properties:{Order:0,Title:'Owned message styles'},Blocks:blocks}}};
}
module.exports=async function(frame,out,theme){
 const results=[];
 for(const file of files){
  // Execute the exact LucidImporter closure from each distributed build, not
  // unrelated integrate bootstrap code. The real editor conversion API follows.
  await frame.addScriptTag({content:importer(file)});
  const observed=await frame.evaluate(async input=>{
   const xml=await new Promise((resolve,reject)=>ownedUi.convertLucidChart(JSON.stringify(input),resolve,reject));
   ownedUi.setFileData(xml);
   const graph=ownedEditor.graph,found=[];
   for(const cell of Object.values(graph.model.cells)){
    if(typeof cell.value==='string'&&cell.value.startsWith('Owned ')&&!cell.value.startsWith('Owned EI')){
     const state=graph.view.getState(cell);found.push({value:cell.value,style:cell.style,rendered:!!state?.text?.node,computed:state?{size:state.style.fontSize,color:state.style.fontColor,fontStyle:state.style.fontStyle}:null,text:state?.text?{size:state.text.size,color:state.text.color,fontStyle:state.text.fontStyle}:null});
    }
   }
   return found;
  },fixture());
  for(const kind of ['EIMessageBlock','EIMessageHistoryBlock']){
   for(const [color,size,fontStyle,prefix]of [['#d12f23',18,1,'Owned red '],['#1948d1',12,2,'Owned blue ']]){
    const cell=observed.find(c=>c.value===prefix+kind);assert.ok(cell,file+' message exists '+prefix+kind);
    assert.match(cell.style,new RegExp('(?:^|;)fontSize='+size+';'),file+' preserves message font size');
    assert.ok(cell.style.includes('fontColor='+color+';'),file+' preserves per-message color');
    assert.ok(cell.style.includes('fontStyle='+fontStyle+';'),file+' preserves per-message bold/italic');
    assert.ok(cell.rendered,'imported message is rendered in actual graph');
    for(const actual of [cell.computed,cell.text]){assert.equal(Number(actual.size),size);assert.equal(actual.color.toLowerCase(),color);assert.equal(Number(actual.fontStyle),fontStyle);}
   }
  }
  const existing=observed.find(c=>c.value==='Owned existing history');assert.ok(existing?.style.includes('fontSize=15;'),'already correct history header remains styled');
  if(file==='extensions.min.js')await frame.page().screenshot({path:path.join(out,theme+'-lucid-message-styles.png')});
  results.push({case:'actual Lucidchart conversion/render keeps independent EI message/history font size/color/emphasis',file,theme,result:'PASS'});
 }
 return results;
};
module.exports.fixture=fixture;module.exports.importer=importer;
