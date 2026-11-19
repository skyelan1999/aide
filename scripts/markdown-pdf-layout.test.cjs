'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('internal/server/web/markdown-export.js','utf8');
const code=source.slice(source.indexOf(' function compactPdfCssColors('),source.indexOf(' async function renderVectorChunk('));
assert(code.includes('function vectorPdfDefinition'), 'vector PDF definition must be present');
const longID='a81b90ef7d4b50f2'.repeat(10);
const htmlToPdfmake=()=>[{table:{body:[[{text:'File'},{text:'Size'}],[{text:longID},{text:'18 MB'}]]}}];
const context={window:{htmlToPdfmake}};
vm.createContext(context);
vm.runInContext(code,context);
assert.equal(vm.runInContext("compactPdfCssColors('rgb(10, 20, 30) rgba(1, 2, 3, 0.5)')",context),'rgb(10,20,30) rgba(1,2,3,0.5)');
const row=(widths,values=[])=>({cells:widths.map((width,index)=>({colSpan:1,getBoundingClientRect:()=>({width}),textContent:values[index]}))});
const table={rows:[row([600,200]),row([600,200],[longID,'18 MB'])],querySelectorAll:selector=>selector==='thead tr'?[{}]:[]};
const body={innerHTML:'<table><thead><tr><th>File</th><th>Size</th></tr></thead><tbody><tr><td>report.pdf</td><td>18 MB</td></tr></tbody></table>',ownerDocument:{defaultView:{getComputedStyle:()=>({})}},querySelectorAll:selector=>selector==='table'?[table]:[]};
context.body=body;
const result=vm.runInContext('vectorPdfDefinition(body)',context);
assert.equal(result.pageSize,'A4');assert.equal(result.pageOrientation,'portrait');
assert.deepEqual(Array.from(result.pageMargins),[48,48,48,48]);
assert.equal(result.header,undefined,'empty preferences add no running header');
assert.deepEqual(Array.from(result.content[0].table.widths),['75.000%','25.000%'],'table columns retain measured proportions');
assert.equal(result.content[0].table.headerRows,1,'header repeats across pages');
assert.equal(result.defaultStyle.font,'NotoSansSC','Chinese glyphs use the bundled font');
assert.equal(result.content[0].table.body[1][0].wordBreak,'break-all','long table tokens can wrap');
assert.equal(result.content[0].table.body[1][1].wordBreak,undefined,'short cells retain natural word wrapping');
assert.equal(result.content[0].layout.vLineWidth(),0,'table grid uses light horizontal rules');
body.querySelectorAll=selector=>selector==='table'?[{...table,rows:[row([100,100,100,100,100,100])],querySelectorAll:()=>[]}]:[];
const wide=vm.runInContext('vectorPdfDefinition(body)',context);
assert.equal(wide.pageOrientation,'landscape','wide tables use landscape pages');
const branded=vm.runInContext("vectorPdfDefinition(body, [], null, {color:'#1264a3',logo:'fixture',rows:[[{text:'Company',bold:true}],[{text:'Internal'}]],lineCount:2})",context);
assert.equal(branded.pageOrientation,'landscape','appearance does not change automatic orientation');
assert.deepEqual(Array.from(branded.pageMargins),[48,66,48,48],'reserve room for the logo and text');
assert.deepEqual(Array.from(branded.header().margin),[48,18,136,0]);
const leftLogo=vm.runInContext("vectorPdfDefinition(body, [], null, {logo:'fixture',logoSide:'left',rows:[[{text:'Company'}]],lineCount:1})",context);
assert.deepEqual(Array.from(leftLogo.header().margin),[136,18,48,0],'header text leaves room on the selected side');
assert.equal(branded.header().stack[0].text[0].bold,true,'HTML formatting is retained in the running header');
const logoOnly=vm.runInContext("vectorPdfDefinition(body, [], null, {logo:'fixture'})",context);
assert.equal(logoOnly.header,undefined,'a logo does not insert a text header');
assert.equal(logoOnly.pageMargins[1],66,'logo-only layouts also reserve space');
const tall=vm.runInContext('fitPdfMedia(800,9000,499,700)',context);
assert.equal(tall.height,700);
assert.ok(Math.abs(tall.width/tall.height-800/9000)<1e-10,'portrait image keeps aspect ratio');
const panorama=vm.runInContext('fitPdfMedia(8000,700,499,700)',context);
assert.equal(panorama.width,499);
assert.ok(Math.abs(panorama.width/panorama.height-8000/700)<1e-10,'panorama keeps aspect ratio');
console.log('PASS: vector PDF table proportions, repeat headers, wide-table layout, CSS colors and Chinese font');

const attached=vm.runInContext("vectorPdfDefinition(body,[{name:'sample.bin',dataUrl:'data:application/octet-stream;base64,AA==',index:0}])",context);
assert.equal(attached.content.at(-1).columns[1].attachment.hidden,false,'embedded files must be discoverable in reader attachment panels');
assert.equal(attached.content.at(-1).columns[1].attachment.relationship,'Data');

const node=(tagName,childNodes=[],style='')=>({
 nodeType:1,tagName,childNodes,getAttribute:name=>name==='style'?style:null
});
const literal=data=>({nodeType:3,data});
const host={createElement:name=>{
 assert.equal(name,'template','HTML is parsed away from the live document');
 return{content:{childNodes:[
  node('P',[node('STRONG',[literal('Company')]),node('BR'),node('SPAN',[literal('Internal')],'color:#812852')]),
  node('SCRIPT',[literal('active script')]),
  node('svg',[node('script',[literal('foreign script')])]),
  node('IMG')
 ]}};
}};
const appearanceContext={window:{},document:host,localStorage:{getItem:()=>null}};
vm.createContext(appearanceContext);
vm.runInContext(fs.readFileSync('internal/server/web/markdown-pdf-appearance.js','utf8'),appearanceContext);
const appearance=appearanceContext.window.AideMarkdownPdfAppearance;
assert.equal(appearance.read('document').header,'','new document profiles have no default header');
assert.equal(appearance.read('document').logoSide,'right','existing settings without a position keep the right-side logo');
assert.equal(appearance.compile({logoSide:'left'}).logoSide,'left');
const compiled=appearance.compile({header:'test markup'});
assert.deepEqual(JSON.parse(JSON.stringify(compiled.rows)),[[{text:'Company',bold:true}],[{text:'Internal',color:'#812852'}]]);
assert.equal(compiled.lineCount,2);
