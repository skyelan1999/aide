'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {assignment}=require('./test_drawio_layout_fixes.cjs');
const {methods}=require('./drawio-bpmn-palette-review.cjs');
module.exports=async function(frame,out,theme){
 const checks=[];
 for(const variant of ['source','app.min.js','integrate.min.js']){
  const code=methods.map(([file,name])=>assignment(fs.readFileSync('internal/server/web/vendor/drawio/js/'+(variant==='source'?file:variant),'utf8'),name));
  const observed=await frame.evaluate(async code=>{
   function CapturedSidebar(){}CapturedSidebar.prototype=Object.create(Sidebar.prototype);
   Function('Sidebar',code.join('\n'))(CapturedSidebar);
   const sb=new CapturedSidebar(),entries=[],mismatches=[];sb.taglist=Object.create(null);sb.searchIndexData=null;
   sb.addPalette=()=>{};sb.addPaletteFunctions=()=>{};let family;
   const addEntry=Sidebar.prototype.addEntry;
   sb.addEntry=function(tags,fn){const entry={family,tags,fn};entries.push(entry);fn.ownedReview=entry;return addEntry.call(this,tags,fn);};
   sb.createVertexTemplateFromCells=(cells,width,height,title)=>({cells,width,height,title});
   family='azure';sb.addAzure2OtherPalette('mxgraph.azure2',400,sb,'image;aspect=fixed;html=1;points=[];align=center;fontSize=12;image=img/lib/azure2/other/');
   family='choreography';sb.addBPMN2ChoreographiesPalette('mxgraph.bpmn2',400,sb);
   family='gateway';sb.addBPMN2GatewaysPalette('mxgraph.bpmn2',400,sb);
   const snapshot=cell=>({value:cell.value,style:cell.style,vertex:cell.vertex,edge:cell.edge,geometry:cell.geometry?{x:cell.geometry.x,y:cell.geometry.y,width:cell.geometry.width,height:cell.geometry.height,relative:cell.geometry.relative}:null,children:(cell.children||[]).map(snapshot)});
   for(const entry of entries){entry.template=entry.fn();if(!entry.template?.cells)throw Error('factory returned no cells');}
   const search=(query)=>{let found;sb.searchEntries(query,1000,0,fns=>{found=fns.map(fn=>fn.ownedReview);},null,true);return found;};
   const azure=entries.filter(e=>e.family==='azure'&&e.template.cells.some(c=>c.style.includes('/App_Space_Component.svg;')));
   if(azure.length!==1||azure[0].template.title!=='App Space Component')mismatches.push('196 App Space Component displayed title');
   if(azure.length===1&&!search('app space component').includes(azure[0]))mismatches.push('196 query misses actual factory');
   const plainTags='bpmn business process model notation choreography call choreography activity calling global task';
   const plain=entries.filter(e=>e.family==='choreography'&&e.tags===plainTags);
   if(plain.length!==2)mismatches.push('197 plain call choreography count '+plain.length);
   const placeholders=entries.filter(e=>e.template.title==='bzzzzt');
   if(placeholders.length)mismatches.push('197 placeholder factory returns '+placeholders[0].template.cells[0].getChildCount()+' children');
   const intended=entries.filter(e=>e.template.title==='Call Choreography calling Global');
   if(intended.length!==2||intended.some(e=>e.template.cells.length!==1||e.template.cells[0].getChildCount()!==3))mismatches.push('197 two intended three-part templates missing');
   if(intended.some(e=>!search('call choreography activity calling global task').includes(e)))mismatches.push('197 intended missing query');
   if(new Set(intended.map(e=>e.template.cells[0].children[0].style)).size!==2)mismatches.push('197 participant orientation control lost');
   const start=entries.find(e=>e.family==='gateway'&&e.template.title==='Multiple Start'),end=entries.find(e=>e.family==='gateway'&&e.template.title==='Multiple End');
   if(!start||!start.tags.split(' ').includes('start')||start.tags.split(' ').includes('end'))mismatches.push('198 start explicit tags');
   if(!search('multiple start').includes(start))mismatches.push('198 start query miss');
   if(sb.taglist.end?.entries?.includes(start?.fn))mismatches.push('198 start in exact end bucket');
   if(!end||!end.tags.split(' ').includes('end')||!search('multiple end').includes(end))mismatches.push('198 end control changed');
   const variants=entries.filter(e=>e.template.title?.startsWith('Call Choreography calling Global,'));
   if(variants.length!==6||variants.some(e=>e.template.cells[0].getChildCount()!==3))mismatches.push('197 six populated call variants');
   const selected=[...azure,...placeholders,...intended,...variants,start,end].filter(Boolean);
   const graph=ownedEditor.graph;graph.selectAll();graph.removeCells(graph.getSelectionCells());graph.clearSelection();
   const rendered=[];
   for(const [i,entry]of selected.entries()){
    const cells=graph.importCells(entry.template.cells,70+i%4*230,80+Math.floor(i/4)*220,graph.getDefaultParent());
    graph.insertVertex(graph.getDefaultParent(),null,entry.template.title,50+i%4*230,210+Math.floor(i/4)*220,190,60,'text;html=0;whiteSpace=wrap;align=center;');
    const root=cells[0],state=graph.view.getState(root),all=graph.model.getDescendants(root);
    if(!state?.shape?.node?.getBoundingClientRect().width||all.some(c=>!graph.view.getState(c)?.shape))mismatches.push('unrendered actual factory '+entry.template.title);
    if(state.style.image)await new Promise((resolve,reject)=>{const image=new Image();image.onload=resolve;image.onerror=()=>reject(Error('asset decode failed '+state.style.image));image.src=state.style.image;});
    rendered.push({title:entry.template.title,rootCount:cells.length,children:root.getChildCount(),renderedCells:all.length,shape:state.style.shape,image:state.style.image,visible:state.shape.node.getBoundingClientRect().width>0});
   }
   const serialize=e=>({family:e.family,tags:e.tags,title:e.template.title,width:e.template.width,height:e.template.height,cells:e.template.cells.map(snapshot)});
   return {mismatches,totalFactoryCount:entries.length,plainCount:plain.length,placeholderCount:placeholders.length,targets:selected.map(serialize),choreographyTemplates:entries.filter(e=>e.family==='choreography').map(serialize),rendered};
  },code);
  fs.writeFileSync(path.join(out,theme+'-bpmn-palette-'+variant+'.json'),JSON.stringify(observed,null,2)+'\n');
  await frame.evaluate(()=>ownedUi.actions.get('fitWindow').funct());await frame.page().screenshot({path:path.join(out,theme+'-bpmn-palette-'+variant+'.png')});
  if(observed.mismatches.length)console.log('BPMN palette diagnostics '+JSON.stringify({variant,theme,mismatches:observed.mismatches}));
  assert.deepEqual(observed.mismatches,[],variant+' title/placeholder/gateway metadata and actual factory insertion');
  checks.push({case:'196 Azure title/asset/query;197 two populated call orientations/six variants inserted;198 start index/end control',variant,theme,result:'PASS',plainCount:observed.plainCount,renderedTemplates:observed.rendered.length});
 }
 return checks;
};
