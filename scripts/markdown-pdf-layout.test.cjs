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
// An oversized table row has parallel cells. A tall first cell must not stop
// the traversal before other cells intersecting the same page are copied.
const box=(tag,left,width,height,children=[])=>({nodeType:1,tagName:tag,children,childNodes:children,ownerDocument:{defaultView:{getComputedStyle:()=>({display:'block',borderLeftWidth:'0',borderTopWidth:'0'})}},getBoundingClientRect:()=>rect(left,100,width,height),cloneNode(deep){return {tagName:tag,style:{},children:deep?children:[],append(child){this.children.push(child);},querySelectorAll(){return[];}};},querySelectorAll:()=>[],getAttribute:()=>null});
const row=box('TR',60,674,1800,[box('TD',60,337,1800,[box('P',69,319,24)]),box('TD',397,337,1800,[box('P',406,319,24)])]);
Object.assign(context,{PAGE:987,measured:new WeakMap(),childCursor:new WeakMap(),sourceBox:{top:100,left:60},y:0,h:987,row});
vm.runInContext(source.slice(source.indexOf('     const sliceNode='),source.indexOf('     for(let i=firstBlock;'))+'\nresult=sliceNode(row,{left:60,top:100});',context);
assert.equal(context.result.children.length,2,'all visible cells of a tall row must be retained on each page');
const late=box('P',69,319,24);late.getBoundingClientRect=()=>rect(69,1100,319,24);row.children[0].children.push(late);
const lateRight=box('P',406,319,24);lateRight.getBoundingClientRect=()=>rect(406,1100,319,24);row.children[1].children.push(lateRight);
context.y=987;vm.runInContext('result=sliceNode(row,{left:60,top:1087});',context);
assert.equal(context.result.children.length,2,'both cells continue on the next page');assert(context.result.children.every(cell=>cell.children.length===1),'earlier cell content is pruned and later content retained');
console.log('PASS: PDF mixed-inline measured lines, clipping, typography, code whitespace and parallel tall-cell continuation');
