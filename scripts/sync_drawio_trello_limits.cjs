'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict');
const tool=process.env.AIDE_TERSER_MODULE||'terser';assert.equal(require(tool+'/package.json').version,'5.39.0');
const {minify}=require(tool),base='internal/server/web/vendor/drawio/js/';
async function main(){
 const source=fs.readFileSync(base+'diagramly/TrelloClient.js','utf8');
 const a=source.indexOf('TrelloClient.prototype.writeFile ='),b=source.indexOf('TrelloClient.prototype.pickLibrary =',a);assert.ok(a>=0&&b>a);
 const marker='/* AIDE Trello byte limit #180 */',compact=marker+(await minify(source.slice(a,b),{compress:false,mangle:true,format:{comments:false}})).code+'\n';
 const file=base+'app.min.js',original=fs.readFileSync(file,'utf8');let start=original.indexOf(marker);if(start<0)start=original.indexOf('TrelloClient.prototype.writeFile=function');
 const end=original.indexOf('TrelloClient.prototype.pickLibrary=',start);assert.ok(start>=0&&end>start);
 const output=original.slice(0,start)+compact+original.slice(end);
 if(process.argv.includes('--check'))assert.equal(output,original,'Trello writeFile must match source');else fs.writeFileSync(file,output);
 console.log('PASS Trello byte guard '+(process.argv.includes('--check')?'matches source':'synchronized'));
}
main().catch(e=>{console.error(e.message);process.exitCode=1;});
