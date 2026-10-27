'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const {assignment}=require('./test_drawio_layout_fixes.cjs');
const base='internal/server/web/vendor/drawio/js/';
const methods=[['diagramly/sidebar/Sidebar-AWS3D.js','Sidebar.prototype.addAWS3DPalette']];
function capture(file,name){
  function Sidebar(){}const entries=[];
  const context={Sidebar,mxConstants:{STYLE_VERTICAL_LABEL_POSITION:'verticalLabelPosition',STYLE_VERTICAL_ALIGN:'verticalAlign',STYLE_STROKEWIDTH:'strokeWidth'},mxUtils:{bind:(obj,fn)=>fn.bind(obj)}};
  vm.runInNewContext(assignment(fs.readFileSync(base+file,'utf8'),name),context);
  const sb=new Sidebar();
  sb.setCurrentSearchEntryLibrary=()=>{};sb.addPaletteFunctions=()=>{};
  sb.addEntry=(_tags,fn)=>fn;sb.createEdgeTemplateEntry=()=>{};
  sb.getTagsForStencil=(gn,name,more)=>[gn,name,more];
  sb.createVertexTemplateEntry=(style,width,height,value,title,_a,_b,tags)=>{const entry={style,width,height,value,title,tags};entries.push(entry);return entry;};
  sb.addAWS3DPalette();return entries;
}
function main(){
  for(const file of [methods[0][0],'app.min.js','integrate.min.js']){
    const entries=capture(file,methods[0][1]);
    for(const [suffix,words] of [['NE','north east northeast'],['SE','south east southeast'],['SW','south west southwest'],['NW','north west northwest']]){
      const entry=entries.find(e=>e.title==='Arrow '+suffix);assert.ok(entry,'entry '+suffix);
      const tokens=entry.tags.split(' ');
      for(const word of ['arrow',suffix.toLowerCase(),...words.split(' ')])assert.ok(tokens.includes(word),file+' '+entry.title+' missing '+word);
      for(const wrong of ['north','south','east','west'].filter(w=>!words.split(' ').includes(w)))assert.ok(!tokens.includes(wrong),file+' '+entry.title+' wrong direction '+wrong);
      assert.ok(entry.style.includes('mxgraph.aws3d.arrow'+suffix+';'),'icon identity unchanged');
    }
    for(const title of ['Dashed Edge Double Arrow','Dashed Arrowless Edge','Flat Double Edge']){
      const entry=entries.find(e=>e.title===title),tokens=entry.tags.split(' ');
      for(const word of ['nw','se','north','west','northwest','south','east','southeast'])assert.ok(tokens.includes(word),file+' '+title+' axis '+word);
      assert.ok(!tokens.includes('ne')&&!tokens.includes('northeast'),file+' '+title+' is not NE');
    }
    for(const title of ['Arrowless NE','Dashed Edge','Flat Edge']){
      const entry=entries.find(e=>e.title===title);assert.ok(entry.tags.split(' ').includes('northeast'),title+' remains NE');
    }
    console.log('PASS AWS3D directional search metadata '+file+' (4 directions,3NW/SE axes,3NE controls)');
  }
}
module.exports={methods,capture};
if(require.main===module)try{main();}catch(e){console.error(e.message);process.exitCode=1;}
