'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict');
const fs=require('node:fs'),os=require('node:os'),path=require('node:path'),crypto=require('node:crypto');
const {Store}=require('./store.cjs');const {assistantAgent}=require('./agent.cjs');
const T0='2026-10-01T00:00:00.000Z',at=d=>new Date(Date.parse(T0)+d*86400000).toISOString();
const report={timeBasis:'injected UTC timestamps; OS clock untouched',curves:[],weights:{},time:{},limits:{dormantIsRecoverable:true,pinnedAndOpenPromisesProtected:true}};
function fixture(t){const directory=fs.mkdtempSync(path.join(os.tmpdir(),'mixsoul-adaptive-'));t.after(()=>fs.rmSync(directory,{recursive:true,force:true}));return {directory,key:crypto.randomBytes(32).toString('base64'),scope:'d'.repeat(32)}}
function store(t){const s=new Store(fixture(t));t.after(()=>s.close());s.registerSpace('controlled-v1',{dimensions:2,model:'synthetic-controlled',version:'1',metric:'cosine'});return s}
function row(s,kind,text,now=T0,extra={},vec=[1,0]){const r=s.add(kind,text,now,extra);s.setVector(r.id,'controlled-v1',vec);return r}
const retrieve=(s,now,includeDormant=false,limit=20)=>s.retrieve('',now,{space:'controlled-v1',queryVector:[1,0],includeDormant,limit});
const score=(s,id,now)=>retrieve(s,now).find(r=>r.id===id)?.score??null;

test('age materially changes rank and eligibility; dormant records remain recoverable and encrypted',t=>{
 const s=store(t),episode=row(s,'episode','synthetic episode'),fact=row(s,'fact','synthetic fact'),pin=row(s,'fact','synthetic pinned',T0,{pinned:true}),promise=row(s,'promise','synthetic promise');
 for(const days of [0,3,6,9,30,45,90,180,3650])report.curves.push({days,episode:score(s,episode.id,at(days)),fact:score(s,fact.id,at(days)),pinned:score(s,pin.id,at(days)),promise:score(s,promise.id,at(days))});
 assert.ok(score(s,episode.id,at(3))<score(s,episode.id,T0)*.7);assert.equal(score(s,episode.id,at(9)),null);assert.equal(score(s,fact.id,at(180)),null);
 s.pulse(at(180));assert.equal(s.data.records.find(r=>r.id===episode.id).status,'dormant');assert.ok(retrieve(s,at(180),true).some(r=>r.id===episode.id&&r.status==='dormant'));
 assert.equal(score(s,pin.id,at(3650)),score(s,pin.id,T0));assert.equal(score(s,promise.id,at(3650)),score(s,promise.id,T0));
 const state=JSON.stringify(s.data.records);for(let i=0;i<100;i++)retrieve(s,at(180),true);assert.equal(JSON.stringify(s.data.records),state);
});

test('importance/confidence affect competition; strong relevance can outweigh importance',t=>{
 const s=store(t),low=row(s,'fact','low importance',T0,{importance:.1,confidence:.6}),high=row(s,'fact','high importance',T0,{importance:.9,confidence:.6});assert.equal(retrieve(s,T0,false,1)[0].id,high.id);report.weights.importanceDifference=Number((score(s,high.id,T0)-score(s,low.id,T0)).toFixed(3));
 const c0=row(s,'fact','low confidence',T0,{importance:.5,confidence:.1}),c1=row(s,'fact','high confidence',T0,{importance:.5,confidence:.9});assert.ok(score(s,c1.id,T0)>score(s,c0.id,T0));report.weights.confidenceDifference=Number((score(s,c1.id,T0)-score(s,c0.id,T0)).toFixed(3));
 const related=row(s,'fact','related lower importance',T0,{importance:.1}),weak=row(s,'fact','weakly related',T0,{importance:.95},[.1,Math.sqrt(.99)]);assert.ok(score(s,related.id,T0)>score(s,weak.id,T0));
});

test('source-backed model importance is bounded; missing reasons and invented evidence cannot raise it',t=>{
 const s=store(t),heard='合成用户希望回答简洁并保留关键约定',context=s.begin('learn',heard,T0);
 s.consolidate({items:[{kind:'preference',text:'简洁偏好',quote:'希望回答简洁',importance:99,reason:'用户明确的长期表达偏好'},{kind:'decision',text:'默认决定',quote:'保留关键约定',importance:.99},{kind:'promise',text:'虚构承诺',quote:'不存在的原文',importance:.8,reason:'编造理由'}]},context.sourceId,heard,T0);
 assert.equal(s.data.records.find(r=>r.text==='简洁偏好').importance,.8);assert.equal(s.data.records.find(r=>r.text==='默认决定').importance,.5);assert.ok(!s.data.records.some(r=>r.text==='虚构承诺'));
 report.weights.boundedModelImportance=.8;report.weights.unreasonedImportance=.5;
});

