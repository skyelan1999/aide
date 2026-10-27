'use strict';
// Regenerate only reviewed palette assignments, preserving unrelated bundles.
const assert=require('node:assert/strict'),fs=require('node:fs');
const {assignment}=require('./test_drawio_layout_fixes.cjs');
const {methods}=require('./test_drawio_palette_search.cjs');
const tool=process.env.AIDE_TERSER_MODULE||'terser';
assert.equal(require(tool+'/package.json').version,'5.39.0');
const base='internal/server/web/vendor/drawio/js/';
async function main(){
  const compiled=[];
  for(const [file,name]of methods)compiled.push([name,(await require(tool).minify(assignment(fs.readFileSync(base+file,'utf8'),name),{compress:false,mangle:true,format:{comments:false}})).code]);
  for(const file of ['app.min.js','integrate.min.js']){
    const original=fs.readFileSync(base+file,'utf8');let output=original;
    for(const [name,code]of compiled){const old=assignment(output,name),i=output.indexOf(old.slice(0,-1));assert.ok(i>=0);const end=i+old.length-1;output=output.slice(0,i)+code.slice(0,-1)+output.slice(end);}
    if(process.argv.includes('--check'))assert.ok(output===original,file+' palette assignments must match source');else fs.writeFileSync(base+file,output);
    console.log('PASS palette search '+file+' '+(process.argv.includes('--check')?'matches source':'synchronized'));
  }
}
main().catch(e=>{console.error(e.message);process.exitCode=1;});
