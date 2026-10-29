'use strict';
const fs=require('node:fs'),assert=require('node:assert/strict'),acorn=require('../internal/server/code_analysis/acorn.cjs');
function ttcHeader(count,version=1,complete=true){const b=Buffer.alloc(12+(complete?count*4+(version===2?12:0):0));b.write('ttcf');b.writeUInt16BE(version,4);b.writeUInt32BE(count,8);return b;}
function cmap12(groups,declaredGroups=groups.length){const length=16+groups.length*12,b=Buffer.alloc(12+length);b.writeUInt16BE(1,2);b.writeUInt16BE(3,4);b.writeUInt16BE(1,6);b.writeUInt32BE(12,8);b.writeUInt16BE(12,12);b.writeUInt32BE(length,16);b.writeUInt32BE(declaredGroups,24);groups.forEach((g,i)=>g.forEach((n,k)=>b.writeUInt32BE(n,28+i*12+k*4)));return b;}
function meshRecords(count){const b=Buffer.alloc(count*6);for(let i=0;i<count;i++){b[i*6+1]=i%3===1||i%6===2||i%6===4?255:0;b[i*6+2]=i%6===2||i%6===4||i%6===5?255:0;b[i*6+3]=255;}return b;}
function meshPatch(type){const coordinates=[[0,0],[0,85],[0,170],[0,255],[85,255],[170,255],[255,255],[255,170],[255,85],[255,0],[170,0],[85,0]];if(type===7)coordinates.push([85,85],[85,170],[170,170],[170,85]);return Buffer.from([0,...coordinates.flat(),...Array.from({length:4},()=>[255,0,0]).flat()]);}
async function main(){
 const source=fs.readFileSync(process.env.AIDE_PDF_PARSER_BASELINE||'internal/server/web/vendor/pdfjs/pdf.worker.min.mjs','utf8'),ast=acorn.parse(source,{ecmaVersion:'latest',sourceType:'module'}),functions={};
 function walk(n){if(!n||typeof n!=='object')return;if(['readTrueTypeCollectionHeader','readCmapTable'].includes(n.id?.name))functions[n.id.name]=source.slice(n.start,n.end);for(const v of Object.values(n))if(Array.isArray(v))v.forEach(walk);else if(v&&typeof v==='object')walk(v);}walk(ast);
 const suffix='\nconst readTTC='+functions.readTrueTypeCollectionHeader+';const readCmap='+functions.readCmapTable+';export{readTTC,readCmap,Stream,Dict,Name,LocalColorSpaceCache,MeshShading,PostScriptParser,PostScriptLexer,StringStream,PostScriptCompiler,PDFFunction,XMLParserBase,MarkupAnnotation,WidgetAnnotation};';
 const {readTTC,readCmap,Stream,Dict,Name,LocalColorSpaceCache,MeshShading,PostScriptParser,PostScriptLexer,StringStream,PostScriptCompiler,PDFFunction,XMLParserBase,MarkupAnnotation,WidgetAnnotation}=await import('data:text/javascript;base64,'+Buffer.from(source+suffix).toString('base64'));
 const rows=[],format=e=>e.name==='FormatError',dict=entries=>{const d=new Dict();for(const[k,v]of Object.entries(entries))d.set(k,v);return d;};
 async function test(name,fn){try{rows.push({name,status:'PASS',detail:await fn()});}catch(e){rows.push({name,status:'FAIL',error:e.message});}}
 await test('552 TTC counts/truncation stop before offset expansion',()=>{
  for(const count of[1000000,4294967295]){const stream=new Stream(ttcHeader(count,1,false)),get=stream.getInt32.bind(stream);let reads=0;stream.getInt32=()=>{if(++reads>1024)throw Error('owned TTC offset trap');return get();};assert.throws(()=>readTTC(stream),format);assert(reads<1024);}
  assert.throws(()=>readTTC(new Stream(ttcHeader(1,1,false))),format);
 });
 await test('552 valid1/256 offset layouts and version2 footer',()=>{for(const count of[1,256]){const header=readTTC(new Stream(ttcHeader(count)));assert.equal(header.offsetTable.length,count);assert.equal(header.numFonts,count);}const version2=readTTC(new Stream(ttcHeader(1,2)));assert.equal(version2.dsigLength,0);assert.equal(version2.majorVersion,2);});
 const readMap=bytes=>{const stream=new Stream(bytes),read=stream.getInt32.bind(stream);let reads=0;stream.getInt32=()=>{if(++reads>1024)throw Error('owned cmap group-read trap');return read();};return readCmap({offset:0,length:bytes.length},stream,false,false);};
 await test('553 excessive Unicode/glyph spans reject before append',()=>{
  for(const group of[[0x10000,0x10ffff,1],[0,5000000,0],[1,0,0],[0,65535,1]]){const bytes=cmap12([group]),push=Array.prototype.push;let writes=0,caught;Array.prototype.push=function(...values){if(++writes>256)throw Error('owned cmap append trap');return push.apply(this,values);};try{readMap(bytes);}catch(e){caught=e;}finally{Array.prototype.push=push;}assert(caught&&format(caught));assert.equal(writes,0);}
  assert.throws(()=>readMap(cmap12([],1000000)),format);
 });
 await test('553 normal format12 maps, stable deduplication and aggregate budget',()=>{
  assert.deepEqual(readMap(cmap12([[65,65,1]])).mappings,[{charCode:65,glyphId:1}]);assert.deepEqual(readMap(cmap12([[65,65,1],[65,65,2]])).mappings,[{charCode:65,glyphId:1}]);
  if(!process.env.AIDE_PDF_PARSER_BASELINE){const all=readMap(cmap12(Array.from({length:4},()=>[0,65535,0])));assert.equal(all.mappings.length,65536);assert.throws(()=>readMap(cmap12(Array.from({length:5},()=>[0,65535,0]))),format);}
 });
 const meshOwner=()=>Object.assign(Object.create(MeshShading.prototype),{coords:[],colors:[],figures:[]});
 await test('554 types4/5 bound vertices before next reader access',()=>{
  for(const type of[4,5]){const mesh=meshOwner();let reads=0;const coordinate=[0,0],color=new Uint8Array([255,0,0]),reader={get hasData(){return reads<70000;},readFlag:()=>0,readCoordinate(){if(++reads>65536)throw Error('owned mesh read trap');return coordinate;},readComponents:()=>color,align(){}};assert.throws(()=>mesh['_decodeType'+type+'Shading'](reader,2),format);assert.equal(reads,65536);assert.equal(mesh.coords.length,65536);}
 });
 await test('554 patch output checked before allocation and document quotas shared',()=>{
  const mesh=meshOwner();mesh.coords.length=65536;for(let i=0;i<16;i++)mesh.coords[i]=[i%4,Math.floor(i/4)];mesh.colors=Array.from({length:4},()=>new Uint8Array([255,0,0]));mesh.bounds=[0,0,3,3];mesh.figures=[{type:'patch',coords:Int32Array.from({length:16},(_,i)=>i),colors:Int32Array.from([0,1,2,3])}];
  const Native=globalThis.Int32Array;let allocations=0;globalThis.Int32Array=new Proxy(Native,{construct(){allocations++;throw Error('owned patch allocation trap');}});try{assert.throws(()=>mesh._buildFigureFromPatch(0),format);assert.equal(allocations,0);}finally{globalThis.Int32Array=Native;}
  const budget={coordinates:262143,colors:262143,figures:0},first=meshOwner(),second=meshOwner();first._aideDocBudget=second._aideDocBudget=budget;first._aideReserveGeometry(1,1);assert.throws(()=>second._aideReserveGeometry(1,1),format);assert.equal(budget.coordinates,262144);
 });
 await test('554 normal actual mesh constructor and decode parameters',()=>{
  const xref={fetchIfRef:x=>x},properties={ShadingType:4,ColorSpace:Name.get('DeviceRGB'),BitsPerCoordinate:8,BitsPerComponent:8,BitsPerFlag:8,Decode:[0,100,0,100,0,1,0,1,0,1]},bytes=meshRecords(6),make=extra=>new MeshShading(new Stream(bytes,0,bytes.length,dict({...properties,...extra})),xref,Dict.empty,null,new LocalColorSpaceCache());const mesh=make({});assert.equal(mesh.coords.length,12);assert.equal(mesh.colors.length,18);assert.equal(mesh.figures[0].coords.length,6);if(!process.env.AIDE_PDF_PARSER_BASELINE)assert.throws(()=>make({BitsPerCoordinate:0}),format);
 });
 await test('554 types6/7 record count is bounded before another patch',()=>{
  for(const type of[6,7]){const mesh=meshOwner();let patches=0;const coordinate=[0,0],color=new Uint8Array([255,0,0]),reader={get hasData(){return patches<4100;},readFlag(){if(++patches>4096)throw Error('owned patch record trap');return 0;},readCoordinate:()=>coordinate,readComponents:()=>color};assert.throws(()=>mesh['_decodeType'+type+'Shading'](reader),format);assert.equal(patches,4096);assert.equal(mesh.figures.length,4096);}
 });
 await test('554 actual Coons/tensor patch tessellation preserves normal geometry',()=>{
  for(const type of[6,7]){const bytes=meshPatch(type),d=dict({ShadingType:type,ColorSpace:Name.get('DeviceRGB'),BitsPerCoordinate:8,BitsPerComponent:8,BitsPerFlag:8,Decode:[0,100,0,100,0,1,0,1,0,1]}),mesh=new MeshShading(new Stream(bytes,0,bytes.length,d),{fetchIfRef:x=>x},Dict.empty,null,new LocalColorSpaceCache());assert.equal(mesh.figures[0].type,'lattice');assert(mesh.figures[0].verticesPerRow>1);assert(mesh.coords.every(Number.isFinite));assert(mesh.colors.some(v=>v===255));}
 });
 await test('556 parser operator quota and expression depth avoid stack overflow',()=>{
  const parser=new PostScriptParser(new PostScriptLexer(new StringStream('{ '+'1 pop '.repeat(2049)+'}')));parser.operators=new Proxy([],{set(target,key,value){if(/^\d+$/.test(String(key))&&Number(key)>=4096)throw Error('owned PS operator trap');return Reflect.set(target,key,value);}});assert.throws(()=>parser.parse(),format);assert.equal(parser._aideBlockDepth,0);
  assert.throws(()=>new PostScriptCompiler().compile([...[1,'add'],...Array.from({length:200},()=>[1,'add']).flat()],[0,1],[0,300]),format);
 });
 await test('556 valid deep expression falls back to bounded interpreter',()=>{
  for(const enabled of[false,true]){const expression='{ '+'1 add '.repeat(500)+'500 sub }',d=dict({FunctionType:4,Domain:[0,1],Range:[0,1]}),fn=PDFFunction.constructPostScript({xref:{fetchIfRef:x=>x},isEvalSupported:enabled,fn:new StringStream(expression),dict:d}),output=new Float64Array(1);fn([.25],0,output,0);assert(Math.abs(output[0]-.25)<1e-9);}
 });
 await test('557 entity scans skip unclosed suffix and preserve complete-prefix semantics',()=>{
  const object={onResolveEntity:s=>'['+s+']'},native=String.prototype.replaceAll;let scanned=0;String.prototype.replaceAll=function(...args){if(!String(this).endsWith(';'))throw Error('owned unclosed entity regex trap');scanned+=this.length;return native.apply(this,args);};try{const suffix='&'.repeat(220000);assert.equal(XMLParserBase.prototype._resolveEntities.call(object,suffix),suffix);assert.equal(scanned,0);const prefix='&amp;'.repeat(50000);assert.equal(XMLParserBase.prototype._resolveEntities.call(object,prefix+suffix),'&'.repeat(50000)+suffix);assert.equal(scanned,prefix.length);}finally{String.prototype.replaceAll=native;}
  assert.equal(XMLParserBase.prototype._resolveEntities.call(object,'&amp; &#65; &#x42; &bogus; &&amp;'),'& A B [bogus] [&amp]');
 });
 await test('558 negative and mixed appearance bounds remain exact',()=>{
  for(const box of[[-120,-120,-20,-20],[-120,-20,20,120],[20,20,120,120]]){const object=Object.create(MarkupAnnotation.prototype);object.data={quadPoints:Float32Array.from([box[0],box[3],box[2],box[3],box[0],box[1],box[2],box[1]])};object._streams=[];object._setDefaultAppearance({xref:null,pointsCallback:(_ops,q)=>[q[0],q[2],q[5],q[1]]});assert.deepEqual(object.data.rect,box);assert.deepEqual(object.appearance.dict.get('BBox'),box);}
 });
 const widget=()=>Object.assign(Object.create(WidgetAnnotation.prototype),{data:{id:'owned',defaultAppearanceData:{fontSize:0,fontName:'Helv',fontColor:new Uint8Array([0,0,0])}}});
 await test('559 no usable geometry returns before font work',async()=>{
  for(const height of[-1,0,Infinity]){const object=widget(),font={encodeString(){throw Error('owned unusable widget font trap');}};const result=object._computeFontSize(height,100,'A',font,1);assert.equal(result[1],0);assert.equal(result[2],0);}
  const object=widget();Object.assign(object.data,{rect:[0,0,100,1],fieldValue:'A',multiLine:true});object._needAppearances=true;object.rotation=0;object.hasFieldFlag=()=>false;object.getBorderAndBackgroundAppearances=()=> 'BORDER ';const native=WidgetAnnotation._getFontData;let calls=0;WidgetAnnotation._getFontData=async()=>{calls++;throw Error('owned zero-height font loading trap');};try{assert.match(await object._getAppearance(null,null,0,null),/BORDER/);assert.equal(calls,0);}finally{WidgetAnnotation._getFontData=native;}
 });
 await test('559 multiline sizing uses bounded monotone search',()=>{
  const object=widget();let calls=0;object._splitLine=(_ctx,_font,size,width,line)=>{if(++calls>64)throw Error('owned linear font-size search trap');return Array(Math.max(1,Math.ceil(line.line.length*size*.5/width))).fill('');};const font={encodeString:x=>[x],charsToGlyphs:()=>[],getCharPositions:()=>[]},result=object._computeFontSize(3,1,'A'.repeat(10000),font,1);assert(result[1]>0&&Number.isFinite(result[2]));assert(Math.ceil(10000*result[1]*.5)*result[1]<=3);assert(calls<=64);return{checks:calls,fontSize:result[1]};
 });
 console.log(JSON.stringify({checks:rows.length,rows}));if(!process.env.AIDE_PDF_EXPECT_FAILURES)assert(rows.every(r=>r.status==='PASS'));
}
if(require.main===module)main().catch(e=>{console.error(e.name+': '+e.message);process.exitCode=1;});
module.exports={ttcHeader,cmap12,meshRecords,meshPatch};
