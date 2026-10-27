'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const files=['diagramly/Extensions.js','extensions.min.js','integrate.min.js'];
const mappings=[
 [170,'AndroidIconRefresh',{shape:'mxgraph.ios7.icons.repeat'}],
 [171,'EE_BJT_NPN1',{shape:'mxgraph.electrical.transistors.npn_transistor_1'}],
 [171,'EE_BJT_PNP1',{shape:'mxgraph.electrical.transistors.pnp_transistor_1'}],
 [172,'PEPlugValveBlock',{shape:'mxgraph.pid2valves.valve',valveType:'plug'}],
 [173,'AmazonRDSMySQLDBinstance2017',{shape:'mxgraph.aws3.mysql_db_instance'}],
 [175,'IoT_AlexaVoiceServiceAWS19',{shape:'mxgraph.aws4.alexa_voice_service_2'}],
 [null,'IoT_AlexaSkillAWS19',{shape:'mxgraph.aws4.alexa_skill'}],
 [174,'AWSAppSyncAWS19',{shape:'mxgraph.aws4.resourceIcon',resIcon:'mxgraph.aws4.appsync'}],
 [null,'AndroidIconInformation',{shape:'mxgraph.ios7.icons.info'}],
 [null,'EE_BJT_NPN1_V2',{shape:'mxgraph.electrical.transistors.npn_transistor_1'}],
 [null,'EE_BJT_PNP1_V2',{shape:'mxgraph.electrical.transistors.pnp_transistor_1'}],
 [null,'PEBallValveBlock',{shape:'mxgraph.pid2valves.valve',valveType:'ball'}],
 [null,'AmazonRDSMSSQLinstance2017',{shape:'mxgraph.aws3.ms_sql_instance'}],
 [null,'AWSAppSync_redAWS19',{shape:'mxgraph.aws4.resourceIcon',resIcon:'mxgraph.aws4.appsync'}]
];

