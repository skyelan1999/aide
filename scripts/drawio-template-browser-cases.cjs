'use strict';
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
module.exports=async(frame,out,theme)=>{const fixtures='scripts/fixtures/dompurify-template-controls.json',controls=fs.existsSync(fixtures)?JSON.parse(fs.readFileSync(fixtures)):null;const result=await frame.evaluate(controls=>{const samples=['<b title="ok" onclick="bad">safe</b>','<div>before<template><i onerror="bad">inert</i></template>after</div>','<template><template><span>nested</span></template></template>','<p>{{name}} ${value} &lt;safe&gt;</p>','<svg><text>vector</text></svg>','<div>one<!-- comment -->two</div>','<img src="owned-invalid" onerror="bad">','<p><a href="javascript:bad()">link</a></p>'];const purifier=DOMPurify(window),rows=[],checks=[];for(const mode of ['graph','safe-templates'])for(const input of samples){const output=String(mode==='graph'?Graph.sanitizeHtml(input):purifier.sanitize(input,{SAFE_FOR_TEMPLATES:true}));rows.push({mode,input,output});}if(controls&&JSON.stringify(rows)!==JSON.stringify(controls))throw Error('ordinary template sanitizer output changed');
for(const mode of ['graph','safe-templates']){const input='<div>before'+'<template>'.repeat(20000)+'<img src="owned-invalid" onerror="bad">tail'+'</template>'.repeat(20000)+'</div>';const start=performance.now();try{const output=String(mode==='graph'?Graph.sanitizeHtml(input):purifier.sanitize(input,{SAFE_FOR_TEMPLATES:true}));if(output.includes('onerror')||!output.includes('tail')||!output.includes('before'))throw Error('sanitization/ordinary content lost');checks.push({mode,depth:20000,status:'PASS',ms:performance.now()-start,outputLength:output.length,templateCount:(output.match(/<template/g)||[]).length});}catch(e){checks.push({mode,depth:20000,status:'FAIL',error:e.message,ms:performance.now()-start})}}
for(const inPlace of [false,true]) {
 const root=document.createElement('div');root.append('before');let tail=root;
 for(let depth=0;depth<6000;depth++){const t=document.createElement('template');tail.append(t);tail=t.content}
 const img=document.createElement('img');img.setAttribute('onerror','bad');tail.append(img,'tail');
 const start=performance.now();try{
  const clean=inPlace?purifier.sanitize(root,{IN_PLACE:true}):Graph.domPurify(root,false);
  const output=inPlace?clean.outerHTML:String(clean);
  if(output.includes('onerror')||!output.includes('tail'))throw Error('deep node not sanitized');
  checks.push({mode:'node-'+inPlace,depth:6000,status:'PASS',ms:performance.now()-start});
 }catch(e){checks.push({mode:'node-'+inPlace,depth:6000,status:'FAIL',error:e.message,stack:String(e.stack).slice(0,600),ms:performance.now()-start})}
}
return{rows,checks};},controls);fs.writeFileSync(path.join(out,theme+'-templates.json'),JSON.stringify(result,null,2)+'\n');if(!process.env.AIDE_TEMPLATE_CATALOG)assert(result.checks.every(c=>c.status==='PASS'),JSON.stringify(result.checks));return result.checks.map(c=>({case:'425 '+c.mode,theme,result:c.status,depth:c.depth}));};
