'use strict';
const assert=require('assert/strict'),vm=require('vm'),review=require('./drawio-mermaid-native-review.cjs'),cases=require('./fixtures/mermaid-native-issue-cases.json');
function layout(c,source){const parsed=c.MermaidParser.parse(source);return c.MermaidLayout.layout(parsed.type,parsed.ir,parsed.config);}
function nodes(ir){const list=[],stack=[ir];while(stack.length){const n=stack.pop();if(!n||typeof n!=='object')continue;list.push(n);for(const child of n.children||[])stack.push(child);}return list;}
if(!process.env.AIDE_NATIVE_REVIEW_BASELINE)assert.equal(review.moduleSource('standalone').trim(),review.moduleSource('extensions').trim(),'runtime module mirrors standalone');
const results=[];
for(const copy of ['standalone','extensions'])for(const row of cases){try{const c=review.context(copy),before=vm.runInContext('JSON.stringify(Object.getOwnPropertyNames(Object.prototype).sort())',c);const parsed=c.MermaidParser.parse(row.source),ir=parsed.ir;
 if(row.issue===434){assert.equal(ir.actors[0].links.length,1,'sequence link map not discarded');const git=c.MermaidParser.parse('gitGraph\ncommit id:"hasOwnProperty"\ncommit id:"hasOwnProperty"');assert(git.ir,'duplicate special commit ID supported');}
 if(row.issue===436){layout(c,row.source);assert(nodes(ir).some(n=>n.id==='__proto__'),'special node retained');assert.equal(vm.runInContext('JSON.stringify(Object.getOwnPropertyNames(Object.prototype).sort())',c),before,'parser cannot modify Object.prototype');}
 if(row.issue===445)assert.equal(nodes(ir).filter(n=>n.id==='__proto__').length,1,'state ID retained');
 if(row.issue===446){const ids=nodes(ir).map(n=>n.id);assert.equal(new Set(ids).size,ids.length,'generated note/group IDs cannot collide');assert(nodes(ir).some(n=>n.id==='_note_0'),'original state retained');}
 if(row.issue===447)assert.equal(ir.columns.length,2,'style statements are not columns');
 if(row.issue===448){const normal=layout(c,row.source);assert.equal(normal.ringRadii.length,5);assert.throws(()=>layout(c,row.source.replace('ticks 5','ticks 10000')),/Radar.*1000/i,'bounded parser/layout ticks');const forged=c.MermaidParser.parse(row.source).ir;forged.ticks=1000000000;assert.throws(()=>c.MermaidLayout.layout('radar',forged,{}),/Radar.*1000/i,'layout also bounds programmatic IR');}
 if(row.issue===449){const result=layout(c,row.source);assert(result.nodes.some(n=>n.name==='__proto__'||n.id==='__proto__'),'Sankey special source retained');assert.equal(result.sankeyLinks.length,1);}
 if(row.issue===450){const result=layout(c,row.source),control=layout(c,row.source.replace('rf 06','tf 06'));assert.equal(result.relations.length,control.relations.length,'reset retains explicit source relation');for(const rel of control.relations)assert(result.relations.some(r=>['x1','x2','y1','y2'].every(k=>Math.abs(r[k]-rel[k])<1e-6)),'explicit source/target anchors preserved');}
 if(row.issue===451){layout(c,row.source);const depth=3000,deep='flowchart TD\n'+Array.from({length:depth},(_,i)=>'subgraph S'+i+'\n').join('')+'A\n'+'end\n'.repeat(depth);assert.throws(()=>layout(c,deep),/Mermaid.*(?:64|65)/i,'explicit nesting error before stack overflow');}
 results.push({issue:row.issue,copy,result:'PASS'});
 }catch(e){results.push({issue:row.issue,copy,result:'FAIL',error:e.message});}}
for(const copy of ['standalone','extensions']){const c=review.context(copy);for(const source of ['flowchart TD\nA-->B','stateDiagram-v2\nA-->B','classDiagram\nA <|-- B','erDiagram\nA ||--o{ B : owns','sankey-beta\nA,B,5']){const value=layout(c,source);assert(value);}const root={id:'root',children:[]};let cursor=root;for(let i=0;i<70;i++){const next={id:'n'+i,children:[]};cursor.children.push(next);cursor=next;}assert.throws(()=>c.MermaidLayout.layout('flowchart',root,{}),/Mermaid.*65/);}
console.log(JSON.stringify(results));if(!process.env.AIDE_EXPECT_REVIEW_FAILURES)assert.deepEqual(results.filter(x=>x.result!=='PASS'),[]);
