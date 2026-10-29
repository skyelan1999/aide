'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict');
async function runtime(){const s=fs.readFileSync(process.env.AIDE_PDF_STRUCTURE_BASELINE||'internal/server/web/vendor/pdfjs/pdf.worker.min.mjs','utf8');return import('data:text/javascript;base64,'+Buffer.from(s+'\nexport {Parser,Lexer,StringStream,XRef,Dict,Stream};').toString('base64'));}
async function main(){
 const {Parser,Lexer,StringStream,XRef,Dict,Stream}=await runtime(),rows=[];
 const parse=text=>new Parser({lexer:new Lexer(new StringStream(text))});
 const dict=values=>{const d=new Dict();for(const[k,v]of Object.entries(values))d.set(k,v);return d;};
 function test(name,fn){try{rows.push({name,status:'PASS',detail:fn()})}catch(e){rows.push({name,status:'FAIL',error:e.message})}}
 const budget=error=>error.name==='FormatError'&&/preview limit/.test(error.message);
 test('object depth rejects before stack overflow and restores parser state',()=>{
  for(const [open,close]of [['[',']'],['<< /K ',' >>']]){
   const parser=parse(open.repeat(4000)+'0'+close.repeat(4000));assert.throws(()=>parser.getObj(),budget);
   parser.lexer=new Lexer(new StringStream('7'));parser.refill();assert.equal(parser.getObj(),7);
  }
 });
 test('normal depth boundary, flat objects and decryption controls',()=>{
  let value=parse('['.repeat(127)+'0'+']'.repeat(127)).getObj();for(let i=0;i<127;i++)value=value[0];assert.equal(value,0);
  assert.throws(()=>parse('['.repeat(128)+'0'+']'.repeat(128)).getObj(),budget);
  const flat=parse('['+'0 '.repeat(5000)+']').getObj();assert.equal(flat.length,5000);
  const d=parse('<< /A [1 2 << /B (owned) >>] >>').getObj();assert.equal(d.get('A')[2].get('B'),'owned');
  assert.equal(parse('(owned)').getObj({decryptString:value=>'decrypted:'+value}),'decrypted:owned');
 });
 function guardedEntries(xref){let writes=0;const target=xref.entries;xref.entries=new Proxy(target,{set(target,key,value){if(key==='length'&&value===0)return Reflect.set(target,key,value);writes++;throw Error('owned entry allocation trap');}});return()=>writes;}
 function stream(index,size=1000001,widths=[0,0,0],bytes=[]){const d=dict({W:widths,Index:index,Size:size});return new Stream(Uint8Array.from(bytes),0,bytes.length,d);}
 test('XRef range, field widths and object IDs reject before allocation',()=>{
  const cases=[[[0,1000001]],[undefined,1000001],[undefined,-1],[undefined,1.5],[[0,1,5]],[[0,-1]],[[1000000,1]],[[0,1.5]],[[0,1],1,[1,10000000,0]],[[0,1],1,[1,-1,0]],[[0,1],1,[1,2]],[[0,1],1,[1,2,0,0]],[Array(4098).fill(0)]];
  for(const args of cases){const x=new XRef(null,{}),writes=guardedEntries(x);assert.throws(()=>x.processXRefStream(stream(...args)),budget);assert.equal(writes(),0);}
  return{cases:cases.length};
 });
 test('exact XRef count and last object ID boundaries remain accepted',()=>{
  const pending=Error('owned source-read boundary'),x=new XRef(null,{}),source=stream([0,1000000],1000000,[1,0,0]);let reads=0;source.getByte=()=>{reads++;throw pending;};assert.throws(()=>x.processXRefStream(source),e=>e===pending);assert.equal(reads,1);assert.equal(x.entries.length,0);
  const sparse=new XRef(null,{});sparse.processXRefStream(stream([999999,1],1000000));assert.equal(sparse.entries.length,1000000);assert.equal(Object.keys(sparse.entries).length,1);assert.equal(sparse.entries[999999].uncompressed,true);
 });
 function smallStream(){return stream([0,3],3,[1,3,2],[0,0,0,0,255,255,1,0,0,10,0,0,2,0,0,8,0,1]);}
 test('normal XRef stream/table and cumulative processing budget',()=>{
  const x=new XRef(null,{});x.processXRefStream(smallStream());assert.equal(x.entries.length,3);assert.equal(x.entries[0].free,true);assert.equal(x.entries[1].offset,10);assert.equal(x.entries[1].uncompressed,true);assert.equal(x.entries[2].offset,8);assert.equal(x.entries[2].gen,1);
  const writes=guardedEntries(x);assert.throws(()=>x.processXRefStream(stream([0,999998],999998)),budget);assert.equal(writes(),0);
  const classic=new XRef(null,{}),trailer=classic.processXRefTable(parse('0 2\n0000000000 65535 f\n0000000010 00000 n\ntrailer << /Size 2 >>'));assert.equal(trailer.get('Size'),2);assert.equal(classic.entries[1].offset,10);
 });
 test('resumed XRef stream reserves its range exactly once',()=>{
  const x=new XRef(null,{}),reserve=x.aideReserveXRefRecords.bind(x);let reservations=0,reads=0,ready=false;
  x.aideReserveXRefRecords=function(...args){reservations++;return reserve(...args);};
  const pending=Error('owned missing-byte boundary'),source={dict:dict({W:[1,0,0],Index:[0,1],Size:1}),pos:0,getByte(){reads++;if(!ready)throw pending;return 1;}};
  for(let i=0;i<2;i++)assert.throws(()=>x.processXRefStream(source),e=>e===pending);
  assert.equal(reservations,1);assert.equal(x.entries.length,0);ready=true;x.processXRefStream(source);assert.equal(reads,3);assert.equal(reservations,1);assert.equal(x.entries[0].uncompressed,true);
 });
 test('classic table and recovery index share the allocation guard',()=>{
  const classic=new XRef(null,{}),writes=guardedEntries(classic),parser=parse('0 1000001\n');const get=parser.getObj.bind(parser);let calls=0;parser.getObj=function(){if(++calls>2)throw Error('owned entry read trap');return get();};assert.throws(()=>classic.processXRefTable(parser),budget);assert.equal(writes(),0);
  const recovery=new XRef(new StringStream('%PDF-1.4\n1000001 0 obj\n<< /Type /Catalog >>\nendobj\n'),{}),recoveryWrites=guardedEntries(recovery);assert.throws(()=>recovery.indexObjects(),budget);assert.equal(recoveryWrites(),0);
 });
 console.log(JSON.stringify({checks:rows.length,rows}));if(!process.env.AIDE_PDF_EXPECT_FAILURES)assert(rows.every(r=>r.status==='PASS'));
}
if(require.main===module)main().catch(e=>{console.error(e.name+': '+e.message);process.exitCode=1;});
module.exports={runtime};
