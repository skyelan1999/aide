'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict');
const number=value=>{if(value>=-107&&value<=107)return Buffer.from([value+139]);if(value>=108&&value<=1131){const n=value-108;return Buffer.from([247+(n>>8),n&255]);}if(value<=-108&&value>=-1131){const n=-value-108;return Buffer.from([251+(n>>8),n&255]);}const b=Buffer.alloc(5);b[0]=255;b.writeInt32BE(value,1);return b;};
const dictNumber=value=>{const b=Buffer.alloc(5);b[0]=29;b.writeInt32BE(value,1);return b;};
function index(objects){if(!objects.length)return Buffer.from([0,0]);const length=objects.reduce((n,b)=>n+b.length,0)+1,size=length<256?1:length<65536?2:4,head=Buffer.alloc(3+(objects.length+1)*size);head.writeUInt16BE(objects.length);head[2]=size;let cursor=3,offset=1;for(const object of[...objects,Buffer.alloc(0)]){head.writeUIntBE(offset,cursor,size);cursor+=size;offset+=object.length;}return Buffer.concat([head,...objects]);}
function cffProgram(program){const name=index([Buffer.from('Test')]),top=offset=>index([Buffer.concat([dictNumber(offset),Buffer.from([17])])]),prefix=Buffer.concat([Buffer.from([1,0,4,4]),name,top(0),index([]),index([])]);return Buffer.concat([Buffer.from([1,0,4,4]),name,top(prefix.length),index([]),index([]),index([Buffer.from([14]),Buffer.from(program)])]);}
function cffCharset(valid){return Buffer.from([1,0,4,4,0,1,1,1,2,65,0,1,1,1,5,170,15,162,17,0,0,0,0,0,2,1,1,2,3,14,14,1,...(valid?[0,1,0]:[])]);}
function cidCff(valid){
 const name=index([Buffer.from('CIDTest')]),strings=index([Buffer.from('Adobe'),Buffer.from('Identity')]),global=index([]),chars=index([Buffer.from([14]),Buffer.from([14])]),charset=Buffer.from([0,0,1]),fdArray=index([Buffer.alloc(0)]);
 const select=valid?Buffer.from([3,0,1,0,0,0,0,2]):Buffer.from([3,0,150,...Array.from({length:150},(_,i)=>i%2?[255,255,0]:[0,0,0]).flat(),0,0]);
 const top=offsets=>index([Buffer.concat([dictNumber(391),dictNumber(392),dictNumber(0),Buffer.from([12,30]),dictNumber(2),Buffer.from([12,34]),dictNumber(offsets[0]),Buffer.from([15]),dictNumber(offsets[1]),Buffer.from([17]),dictNumber(offsets[2]),Buffer.from([12,36]),dictNumber(offsets[3]),Buffer.from([12,37])])]);
 const start=4+name.length+top([0,0,0,0]).length+strings.length+global.length,offsets=[start,start+charset.length,start+charset.length+chars.length,start+charset.length+chars.length+fdArray.length];
 return Buffer.concat([Buffer.from([1,0,4,4]),name,top(offsets),strings,global,charset,chars,fdArray,select]);
}
function type1Font(recursive){
 const header=Buffer.from('%!PS-AdobeFont-1.0: TinyType1 1.0\n11 dict begin /FontName /TinyType1 def /PaintType 0 def /FontType 1 def /FontMatrix [0.001 0 0 0.001 0 0] def /FontBBox [0 0 500 500] def /Encoding 256 array 0 1 255 {1 index exch /.notdef put} for dup 65 /A put def currentdict end currentfile eexec\n');
 const glyph=Buffer.concat([number(0),number(500),Buffer.from([13]),number(0),Buffer.from([10]),number(0),number(0),Buffer.from([21]),number(500),number(0),Buffer.from([5]),number(-250),number(500),Buffer.from([5]),number(-250),number(-500),Buffer.from([5,9,14])]),subr=recursive?Buffer.from([139,10,11]):Buffer.from([11]);
 const plain=Buffer.concat([Buffer.alloc(4),Buffer.from('dup /Private 8 dict dup begin /lenIV -1 def /RD {string currentfile exch readstring pop} executeonly def /ND {noaccess def} executeonly def /NP {noaccess put} executeonly def /Subrs 1 array dup 0 '+subr.length+' RD '),subr,Buffer.from(' NP def /CharStrings 1 dict dup begin /A '+glyph.length+' RD '),glyph,Buffer.from(' ND end end readonly put noaccess put dup /FontName get exch definefont pop mark currentfile closefile\n')]);
 const cipher=Buffer.alloc(plain.length);let key=55665;for(let i=0;i<plain.length;i++){const c=plain[i]^(key>>8);cipher[i]=c;key=((c+key)*52845+22719)&65535;}
 return{bytes:Buffer.concat([header,cipher,Buffer.from('\n'+'0'.repeat(512)+'\ncleartomark\n')]),length1:header.length,length2:cipher.length};
}
function compositeGlyph(index,components=1){const b=Buffer.alloc(10+components*8);b.writeInt16BE(-1);for(let n=0;n<components;n++){const p=10+n*8;b.writeUInt16BE(3+(n+1<components?32:0),p);b.writeUInt16BE(index,p+2);}return b;}
async function main(){
 const source=fs.readFileSync(process.env.AIDE_PDF_FONT_PROGRAM_BASELINE||'internal/server/web/vendor/pdfjs/pdf.worker.min.mjs','utf8');
 const {CFFParser,Stream,Type1CharString,compileGlyf,TrueTypeCompiled,Parser,Lexer,StringStream,CFFCompiler}=await import('data:text/javascript;base64,'+Buffer.from(source+'\nexport{CFFParser,Stream,Type1CharString,compileGlyf,TrueTypeCompiled,Parser,Lexer,StringStream,CFFCompiler};').toString('base64'));
 const parser=bytes=>new CFFParser(new Stream(Uint8Array.from(bytes)),{},false),format=e=>e.name==='FormatError',rows=[];
 async function test(name,fn){if(process.env.AIDE_PDF_TEST_TRACE)console.log('START '+name);try{rows.push({name,status:'PASS',detail:await fn()});}catch(e){rows.push({name,status:'FAIL',error:e.message});}}
 await test('545 long inline dictionary bypasses cache without losing image bytes',()=>{
  for(const count of[0,256*1024]){const p=new Parser({lexer:new Lexer(new StringStream('BI /W 1 /H 1 /CS /RGB /BPC 8 %'+'x'.repeat(count)+'\nID \xff\0\0 EI Q\n')),xref:{fetchIfRef:x=>x}}),image=p.getObj();assert.deepEqual([...image.getBytes(3)],[255,0,0]);assert.equal(Object.keys(p.imageCache).length,count?0:1);}
 });
 await test('546 truncated formats1/2 reject before nonprogressing loops',()=>{
  for(const mode of[1,2]){const bytes=Uint8Array.from([0,0,0,mode]),p=parser(bytes);let reads=0;p.bytes=new Proxy(bytes,{get(target,key){if(typeof key==='string'&&/^\d+$/.test(key)&&++reads>1024)throw Error('owned charset read trap');const v=Reflect.get(target,key,target);return typeof v==='function'?v.bind(target):v;}});assert.throws(()=>p.parseCharsets(3,2,{get:x=>'sid'+x},false),format);assert(reads<1024);}
  if(!process.env.AIDE_PDF_FONT_PROGRAM_BASELINE)assert.equal(parser([0,0,0,1,0,1,5]).parseCharsets(3,2,{get:x=>x},false).charset.length,2);
 });
 await test('546 valid explicit/range charsets and complete tiny font',()=>{
  for(const [bytes,expected]of [[[0,0,0,0,0,1,0,2],['.notdef','s1','s2']],[[0,0,0,1,0,1,1],['.notdef','s1','s2']],[[0,0,0,2,0,1,0,1],['.notdef','s1','s2']]])assert.deepEqual(parser(bytes).parseCharsets(3,3,{get:x=>'s'+x},false).charset,expected);
  assert.equal(parser(cffCharset(true)).parse().charStrings.count,2);if(!process.env.AIDE_PDF_FONT_PROGRAM_BASELINE)assert.throws(()=>parser(cffCharset(false)).parse(),format);
 });
 await test('547 malformed FDSelect ranges reject before append amplification',()=>{
  const bytes=Buffer.from([3,0,150,...Array.from({length:150},(_,i)=>i%2?[255,255,0]:[0,0,0]).flat(),0,0]),p=parser(bytes),push=Array.prototype.push;let writes=0;Array.prototype.push=function(...values){if(++writes>512)throw Error('owned FDSelect append trap');return push.apply(this,values);};
  try{assert.throws(()=>p.parseFDSelect(0,2),format);assert.equal(writes,0);}finally{Array.prototype.push=push;}
  for(const value of[[3,0,1,0,0,0,255,255],[3,0,2,0,0,0,0,1,0,0,0],[0,1]])assert.throws(()=>parser(value).parseFDSelect(0,2),format);
 });
 await test('547 valid formats0/3, full16bit glyph count and complete CID font',()=>{
  for(const bytes of[[0,1,2],[3,0,2,0,0,1,0,1,2,0,2]]){const select=parser(bytes).parseFDSelect(0,2);assert.equal(select.getFDIndex(0),1);assert.equal(select.getFDIndex(1),2);}
  const full=parser([3,0,1,0,0,0,255,255]).parseFDSelect(0,65535);assert.equal(full.fdSelect.length,65535);const original=parser(cidCff(true)).parse();assert.equal(original.fdSelect.fdSelect.length,2);assert.equal(parser(new CFFCompiler(original).compile()).parse().charStrings.count,2);if(!process.env.AIDE_PDF_FONT_PROGRAM_BASELINE)assert.throws(()=>parser(cidCff(false)).parse(),format);
 });
 const state=()=>({callDepth:0,stackSize:0,stack:[],undefStack:true,hints:0,firstStackClearing:true,seac:null,width:null,hasVStems:false});
 await test('548 stack48 boundary and stack-producing operators reject before write49',()=>{
  const p=parser([]),s=state();s.stack=new Proxy([],{set(target,key,value){if(/^\d+$/.test(String(key))&&Number(key)>=48)throw Error('owned Type2 stack write trap');return Reflect.set(target,key,value);}});assert.equal(p.parseCharString(s,Uint8Array.from([...Array(49).fill(139),1,14]),null,null),false);
  const valid=state();assert.equal(p.parseCharString(valid,Uint8Array.from([...Array(48).fill(139),1,14]),null,null),true);assert.equal(valid.hints,24);
  const duplicate=state();duplicate.stackSize=48;duplicate.stack=Array(48).fill(0);assert.equal(p.parseCharString(duplicate,Uint8Array.from([12,27,14]),null,null),false);
 });
 await test('548 shared token/subroutine work bound and depth cleanup',()=>{
  const p=parser([]),program=Uint8Array.from([...Array.from({length:50000},()=>[139,12,18]).flat(),14]),s=state();assert.equal(p.parseCharString(s,program,null,null),false);assert(s._aideTokenCount<=100001);const root=state(),subr=Uint8Array.from([32,10,11]),index={count:1,get:()=>subr};assert.equal(p.parseCharString(root,subr,index,null),false);assert.equal(root.callDepth,0);
 });
 await test('549 recursive Type1 subr invalidates only glyph and releases state',()=>{
  const compiler=new Type1CharString(),body=Uint8Array.from([139,10,11]);let reads=0;const subrs=new Proxy([body],{get(target,key){if(key==='0'&&++reads>512)throw Error('owned Type1 recursion trap');return Reflect.get(target,key);}});assert.equal(compiler.convert(body,subrs,false),true);assert(reads<512);assert.equal(compiler._aideSubrDepth,0);assert.equal(compiler._aideActiveSubrs.size,0);assert.equal(compiler.convert(Uint8Array.from([139,10,139,10,14]),[Uint8Array.from([11])],false),false);
 });
 await test('549 allowed Type1 nesting64 and overflow65',()=>{
  for(const count of[63,64]){const subrs=Array.from({length:count},(_,i)=>i+1===count?Uint8Array.from([11]):Uint8Array.from([...number(i+1),10,11]));assert.equal(new Type1CharString().convert(Uint8Array.from([139,10,14]),subrs,false),count===64);}
 });
 await test('550 composite self/mutual cycles fail before owned command trap',()=>{
  for(const mutual of[false,true]){const glyphs=mutual?[compositeGlyph(1),compositeGlyph(0)]:[compositeGlyph(0)];let calls=0;const path={add(){if(++calls>512)throw Error('owned composite command trap');}};assert.throws(()=>compileGlyf(glyphs[0],path,{glyphs}),format);assert(calls<512);assert.throws(()=>new TrueTypeCompiled(glyphs,[]).compileGlyph(glyphs[0],0),format);}
 });
 await test('550 combined depth and repeated-component command budget with sibling reuse',()=>{
  for(const count of[64,65]){const glyphs=Array.from({length:count},(_,i)=>i+1===count?Buffer.alloc(12):compositeGlyph(i+1));if(count===64)compileGlyf(glyphs[0],{add(){}},{glyphs});else assert.throws(()=>compileGlyf(glyphs[0],{add(){}},{glyphs}),format);}
  const glyphs=[compositeGlyph(1,40000),Buffer.alloc(12)];let calls=0;assert.throws(()=>compileGlyf(glyphs[0],{add(){if(++calls>100000)throw Error('owned path command trap');}},{glyphs}),format);assert.equal(calls,100000);compileGlyf(compositeGlyph(1,10),{add(){}},{glyphs});
 });
 console.log(JSON.stringify({checks:rows.length,rows}));if(!process.env.AIDE_PDF_EXPECT_FAILURES)assert(rows.every(r=>r.status==='PASS'));
}
if(require.main===module)main().catch(e=>{console.error(e.name+': '+e.message);process.exitCode=1;});
module.exports={number,index,cffProgram,cffCharset,cidCff,type1Font,compositeGlyph};
