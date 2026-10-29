'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict');
const {begin,end,range}=require('./drawio-freehand-source.cjs');
const tool=process.env.AIDE_TERSER_MODULE||'terser';assert.equal(require(tool+'/package.json').version,'5.39.0');const {minify}=require(tool),base='internal/server/web/vendor/drawio/js/';
async function main(){
 const source=fs.readFileSync(base+'diagramly/mxFreehand.js','utf8'),compact=begin+(await minify(source,{compress:false,mangle:true,format:{comments:false}})).code+end;
 for(const file of ['app.min.js','integrate.min.js']){
  const original=fs.readFileSync(base+file,'utf8'),[a,b]=range(original),output=original.slice(0,a)+compact+original.slice(b);
  if(process.argv.includes('--check'))assert.equal(output,original,file+' must match source');else fs.writeFileSync(base+file,output);
  console.log('PASS freehand preview '+file+' '+(process.argv.includes('--check')?'matches source':'synchronized'));
 }
}
main().catch(e=>{console.error(e.message);process.exitCode=1;});
