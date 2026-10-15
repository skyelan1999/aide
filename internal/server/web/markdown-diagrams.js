/* Shared Mermaid palette and render queue for previews and light-paper exports. */
(() => {
 'use strict';
 const sources=new WeakMap();
 let queue=Promise.resolve();
 function configuration(dark=false){
  const p=dark?{paper:'#161f2a',text:'#edf5ff',node:'#243c55',border:'#86acd0',secondary:'#23453f',tertiary:'#4b3e29',cluster:'#1c2937',line:'#a1b7ce',label:'#202e3c'}:{paper:'#f8fafc',text:'#17324d',node:'#eaf2ff',border:'#6583a5',secondary:'#e8f5f1',tertiary:'#fff3dd',cluster:'#f1f5fa',line:'#526f8b',label:'#f8fafc'};
  return {startOnLoad:false,securityLevel:'strict',theme:'base',fontFamily:'-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
   themeVariables:{darkMode:dark,background:p.paper,primaryColor:p.node,primaryTextColor:p.text,primaryBorderColor:p.border,secondaryColor:p.secondary,secondaryTextColor:p.text,secondaryBorderColor:p.border,tertiaryColor:p.tertiary,tertiaryTextColor:p.text,tertiaryBorderColor:p.border,textColor:p.text,nodeTextColor:p.text,lineColor:p.line,edgeLabelBackground:p.label,clusterBkg:p.cluster,clusterBorder:p.border,titleColor:p.text,fontSize:'15px'},
   flowchart:{htmlLabels:true,curve:'linear',nodeSpacing:36,rankSpacing:48,padding:16},
   themeCSS:`.node rect.basic{rx:8px;ry:8px}.node polygon{fill:${p.tertiary}}.node rect:not(.basic){fill:${p.secondary}}.node .label{font-weight:500}.edgeLabel{font-size:13px}.edgeLabel p{padding:2px 5px}.flowchart-link{stroke-width:1.6px}.cluster rect{rx:10px;ry:10px}`};
 }
 function render(host,{load,dark=document.documentElement.dataset.theme==='dark',errorText='流程图渲染失败',strict=false}={}){
  const theme=dark?'dark':'light';
  const jobs=[...host.querySelectorAll('div.mermaid')].map(node=>{
   let entry=sources.get(node);
   if(!entry){entry={source:node.textContent,version:0};sources.set(node,entry);}
   if(entry.pending===theme||(!entry.pending&&node.dataset.diagramTheme===theme))return null;
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
      const result=await window.mermaid.render('aideDiagram'+crypto.randomUUID().replaceAll('-',''),entry.source);
      if(entry.version!==version)continue;
      node.innerHTML=result.svg;node.dataset.processed='1';node.dataset.diagramTheme=theme;
     }catch(e){if(entry.version===version){node.textContent=errorText;delete node.dataset.diagramTheme;delete node.dataset.processed;}if(strict)throw e;}
     finally{if(entry.version===version)entry.pending=null;}
    }
   }catch(e){for(const {node,entry,version} of jobs)if(entry.version===version){entry.pending=null;node.textContent=errorText;delete node.dataset.processed;}if(strict)throw e;}
  };
  const result=queue.then(work);queue=result.catch(()=>{});return result;
 }
 window.AideMarkdownDiagrams=Object.freeze({configuration,render});
})();
