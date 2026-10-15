'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/server/web/markdown-export.js','utf8');
// Exercise the production text slicer against measured line boxes. Mixed inline
// runs start mid-line; assigning each a full parent width caused wrapping/overlap.
const code=source.slice(source.indexOf('     const place='),source.indexOf('     const sliceNode='));
const rect=(left,top,width,height=14)=>({left,top,right:left+width,bottom:top+height,width,height});
const glyphs=Array.from({length:12},(_,i)=>rect(100+(i%4)*8,200+Math.floor(i/4)*22,8));
let start=0,end=0;
const text='abcdEFGHijkl';
const range={setStart(_n,i){start=i;},setEnd(_n,i){end=i;},getBoundingClientRect(){const rs=glyphs.slice(start,end);return rect(Math.min(...rs.map(r=>r.left)),Math.min(...rs.map(r=>r.top)),Math.max(...rs.map(r=>r.right))-Math.min(...rs.map(r=>r.left)),Math.max(...rs.map(r=>r.bottom))-Math.min(...rs.map(r=>r.top)));}};
const renderDoc={createElement(){return {style:{}};},createDocumentFragment(){return {children:[],append(n){this.children.push(n);}};}};
const style={font:'italic 12px monospace',whiteSpace:'pre-wrap',color:'#123456',letterSpacing:'normal',textDecoration:'none'};
const node={nodeValue:text,parentElement:{},ownerDocument:{createRange:()=>range,defaultView:{getComputedStyle:()=>style}}};
const context={renderDoc,sourceBox:{top:100},y:110,h:50,node,parent:{left:80,top:180}};
vm.createContext(context);vm.runInContext(code+'\nresult=sliceText(node,parent);',context);
assert.deepEqual(Array.from(context.result.children,n=>n.textContent),['abcd','EFGH','ijkl'],'only intersecting measured lines are copied, separately');
assert.deepEqual(Array.from(context.result.children,n=>n.style.top),['20px','42px','64px']);
for(const span of context.result.children){assert.equal(span.style.left,'20px');assert.equal(span.style.width,'max-content');assert.equal(span.style.whiteSpace,'pre');assert.equal(span.style.font,style.font);}
context.y=125;context.h=14;vm.runInContext('result=sliceText(node,parent);',context);assert.deepEqual(Array.from(context.result.children,n=>n.textContent),['EFGH'],'next page excludes lines outside its viewport');
// Preserve source whitespace rules while keeping code indentation exact.
node.nodeValue='a  bEFGHijkl';style.whiteSpace='normal';context.y=100;context.h=14;vm.runInContext('result=sliceText(node,parent);',context);assert.equal(context.result.children[0].textContent,'a b');
style.whiteSpace='pre-wrap';vm.runInContext('result=sliceText(node,parent);',context);assert.equal(context.result.children[0].textContent,'a  b');
console.log('PASS: PDF mixed-inline measured line positions, page clipping, typography and code whitespace');
