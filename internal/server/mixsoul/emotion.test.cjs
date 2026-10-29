'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),os=require('node:os'),path=require('node:path'),crypto=require('node:crypto');
const {Store}=require('./store.cjs');const {assistantAgent}=require('./agent.cjs');const {currentEmotion,validateAppraisal}=require('./emotion.cjs');
const T='2026-10-25T00:00:00.000Z',at=d=>new Date(Date.parse(T)+d*86400000).toISOString();
function fixture(t){const directory=fs.mkdtempSync(path.join(os.tmpdir(),'mixsoul-emotion-'));t.after(()=>fs.rmSync(directory,{recursive:true,force:true}));return {directory,key:crypto.randomBytes(32).toString('base64'),scope:'e'.repeat(32)}}
const sadness=(quote,ids=[])=>({labels:['sadness'],basis:'explicit',quote,confidence:.99,linkedMemoryIds:ids});
test('current self-report is bounded and third-party/past/negative evidence is not today\'s mood',()=>{
 assert.deepEqual(currentEmotion('我今天很难过',T).labels,['sadness']);
 for(const heard of ['朋友说我今天很难过','小说台词“我很难过”','我去年很难过','我不难过了','不是我很难过，是他很难过'])assert.deepEqual(currentEmotion(heard,T).labels,[],heard);
 assert.deepEqual(currentEmotion('我今天很难过，但我也很开心',T).labels,['joy','sadness']);
});
test('inferred feelings remain tentative; invented quote/label/ID and past/future claims rejected',()=>{
 const heard='努力了这么久还是没有结果';const r=validateAppraisal(sadness(heard,['outside']),heard,T,['inside']);assert.equal(r.basis,'inferred');assert.equal(r.confidence,.4);assert.deepEqual(r.linkedMemoryIds,[]);
 assert.equal(validateAppraisal(sadness('不存在'),heard,T).basis,'unknown');
 assert.deepEqual(validateAppraisal({labels:['diagnosis'],basis:'explicit',quote:heard},heard,T).labels,[]);
 for(const h of ['我不难过了','我昨天很难过，现在好了','我明天可能很难过'])assert.deepEqual(validateAppraisal(sadness(h),h,T).labels,[],h);
});
test('explicit affect links only exposed IDs; it never strengthens a linked fact or changes habits',t=>{
 const s=new Store(fixture(t)),fact=s.add('fact','合成项目的蓝色方案',T,{importance:.7,confidence:.6}),heard='合成项目我今天很难过';const c=s.begin('one',heard,T),before=JSON.stringify(fact),habits=JSON.stringify(s.data.habits);
 const got=s.appraise(sadness('我今天很难过',[fact.id,'forged',fact.id]),c,heard,T);
 assert.equal(got.emotion.current.confidence,.75);assert.deepEqual(got.emotion.current.linkedMemoryIds,[fact.id]);assert.equal(JSON.stringify(fact),before);assert.equal(JSON.stringify(s.data.habits),habits);
 const row=s.data.records.find(r=>r.kind==='emotion');assert.equal(row.sourceIds[0],c.sourceId);assert.deepEqual(row.emotion.linkedMemoryIds,[fact.id]);assert.equal(row.importance,.4);assert.equal(row.confidence,.55);assert.equal(row.emotion.historical,true);s.close();
});
test('emotion and source survive encrypted reopening; future conversation never inherits old mood',t=>{
 const storage=fixture(t);let s=new Store(storage),heard='合成天青项目我今天很难过',c=s.begin('one',heard,T);s.appraise(sadness('我今天很难过'),c,heard,T);const id=s.data.records.find(r=>r.kind==='emotion').id;s.close();
 assert.ok(!fs.readFileSync(path.join(storage.directory,'memory.enc'),'utf8').includes('难过'));
 s=new Store(storage);const later=s.context('合成天青项目',at(1));assert.deepEqual(later.emotion.current.labels,[]);assert.equal(later.emotion.policy.historicalEmotionIsCurrent,false);assert.ok(later.references.some(r=>r.id===id&&r.emotion.historical));
 const positive=s.context('我今天很开心，合成天青项目',at(1));assert.deepEqual(positive.emotion.current.labels,['joy']);s.close();
});
test('no-store and tentative evidence cannot create inferred emotional biography',t=>{
 const s=new Store(fixture(t));let heard='不要保存，我今天很难过',c=s.begin('no',heard,T);s.appraise(sadness('我今天很难过'),c,heard,T);assert.equal(s.data.records.length,0);
 heard='努力了这么久还是没有结果';c=s.begin('maybe',heard,T);const got=s.appraise(sadness(heard),c,heard,T);assert.equal(got.emotion.current.basis,'inferred');assert.ok(!s.data.records.some(r=>r.kind==='emotion'));s.close();
});
test('appraisal source is idempotent and rejects stale/mismatched evidence',t=>{
 const s=new Store(fixture(t)),heard='我今天很难过',c=s.begin('one',heard,T);
 s.appraise(sadness(heard),c,heard,T);s.appraise(sadness(heard),c,heard,T);assert.equal(s.data.records.filter(r=>r.kind==='emotion').length,1);
 const c2=s.begin('two',heard,T);s.appraise(sadness(heard),c2,heard,at(1));assert.equal(s.data.records.filter(r=>r.kind==='emotion').length,1);
 s.appraise(sadness('我很难过'),c2,'我很难过',T);assert.equal(s.data.records.filter(r=>r.kind==='emotion').length,1);s.close();
});
test('historical emotion decays, never gains weight on recall/pulse, and is deleted with source',t=>{
 const s=new Store(fixture(t)),heard='合成紫檀项目我今天很难过',c=s.begin('one',heard,T);s.appraise(sadness('我今天很难过'),c,heard,T);const row=s.data.records.find(r=>r.kind==='emotion');assert.equal(s.retention(row,at(7)),.5);const before=[row.importance,row.stability,row.lastConfirmedAt];for(let i=0;i<50;i++)s.context('合成紫檀项目',at(1));s.pulse(at(1));assert.deepEqual([row.importance,row.stability,row.lastConfirmedAt],before);assert.equal(s.reinforce(row,s.data.records[0],at(1),'sad again'),false);
 assert.equal(s.forget('合成紫檀项目').removed,2);assert.ok(!s.data.records.some(r=>r.kind==='emotion'));s.close();
});
test('deleting a linked fact prunes affect links without inventing a replacement',t=>{
 const s=new Store(fixture(t)),fact=s.add('fact','合成独立锚点',T),heard='我今天很难过',c=s.begin('one',heard,T);c.references=[{id:fact.id}];s.appraise(sadness(heard,[fact.id]),c,heard,T);s.forget('合成独立锚点');assert.deepEqual(s.data.records.find(r=>r.kind==='emotion').emotion.linkedMemoryIds,[]);s.close();
});
test('do not mention boundary suppresses past links and personality inference is rejected',t=>{
 const s=new Store(fixture(t)),heard='我今天很难过，别再提合成往事',c=s.begin('one',heard,T);c.references=[{id:'old'}];const got=s.appraise(sadness('我今天很难过',['old']),c,heard,T);assert.equal(got.emotion.policy.avoidHistoricalMention,true);assert.deepEqual(got.emotion.current.linkedMemoryIds,[]);
 s.consolidate({items:[{kind:'preference',text:'用户有忧郁人格',quote:'我今天很难过',importance:.8,reason:'不能这样推断'}]},c.sourceId,heard,T,['old']);assert.ok(!s.data.records.some(r=>r.kind==='preference'));s.close();
});
test('text appraisal changes the actual next dialogue prompt, with no tools during appraisal',async t=>{
 const api={storage:fixture(t),settings:{}},heard='合成天青项目我今天很难过',input={phase:'begin',now:T,turnId:'turn',heard,identity:'test',history:[],hostTools:['dispatch_to_aide']};const first=await assistantAgent(input,api);assert.equal(first.state.stage,'appraise');assert.deepEqual(first.tools,[]);
 const next=await assistantAgent({...input,phase:'step',state:first.state,modelText:JSON.stringify(sadness('我今天很难过'))},api);assert.equal(next.state.stage,'dialogue');assert.equal(next.state.context.emotion.current.basis,'explicit');assert.match(next.messages[0].content,/acknowledge-gently/);assert.match(next.messages[0].content,/mentionPastEmotionOnlyIfInvited/);assert.deepEqual(next.tools,['dispatch_to_aide']);
 const learned=await assistantAgent({...input,phase:'step',state:next.state,modelText:'听起来今天不太好受。'},api);const end=await assistantAgent({...input,phase:'step',state:learned.state,modelText:'{"items":[]}'},api);assert.equal(end.reply,'听起来今天不太好受。');
 const s=new Store(api.storage);assert.equal(s.data.records.filter(r=>r.kind==='emotion').length,1);s.close();
});
test('appraisal provider failure degrades to existing evidence, no silent tool dispatch',async t=>{
 const api={storage:fixture(t),settings:{}},input={phase:'begin',now:T,turnId:'error',heard:'我今天很难过',history:[],hostTools:[]};const first=await assistantAgent(input,api);const fallback=await assistantAgent({...input,phase:'model.error',state:first.state},api);assert.equal(fallback.state.stage,'dialogue');assert.deepEqual(fallback.tools,[]);assert.match(fallback.messages[0].content,/sadness/);
});
test('native context uses immediate evidence, learning adds an event, time-only refresh has no current mood',async t=>{
 const api={storage:fixture(t),settings:{}},heard='合成项目我今天很难过';const observe=await assistantAgent({phase:'native.observe',role:'user',text:heard,turnId:'native',now:T},api);assert.match(observe.instructions,/sadness/);
 const learn=await assistantAgent({phase:'native.learn',turnId:'native',heard,now:T},api);await assistantAgent({phase:'step',heard,now:T,state:learn.state,modelText:JSON.stringify({items:[],emotion:sadness('我今天很难过')})},api);
 const s=new Store(api.storage);assert.equal(s.data.records.filter(r=>r.kind==='emotion').length,1);s.close();
 const clock=await assistantAgent({phase:'native.time',now:at(1)},api);assert.match(clock.instructions,/"labels":\[\]/);assert.ok(!clock.instructions.includes('"basis":"explicit"'));
});
test('malformed/primitive appraisal cannot crash dialogue or invent a label',async t=>{
 for(const modelText of ['null','[]','42','{"labels":["diagnosis"],"quote":"不存在"}']){
  const api={storage:fixture(t),settings:{}},input={phase:'begin',now:T,turnId:modelText,heard:'努力了这么久还是没有结果',history:[],hostTools:[]};const first=await assistantAgent(input,api);const next=await assistantAgent({...input,phase:'step',state:first.state,modelText},api);assert.equal(next.state.stage,'dialogue');assert.deepEqual(next.state.context.emotion.current.labels,[]);
 }
});
test('a current positive emotion is not reversed by a model-selected old negative record',t=>{
 const s=new Store(fixture(t));let h='合成青铜项目我今天很难过',c=s.begin('past',h,T);s.appraise(sadness('我今天很难过'),c,h,T);
 h='合成青铜项目我今天很开心';c=s.begin('now',h,at(1));const got=s.appraise({labels:['sadness'],basis:'explicit',quote:'我今天很开心',confidence:1},c,h,at(1));assert.equal(got.emotion.current.basis,'explicit');assert.deepEqual(got.emotion.current.labels,['joy']);assert.ok(!s.data.records.some(r=>r.kind==='emotion'&&r.emotion.at===at(1)&&r.emotion.labels.includes('sadness')));s.close();
});
test('questions and explicitly fictional self-statements are not stored as current affect',t=>{
 const s=new Store(fixture(t));for(const h of ['我今天很难过吗？','你觉得我今天很难过吗','我很难过，是假的，开玩笑']){const c=s.begin(h,h,T);s.appraise(sadness(h),c,h,T);assert.ok(!s.data.records.some(r=>r.kind==='emotion'),h);}s.close();
});
test('emotional confirmation alone cannot reinforce an exposed factual memory',t=>{
 const s=new Store(fixture(t)),fact=s.add('fact','合成项目采用蓝色',T,{importance:.7,confidence:.6}),heard='是的，我今天很难过',c=s.begin('one',heard,T);
 s.consolidate({updates:[{id:fact.id,action:'reinforce',quote:'我今天很难过',reason:'情绪不是确认事实的证据'}]},c.sourceId,heard,T,[fact.id]);assert.equal(fact.importance,.7);assert.equal(fact.stability,1);assert.equal(fact.confidence,.6);s.close();
});
test('a remembered ID deleted between retrieval and appraisal cannot receive an emotion link',t=>{
 const s=new Store(fixture(t)),old=s.add('fact','合成临时锚点',T),heard='我今天很难过',c=s.begin('one',heard,T);c.references=[{id:old.id}];s.forget('合成临时锚点');const got=s.appraise(sadness(heard,[old.id]),c,heard,T);assert.deepEqual(got.emotion.current.linkedMemoryIds,[]);s.close();
});
test('negation in the previous clause does not suppress a new explicit calm self-report',()=>{
 const h='不要保存本轮内容。合成纠正项目，我不难过了，我今天很平静。请就今天的状态回应一句。';const seed=currentEmotion(h,T);assert.deepEqual(seed.labels,['calm']);const got=validateAppraisal({labels:['calm','relief'],basis:'explicit',quote:'我今天很平静',confidence:.8},h,T);assert.deepEqual(got.labels,['calm']);assert.equal(got.basis,'explicit');
});
test('an extra unsupported emotion cannot discard a supported emotion and valid event link',t=>{
 const s=new Store(fixture(t)),fact=s.add('fact','合成项目评审推迟',T),h='合成项目我今天很难过，因为评审又推迟了',c=s.begin('mixed-label',h,T);c.references=[{id:fact.id}];const got=s.appraise({labels:['sadness','frustration'],basis:'explicit',quote:'我今天很难过',confidence:.8,linkedMemoryIds:[fact.id]},c,h,T);assert.deepEqual(got.emotion.current.labels,['sadness']);assert.deepEqual(got.emotion.current.linkedMemoryIds,[fact.id]);assert.deepEqual(s.data.records.find(r=>r.kind==='emotion').emotion.linkedMemoryIds,[fact.id]);s.close();
});
