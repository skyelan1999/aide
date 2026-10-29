'use strict';
// Synchronize the complete edge-curve method; preserve its captured namespace.
const fs=require('node:fs'),assert=require('node:assert/strict');
const tool=process.env.AIDE_TERSER_MODULE||'terser';assert.equal(require(tool+'/package.json').version,'5.39.0');
const {minify}=require(tool),base='internal/server/web/vendor/drawio/js/';
async function main(){
 const source=fs.readFileSync(base+'diagramly/vsdx/importer.js','utf8');
 const a=source.indexOf('VsdxShape.prototype.getControlPoints ='),b=source.indexOf('VsdxShape.prototype.getStyleFromEdgeShape',a);assert.ok(a>=0&&b>a);
 // The source method has its own local variable named g. Capture the bundle's
 // namespace argument in a distinct closure before mangling either scope.
 const code='(function(aideVisioNamespace){'+source.slice(a,b).replace('VsdxShape.prototype.getControlPoints','c.prototype.getControlPoints').replace(/\bcom\b/g,'aideVisioNamespace')+'})(g);';
 const marker='/* AIDE Visio NURBS budgets #178 */';
 const compact=marker+(await minify(code,{compress:false,mangle:true,format:{comments:false}})).code+'\n';
 for(const file of ['extensions.min.js','integrate.min.js']){
  const original=fs.readFileSync(base+file,'utf8');let start=original.indexOf(marker);if(start<0)start=original.indexOf('c.prototype.getControlPoints=function');
  const end=original.indexOf('c.prototype.getStyleFromEdgeShape=',start);assert.ok(start>=0&&end>start);
  const output=original.slice(0,start)+compact+original.slice(end);
  if(process.argv.includes('--check'))assert.equal(output,original,file+' must match source');else fs.writeFileSync(base+file,output);
  console.log('PASS NURBS budgets '+file+' '+(process.argv.includes('--check')?'matches source':'synchronized'));
 }
}
main().catch(e=>{console.error(e.message);process.exitCode=1;});