test('explicit confirmation reinforces with evidence, rate limits, and survives reopening',t=>{
 const storage=fixture(t);let s=new Store(storage);const original=s.begin('original','我喜欢合成简洁风格',T0);
 s.consolidate({items:[{kind:'preference',text:'合成简洁偏好',quote:'喜欢合成简洁',importance:.5,reason:'用户自己的偏好'}]},original.sourceId,'我喜欢合成简洁风格',T0);
 const r=s.data.records.find(r=>r.kind==='preference'),id=r.id;const heard='是的，继续保持合成简洁风格';let c=s.begin('confirm',heard,at(1));
 const update={items:[],updates:[{id,action:'reinforce',quote:'继续保持合成简洁风格',reason:'用户再次明确确认'}]};s.consolidate(update,c.sourceId,heard,at(1),[id]);assert.equal(r.stability,1.25);assert.equal(r.importance,.55);assert.equal(r.lastConfirmedAt,at(1));
 assert.equal(s.consolidate(update,c.sourceId,heard,at(1),[id]),0);c=s.begin('same-day',heard,at(1));s.consolidate(update,c.sourceId,heard,at(1),[id]);assert.equal(r.stability,1.25);
 s.close();s=new Store(storage);assert.equal(s.data.records.find(r=>r.id===id).stability,1.25);assert.ok(s.data.records.find(r=>r.id===id).adjustments[0].sourceId);s.close();
 report.weights.confirmedStability=1.25;report.weights.selfReadingReinforces=false;report.weights.sameDayRepeatedBoost=false;
});

test('unexposed IDs, vague statements and expired evidence cannot reinforce',t=>{
 const s=store(t),r=s.add('preference','合成偏好',T0),heard='是的，继续保持';let c=s.begin('unexposed',heard,at(1));const change={items:[],updates:[{id:r.id,action:'reinforce',quote:'继续保持',reason:'用户确认偏好'}]};s.consolidate(change,c.sourceId,heard,at(1),[]);assert.equal(r.stability,1);
 const vague='也许是吧';c=s.begin('vague',vague,at(2));s.consolidate({items:[],updates:[{id:r.id,action:'reinforce',quote:vague,reason:'模型猜测确认'}]},c.sourceId,vague,at(2),[r.id]);assert.equal(r.stability,1);
});

test('explicit correction supersedes old evidence; completed promises stop automatic recall',t=>{
 const s=store(t),old=s.add('preference','旧合成偏好',T0),promise=s.add('promise','合成承诺',T0);
 let heard='更正，合成偏好改为蓝色';let c=s.begin('correct',heard,at(1));s.consolidate({updates:[{id:old.id,action:'correct',text:'合成偏好蓝色',quote:'合成偏好改为蓝色',reason:'用户明确更正'}]},c.sourceId,heard,at(1),[old.id]);assert.equal(old.status,'superseded');assert.ok(s.data.records.some(r=>r.text==='合成偏好蓝色'));
 heard='合成承诺已经完成';c=s.begin('resolve',heard,at(2));s.consolidate({updates:[{id:promise.id,action:'resolve',quote:'已经完成',reason:'用户明确报告完成'}]},c.sourceId,heard,at(2),[promise.id]);assert.equal(promise.status,'resolved');assert.ok(!s.retrieve('合成承诺',at(365)).some(r=>r.id===promise.id));
});

test('explicit expiry and supersession exclude even protected records',t=>{
 const s=store(t),expired=row(s,'fact','expired pinned',T0,{pinned:true,validTo:at(1)}),old=row(s,'fact','superseded'),next=row(s,'fact','replacement');s.supersede(old.id,next.id,at(1));assert.ok(!retrieve(s,at(2)).some(r=>r.id===expired.id||r.id===old.id));assert.ok(retrieve(s,at(2)).some(r=>r.id===next.id));
});

test('time context survives reopen and is identical in text and native channels',async t=>{
 const storage=fixture(t);let s=new Store(storage);s.begin('first','合成事件',T0);s.close();s=new Store(storage);const p=s.pulse(at(2));assert.equal(p.idleSeconds,172800);assert.equal(p.modelCalls,0);assert.equal(p.newEvents,0);s.close();
 const input={now:at(2),timeZone:'Asia/Shanghai',identity:'Synthetic identity'},api={storage,settings:{}};
 const native=await assistantAgent({...input,phase:'native.begin'},api);const text=await assistantAgent({...input,phase:'begin',turnId:'next',heard:'不要保存本轮内容',history:[],hostTools:[]},api);
 for(const value of ['172800','Asia/Shanghai','2026-10-03'])assert.ok(native.instructions.includes(value));assert.equal(text.state.context.idleSeconds,172800);assert.equal(text.state.context.timezone,'Asia/Shanghai');assert.ok(text.state.context.localTime.endsWith('08:00:00'));
 report.time.idleSecondsAfterReopen=172800;report.time.nativeIncludesCalculatedIdle=true;report.time.timezone='Asia/Shanghai';
});

