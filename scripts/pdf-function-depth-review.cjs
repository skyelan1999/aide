'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict');
async function runtime(){const source=fs.readFileSync(process.env.AIDE_PDF_FUNCTION_BASELINE||'internal/server/web/vendor/pdfjs/pdf.worker.min.mjs','utf8');return import('data:text/javascript;base64,'+Buffer.from(source+'\nexport {ColorSpace,Dict,Name,Stream,StringStream,LocalColorSpaceCache,PDFFunction,PostScriptParser,PostScriptLexer};').toString('base64'));}
async function main(){const{ColorSpace,Dict,Name,Stream,StringStream,LocalColorSpaceCache,PDFFunction,PostScriptParser,PostScriptLexer}=await runtime(),rows=[];
 const dict=entries=>{const value=new Dict();for(const[k,v]of Object.entries(entries))value.set(k,v);return value;},format=e=>e.name==='FormatError';
 async function test(name,fn){try{rows.push({name,status:'PASS',detail:await fn()});}catch(error){rows.push({name,status:'FAIL',error:error.message});}}
 await test('507 cyclic aliases stop before controlled recursion trap and recover',()=>{for(const mutual of[false,true]){let reads=0;const xref={fetchIfRef(value){if(++reads>1024)throw Error('owned recursion trap');return value;}},resources=dict({ColorSpace:dict({A:Name.get(mutual?'B':'A'),B:Name.get('A')})}),cache=new LocalColorSpaceCache();assert.throws(()=>ColorSpace.parse({cs:Name.get('A'),xref,resources,localColorSpaceCache:cache}),format);assert(reads<1024);resources.get('ColorSpace').set('A',Name.get('DeviceRGB'));assert.equal(ColorSpace.parse({cs:Name.get('A'),xref,resources,localColorSpaceCache:cache}).numComps,3);}});
 await test('507 alias-depth boundary, normal Indexed and independent async controls',async()=>{const xref={fetchIfRef:x=>x};function chain(count){const aliases=new Dict();for(let i=0;i<count;i++)aliases.set('N'+i,Name.get(i===count-1?'DeviceRGB':'N'+(i+1)));return dict({ColorSpace:aliases});}
 assert.equal(ColorSpace._parse(Name.get('N0'),xref,chain(127)).numComps,3);assert.throws(()=>ColorSpace._parse(Name.get('N0'),xref,chain(128)),format);
 const color=ColorSpace._parse([Name.get('Indexed'),Name.get('DeviceRGB'),0,'\xff\0\0'],xref);assert.deepEqual([...color.getRgb([0],0)],[255,0,0]);
 const colors=await Promise.all(Array.from({length:8},()=>ColorSpace.parseAsync({cs:Name.get('N0'),xref,resources:chain(2),localColorSpaceCache:new LocalColorSpaceCache()})));assert(colors.every(value=>value.numComps===3));});
 await test('508 reject invalid or excessive samples before array allocation/read',()=>{
 const saved=globalThis.Array;let allocations=0,reads=0;globalThis.Array=new Proxy(saved,{construct(target,args){if(args.length===1&&typeof args[0]==='number'&&args[0]>1048576){allocations++;throw Error('owned array allocation trap');}return Reflect.construct(target,args);}});
 try{const stream={getBytes(){reads++;throw Error('owned byte read trap');}};for(const[size,outputs,bits]of[[[1000000],3,8],[[1431655766],3,8],[[],1,8],[[2,0],1,8],[[2,-1],1,8],[[2,1.5],1,8],[[Infinity],1,8],[[2],33,8],[[2],0,8],[[2],1,3],[saved(9).fill(1),1,8]])assert.throws(()=>PDFFunction.getSampleArray(size,outputs,bits,stream),format);assert.equal(allocations,0);assert.equal(reads,0);assert.throws(()=>PDFFunction.getSampleArray([1048576],1,8,stream),/owned byte read trap/);assert.equal(reads,1);}finally{globalThis.Array=saved;}
 });
 await test('508 packed sample controls, EOF and611 unsigned32 bit maximum',()=>{
 for(const bits of[1,2,4,8,12,16,24,32]){const maximum=2**bits-1,words=[0,Math.floor(maximum/2),maximum],binary=words.map(n=>n.toString(2).padStart(bits,'0')).join(''),padded=binary+'0'.repeat((8-binary.length%8)%8),bytes=Uint8Array.from(padded.match(/.{8}/g),value=>parseInt(value,2)),samples=PDFFunction.getSampleArray([3],1,bits,new Stream(bytes));for(let i=0;i<3;i++)assert(Math.abs(samples[i]-words[i]/maximum)<1e-12,JSON.stringify({bits,index:i,actual:samples[i]}));}
 assert.throws(()=>PDFFunction.getSampleArray([2],3,8,new Stream(new Uint8Array(1))),format);
 });
 await test('508 actual sampled function gradients and dimension/encode limits',()=>{
 const values={FunctionType:0,Domain:[0,1],Range:[0,1,0,1,0,1],Size:[2],BitsPerSample:8},create=extra=>{const d=dict({...values,...extra}),stream=new Stream(Uint8Array.from([255,0,0,0,0,255]),0,6,d);return PDFFunction.constructSampled({xref:{fetchIfRef:x=>x},isEvalSupported:false,fn:stream,dict:d});};
 const fn=create({}),out=new Float64Array(3);fn([0.5],0,out,0);assert.deepEqual([...out],[0.5,0,0.5]);for(const patch of[{Domain:[]},{Domain:[0,1,2]},{Domain:Array(18).fill(1)},{Range:Array(66).fill(1)},{Size:[2,2]},{Encode:[0]},{Decode:[0,1]}])assert.throws(()=>create(patch),format);
 });
 await test('509 nested conditional depth rejects before stack and restores parser state',()=>{
 const parser=new PostScriptParser(new PostScriptLexer(new StringStream('{ '+'true { '.repeat(4000)+'0 '+'} if '.repeat(4000)+'}')));assert.throws(()=>parser.parse(),format);assert.equal(parser._aideBlockDepth,0);parser.lexer=new PostScriptLexer(new StringStream('{ 0.2 0.4 0.6 }'));parser.operators=[];assert.deepEqual(parser.parse(),[0.2,0.4,0.6]);
 const valid=new PostScriptParser(new PostScriptLexer(new StringStream('{ '+'true { '.repeat(127)+'0 '+'} if '.repeat(127)+'}')));assert(valid.parse().length>127);
 });
 await test('509 normal if/ifelse operator controls',()=>{for(const[source,expected]of[['{ 2 3 add }',[2,3,'add']],['{ true { 1 } if }',['true',4,'jz',1]],['{ true { 1 } { 2 } ifelse }',['true',6,'jz',1,7,'j',2]]])assert.deepEqual(new PostScriptParser(new PostScriptLexer(new StringStream(source))).parse(),expected);});
 console.log(JSON.stringify({checks:rows.length,rows}));if(!process.env.AIDE_PDF_EXPECT_FAILURES)assert(rows.every(row=>row.status==='PASS'));
}
if(require.main===module)main().catch(error=>{console.error(error.name+': '+error.message);process.exitCode=1;});
