'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict'),crypto=require('node:crypto');
const file='internal/server/web/vendor/drawio/js/plantuml/drawio-plantuml.min.js';
const controls=[
 '@startuml\nstate Outer {\nstate Inner {\nstate Leaf\n}\n}\nOuter --> Done\n@enduml',
 '@startuml\npackage P {\npackage Q {\nclass A\n}\nclass B\n}\nA --> B\n@enduml',
 '@startuml\nmap M {\na *-> A\nb *-> B\n}\nA --> B\n@enduml',
 '@startuml\nobject A\nnote right of A: first\nnote right of A: second\n@enduml',
 '@startuml\nif (ready) then (yes)\nif (ok) then (yes)\n:work;\nelse (no)\n:retry;\nendif\nelse (no)\n:stop;\nendif\n@enduml',
 '@startuml\n!$parts = %splitstr_regex("one, two;three", "[,;] *")\nAlice -> Bob: $parts\n@enduml',
 '@startuml\nstate Parallel {\nstate Left\n--\nstate Right\n}\nLeft --> Right\n@enduml',
 '@startuml\nmap M {\na *-> A\nb *-> A\n}\nA --> A\n@enduml',
 '@startuml\nmap M {\na *-> A\na *-> B\n}\n@enduml'
];
function runtime(instrument=true){
 let code=fs.readFileSync(process.env.AIDE_PLANTUML_RESOURCE_BASELINE||file,'utf8');const c={console,ownedMapScans:0,ownedStateMeasures:0,ownedPackageDepth:0,ownedActivityDepth:0};vm.createContext(c,{codeGeneration:{strings:false,wasm:false}});
 if(instrument){
  if(code.includes('function aideWithBudget('))code=code.replace('function Tt(f,d){','globalThis.ownedTransaction=aideWithBudget;globalThis.ownedWork=()=>aidePlantUmlWork;function Tt(f,d){');
  code=code.replace('function Tt(f,d){','globalThis.ownedRawSplit=Tt;function Tt(f,d){');
  code=code.replace('function Ve(r){','function Ve(r){if(++globalThis.ownedStateMeasures>10000)throw Error("owned state-work trap");');
  code=code.replace('Yo=function(Ye){','Yo=function(Ye){if(++globalThis.ownedPackageDepth>256)throw Error("owned package-depth trap");');
  code=code.replace('function lr(A,s,k,o,a){','function lr(A,s,k,o,a){if(++globalThis.ownedActivityDepth>256)throw Error("owned activity-depth trap");');
  const before='for(var Da=0,jt=0;jt<i.length;jt++)jt!==Ga&&(i[jt].from===hn.to||i[jt].to===hn.to)&&Da++;';
  code=code.replace(before,'for(var Da=0,jt=0;jt<i.length;jt++)(globalThis.ownedMapScans++,jt!==Ga&&(i[jt].from===hn.to||i[jt].to===hn.to)&&Da++);');
 }
 vm.runInContext(code,c);return c;
}
const parse=(c,source)=>c.mxPlantUmlToDrawio.parseText(source);
const uml=body=>'@startuml\n'+body+'\n@enduml';
function nested(kind,count){let s=kind==='activity'?':leaf;':kind==='package'?'class A':'state Leaf';for(let i=0;i<count;i++)s=kind==='activity'?`if (x${i}) then (yes)\n${s}\nendif`:`${kind} P${i} {\n${s}\n}`;return uml(s);}
function main(){
 if(process.argv.includes('--snapshot')){assert(process.env.AIDE_PLANTUML_RESOURCE_BASELINE,'Set an explicit reviewed baseline before recording normal XML');const data=controls.map(source=>({source,sha256:crypto.createHash('sha256').update(parse(runtime(false),source)).digest('hex')}));fs.writeFileSync('scripts/fixtures/plantuml-resource-controls.json',JSON.stringify(data,null,2)+'\n');console.log('normal XML baseline controls',data.length);return;}
 const rows=[];function test(name,fn){try{rows.push({name,status:'PASS',detail:fn()})}catch(e){rows.push({name,status:'FAIL',error:e.message})}}
 const limit=e=>e.code==='AIDE_PLANTUML_LIMIT';
 test('bounded regex payload and MIT license match recorded provenance',()=>{
  const base='internal/server/web/vendor/drawio/js/plantuml/',meta=JSON.parse(fs.readFileSync(base+'re2js-provenance.json','utf8'));
  assert.equal(meta.version,'2.9.1');assert.equal(meta.license,'MIT');for(const entry of [meta.payload,meta.licenseFile])assert.equal(crypto.createHash('sha256').update(fs.readFileSync(base+entry.file)).digest('hex'),entry.sha256);assert.equal(fs.statSync(base+meta.payload.file).size,meta.payload.bytes);
 });
 test('splitstr_regex does not execute a user pattern in native backtracking engine',()=>{
  const c=runtime();vm.runInContext(`globalThis.ownedNativeCalls=0;const original=RegExp.prototype.exec;RegExp.prototype.exec=function(input){if(this.source==='(a+)+$'&&input==='${'a'.repeat(32)}!'){ownedNativeCalls++;throw Error('owned native regex execution trap');}return original.call(this,input);};`,c);
  const result=c.ownedRawSplit('a'.repeat(32)+'!','(a+)+$');assert.deepEqual(Array.from(result),['a'.repeat(32)+'!']);assert.equal(c.ownedNativeCalls,0);
 });
 test('normal delimiters, captures, zero-width and Unicode whitespace preserve results',()=>{
  const c=runtime(),cases=[['a,b;c','[,;]'],['a,b;c','(,|;)'],['one  two\nthree','\\s+'],['甲　乙\u00a0丙','\\s+'],['甲　乙','[^\\S\\n]+'],['a,b,,',','],['abc',''],['abc','()'],['abc','(x)?'],['abc','^'],['abc','$'],['abc','z*'],['ab','a*'],['','x'],['a[bc]','\\[|\\]'],['a\bZ','[\\b]'],['p{L}','\\p{L}'],['literal','['],['a\rb\u2028c','.'],['甲😀,乙😎',','],['','[']];for(const [input,pattern]of cases){
   let expected;try{expected=input.split(new RegExp(pattern));while(expected.length&&expected.at(-1)==='')expected.pop();}catch{expected=[input];}
   assert.deepEqual(Array.from(c.ownedRawSplit(input,pattern)),expected,JSON.stringify({input,pattern}));
  }return{controls:cases.length};
 });
 test('regex input/program/output limits reject with a recoverable error',()=>{
  const c=runtime();for(const [input,pattern]of [['x'.repeat(16385),','],['x','a'.repeat(257)],['x','.{1000}'],['x','()'.repeat(33)],['x,'.repeat(4097),',']])assert.throws(()=>c.ownedRawSplit(input,pattern),limit);
  assert.throws(()=>c.ownedRawSplit('aa','(a)\\1'),e=>e.code==='AIDE_PLANTUML_REGEX_UNSUPPORTED');assert.throws(()=>c.ownedRawSplit('a\n','$'),e=>e.code==='AIDE_PLANTUML_REGEX_UNSUPPORTED');assert.deepEqual(Array.from(c.ownedRawSplit('a,b',',')),['a','b']);
 });
 test('conversion budget aggregates work and clears cache after errors',()=>{
  const c=runtime();assert.equal(typeof c.ownedTransaction,'function');
  assert.throws(()=>c.ownedTransaction(()=>{for(let i=0;i<1000;i++)c.ownedRawSplit('x'.repeat(16000),',');}),limit);assert.equal(c.ownedWork(),null);
  assert.throws(()=>c.ownedTransaction(()=>{for(let i=0;i<65;i++)c.ownedRawSplit('x','separator'+i);}),limit);assert.equal(c.ownedWork(),null);
  assert.deepEqual(Array.from(c.ownedRawSplit('a,b',',')),['a','b']);
 });
 test('nested state layout reuses descendant measurements',()=>{const c=runtime(),xml=parse(c,nested('state',23));assert(xml.includes('Leaf'));assert(c.ownedStateMeasures<1000);return{measurements:c.ownedStateMeasures};});
 test('map-row links do not rescan all links per row',()=>{const c=runtime(),n=1000,xml=parse(c,uml('map M {\n'+Array.from({length:n},(_,i)=>`k${i} *-> City${i}`).join('\n')+'\n}'));assert(xml.includes('City999'));assert(c.ownedMapScans<=n*16);return{rows:n,oldInnerIterations:c.ownedMapScans};});
 test('note placement is bounded before prior-note collision work',()=>{const c=runtime();assert.throws(()=>parse(c,uml('object A\n'+Array.from({length:3000},(_,i)=>'note right of A: Note '+i).join('\n'))),limit);assert(parse(c,uml('object A')).includes('A'));});
 test('package and activity recursion stop before stack exhaustion',()=>{for(const [kind,n]of [['package',5000],['activity',500]]){const c=runtime();assert.throws(()=>parse(c,nested(kind,n)),limit);assert(parse(c,uml('Alice -> Bob')).includes('Alice'));}});
 test('normal XML stays byte-identical to the pinned baseline',()=>{const fixtures=require('./fixtures/plantuml-resource-controls.json');for(const row of fixtures)assert.equal(crypto.createHash('sha256').update(parse(runtime(false),row.source)).digest('hex'),row.sha256);return{controls:fixtures.length};});
 console.log(JSON.stringify({checks:rows.length,rows}));if(!process.env.AIDE_PLANTUML_EXPECT_FAILURES)assert(rows.every(r=>r.status==='PASS'));
}
if(require.main===module)try{main()}catch(e){console.error(e.message);process.exitCode=1;}
module.exports={runtime,parse,uml,nested,controls};
