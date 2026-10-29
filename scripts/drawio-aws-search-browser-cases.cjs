'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {assignment}=require('./test_drawio_layout_fixes.cjs');
const {methods,cases}=require('./drawio-aws-search-review.cjs');
const base='internal/server/web/vendor/drawio/js/';
module.exports=async function(frame,out,theme){
  const results=[];
  for(const variant of ['source','app.min.js','integrate.min.js']){
    const text=file=>fs.readFileSync(base+(variant==='source'?file:variant),'utf8');
    const selected=methods.map(([file,name])=>assignment(text(file),name));
    const top=['Sidebar.prototype.addAWS4Palette','Sidebar.prototype.addAWS4bPalette'].map((name,i)=>assignment(text('diagramly/sidebar/Sidebar-AWS4'+(i?'b':'')+'.js'),name));
    const observed=await frame.evaluate(({selected,top,names,cases})=>{
      const descriptors=[];
      function CapturedSidebar(){}
      CapturedSidebar.prototype=Object.create(Sidebar.prototype);
      Function('Sidebar',selected.join('\n')+'\n'+top.join('\n'))(CapturedSidebar);
      const sb=new CapturedSidebar();sb.taglist=Object.create(null);
      for(const key of Object.getOwnPropertyNames(Sidebar.prototype)){
        if(/^addAWS4/.test(key)&&key.endsWith('Palette')&&!names.includes(key)&&!['addAWS4Palette','addAWS4bPalette'].includes(key))sb[key]=()=>{};
      }
      sb.setCurrentSearchEntryLibrary=()=>{};sb.addPaletteFunctions=()=>{};
      const factory=Sidebar.prototype.createVertexTemplateEntry;let family;
      sb.createVertexTemplateEntry=function(style,width,height,value,title,showLabel,showTitle,tags){
        const fn=factory.apply(this,arguments),record={style,width,height,value,title,tags,family};
        fn.ownedReview=record;descriptors.push(record);return fn;
      };
      family='current';sb.addAWS4Palette();family='legacy';sb.addAWS4bPalette();
      const selectedEntries=cases.map(([family,shape,query,wrong,title])=>{
        const candidates=descriptors.filter(e=>e.family===family&&new RegExp('(?:shape|resIcon|prIcon)=mxgraph\\.aws4\\.'+shape+';').test(e.style));
        if(candidates.length!==1)throw Error('expected one target '+family+'/'+shape+' got '+candidates.length);
        const entry=candidates[0];let ranked;
        sb.searchEntries(query,1000,0,fns=>{ranked=fns.map(fn=>fn.ownedReview).filter(Boolean);},null,true);
        return {entry,query,wrong,expectedTitle:title,found:ranked.some(e=>e===entry),rank:ranked.indexOf(entry),tokens:entry.tags.split(' ')};
      });
      const graph=ownedEditor.graph;graph.selectAll();graph.removeCells(graph.getSelectionCells());graph.clearSelection();
      const cells=[];graph.model.beginUpdate();
      try{
        for(const [i,{entry}]of selectedEntries.entries()){
          const x=60+i%5*190,y=80+Math.floor(i/5)*180;
          cells.push(graph.insertVertex(graph.getDefaultParent(),null,entry.value,x,y,entry.width,entry.height,entry.style));
          graph.insertVertex(graph.getDefaultParent(),null,entry.family+' '+(entry.title||entry.value).replace(/\n/g,' '),x-20,y+105,150,50,'text;html=0;whiteSpace=wrap;align=center;verticalAlign=top;');
        }
      }finally{graph.model.endUpdate();}
      const rendered=cells.map(cell=>{
        const state=graph.view.getState(cell),key=state.style.resIcon||state.style.prIcon||state.style.shape;
        return {key,stencil:mxStencilRegistry.getStencil(key)?.desc?.getAttribute('name'),exists:!!state.shape.node,box:state.shape.node.getBoundingClientRect().toJSON()};
      });
      return {selectedEntries,rendered,entriesCaptured:descriptors.length};
    },{selected,top,names:methods.map(([,name])=>name.split('.').at(-1)),cases});
    fs.writeFileSync(path.join(out,theme+'-aws-search-'+variant+'.json'),JSON.stringify(observed,null,2)+'\n');
    await frame.evaluate(()=>ownedUi.actions.get('fitWindow').funct());
    await frame.page().screenshot({path:path.join(out,theme+'-aws-search-'+variant+'.png')});
    const mismatches=[];
    for(const item of observed.selectedEntries){
      const id=item.entry.family+'/'+item.query;
      for(const word of item.query.split(' '))if(!item.tokens.includes(word))mismatches.push(id+' missing '+word);
      for(const wrong of item.wrong)if(item.tokens.includes(wrong))mismatches.push(id+' retains '+wrong);
      if(item.expectedTitle&&item.entry.title!==item.expectedTitle)mismatches.push(id+' wrong title '+item.entry.title);
      if(!item.found)mismatches.push(id+' absent from actual search');
    }
    for(const item of observed.rendered)if(!item.stencil||!item.exists||item.box.width<=0)mismatches.push(item.key+' missing actual stencil render');
    if(mismatches.length)console.log('AWS search diagnostics '+JSON.stringify({variant,theme,mismatches}));
    assert.deepEqual(mismatches,[],variant+' AWS metadata and actual search/render');
    results.push({case:'AWS current/legacy actual indexed14target search and stencil render',variant,theme,result:'PASS',targets:14,captured:observed.entriesCaptured});
  }
  return results;
};