function importer(file){
 const text=fs.readFileSync('internal/server/web/vendor/drawio/js/'+file,'utf8');
 if(file.endsWith('Extensions.js'))return text;
 const a=text.indexOf('LucidImporter={};'),b=text.indexOf('function BmpDecoder(',a);assert.ok(a>=0&&b>a);
 return text.slice(a,b);
}
// Extract the actual private cached text for the untrusted-input control.
// This VM only decodes shipped constants; browser assertions use real XML APIs.
function cachedAsset(name){
 let text;const registry={loadStencil:()=>null};
 const context={mxStencilRegistry:registry,pako:require('../internal/server/web/vendor/drawio/js/deflate/pako.min.js'),Uint8Array,atob,STENCIL_PATH:'stencils',window:{setTimeout},DOMParser:class{parseFromString(value){text=value;return {documentElement:{},getElementsByTagName:()=>[]};}}};
 vm.runInNewContext(fs.readFileSync('internal/server/web/vendor/drawio/js/stencils.min.js','utf8'),context);
 registry.loadStencil('stencils/'+name);assert.equal(typeof text,'string');return text;
}
function fixture(){
 const text=(t,size,color,style)=>({t,m:[{n:'s',v:size,s:0},{n:'c',v:color,s:0},{n:style,v:true,s:0}]});
 const blocks={};
 for(const [index,kind] of ['EIMessageBlock','EIMessageHistoryBlock'].entries())blocks['owned-'+index]={Class:kind,Properties:{
  BoundingBox:{x:80+index*360,y:100,w:280,h:240},Text:{t:'Owned '+kind,m:[]},Messages:2,HistoryMessages:2,
  message_0:text('Owned existing history',20,'#008040','b'),message_1:text('Owned red '+kind,24,'#d12f23','b'),message_2:text('Owned blue '+kind,16,'#1948d1','i')}};
 for(const [index,[,kind]]of mappings.entries())blocks['owned-map-'+kind]={Class:kind,Properties:{BoundingBox:{x:80+(index%6)*130,y:420+Math.floor(index/6)*120,w:70,h:70},FillColor:kind.includes('AWS19')?'#C925D1':'#ffffff',StrokeColor:'#1f2937',Text:{t:'',m:[]}}};
 return {Pages:{owned:{Properties:{Order:0,Title:'Owned message styles'},Blocks:blocks}}};
}
module.exports=async function(frame,out,theme){
 const results=[];
 for(const file of files){
  // Execute the exact LucidImporter closure from each distributed build, not
  // unrelated integrate bootstrap code. The real editor conversion API follows.
  await frame.addScriptTag({content:importer(file)});
  if(file===files[0]){
   const cache=await frame.evaluate(async()=>{
    const path=STENCIL_PATH+'/aws4.xml',sync=mxStencilRegistry.loadStencil(path);
    const asyncDoc=await new Promise(resolve=>mxStencilRegistry.loadStencil(path,resolve));
    if(sync.documentElement.getAttribute('name')!=='mxgraph.aws4'||asyncDoc.documentElement.getAttribute('name')!=='mxgraph.aws4')throw Error('bundled cache parse failed');
    const inflate=pako.inflateRaw;let calls=0,refused=false;
    try{
     pako.inflateRaw=function(){return '<broken'};
     try{mxStencilRegistry.loadStencil(path);}catch(e){refused=e.message==='Invalid bundled stencil XML';}
     if(!refused)throw Error('corrupt cache XML accepted');
     pako.inflateRaw=function(){calls++;return '<bad>'};
     Object.defineProperty(Object.prototype,'owned-untrusted.xml',{value:'AAAA',configurable:true});
     try{mxStencilRegistry.loadStencil(STENCIL_PATH+'/owned-untrusted.xml');}catch(e){}
     if(calls!==0)throw Error('inherited entry treated as shipped cache');
    }finally{pako.inflateRaw=inflate;delete Object.prototype['owned-untrusted.xml'];}
    return {shapes:sync.getElementsByTagName('shape').length,guardStillInstalled:AideDrawioSafety.installed};
   });
   assert.ok(cache.shapes>1000&&cache.guardStillInstalled);
   results.push({case:'private bundled AWS4 cache sync/async parse, corrupt XML and inherited cache entries rejected',theme,result:'PASS'});
  }
  const observed=await frame.evaluate(async input=>{
   const xml=await new Promise((resolve,reject)=>ownedUi.convertLucidChart(JSON.stringify(input),resolve,reject));
   ownedUi.setFileData(xml);
   const graph=ownedEditor.graph,found=[];
   for(const cell of Object.values(graph.model.cells)){
    const lucidId=(cell.style||'').match(/(?:^|;)lucidId=([^;]+)/)?.[1];
    if((typeof cell.value==='string'&&cell.value.startsWith('Owned ')&&!cell.value.startsWith('Owned EI'))||lucidId?.startsWith('owned-map-')){
     const state=graph.view.getState(cell);found.push({value:cell.value,lucidId,style:cell.style,rendered:!!state?.text?.node,computed:state?{size:state.style.fontSize,color:state.style.fontColor,fontStyle:state.style.fontStyle,shape:state.style.shape,valveType:state.style.valveType,resIcon:state.style.resIcon}:null,icon:state?.shape?.stencil?.desc?.getAttribute('name'),resourceIcon:state?.style?.resIcon?mxStencilRegistry.getStencil(state.style.resIcon)?.desc?.getAttribute('name'):null,shapeRegistered:!!state?.shape&&(!!state.shape.stencil||mxCellRenderer.defaultShapes[state.style.shape]===state.shape.constructor),rendererStyle:state?.shape?{shape:state.shape.style.shape,valveType:state.shape.style.valveType,resIcon:state.shape.style.resIcon}:null,text:state?.text?{size:state.text.size,color:state.text.color,fontStyle:state.text.fontStyle}:null});
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
  const mismatches=[];
  for(const [issue,kind,expected]of mappings){
   const cell=observed.find(c=>c.lucidId==='owned-map-'+kind);assert.ok(cell,'mapped cell exists '+kind);
   for(const [key,value]of Object.entries(expected))if(cell.computed[key]!==value)mismatches.push({issue,kind,key,actual:cell.computed[key],expected:value});
  }
  if(mismatches.length)console.log('Lucid mapping diagnostics '+JSON.stringify({file,theme,mismatches}));
  assert.deepEqual(mismatches,[],file+' mapping identities must match the stencil catalogue');
  for(const [,kind,expected]of mappings){
   const cell=observed.find(c=>c.lucidId==='owned-map-'+kind);assert.ok(!cell.style.includes('NaN'),'mapping cannot serialize NaN');
   assert.ok(cell.shapeRegistered,'actual stencil/custom renderer must exist '+kind);
   for(const [key,value]of Object.entries(expected))assert.equal(cell.rendererStyle[key],value,'renderer receives correct identity '+kind);
   if(expected.shape==='mxgraph.ios7.icons.repeat')assert.equal(cell.icon?.toLowerCase(),'repeat');
   if(expected.shape.includes('.transistors.')||expected.shape.includes('.aws3.'))assert.equal(cell.icon?.toLowerCase().replace(/ /g,'_'),expected.shape.split('.').at(-1));
   if(expected.shape.startsWith('mxgraph.aws4.alexa_'))assert.equal(cell.icon?.toLowerCase().replace(/ /g,'_'),expected.shape.split('.').at(-1));
   if(expected.resIcon)assert.equal(cell.resourceIcon?.toLowerCase(),'appsync');
  }
  results.push({case:'Lucidchart refresh, BJT polarity, plug valve, MySQL, AppSync and distinct Alexa service/skill identities with registered renderers and unchanged neighbors',file,theme,result:'PASS'});
  const existing=observed.find(c=>c.value==='Owned existing history');assert.ok(existing?.style.includes('fontSize=15;'),'already correct history header remains styled');
  if(file==='extensions.min.js')await frame.page().screenshot({path:path.join(out,theme+'-lucid-message-styles.png')});
  results.push({case:'actual Lucidchart conversion/render keeps independent EI message/history font size/color/emphasis',file,theme,result:'PASS'});
 }
 return results;
};
module.exports.fixture=fixture;module.exports.importer=importer;module.exports.cachedAsset=cachedAsset;
