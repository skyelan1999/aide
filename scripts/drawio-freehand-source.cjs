'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict');
const files=['diagramly/mxFreehand.js','app.min.js','integrate.min.js'];
const begin='/* AIDE bounded freehand preview #182 */',end='/* AIDE bounded freehand preview end #182 */';
// This constructor has string literals/comments and no regex/template literals.
// Locate its closing brace without parsing the unrelated multi-megabyte bundle.
function functionEnd(text,start){
 let depth=0,quote=null,seen=false;
 for(let i=start;i<text.length;i++){
  const c=text[i];
  if(quote){if(c==='\\')i++;else if(c===quote)quote=null;continue;}
  if(c==='"'||c==="'"){quote=c;continue;}
  if(c==='/'&&text[i+1]==='/'){const n=text.indexOf('\n',i+2);i=n<0?text.length:n;continue;}
  if(c==='/'&&text[i+1]==='*'){const n=text.indexOf('*/',i+2);assert.ok(n>=0);i=n+1;continue;}
  if(c==='{'){depth++;seen=true;}else if(c==='}'&&--depth===0&&seen)return i+1;
 }
 throw Error('freehand constructor boundary not found');
}
function range(text){
 let a=text.indexOf(begin);if(a>=0){const b=text.indexOf(end,a);assert.ok(b>a);return [a,b+end.length];}
 a=text.indexOf('function mxFreehand(');assert.ok(a>=0);return [a,functionEnd(text,a)];
}
function code(file){const text=fs.readFileSync('internal/server/web/vendor/drawio/js/'+file,'utf8');if(file===files[0])return text;const [a,b]=range(text);return text.slice(a,b);}
module.exports={files,begin,end,range,code};
