'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict');
const file='internal/server/web/vendor/drawio/js/plantuml/drawio-plantuml.min.js',start='/* AIDE PlantUML resource helpers: begin */',end='/* AIDE PlantUML resource helpers: end */';
const source=fs.readFileSync(file,'utf8'),notice=source.match(/^\/\* Modified by Aide;[^\n]*\n/)?.[0]||'',body=source.slice(notice.length),prefix='var PlantUml=(()=>{';assert(body.startsWith(prefix));
const generated=start+'\n'+fs.readFileSync('internal/server/web/vendor/drawio/js/plantuml/re2js.min.js','utf8')+'\n'+fs.readFileSync('internal/server/web/plantuml-resource-helpers.js','utf8')+'\n'+end;
let after;
if(body.includes(start)){const a=body.indexOf(start),b=body.indexOf(end,a)+end.length;assert(b>=end.length);after=body.slice(0,a)+generated+body.slice(b);}
else after=prefix+generated+body.slice(prefix.length);
after=notice+after;
if(process.argv.includes('--check'))assert(source===after,'PlantUML resource helper prefix is stale');else fs.writeFileSync(file,after);
console.log('PASS PlantUML private resource helper/payload sync');
