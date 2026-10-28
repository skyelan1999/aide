/* Shared Mermaid palette and render queue for previews and light-paper exports. */
(() => {
 'use strict';
 const sources=new WeakMap();
 let queue=Promise.resolve();
 // Mermaid emits SVG <style> and style attributes. The workbench deliberately
 // disallows inline CSS; use browser CSSOM without weakening the page CSP.
 const sheets=new Map(),styleCache=new WeakMap();
 let observer;
 function clearStyles(node){
  const sheet=sheets.get(node);if(!sheet)return;
  const doc=node.ownerDocument;doc.adoptedStyleSheets=doc.adoptedStyleSheets.filter(s=>s!==sheet);sheets.delete(node);
 }
 function releaseStyles(){
  for(const [node] of sheets)if(!node.isConnected){
   clearStyles(node);
  }
 }
 function applyDeclarations(root,selector){
  for(const element of root.querySelectorAll(selector)){
   const declarations=root.ownerDocument.createElement('span').style;declarations.cssText=element.getAttribute('style');
   element.removeAttribute('style');
   for(let i=0;i<declarations.length;i++){const name=declarations[i];element.style.setProperty(name,declarations.getPropertyValue(name),declarations.getPropertyPriority(name));}
  }
 }
 // SVG selectors are scoped by Mermaid, but CSS keyframe names are document-global.
 function scopedCSS(root,selector){
  return [...root.querySelectorAll(selector)].map(style=>{
   const source=style.textContent,cached=styleCache.get(style);if(cached?.source===source)return cached.css;
   const View=style.ownerDocument.defaultView||document.defaultView,sheet=new View.CSSStyleSheet();sheet.replaceSync(source);
   let changed=false;
   const removeKeyframes=group=>{for(let i=group.cssRules.length-1;i>=0;i--){const rule=group.cssRules[i];if(rule.type===View.CSSRule.KEYFRAMES_RULE){group.deleteRule(i);changed=true;}else if(rule.cssRules&&typeof rule.deleteRule==='function')removeKeyframes(rule);}};
   removeKeyframes(sheet);
   const css=changed?[...sheet.cssRules].map(rule=>rule.cssText).join('\n'):source;
   styleCache.set(style,{source:css,css});if(css!==source)style.textContent=css;return css;
  }).join('\n');
 }
 function applyStyles(node){
  const doc=node.ownerDocument,View=doc.defaultView;
  clearStyles(node);
  const css=scopedCSS(node,'svg style');
  if(css){
   const sheet=new View.CSSStyleSheet();sheet.replaceSync(css);
   doc.adoptedStyleSheets=[...doc.adoptedStyleSheets,sheet];sheets.set(node,sheet);
  }
  // Setting individual CSS declarations through CSSOM is permitted by CSP.
  // Keep serialized SVG styles for HTML/PDF export and author classDef colours.
  applyDeclarations(node,'svg[style],svg [style]');
  if(!observer){observer=new View.MutationObserver(records=>{if(records.some(r=>r.removedNodes.length))releaseStyles();});observer.observe(doc.documentElement,{childList:true,subtree:true});}
  releaseStyles();
 }
 async function renderStyled(id,source){
  const doc=document,View=doc.defaultView;
  let sheet,previousCSS='';
  const refresh=()=>{
   const svg=doc.getElementById(id);if(!svg)return;
   const css=scopedCSS(svg,'style');
   if(css!==previousCSS){
    if(!sheet&&css){sheet=new View.CSSStyleSheet();doc.adoptedStyleSheets=[...doc.adoptedStyleSheets,sheet];}
    if(sheet)sheet.replaceSync(css);previousCSS=css;
   }
   applyDeclarations(svg,'[style]');
  };
  // Mermaid measures its temporary SVG before returning it. Install its scoped
  // CSS as nodes arrive, so text metrics use the same fonts as the final view.
  const watch=new View.MutationObserver(refresh);watch.observe(doc.documentElement,{childList:true,subtree:true,characterData:true});
  try{
   const result=await window.mermaid.render(id,source),template=doc.createElement('template');
   template.innerHTML=result.svg;scopedCSS(template.content,'style');
   return {...result,svg:template.innerHTML};
  }
  finally{watch.disconnect();if(sheet)doc.adoptedStyleSheets=doc.adoptedStyleSheets.filter(s=>s!==sheet);}
 }
 function configuration(dark=false){
  const p=dark?{paper:'#161f2a',text:'#edf5ff',node:'#243c55',border:'#86acd0',secondary:'#23453f',tertiary:'#4b3e29',cluster:'#1c2937',line:'#a1b7ce',label:'#202e3c'}:{paper:'#f8fafc',text:'#17324d',node:'#eaf2ff',border:'#6583a5',secondary:'#e8f5f1',tertiary:'#fff3dd',cluster:'#f1f5fa',line:'#526f8b',label:'#f8fafc'};
  return {startOnLoad:false,securityLevel:'strict',theme:'base',htmlLabels:false,fontFamily:'-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
   themeVariables:{darkMode:dark,background:p.paper,primaryColor:p.node,primaryTextColor:p.text,primaryBorderColor:p.border,secondaryColor:p.secondary,secondaryTextColor:p.text,secondaryBorderColor:p.border,tertiaryColor:p.tertiary,tertiaryTextColor:p.text,tertiaryBorderColor:p.border,textColor:p.text,nodeTextColor:p.text,lineColor:p.line,edgeLabelBackground:p.label,clusterBkg:p.cluster,clusterBorder:p.border,titleColor:p.text,fontSize:'15px'},
   flowchart:{htmlLabels:false,curve:'linear',nodeSpacing:36,rankSpacing:48,padding:16},
   themeCSS:`.node rect.basic{rx:8px;ry:8px}.node polygon{fill:${p.tertiary}}.node rect:not(.basic){fill:${p.secondary}}.node .label{font-weight:500}.edgeLabel{font-size:13px}.edgeLabel p{padding:2px 5px}.flowchart-link{stroke-width:1.6px}.cluster rect{rx:10px;ry:10px}`};
 }
 function render(host,{load,dark=document.documentElement.dataset.theme==='dark',errorText='流程图渲染失败',strict=false}={}){
  const theme=dark?'dark':'light';
  const jobs=[...host.querySelectorAll('div.mermaid')].map(node=>{
   let entry=sources.get(node);
   if(!entry){entry={source:node.textContent,version:0};sources.set(node,entry);}
   if(entry.pending===theme)return null;
   if(!entry.pending&&node.dataset.diagramTheme===theme){if(node.isConnected&&!sheets.has(node))applyStyles(node);return null;}
   const version=++entry.version;entry.pending=theme;
   return {node,entry,version};
  }).filter(Boolean);
  if(!jobs.length)return Promise.resolve();
  const work=async()=>{
   try{
    await load('/vendor/mermaid.min.js',()=>!!window.mermaid);
    for(const {node,entry,version} of jobs){
     if(entry.version!==version)continue;
     try{
      window.mermaid.initialize(configuration(dark));
      const result=await renderStyled('aideDiagram'+crypto.randomUUID().replaceAll('-',''),entry.source);
      if(entry.version!==version)continue;
      node.innerHTML=result.svg;applyStyles(node);node.dataset.processed='1';node.dataset.diagramTheme=theme;
     }catch(e){if(entry.version===version){clearStyles(node);node.textContent=errorText;delete node.dataset.diagramTheme;delete node.dataset.processed;}if(strict)throw e;}
     finally{if(entry.version===version)entry.pending=null;}
    }
   }catch(e){for(const {node,entry,version} of jobs)if(entry.version===version){entry.pending=null;clearStyles(node);node.textContent=errorText;delete node.dataset.processed;}if(strict)throw e;}
  };
  const result=queue.then(work);queue=result.catch(()=>{});return result;
 }
 window.AideMarkdownDiagrams=Object.freeze({configuration,render});
})();
