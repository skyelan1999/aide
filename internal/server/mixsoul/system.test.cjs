'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),os=require('node:os'),path=require('node:path'),crypto=require('node:crypto');
const {Store,SPACE}=require('./store.cjs'),{assistantAgent}=require('./agent.cjs'),{expression,signature,routeContext,SEMANTIC_SPACE,DIMENSIONS,boundedHistory}=require('./cognition.cjs');
const T='2026-10-25T00:00:00.000Z',later='2026-10-26T00:00:00.000Z';
function fixture(t){const directory=fs.mkdtempSync(path.join(os.tmpdir(),'mixsoul-system-'));t.after(()=>fs.rmSync(directory,{recursive:true,force:true}));return {directory,key:crypto.randomBytes(32).toString('base64'),scope:'f'.repeat(32)}}
function learn(s,i,heard,text,kind='preference',extra={}){const c=s.begin('turn-'+i,heard,T);s.consolidate({items:[{kind,text,quote:heard,importance:.6,reason:'用户明确自己的偏好',...extra}]},c.sourceId,heard,T);return c;}
test('ten equivalent expression paraphrases share one sourced memory; opposite/entity preferences stay separate',t=>{
 const s=new Store(fixture(t));const texts=['希望回答简洁','回答请简短','回复精简','我希望回复简明','回答请言简意赅','回答长话短说','回答直接说重点','回复只说重点','回答不要啰嗦','回复别啰嗦'];
 for(const [i,text] of texts.entries())learn(s,i,text,text);
 const prefs=s.data.records.filter(r=>r.kind==='preference');assert.equal(prefs.length,1);assert.equal(prefs[0].sourceIds.length,10);assert.equal(prefs[0].variants.length,9);
 learn(s,20,'我希望回答详细','回答请详细');learn(s,21,'我不喜欢简洁回答','我不喜欢简洁回答');learn(s,22,'我喜欢项目青铜的蓝色','项目青铜偏好蓝色');learn(s,23,'我喜欢项目紫檀的蓝色','项目紫檀偏好蓝色');
 assert.equal(s.data.records.filter(r=>r.kind==='preference').length,5);s.close();
});
test('idle jobs merge only known equivalent preferences, survive reopen, and allow undo without reinforcement',t=>{
 const storage=fixture(t);let s=new Store(storage);const a=s.add('preference','回答简洁',T),b=s.add('preference','回答请简短',T);s.begin('job','合成排队工作',T);s.save();s.close();s=new Store(storage);assert.equal(s.data.queue.length,1);const p=s.pulse(later);assert.equal(p.modelCalls,0);assert.equal(p.maintenance.processed,1);assert.equal(s.data.queue.length,0);assert.equal(s.data.records.find(r=>r.id===b.id).mergedInto,a.id);assert.equal(s.data.records.find(r=>r.id===a.id).stability,1);assert.equal(s.undoMerge(b.id),true);assert.equal(s.data.records.find(r=>r.id===b.id).status,'active');s.close();
});
test('current corrections exclude old version; past, third-party and temporary statements cannot create current preferences',t=>{
 const s=new Store(fixture(t)),old=s.add('preference','合成项目喜欢蓝色',T);const heard='更正，合成项目现在改为绿色',c=s.begin('fix',heard,T);s.consolidate({updates:[{id:old.id,action:'correct',quote:heard,text:'合成项目喜欢绿色',reason:'用户明确更正当前安排'}]},c.sourceId,heard,T,[old.id]);assert.ok(!s.context('合成项目',T).references.some(r=>r.id===old.id));
 for(const [i,h] of ['朋友说他喜欢红色','过去我喜欢红色','这次回答详细一点'].entries())learn(s,'reject'+i,h,'喜欢红色');assert.ok(!s.data.records.some(r=>r.text==='喜欢红色'));s.close();
});
test('expiry must have literal date evidence and expires protected promises, not invented model dates',t=>{
 const s=new Store(fixture(t));learn(s,1,'合成项目约定有效至2026-10-26，过期作废','合成项目承诺','promise',{validTo:later});learn(s,2,'我承诺完成合成另一个项目','另一个承诺','promise',{validTo:later});assert.ok(!s.retrieve('合成项目',later).some(r=>r.text==='合成项目承诺'));assert.ok(s.retrieve('另一个承诺',later).some(r=>r.text==='另一个承诺'));s.close();
});
test('persistent mention boundary hides source and derived memories across reopen; allow and delete are distinct',t=>{
 const storage=fixture(t);let s=new Store(storage);learn(s,1,'我喜欢合成青铜项目蓝色','青铜蓝色');const n=s.data.records.length;s.begin('block','别再提合成青铜项目',T);assert.ok(!s.context('青铜蓝色',later).references.some(r=>r.text==='青铜蓝色'));assert.ok(s.data.records.length>n);s.close();s=new Store(storage);assert.ok(!s.context('青铜蓝色',later).references.some(r=>r.text==='青铜蓝色'));s.begin('allow','可以再提合成青铜项目',later);assert.ok(s.context('青铜蓝色',later).references.some(r=>r.text==='青铜蓝色'));s.begin('block2','别再提合成青铜项目',later);s.forget('合成青铜项目');assert.ok(!s.data.records.some(r=>r.text==='青铜蓝色'));assert.equal(s.data.mentionRules.length,0);s.close();
});
test('salience feedback changes retention only with literal matching evidence; no-store never changes it',t=>{
 const s=new Store(fixture(t)),r=s.add('fact','合成青铜项目决定',T,{confidence:.6});s.begin('salience','合成青铜项目决定，这件事很重要',T);assert.equal(r.salience,1);assert.equal(r.stability,1.25);assert.equal(r.confidence,.6);s.begin('private','不要保存：合成青铜项目决定，这段记忆淡一点',later);assert.equal(r.stability,1.25);s.begin('reduce','合成青铜项目决定，这段记忆淡一点',later);assert.equal(r.stability,1);s.close();
});
test('context budget reserves promise/preferences, removes derived source repetition and bounds history',()=>{
 const rows=[...Array.from({length:12},(_,i)=>({id:'p'+i,kind:'promise',text:'合成承诺'+i,sourceIds:[]})),{id:'pref',kind:'preference',text:'回答简洁',sourceIds:['src']},{id:'fact',kind:'fact',text:'合成重要事实',sourceIds:[]},{id:'src',kind:'episode',text:'希望回答简洁',sourceIds:[]}];const got=routeContext(rows,{maxChars:1200});assert.ok(got.references.some(r=>r.id==='pref'));assert.ok(got.references.some(r=>r.id==='fact'));assert.ok(!got.references.some(r=>r.id==='src'));assert.ok(got.budget.used<=got.budget.limit);assert.equal(got.references.filter(r=>r.kind==='promise').length,2);assert.ok(boundedHistory(Array.from({length:12},()=>({role:'user',content:'x'.repeat(1500)}))).reduce((n,m)=>n+m.content.length,0)<=4000);
});
test('contextual expression overrides habits without permanently changing them',t=>{
 const s=new Store(fixture(t));s.begin('style','回答简洁点，别总追问我，别说套话',T);const before=JSON.stringify(s.data.habits);const c=s.context('详细解释这段代码',later);assert.equal(c.expression.verbosity,'detailed');assert.equal(c.expression.tone,'precise');assert.equal(c.expression.questions,'only-if-essential');assert.equal(JSON.stringify(s.data.habits),before);assert.equal(expression('我很难过',{}).tone,'gentle');s.close();
});
test('local semantic baseline and replaceable named spaces never mix query dimensions or versions',t=>{
 const s=new Store(fixture(t));const r=s.add('preference','回答简洁',T);assert.ok(s.retrieve('回复精简',T,{space:SEMANTIC_SPACE}).some(x=>x.id===r.id));assert.equal(s.data.spaces[SEMANTIC_SPACE].dimensions,DIMENSIONS);s.registerSpace('local-model-v1',{dimensions:3,model:'offline-fixture',version:'1',metric:'cosine'});s.setVector(r.id,'local-model-v1',[1,0,0]);assert.throws(()=>s.retrieve('query',T,{space:'local-model-v1'}));assert.equal(s.retrieve('',T,{space:'local-model-v1',queryVector:[1,0,0]})[0].id,r.id);s.close();
});
test('6000 encrypted records reopen beyond previous limit; atomic manifest retains old generation on failed write',t=>{
 const storage=fixture(t);let s=new Store(storage);for(let i=0;i<6000;i++)s.add('fact','合成容量 '+i,T);s.save();assert.equal(s.data.version,3);assert.ok(s.data.chunks.length>20);const manifest=fs.readFileSync(path.join(storage.directory,'memory.enc'));const count=s.data.chunks.length;s.close();s=new Store(storage);assert.equal(s.data.records.length,6000);const original=s.write.bind(s);let calls=0;s.write=(name,value)=>{if(++calls===2)throw Error('synthetic power interruption');return original(name,value);};s.add('fact','合成未提交的记录',T);assert.throws(()=>s.save());assert.deepEqual(fs.readFileSync(path.join(storage.directory,'memory.enc')),manifest);s.close();s=new Store(storage);assert.equal(s.data.records.length,6000);assert.equal(s.data.chunks.length,count);for(const name of s.data.chunks)assert.ok(!fs.readFileSync(path.join(storage.directory,name)).includes(Buffer.from('合成容量')));s.save();s.close();
});
test('authenticated chunk corruption fails closed and management uses explicit sourced correction',t=>{
 const storage=fixture(t);let s=new Store(storage);const r=s.add('fact','合成旧值',T);let result=s.begin('manage','更正记忆 '+r.id+'：合成新值',T);assert.match(result.management,/已更正/);const next=s.data.records.find(r=>r.text==='合成新值');assert.ok(next.sourceIds.length);assert.ok(!s.context('合成旧值',T).references.some(x=>x.id===r.id));for(let i=0;i<520;i++)s.add('fact','填充合成'+i,T);s.save();const file=s.data.chunks[0];s.close();const env=JSON.parse(fs.readFileSync(path.join(storage.directory,file)));env.tag=Buffer.alloc(16).toString('base64');fs.writeFileSync(path.join(storage.directory,file),JSON.stringify(env));assert.throws(()=>new Store(storage));
});
test('text and native contexts enforce the same suppression and budget, no-store wording policy is in prompt',async t=>{
 const api={storage:fixture(t),settings:{}};let s=new Store(api.storage);s.begin('a','记住：合成禁止话题',T);s.begin('b','别再提合成禁止话题',T);s.close();const native=await assistantAgent({phase:'native.recall',heard:'合成禁止话题',now:T},api);assert.ok(!native.instructions.includes('"text":"合成禁止话题"'));assert.match(native.instructions,/mentionPolicy/);const reply=await assistantAgent({phase:'begin',heard:'不要保存，这次简短回复',now:T,turnId:'no'},api);assert.equal(reply.state.context.noStore,true);assert.match(reply.messages[0].content,/不说“记下了”/);
});
test('persistent suppression filters older user and assistant chat messages, not just vector results',async t=>{
 const storage=fixture(t),api={storage,settings:{}};const s=new Store(storage);learn(s,1,'我喜欢合成青铜项目蓝色','项目蓝色');s.close();
 const out=await assistantAgent({phase:'begin',turnId:'boundary-history',heard:'别再提合成青铜项目',history:[{role:'user',content:'合成青铜项目旧事情'},{role:'assistant',content:'项目蓝色'}],now:T},api);assert.ok(!out.messages.slice(1,-1).some(m=>/合成青铜|项目蓝色/.test(m.content)));
});
test('new explicit expression preference replaces the opposite current value without erasing provenance',t=>{
 const s=new Store(fixture(t));learn(s,1,'希望回答简洁','希望回答简洁');const old=s.data.records.find(r=>r.kind==='preference');learn(s,2,'以后回答详细一点','回答详细');assert.equal(old.status,'superseded');assert.ok(!s.context('回答风格',T).references.some(r=>r.id===old.id));assert.ok(!s.context('回答风格',T).references.some(r=>r.kind==='episode'&&r.text==='希望回答简洁'));s.close();
});
test('loopback embedding adapter performs bounded resumable indexing and own-space search; remote URLs rejected',async t=>{
 const http=require('node:http'),{run,embedding}=require('../../../scripts/mixsoul-local.cjs');const storage=fixture(t);let s=new Store(storage);const r=s.add('fact','合成向量事实',T);s.save();s.close();let calls=0;
 const server=http.createServer((req,res)=>{let body='';req.on('data',chunk=>body+=chunk);req.on('end',()=>{calls++;const parsed=JSON.parse(body);res.setHeader('Content-Type','application/json');res.end(JSON.stringify({data:parsed.input.map((_,index)=>({index,embedding:[1,0,0]}))}));});});await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));t.after(()=>new Promise(resolve=>server.close(resolve)));const endpoint='http://127.0.0.1:'+server.address().port+'/v1/embeddings';
 const result=await run({storage,action:'embed',now:T,space:'local-fixture-v1',model:'controlled-fixture',dimensions:3,endpoint});assert.equal(result.indexed,1);assert.equal(result.remaining,0);const second=await run({storage,action:'embed',now:T,space:'local-fixture-v1',model:'controlled-fixture',dimensions:3,endpoint});assert.equal(second.indexed,0);const search=await run({storage,action:'search',query:'任意查询',now:T,space:'local-fixture-v1',endpoint});assert.equal(search.references[0].id,r.id);assert.equal(calls,2);await assert.rejects(()=>embedding('https://example.com/','x',['secret']));
});
test('named vectors reject wrong counts and local model eval has no remote fallback',async()=>{
 const {run}=require('../../../scripts/mixsoul-model-eval.cjs');await assert.rejects(()=>run({endpoint:'https://example.com/v1/chat/completions',model:'small'}));const report=require('../../../scripts/mixsoul-eval.cjs').evaluate();assert.equal(report.modelCalls,0);assert.deepEqual(report.summary.mixsoul,{pass:6,total:6});assert.equal(report.smallModel.status,'NOT_RUN');
});
test('completion deadline makes an unresolved promise overdue, never silently expired',t=>{
 const s=new Store(fixture(t));learn(s,1,'我承诺在2026-10-26之前完成合成安排','合成安排待完成','promise',{validTo:later,dueAt:later});const ref=s.retrieve('合成安排',later).find(r=>r.text==='合成安排待完成');assert.ok(ref);assert.equal(ref.reviewDue,true);assert.equal(ref.validTo,null);s.close();
});
test('negated wish and context-specific feedback cannot collapse into the positive preference',()=>{
 assert.notEqual(signature('preference','我不希望回答简洁'),signature('preference','希望回答简洁'));assert.notEqual(signature('preference','我不太喜欢回答简洁'),signature('preference','希望回答简洁'));
});
test('expired derived evidence cannot return through a raw source episode',t=>{
 const s=new Store(fixture(t));learn(s,1,'合成安排有效至2026-10-26，过期作废','合成安排','decision',{validTo:later});const refs=s.context('合成安排',later).references;assert.ok(!refs.some(r=>/合成安排/.test(r.text)));s.close();
});
test('owner management can inspect provenance without vectors and ID deletion removes source-based resurrection',t=>{
 const s=new Store(fixture(t));learn(s,1,'我喜欢合成青铜项目蓝色','合成青铜蓝色');const target=s.data.records.find(r=>r.kind==='preference');const page=s.management({operation:'list'},T);assert.equal(page.total,1);assert.equal(page.records[0].sourceCount,1);assert.ok(!JSON.stringify(page).includes('vectors'));s.management({operation:'suppress',recordId:target.id},T);assert.equal(s.management({operation:'list'},T).records[0].suppressed,true);s.management({operation:'delete',recordId:target.id},T);assert.equal(s.data.records.length,0);assert.deepEqual(s.context('青铜蓝色',T).references,[]);s.close();
});
test('obligations are considered even when many unrelated high-score preferences fill the first candidate page',t=>{
 const s=new Store(fixture(t));for(let i=0;i<40;i++)s.add('preference','合成目标偏好 '+i,T,{importance:.95,confidence:1});const promise=s.add('promise','合成低权重承诺',T,{importance:.1,confidence:.1});const c=s.context('合成目标偏好',T);assert.ok(c.references.some(r=>r.id===promise.id));assert.ok(c.contextBudget.used<=3200);s.close();
});
test('dedup keeps all evidence roots beyond adjustment-history limit so deletion cannot resurrect an old source',t=>{
 const s=new Store(fixture(t));for(let i=0;i<45;i++)learn(s,'many-'+i,'希望回答简洁','希望回答简洁');const r=s.data.records.find(x=>x.kind==='preference');assert.equal(r.sourceIds.length,45);const c=s.context('回答风格',T);assert.ok(c.references.filter(x=>x.id===r.id).every(x=>x.sourceIds.length<=4&&x.sourceCount===45));assert.ok(!c.references.some(x=>x.kind==='episode'));s.management({operation:'delete',recordId:r.id},T);assert.equal(s.data.records.length,0);s.close();
});
test('management page stays within host IPC budget and correction fetches full original text, never its snippet',t=>{
 const s=new Store(fixture(t));for(let i=0;i<20;i++)s.add('fact',String(i)+'长'.repeat(1998),T);for(let i=0;i<128;i++)s.data.mentionRules.push({term:String(i)+'\u0001'.repeat(117),at:T});const page=s.management({operation:'list'},T);assert.equal(page.records.length,10);assert.equal(page.pageSize,10);assert.equal(page.total,128);assert.ok(Buffer.byteLength(JSON.stringify(page))<24000);assert.equal(page.records[0].truncated,true);assert.equal(s.management({operation:'get',recordId:page.records[0].id},T).record.text.length,1999);s.close();
});
test('optional runtime embeddings reach text/native prompts, persist vectors, and idle pulse stays zero-call',async t=>{
 const http=require('node:http'),storage=fixture(t);let hits=0;const server=http.createServer((req,res)=>{let raw='';req.on('data',x=>raw+=x);req.on('end',()=>{hits++;const body=JSON.parse(raw);assert.ok(body.input.length<=16);res.end(JSON.stringify({data:body.input.map((_,index)=>({index,embedding:[1,0,0]}))}));});});await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));t.after(()=>new Promise(resolve=>server.close(resolve)));const cfg={endpoint:'http://127.0.0.1:'+server.address().port+'/v1/embeddings',model:'synthetic-local',revision:'v1',dimensions:3},api={storage,settings:{localEmbedding:cfg,consolidate:false}};
 let s=new Store(storage);s.add('fact','合成神经向量记忆',T);s.save();s.close();const r=await assistantAgent({phase:'native.recall',heard:'合成向量',now:T},api);assert.match(r.instructions,/"status":"ready"/);s=new Store(storage);assert.ok(Object.keys(s.data.records[0].vectors).some(x=>x.startsWith('mixsoul-local-')));s.close();const before=hits;await assistantAgent({phase:'pulse',now:T},api);await assistantAgent({phase:'native.time',now:T},api);await assistantAgent({phase:'native.observe',role:'assistant',text:'助手回复',now:T},api);assert.equal(hits,before);
 const fallback=await assistantAgent({phase:'begin',turnId:'bad-endpoint',heard:'正常提问',now:T},{storage,settings:{localEmbedding:{...cfg,endpoint:'https://example.com/embeddings'}}});assert.equal(fallback.state.context.memoryPolicy.localEmbedding.status,'unavailable');assert.equal(hits,before);
});
test('assistant dialogue is encrypted provenance evidence, never a user fact or independent emotional proof',t=>{
 const storage=fixture(t);let s=new Store(storage);const c=s.begin('evidence','合成青铜项目让我不知道怎么办',T);c.emotion={current:{labels:['frustration'],basis:'inferred',confidence:.35}};
 assert.equal(s.rememberReply(c,'我之前建议先核对合成青铜项目的材料。',T),true);assert.equal(s.data.records.filter(r=>r.kind==='fact').length,0);s.close();s=new Store(storage);
 const evidence=s.context('你刚才对合成青铜项目建议了什么',later).inferenceEvidence;assert.equal(evidence.assistantTurns.length,1);assert.equal(evidence.assistantTurns[0].role,'assistant');assert.equal(evidence.assistantTurns[0].sourceStatement.text,'合成青铜项目让我不知道怎么办');assert.equal(evidence.assistantTurns[0].notUserFact,true);assert.equal(evidence.assistantTurns[0].emotionAssessment.epistemicStatus,'tentative-hypothesis');assert.deepEqual(evidence.assistantTurns[0].sourceIds,[c.sourceId]);
 assert.equal(s.rememberReply({noStore:true,sourceId:c.sourceId},'不要记录',T),false);
 assert.equal(s.context('你之前说了什么','2026-11-03T00:00:00Z').inferenceEvidence.assistantTurns.length,0);
 s.management({operation:'delete',recordId:c.sourceId},later);assert.equal(s.data.assistantTurns.length,0);assert.equal(s.context('你之前说了什么',later).inferenceEvidence.assistantTurns.length,0);s.close();
});
