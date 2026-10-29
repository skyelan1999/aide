'use strict';
// Only replace the locally maintained Visio archive loader and decode method.
// No install/network. Use Terser 5.39.0 in an external tools directory.
const fs=require('node:fs'),assert=require('node:assert/strict');
const tool=process.env.AIDE_TERSER_MODULE||'terser';
assert.equal(require(tool+'/package.json').version,'5.39.0');
const {minify}=require(tool);
const base='internal/server/web/vendor/drawio/js/';
const marker='/* AIDE Visio archive budgets #176 */';
async function main(){
 const source=fs.readFileSync(base+'diagramly/vsdx/importer.js','utf8');
 const a=source.indexOf(marker),b=source.indexOf('mxVsdxCodec.prototype.createMxGraph',a);assert.ok(a>=0&&b>a);
 // Assign to the local constructor while its factory is still running.
 // Namespace references inside method bodies resolve only when invoked later.
 const code=source.slice(a+marker.length,b).replace(/\bmxVsdxCodec\b/g,'com.mxgraph.io.mxVsdxCodec')
  .replace('com.mxgraph.io.mxVsdxCodec.loadArchive =','m.loadArchive =')
  .replace('com.mxgraph.io.mxVsdxCodec.prototype.decodeVsdx =','m.prototype.decodeVsdx =');
 const compact=marker+(await minify(code,{compress:false,mangle:true,format:{comments:false}})).code+'\n';
 for(const file of ['extensions.min.js','integrate.min.js']){
  const original=fs.readFileSync(base+file,'utf8');
  let start=original.indexOf(marker);if(start<0)start=original.indexOf('m.prototype.decodeVsdx=function');
  const end=original.indexOf('m.prototype.createMxGraph=',start);assert.ok(start>=0&&end>start);
  let output=original.slice(0,start)+compact+original.slice(end);
  const stencilMarker='/* AIDE Visio stencil error forwarding #176 */';
  const sa=source.indexOf(stencilMarker),sb=source.indexOf('mxVssxCodec.prototype.normalizeGeo',sa);assert.ok(sa>=0&&sb>sa);
  const stencilCode=source.slice(sa+stencilMarker.length,sb).replace(/\bmxVssxCodec\b/g,'com.mxgraph.io.mxVssxCodec')
   .replace('com.mxgraph.io.mxVssxCodec.prototype.decodeVssx =','k.prototype.decodeVssx =');
  const stencil=stencilMarker+(await minify(stencilCode,{compress:false,mangle:true,format:{comments:false}})).code+'\n';
  let da=output.indexOf(stencilMarker);if(da<0)da=output.indexOf('k.prototype.decodeVssx=function');
  const db=output.indexOf('k.prototype.normalizeGeo=',da);assert.ok(da>=0&&db>da);
  output=output.slice(0,da)+stencil+output.slice(db);
  if(process.argv.includes('--check'))assert.equal(output,original,file+' loader must match source');
  else fs.writeFileSync(base+file,output);
  console.log('PASS Visio archive loader '+file+' '+(process.argv.includes('--check')?'matches source':'synchronized'));
 }
}
main().catch(e=>{console.error(e.message);process.exitCode=1;});
