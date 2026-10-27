'use strict';
// Rebuild only the locally maintained animation functions in the shipped
// bundle. Use Terser 5.39.0 from an external tools directory; no install occurs.
// AIDE_TERSER_MODULE=/path/to/node_modules/terser node scripts/sync_drawio_animation.cjs [--check]
const fs=require('node:fs'),assert=require('node:assert/strict');
const tool=process.env.AIDE_TERSER_MODULE||'terser';
assert.equal(require(tool+'/package.json').version,'5.39.0','use pinned Terser for reproducible output');
const {minify}=require(tool);
const editorFile='internal/server/web/vendor/drawio/js/diagramly/Editor.js';
const dialogFile='internal/server/web/vendor/drawio/js/diagramly/Dialogs.js';
const bundleFile='internal/server/web/vendor/drawio/js/app.min.js';
const marker='/* AIDE animation validation #168 */';
function assignment(source,name){
 const a=source.indexOf(name+' = function('),b=source.indexOf('\n\t};',a);
 assert.ok(a>=0&&b>a,'source assignment '+name);return source.slice(a,b+5);
}
async function compact(code){return (await minify(code,{compress:false,mangle:true,format:{comments:false}})).code;}
async function main(){
 const editor=fs.readFileSync(editorFile,'utf8'),dialogs=fs.readFileSync(dialogFile,'utf8');let bundle=fs.readFileSync(bundleFile,'utf8');
 const validationStart=editor.indexOf(marker),parse=assignment(editor,'Editor.parseAnimationData');assert.ok(validationStart>=0);
 const parseEnd=editor.indexOf(parse)+parse.length;
 const validation=await compact(editor.slice(validationStart,parseEnd));
 let a=bundle.indexOf(marker);if(a<0)a=bundle.indexOf('Editor.parseAnimationData=function');
 let b=bundle.indexOf('Editor.AnimationPlayer=function',a);assert.ok(a>=0&&b>a);
 bundle=bundle.slice(0,a)+marker+validation+'\n'+bundle.slice(b);
 a=bundle.indexOf('Editor.AnimationPlayer.prototype.play=function');b=bundle.indexOf('Editor.AnimationPlayer.prototype.stop=function',a);assert.ok(a>=0&&b>a);
 bundle=bundle.slice(0,a)+await compact(assignment(editor,'Editor.AnimationPlayer.prototype.play'))+bundle.slice(b);
 a=dialogs.indexOf('var AnimationDialog = function');b=dialogs.indexOf('\n};',a);assert.ok(a>=0&&b>a);
 const dialog=await compact(dialogs.slice(a,b+4));assert.ok(dialog.startsWith('var AnimationDialog=')&&dialog.endsWith(';'));
 a=bundle.indexOf('AnimationDialog=function');b=bundle.indexOf(';AnimationDialog.stepsClipboard=null;',a);assert.ok(a>=0&&b>a);
 bundle=bundle.slice(0,a)+dialog.slice(4,-1)+bundle.slice(b);
 if(process.argv.includes('--check'))assert.ok(fs.readFileSync(bundleFile,'utf8')===bundle,'animation bundle must match source and pinned tool');
 else fs.writeFileSync(bundleFile,bundle);
 console.log('PASS animation bundle '+(process.argv.includes('--check')?'matches source':'regenerated from source'));
}
main().catch(e=>{console.error(e.message);process.exitCode=1;});
