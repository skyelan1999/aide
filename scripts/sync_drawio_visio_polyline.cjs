'use strict';
// Rebuild only the PolylineTo handler using external, pinned Terser 5.39.0.
const fs=require('node:fs'),assert=require('node:assert/strict');
const tool=process.env.AIDE_TERSER_MODULE||'terser';assert.equal(require(tool+'/package.json').version,'5.39.0');
const {minify}=require(tool),base='internal/server/web/vendor/drawio/js/';
async function main(){
 const source=fs.readFileSync(base+'diagramly/vsdx/importer.js','utf8');
 const start=source.indexOf('PolylineTo.prototype.handle = function'),end=source.indexOf('return PolylineTo;',start);assert.ok(start>=0&&end>start);
 const code=source.slice(start,end).replace('PolylineTo.prototype.handle','a.prototype.handle');
 const marker='/* AIDE Visio polyline budget #177 */';
 const compact=marker+(await minify(code,{compress:false,mangle:true,format:{comments:false}})).code;
 for(const file of ['extensions.min.js','integrate.min.js']){
  const original=fs.readFileSync(base+file,'utf8'),type=original.indexOf('.PolylineTo=k;k.__class=');assert.ok(type>=0);
  let a=original.lastIndexOf(marker,type);if(a<0)a=original.lastIndexOf('a.prototype.handle=function',type);
  const b=original.indexOf(';return a}(g.mxgraph.io.vsdx.geometry.Row)',a);assert.ok(a>=0&&b>a&&b<type);
  const output=original.slice(0,a)+compact+original.slice(b+1);
  if(process.argv.includes('--check'))assert.equal(output,original,file+' must match source');else fs.writeFileSync(base+file,output);
  console.log('PASS PolylineTo '+file+' '+(process.argv.includes('--check')?'matches source':'synchronized'));
 }
}
main().catch(e=>{console.error(e.message);process.exitCode=1;});
