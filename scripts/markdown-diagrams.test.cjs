'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/server/web/markdown-diagrams.js','utf8'),window={};
const document={documentElement:{dataset:{theme:'light'}},defaultView:{MutationObserver:class{observe(){}disconnect(){}}},getElementById:()=>null,createElement:()=>({content:{querySelectorAll:()=>[]},innerHTML:''})};
vm.runInNewContext(source,{window,document,crypto:require('node:crypto').webcrypto});
const api=window.AideMarkdownDiagrams;
function luminance(hex){const c=hex.slice(1).match(/../g).map(v=>parseInt(v,16)/255).map(v=>v<=.04045?v/12.92:((v+.055)/1.055)**2.4);return c[0]*.2126+c[1]*.7152+c[2]*.0722;}
function contrast(a,b){const x=[luminance(a),luminance(b)].sort((a,b)=>b-a);return(x[0]+.05)/(x[1]+.05);}
for(const dark of [false,true]){assert.equal(api.configuration(dark).htmlLabels,false);assert.equal(api.configuration(dark,true).htmlLabels,true);assert.equal(api.configuration(dark,true).flowchart.htmlLabels,true);assert.equal(api.configuration(dark,true).securityLevel,'strict');const v=api.configuration(dark).themeVariables;for(const fill of ['primaryColor','secondaryColor','tertiaryColor','clusterBkg','edgeLabelBackground'])assert(contrast(v.textColor,v[fill])>=4.5,fill);assert(contrast(v.lineColor,v.background)>=3);assert(contrast(v.primaryBorderColor,v.primaryColor)>=3);}
let removed;
const doc={adoptedStyleSheets:[],documentElement:{},defaultView:{CSSStyleSheet:class{replaceSync(css){this.css=css;this.cssRules=[];}},MutationObserver:class{constructor(fn){removed=fn;}observe(){}}}};
function node(text){return {textContent:text,dataset:{},ownerDocument:doc,isConnected:true,querySelectorAll:s=>s==='svg style'?[{textContent:'#diagram .node{fill:blue}',ownerDocument:doc}]:[],set innerHTML(v){this.svg=v;this.textContent='rendered label';}};}
const a=node('flowchart TD\n A-->B'),host={querySelectorAll:()=>[a]};let calls=[],configuration,release,delay=false;
window.mermaid={initialize:c=>{configuration=c;},render:async(_id,text)=>{const dark=configuration.themeVariables.darkMode;calls.push({dark,text});if(delay){delay=false;await new Promise(r=>release=r);}return {svg:'svg-'+dark};}};
const load=async()=>{};
(async()=>{
 await api.render(host,{load});assert.equal(a.svg,'svg-false');assert.equal(doc.adoptedStyleSheets.length,1);assert(doc.adoptedStyleSheets[0].css.includes('fill:blue'));
 await api.render(host,{load});assert.equal(calls.length,1);
 await api.render(host,{load,dark:true});assert.equal(calls[1].text,'flowchart TD\n A-->B');assert.equal(a.svg,'svg-true');
 delay=true;const light=api.render(host,{load,dark:false});while(!release)await new Promise(r=>setImmediate(r));
 const dark=api.render(host,{load,dark:true});release();await Promise.all([light,dark]);assert.equal(a.svg,'svg-true');
 const exportNode=node('flowchart LR\n X-->Y');await Promise.all([api.render(host,{load,dark:false}),api.render({querySelectorAll:()=>[exportNode]},{load,dark:false,strict:true})]);assert.equal(exportNode.svg,'svg-false');
 const retry=node('flowchart TD\n C-->D'),retryHost={querySelectorAll:()=>[retry]};await api.render(retryHost,{load:async()=>{throw Error('offline');}});assert.equal(retry.dataset.diagramTheme,undefined);await api.render(retryHost,{load});assert.equal(calls.at(-1).text,'flowchart TD\n C-->D');
 window.mermaid.render=async()=>{throw Error('invalid diagram');};await assert.rejects(api.render({querySelectorAll:()=>[node('bad')]},{load,strict:true}));
 const app=fs.readFileSync('internal/server/web/app.js','utf8');assert(!app.includes("theme:'neutral'"));assert(app.includes('dark:false,errorText:'));assert(app.includes("attributeFilter:['data-theme','data-palette']"));
 a.isConnected=false;exportNode.isConnected=false;retry.isConnected=false;removed([{removedNodes:[a]}]);assert.equal(doc.adoptedStyleSheets.length,0);
 console.log('PASS CSP CSSOM lifecycle, diagram contrast, source preservation, theme race, export isolation and retry');
})().catch(e=>{console.error(e);process.exitCode=1;});
