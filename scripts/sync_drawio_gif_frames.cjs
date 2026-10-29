'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict');
const tool=process.env.AIDE_TERSER_MODULE||'terser';assert.equal(require(tool+'/package.json').version,'5.39.0');
const {minify}=require(tool),base='internal/server/web/vendor/drawio/js/';
async function main(){
 const source=fs.readFileSync(base+'diagramly/gif/AnimatedExport.js','utf8');
 const marker='/* AIDE GIF frame budget #181 */',compact=marker+(await minify(source,{compress:false,mangle:true,format:{comments:false}})).code+'\n';
 for(const file of ['app.min.js','integrate.min.js']){
  const original=fs.readFileSync(base+file,'utf8');let a=original.indexOf(marker);if(a<0)a=original.indexOf('function AnimatedGifExport(');
  const last=original.indexOf('AnimatedGifExport.removeAnimationCss=function',a),b=original.indexOf(file==='app.min.js'?'DrawioClient=function':'\nvar $jscomp=',last);assert.ok(a>=0&&last>a&&b>last);
  const output=original.slice(0,a)+compact+original.slice(b);
  if(process.argv.includes('--check'))assert.equal(output,original,file+' must match source');else fs.writeFileSync(base+file,output);
  console.log('PASS GIF frames '+file+' '+(process.argv.includes('--check')?'matches source':'synchronized'));
 }
}
main().catch(e=>{console.error(e.message);process.exitCode=1;});
