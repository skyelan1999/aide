const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/server/web/app.js','utf8');
function fn(name){const a=source.indexOf('function '+name+'(');assert.ok(a>=0,name);return source.slice(a,source.indexOf('\n}',a)+2);}
class Node{constructor(tag,cls,text){this.tag=tag;this.className=cls||'';this.textContent=text||'';this.children=[];this.attrs={};}append(...items){for(const item of items)item.parent=this;this.children.push(...items);}setAttribute(k,v){this.attrs[k]=v;}replaceWith(other){const children=this.parent.children;children[children.indexOf(this)]=other;other.parent=this.parent;}}
const c={el:(...args)=>new Node(...args),t:(s,...args)=>s.replace(/\{(\d+)\}/g,(_,n)=>args[n])};vm.createContext(c);vm.runInContext(fn('fileEntryName'),c);
const name=new Node('span','file-name','report.txt');
const label=c.fileEntryName({unix:{owner:'aide',group:'team',permissions:'-rw-r-----'}},name);
assert.equal(label.children[0],name);const meta=label.children[1];assert.equal(meta.textContent,'aide:team  -rw-r-----');assert.match(meta.attrs['aria-label'],/所有者：aide/);assert.equal(meta.title,meta.attrs['aria-label']);
const input=new Node('input','file-rename');name.replaceWith(input);assert.equal(label.children[1],meta,'renaming does not replace ownership line');input.replaceWith(name);assert.equal(label.children[0],name);
for(const unix of [undefined,{owner:'a',group:'b',permissions:'bad'},{owner:'a',permissions:'-rw-r--r--'}])assert.equal(c.fileEntryName({unix},name),name,'unknown metadata preserves original one-line name');
const unsafe=c.fileEntryName({unix:{owner:'<img onerror=x>',group:'1001',permissions:'drwxrwxr-t+'}},new Node('span','','folder'));assert.equal(unsafe.children[1].children.length,0);assert.match(unsafe.children[1].textContent,/<img onerror=x>/,'names remain literal text');
assert.match(fn('renderFileEntries'),/b\.append\(icon, fileEntryName\(file, nameSpan\)\)/);assert.match(fn('renderFileEntries'),/openFileContextMenu\(ev, b, nameSpan, file\)/,'context actions keep original name target');
const css=fs.readFileSync('internal/server/web/style.css','utf8');assert.match(css,/\.file-item \.file-unix-meta\{[^}]*font:9px\/1\.4/);assert.match(css,/\.file-item \.file-unix-meta\{[^}]*text-overflow:ellipsis/);
console.log('file Unix metadata: small second line, missing data, safe text, full accessible label, rename isolation and original action target passed');
