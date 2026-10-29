'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict');
const file='internal/server/web/vendor/drawio/js/plantuml/drawio-plantuml.min.js',start='/* AIDE PlantUML resource helpers: begin */',end='/* AIDE PlantUML resource helpers: end */';
const source=fs.readFileSync(file,'utf8'),prefix='var PlantUml=(()=>{';assert(source.startsWith(prefix));
const generated=start+'\n'+fs.readFileSync('internal/server/web/vendor/drawio/js/plantuml/re2js.min.js','utf8')+'\n'+fs.readFileSync('internal/server/web/plantuml-resource-helpers.js','utf8')+'\n'+end;
let after;
if(source.includes(start)){const a=source.indexOf(start),b=source.indexOf(end,a)+end.length;assert(b>=end.length);after=source.slice(0,a)+generated+source.slice(b);}
else after=prefix+generated+source.slice(prefix.length);
if(process.argv.includes('--check'))assert(source===after,'PlantUML resource helper prefix is stale');else fs.writeFileSync(file,after);
console.log('PASS PlantUML private resource helper/payload sync');
