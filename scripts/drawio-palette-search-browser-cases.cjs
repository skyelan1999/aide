'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {assignment}=require('./test_drawio_layout_fixes.cjs');
const {methods}=require('./test_drawio_palette_search.cjs');
module.exports=async function(frame,out,theme){
  const results=[];
  for(const file of [methods[0][0],'app.min.js','integrate.min.js']){
    const method=assignment(fs.readFileSync('internal/server/web/vendor/drawio/js/'+file,'utf8'),methods[0][1]);
    const observed=await frame.evaluate(({method})=>{
      const sb=Object.create(Sidebar.prototype);sb.taglist=Object.create(null);
      // Keep the live Sidebar prototype, factory tag indexing, soundex and
      // ranked search. Avoid touching the user's libraries or live sidebar.
      sb.setCurrentSearchEntryLibrary=()=>{};sb.addPaletteFunctions=()=>{};
      sb.createVertexTemplate=(style,width,height,value,title)=>({style,width,height,value,title});
      sb.createEdgeTemplateEntry=()=>null;
      function CapturedSidebar(){}
      const capture=Function('Sidebar',method+'return Sidebar.prototype.addAWS3DPalette;');capture(CapturedSidebar).call(sb);
      const searches=[];
      for(const [suffix,phrase]of [['NE','north east'],['SE','south east'],['SW','south west'],['NW','north west']]){
        for(const query of [phrase,phrase.replace(' ',''),suffix.toLowerCase()]){
          let found;
          sb.searchEntries(query,100,0,entries=>{found=entries;},e=>{throw e;},true);
          const arrows=found.map(fn=>fn()).filter(e=>e?.title?.match(/^Arrow (NE|SE|SW|NW)$/));
          searches.push({query,expected:suffix,arrows:arrows.map(e=>e.title)});
        }
      }
      let axisEntries;sb.searchEntries('south east',100,0,fns=>{axisEntries=fns.map(fn=>fn()).map(e=>e?.title).filter(Boolean);},null,true);
      const graph=ownedEditor.graph;graph.selectAll();graph.removeCells(graph.getSelectionCells());graph.clearSelection();
      const icons=[];graph.model.beginUpdate();
      try{
        for(const [i,title]of ['Arrow NE','Arrow SE','Arrow SW','Arrow NW','Arrowless NE','Dashed Edge Double Arrow','Dashed Arrowless Edge','Dashed Edge','Flat Edge','Flat Double Edge'].entries()){
          let entry;sb.searchEntries(title,100,0,fns=>{entry=fns.map(fn=>fn()).find(e=>e?.title===title);},null,true);
          const factor=i<4?2:1;
          const cell=graph.insertVertex(graph.getDefaultParent(),null,title,80+(i%3)*320,120+Math.floor(i/3)*160,entry.width*factor,entry.height*factor,entry.style);
          icons.push(cell);
        }
      }finally{graph.model.endUpdate();}
      const rendered=icons.map(cell=>{const state=graph.view.getState(cell);return {style:state.style.shape,exists:!!state.shape.node,box:state.shape.node.getBoundingClientRect().toJSON()};});
      return {searches,rendered,axisEntries};
    },{method});
    // Ranked search deliberately includes phonetic alternatives (eg NE/NW).
    // The correct direction must outrank those legitimate fuzzy matches.
    for(const search of observed.searches)assert.equal(search.arrows[0],'Arrow '+search.expected,file+' ranked '+search.query);
    for(const icon of observed.rendered)assert.ok(icon.exists&&icon.box.width>0);
    for(const title of ['Dashed Edge Double Arrow','Dashed Arrowless Edge','Flat Double Edge'])assert.ok(observed.axisEntries.includes(title),file+' south east includes '+title);
    await frame.evaluate(()=>ownedUi.actions.get('fitWindow').funct());
    await frame.page().screenshot({path:path.join(out,theme+'-direction-'+path.basename(file)+'.png')});
    fs.writeFileSync(path.join(out,theme+'-direction-'+path.basename(file)+'.json'),JSON.stringify(observed,null,2)+'\n');
    results.push({case:'AWS3D actual factory index and ranked direction search, real10direction/axis render',file,theme,result:'PASS',queries:13});
  }
  return results;
};