test('clock rollback is propagated without reducing the persisted high-water mark',async t=>{
 const storage=fixture(t);let s=new Store(storage);s.begin('interaction','合成事件',T0);s.pulse(at(2));const p=s.pulse(at(-1));assert.equal(p.clockAnomaly,true);assert.equal(p.lastPulseAt,at(2));s.close();
 const result=await assistantAgent({phase:'native.time',now:at(-1),identity:'fixture'},{storage,settings:{}});assert.ok(result.instructions.includes('"clockAnomaly":true'));assert.ok(result.instructions.includes('"elapsedReliable":false'));report.time.rollbackAnomalyForwarded=true;
});

test('expression feedback gradually relaxes; no-store statements do not train habits',t=>{
 const s=store(t);s.begin('feedback','别总追问我',T0);assert.equal(s.habitsAt(T0).questions,.32);assert.ok(s.habitsAt(at(180)).questions>.45);s.begin('private','不要保存本轮内容，太冷淡，温柔一点',at(1));assert.equal(s.data.habits.warmth,undefined);report.weights.feedbackAt180Days=s.habitsAt(at(180)).questions;
});

test('v1 encrypted state migrates with an immutable encrypted recovery snapshot',t=>{
 const storage=fixture(t);let s=new Store(storage);s.begin('old','记住：合成旧记忆',T0);s.data.version=1;s.save();s.close();
 s=new Store(storage);assert.equal(s.data.version,2);assert.ok(s.data.records.some(r=>r.text.includes('合成旧记忆')));assert.ok(s.read('memory-v1.enc').version===1);const backup=fs.readFileSync(path.join(storage.directory,'memory-v1.enc'));assert.ok(!backup.includes(Buffer.from('合成旧记忆')));s.save();s.close();s=new Store(storage);assert.deepEqual(fs.readFileSync(path.join(storage.directory,'memory-v1.enc')),backup);s.close();
});

test('native observations learn once through the same bounded consolidation path',async t=>{
 const storage=fixture(t),api={storage,settings:{}};const heard='我一直喜欢合成蓝色';await assistantAgent({phase:'native.observe',role:'user',text:heard,turnId:'voice-one',now:T0},api);
 const prepared=await assistantAgent({phase:'native.learn',heard,turnId:'voice-one',now:T0},api);assert.equal(prepared.action,'generate');assert.deepEqual(prepared.tools,[]);
 await assistantAgent({phase:'step',heard,now:T0,state:prepared.state,modelText:JSON.stringify({items:[{kind:'preference',text:'合成蓝色偏好',quote:'一直喜欢合成蓝色',importance:.7,reason:'明确的长期偏好'}]})},api);
 const second=await assistantAgent({phase:'native.learn',heard,turnId:'voice-one',now:T0},api);assert.equal(second.action,'finish');const s=new Store(storage);assert.equal(s.data.records.find(r=>r.kind==='preference').importance,.7);s.close();
});

test.after(()=>{if(process.env.MIXSOUL_TEST_REPORT)fs.writeFileSync(process.env.MIXSOUL_TEST_REPORT,JSON.stringify(report,null,2)+'\n')});

test('ordinary recall never activates dormant records; explicit old-memory recall can recover them',t=>{
 const s=store(t);s.begin('old','合成天青项目使用蓝色标志',T0);s.pulse(at(30));
 assert.ok(!s.context('合成天青项目',at(30)).references.some(r=>r.kind==='episode'));
 const old=s.context('回想以前的合成天青项目',at(30)).references;assert.ok(old.some(r=>r.status==='dormant'));
 assert.equal(s.data.records.find(r=>r.kind==='episode').status,'dormant');
});

test('negated confirmation cannot strengthen old beliefs, and corrected pinned facts stay pinned',t=>{
 const s=store(t);let c=s.begin('pin','记住：合成固定偏好蓝色',T0);const pin=s.data.records.find(r=>r.pinned);
 let heard='是的，但是不再喜欢合成蓝色';c=s.begin('negative',heard,at(1));s.consolidate({updates:[{id:pin.id,action:'reinforce',quote:'不再喜欢合成蓝色',reason:'不应该当作确认'}]},c.sourceId,heard,at(1),[pin.id]);assert.equal(pin.stability,1);
 heard='更正，合成固定偏好改为绿色';c=s.begin('change-pin',heard,at(2));s.consolidate({updates:[{id:pin.id,action:'correct',quote:'合成固定偏好改为绿色',reason:'用户明确更正固定偏好',text:'合成固定偏好绿色'}]},c.sourceId,heard,at(2),[pin.id]);const next=s.data.records.find(r=>r.text==='合成固定偏好绿色');assert.ok(next.pinned);assert.equal(next.importance,.95);
});

test('stale source evidence cannot be replayed to learn or reinforce later',t=>{
 const s=store(t),heard='合成延迟证据：喜欢简短';const c=s.begin('stale',heard,T0);
 assert.equal(s.consolidate({items:[{kind:'preference',text:'过期证据摘要',quote:'喜欢简短',importance:.8,reason:'旧请求被延迟重放'}]},c.sourceId,heard,at(2)),0);
 assert.ok(!s.data.records.some(r=>r.text==='过期证据摘要'));
});
