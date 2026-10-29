'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {assignment}=require('./test_drawio_layout_fixes.cjs'),{methods}=require('./drawio-caption-metadata-review.cjs');
module.exports=async function(frame,out,theme){
 const checks=[];
 for(const variant of ['source','app.min.js','integrate.min.js']){
  const code=methods.map(([file,name])=>assignment(fs.readFileSync('internal/server/web/vendor/drawio/js/'+(variant==='source'?file:variant),'utf8'),name));
  const observed=await frame.evaluate(code=>{
   function CapturedSidebar(){}CapturedSidebar.prototype=Object.create(Sidebar.prototype);
   const sb=new CapturedSidebar(),entries=[],mismatches=[];sb.taglist=Object.create(null);sb.searchIndexData=null;sb.graph=ownedUi.sidebar.graph;
   sb.setCurrentSearchEntryLibrary=()=>{};sb.addPalette=()=>{};sb.addPaletteFunctions=()=>{};let family;
   const add=Sidebar.prototype.addEntry;sb.addEntry=function(tags,fn){const e={family,tags,fn};entries.push(e);fn.ownedReview=e;return add.call(this,tags,fn);};
   sb.createVertexTemplateFromCells=(cells,width,height,title)=>({cells,width,height,title});sb.createEdgeTemplateFromCells=sb.createVertexTemplateFromCells;
   // C4 factories intentionally resolve the live sidebar through free `sb`.
   // Bind it to the capture object without replacing the real editor global.
   Function('Sidebar','sb',code.join('\n'))(CapturedSidebar,sb);
   family='bootstrap';sb.addBootstrapPalette();family='c4';sb.addC4Palette();
   const snap=c=>({value:typeof c.value==='string'?c.value:Object.fromEntries([...c.value.attributes].map(a=>[a.name,a.value])),style:c.style,vertex:c.vertex,edge:c.edge,geometry:c.geometry?{x:c.geometry.x,y:c.geometry.y,width:c.geometry.width,height:c.geometry.height,relative:c.geometry.relative,offset:c.geometry.offset?{x:c.geometry.offset.x,y:c.geometry.offset.y}:null}:null,children:(c.children||[]).map(snap)});
   const factoryErrors=[];for(const e of entries)try{e.template=e.fn();e.snapshot=e.template.cells.map(snap);}catch(error){e.error=error.message;factoryErrors.push({family:e.family,tags:e.tags,error:e.error});}
   if(factoryErrors.length)mismatches.push('actual factories throw '+factoryErrors.length+' errors: '+factoryErrors[0].error);
   const valid=entries.filter(e=>e.template?.cells?.length),selected=[];
   const byTags=tags=>valid.find(e=>e.family==='bootstrap'&&e.tags===tags);
   for(const [issue,tags,title]of [[201,'bootstrap input group with checkbox','Input group with checkbox'],[203,'bootstrap modal content','Modal content']]){
    const e=byTags(tags);if(!e||e.template.title!==title)mismatches.push(issue+' wrong displayed template title');if(e)selected.push(e);
   }
   const horizontal=byTags('list group horizontal');
   if(!horizontal||horizontal.template.cells[0].children[1].value!=='Dapibus ac facilisis in')mismatches.push('202 inserted horizontal list sample misspelling');if(horizontal)selected.push(horizontal);
   const nav=valid.find(e=>e.family==='bootstrap'&&e.template.title==='Nested nav');
   if(!nav||nav.template.cells[0].children[0].children.at(-1).value!=='Item 3-2')mismatches.push('204 second Item3 child label');if(nav)selected.push(nav);
   const titleTypes=[['System Context diagram title','SystemContextDiagramTitle'],['Containers diagram title','ContainerDiagramTitle'],['Components diagram title','ComponentDiagramTitle']];
   for(const [title,type]of titleTypes){const e=valid.find(e=>e.family==='c4'&&e.template.title===title);if(!e||e.template.cells[0].getAttribute('c4Type')!==type)mismatches.push('205 wrong serialized type '+title);if(e)selected.push(e);}
   const controls=valid.filter(e=>(e.family==='c4'&&['SystemScopeBoundary','ContainerScopeBoundary'].includes(e.template.cells[0].getAttribute('c4Type'))&&!e.template.title.includes('diagram title'))||(e.family==='bootstrap'&&e.tags==='bootstrap input group with radio button'));
   const search=(query,e)=>{let hit;sb.searchEntries(query,1000,0,fns=>{hit=fns.includes(e.fn);},null,true);return hit;};
   for(const e of selected)if(!search(e.tags,e))mismatches.push('query misses '+e.template.title);
   const graph=ownedEditor.graph;graph.selectAll();graph.removeCells(graph.getSelectionCells());graph.clearSelection();const rendered=[],ids=[];
   for(const [i,e]of [...selected,...controls].entries()){
    const cells=graph.importCells(e.template.cells,70+i%2*1000,80+Math.floor(i/2)*600,graph.getDefaultParent());
    const states=cells.flatMap(c=>graph.model.getDescendants(c)).map(c=>graph.view.getState(c));
    if(states.some(s=>!s||(s.cell.geometry?.width>0&&s.cell.geometry?.height>0&&!s.shape?.node?.isConnected)))mismatches.push('unrendered target '+e.template.title);
    const text=states.map(s=>s.text?.node?.textContent||'').join('\n');rendered.push({title:e.template.title,roots:cells.length,renderedCells:states.length,text});
    if(e===horizontal&&!text.includes('Dapibus ac facilisis in'))mismatches.push('202 actual text renderer misspelling');
    if(e===nav&&!text.includes('Item 3-2'))mismatches.push('204 actual text renderer label');
    if(e.family==='c4')ids.push({id:cells[0].id,title:e.template.title,attributes:Object.fromEntries([...cells[0].value.attributes].map(a=>[a.name,a.value]))});
   }
   // Legacy file metadata is user data: newer template defaults must not rewrite it.
   for(const [title,type]of [['System Context diagram title','ContainerScopeBoundary'],['Components diagram title','ContainerDiagramTitle']]){
    const e=selected.find(e=>e.family==='c4'&&e.template.title===title);if(!e)continue;
    const cells=graph.importCells(e.template.cells,70+ids.length%2*1000,3200+ids.length*100,graph.getDefaultParent());
    cells[0].setAttribute('c4Type',type);ids.push({id:cells[0].id,title:'Legacy '+title,attributes:Object.fromEntries([...cells[0].value.attributes].map(a=>[a.name,a.value]))});
   }
   window.ownedCaptionExamples={horizontal,nav,titles:selected.filter(e=>e.family==='c4')};
   const serialize=e=>({family:e.family,tags:e.tags,title:e.template.title,width:e.template.width,height:e.template.height,snapshot:e.snapshot});
   return {mismatches,factoryErrors,globalSb:{type:typeof window.sb,sameLive:window.sb===ownedUi.sidebar},materialized:entries.length,targets:selected.map(serialize),controls:controls.map(serialize),rendered,c4Ids:ids};
  },code);
  await frame.evaluate(()=>ownedUi.actions.get('fitWindow').funct());await frame.page().screenshot({path:path.join(out,theme+'-captions-'+variant+'.png')});
  await frame.page().evaluate(()=>{ownedSaves=[];});await frame.getByText('Save',{exact:true}).last().click();await frame.page().waitForFunction(()=>ownedSaves.length>0);
  const saved=await frame.page().evaluate(()=>ownedSaves.pop());
  fs.writeFileSync(path.join(out,theme+'-captions-'+variant+'.drawio'),saved);
  observed.saved=await frame.evaluate(({saved,ids})=>{
   ownedEditor.setGraphXml(mxUtils.parseXml(saved).documentElement);
   return ids.map(e=>{const cell=ownedEditor.graph.model.getCell(e.id);return {title:e.title,preserved:!!cell&&JSON.stringify(Object.fromEntries([...cell.value.attributes].map(a=>[a.name,a.value])))===JSON.stringify(e.attributes),type:cell?.getAttribute('c4Type')};});
  },{saved,ids:observed.c4Ids});
  for(const e of observed.saved)if(!e.preserved)observed.mismatches.push('205 metadata lost during actual Save/reload '+e.title);
  // Inspect the real custom-property editor, then render readable examples.
  await frame.evaluate(id=>{const g=ownedEditor.graph;g.setSelectionCell(g.model.getCell(id));ownedUi.actions.get('editData').funct();},observed.c4Ids[0].id);
  observed.metadataDialog=await frame.evaluate(()=>[...document.querySelectorAll('.geDialog input,.geDialog textarea')].map(e=>e.value));
  if(!observed.metadataDialog.includes('SystemContextDiagramTitle'))observed.mismatches.push('205 actual Edit Data dialog omits corrected type');
  await frame.page().screenshot({path:path.join(out,theme+'-c4-properties-'+variant+'.png')});
  await frame.getByText('Cancel',{exact:true}).last().click();
  await frame.evaluate(()=>{const g=ownedEditor.graph,{horizontal,nav,titles}=ownedCaptionExamples;g.selectAll();g.removeCells(g.getSelectionCells());g.clearSelection();for(const [e,x,y]of [[horizontal,30,30],[nav,550,30],...titles.map((e,i)=>[e,30,120+i*100])])g.importCells(e.template.cells,x,y,g.getDefaultParent());ownedUi.setPageVisible(false);ownedUi.actions.get('fitWindow').funct();delete window.ownedCaptionExamples;});
  await frame.page().screenshot({path:path.join(out,theme+'-readable-captions-'+variant+'.png')});

  fs.writeFileSync(path.join(out,theme+'-captions-'+variant+'.json'),JSON.stringify(observed,null,2)+'\n');
  if(observed.mismatches.length)console.log('Caption metadata diagnostics '+JSON.stringify({variant,theme,mismatches:observed.mismatches,factoryErrors:observed.factoryErrors.slice(0,3)}));
  assert.deepEqual(observed.mismatches,[],variant+' actual captions/cell labels/C4 type and Save/reload');
  checks.push({case:'201–204 actual title/cell text/factory/index/render;205 C4 types and real Save/reload;controls retained',variant,theme,result:'PASS',targets:observed.targets.length,savedC4:observed.saved.length});
 }
 return checks;
};
