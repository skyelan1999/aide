'use strict';
const fs=require('fs'),assert=require('assert/strict'),{assignment}=require('./test_drawio_layout_fixes.cjs'),review=require('./drawio-layout-second-review.cjs'),{propertyAssignment}=require('./drawio-second-layout-browser-cases.cjs');
const moduleName=process.env.AIDE_TERSER_MODULE||'terser',terser=require(moduleName);assert.equal(require(moduleName+'/package.json').version,'5.39.0');
async function compact(code){return(await terser.minify(code,{compress:false,mangle:true,format:{comments:false}})).code.replace(/;$/,'');}
async function main(){for(const f of ['js/app.min.js','js/integrate.min.js','mxgraph/mxClient.js','js/shapes-14-6-5.min.js','js/viewer-static.min.js','js/extensions.min.js','js/orgchart.min.js']){const p=review.base+f,original=fs.readFileSync(p,'utf8');let s=original;for(const [source,name]of review.methods){if(!s.includes(name+'='))continue;const before=assignment(s,name).slice(0,-1),after=await compact(assignment(fs.readFileSync(review.base+'mxgraph/src/'+source,'utf8'),name));s=s.replace(before,after);}
for(const [source,name]of [['mxAWS4.js','mxShapeAws4ProductIcon.prototype.paintVertexShape'],['mxCiscoSafe.js','mxShapeCiscoSafeCompositeIcon.prototype.paintVertexShape']]){if(!s.includes(name+'='))continue;s=s.replace(assignment(s,name).slice(0,-1),await compact(assignment(fs.readFileSync(review.base+'shapes/'+source,'utf8'),name)));}
const name='mxShapeGCP2HexIcon.prototype.customProperties';if(s.includes(name+'='))s=s.replace(propertyAssignment(s,name),await compact(propertyAssignment(fs.readFileSync(review.base+'shapes/mxGCP2.js','utf8'),name)+';'));
for(const [name,want,wrong]of [['branchOptimizerAllSingleColumnLeft','singleColumnLeft','singleColumnRight'],['branchOptimizerAllSingleColumnRight','singleColumnRight','singleColumnLeft']]){
const re=new RegExp('('+name+':function\\([\\w$]+\\)\\{return [\\w$]+\\.getIsAssistantRoot\\(\\)\\?null:)"'+wrong+'"','g');s=s.replace(re,'$1"'+want+'"');}
if(process.argv.includes('--check'))assert(s===original,p+' needs sync');else fs.writeFileSync(p,s);console.log('PASS second layout sync '+f);}}
main().catch(e=>{console.error(e.message);process.exitCode=1;});
