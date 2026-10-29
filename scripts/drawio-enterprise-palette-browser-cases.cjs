'use strict';
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {assignment}=require('./test_drawio_layout_fixes.cjs'),review=require('./drawio-enterprise-palette-review.cjs');
module.exports=async function(frame,out,theme){
 const checks=[];
 for(const variant of process.env.AIDE_ENTERPRISE_CATALOG?['source']:['source','app.min.js','integrate.min.js']){
  const cache=new Map();const code=review.methods.map(([file,name])=>{const key=variant==='source'?file:variant;if(!cache.has(key))cache.set(key,fs.readFileSync('internal/server/web/vendor/drawio/js/'+key,'utf8'));return assignment(cache.get(key),name);});
  const observed=await frame.evaluate(({code,families,catalog})=>{
   function Captured(){}Captured.prototype=Object.create(Sidebar.prototype);
   const sb=new Captured();sb.taglist=Object.create(null);sb.searchIndexData=null;sb.styleToLibs=null;sb.graph=ownedUi.sidebar.graph;
   sb.addPaletteFunctions=()=>{};sb.addPalette=()=>{};
   sb.createVertexTemplateFromCells=(cells,width,height,title)=>({cells,width,height,title});sb.createEdgeTemplateFromCells=sb.createVertexTemplateFromCells;
   Function('Sidebar','sb',code.join('\n'))(Captured,sb);
   const entries=[],errors=[];let family;const original=Sidebar.prototype.addEntry;
   sb.addEntry=function(tags,fn){const e={index:entries.length,family,tags,fn,library:this.currentSearchEntryLibrary};entries.push(e);fn.ownedReview=e;return original.apply(this,arguments);};
   for(const [name,method,args]of families){family=name;try{sb[method](...(args||[]));}catch(error){errors.push({family,method,error:error.message});}}
   family='OwnedMachineProbe';for(const n of ['0','7','256']){const fns=[];sb.addGCP2ExpandedMachineCard('Owned '+n,'app_engine_icon',1,.84,'64GB','2','modifiers_standard_machine','3','modifiers_storage',n,'modifiers_autoscaling',150,'owned storage '+n,fns);}
   const snap=c=>({value:c.value?.nodeType?mxUtils.getXml(c.value):c.value,style:c.style,geometry:c.geometry?{x:c.geometry.x,y:c.geometry.y,width:c.geometry.width,height:c.geometry.height,relative:c.geometry.relative,offset:c.geometry.offset?{x:c.geometry.offset.x,y:c.geometry.offset.y}:null}:null,children:(c.children||[]).map(snap)});
   const texts=c=>[typeof c.value==='string'?c.value:'',...(c.children||[]).flatMap(texts)];const styles=c=>[c.style,...(c.children||[]).flatMap(styles)];
   const rows=[];for(const e of [...entries])try{const t=e.fn();if(!t?.cells?.length)continue;e.template=t;rows.push({index:e.index,family:e.family,tags:e.tags,library:e.library,title:t.title,width:t.width,height:t.height,texts:t.cells.flatMap(texts),styles:t.cells.flatMap(styles),snapshot:t.cells.map(snap)});}catch(error){errors.push({family:e.family,tags:e.tags,error:error.message});}
   window.ownedEnterprise={sb,entries,rows};return {rows:catalog?rows:[],errors,captured:entries.length};
  },{code,families:review.families,catalog:!!process.env.AIDE_ENTERPRISE_CATALOG});
  if(!process.env.AIDE_ENTERPRISE_CATALOG){
   observed.validation=await frame.evaluate(async({validate})=>{
    const {sb,entries,rows}=ownedEnterprise,result=Function('return ('+validate+')')()(rows,sb,entries),graph=ownedEditor.graph;
    graph.selectAll();graph.removeCells(graph.getSelectionCells());graph.clearSelection();const rendered=[],counts={};
    for(const selected of result.selected){const row=rows.find(r=>r.index===selected.index),entry=entries[selected.index];if(!row)continue;
     let hit=false;const words=String(row.tags||'').toLowerCase().replace(/[^a-z0-9 ]/g,' ').split(/\s+/).filter(Boolean);const query=words.slice(-2).join(' ');if(query)sb.searchEntries(query,10000,0,fns=>{hit=fns.includes(entry.fn);},null,true);if(query&&!hit)result.mismatches.push(selected.issue+' actual search misses '+row.title);
     counts[selected.issue]=(counts[selected.issue]||0)+1;const contentCase=(selected.issue===233&&row.texts.some(t=>t.startsWith('Description #')))||(selected.issue===236&&row.texts.some(t=>t.includes('distribution')))||(selected.issue===237&&row.title==='Collaboration Use');if(!contentCase&&counts[selected.issue]>(selected.issue===217?8:selected.issue===219?6:2))continue;
     const i=rendered.length,cells=graph.importCells(entry.template.cells,60+i%4*650,80+Math.floor(i/4)*600,graph.getDefaultParent());const descendants=cells.flatMap(c=>graph.model.getDescendants(c));
     if(descendants.some(c=>!graph.view.getState(c)))result.mismatches.push(selected.issue+' missing actual rendered state');
     const root=graph.view.getState(cells[0]);
     if(selected.issue===218&&(!CSS.supports('color',root.style.fontColor)||root.style.fontColor.toLowerCase()!=='#9e9e9e'))result.mismatches.push('218 invalid rendered font color');
     if(root.style.image)await new Promise((resolve,reject)=>{const image=new Image();image.onload=resolve;image.onerror=()=>reject(Error('Target image unavailable '+root.style.image));image.src=root.style.image;});
     const visibleText=descendants.map(c=>graph.view.getState(c)?.text?.node?.textContent||'').join(' ');if(selected.issue===228&&row.title==='Alphanumeric'&&!visibleText.includes('V W X Y Z'))result.mismatches.push('228 actual alphanumeric renderer');if(selected.issue===233&&contentCase&&![1,2,3,4].every(n=>visibleText.includes('Description #'+n)))result.mismatches.push('233 actual legend renderer');if(selected.issue===236&&contentCase&&(!visibleText.includes('distribution')||visibleText.includes('distributuion')))result.mismatches.push('236 actual rate renderer');if(selected.issue===237&&contentCase&&!visibleText.includes('occurrence'))result.mismatches.push('237 actual occurrence renderer');rendered.push({issue:selected.issue,title:row.title,roots:cells.length,rendered:descendants.length,query,hit,fontColor:root.style.fontColor,image:root.style.image,visibleText});
    }
    graph.clearSelection();ownedUi.setPageVisible(false);ownedUi.actions.get('fitWindow').funct();return {...result,rendered,selectedRows:result.selected.map(e=>rows.find(r=>r.index===e.index))};
   },{validate:review.validate.toString()});
   observed.rows=observed.validation.selectedRows;
   await frame.page().screenshot({path:path.join(out,theme+'-enterprise-'+variant+'.png')});
   await frame.evaluate(()=>{const {entries,rows}=ownedEnterprise,g=ownedEditor.graph;g.selectAll();g.removeCells(g.getSelectionCells());g.clearSelection();const examples=[rows.find(r=>r.family==='GCP2'&&r.title==='Note'),rows.filter(r=>r.family==='OwnedMachineProbe'&&r.title==='Owned 256').at(-1),rows.find(r=>r.family==='SAP'&&r.texts.some(t=>t.startsWith('Description #'))),rows.find(r=>r.family==='Sysml'&&r.title==='Rate'&&r.texts.some(t=>t.includes('distribution')))];for(const [i,r]of examples.entries())g.importCells(entries[r.index].template.cells,50+i%2*420,60+Math.floor(i/2)*420,g.getDefaultParent());ownedUi.actions.get('fitWindow').funct();});await frame.page().screenshot({path:path.join(out,theme+'-enterprise-samples-'+variant+'.png')});
  }
  fs.writeFileSync(path.join(out,theme+'-enterprise-'+variant+'.json'),JSON.stringify(observed,null,2)+'\n');
  assert.deepEqual(observed.errors,[],variant+' actual library registration/factories');
  if(observed.validation){if(observed.validation.mismatches.length)console.log(JSON.stringify({variant,theme,mismatches:observed.validation.mismatches}));assert.deepEqual(observed.validation.mismatches,[],variant+' enterprise target semantics/search/render');}
  checks.push({case:'enterprise catalogue materialization',variant,theme,result:'PASS',captured:observed.captured});
  await frame.evaluate(()=>{delete window.ownedEnterprise;});
 }
 return checks;
};
