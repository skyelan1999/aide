'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict');
const fs=require('node:fs'),os=require('node:os'),path=require('node:path'),crypto=require('node:crypto');
const {assistantAgent,groundedRecallReply}=require('./agent.cjs'),{Store}=require('./store.cjs');
const {calibrateEmotionReply}=require('./emotion.cjs');
const now='2026-10-25T03:00:00Z';
function fixture(t){const directory=fs.mkdtempSync(path.join(os.tmpdir(),'mix-native-memory-'));t.after(()=>fs.rmSync(directory,{recursive:true,force:true}));return {storage:{directory,key:crypto.randomBytes(32).toString('base64'),scope:'b'.repeat(32)},settings:{}};}
async function observe(api,turnId,text,role='user'){return assistantAgent({phase:'native.observe',turnId,text,role,now},api);}
test('native turns retain self advice and explicit affect across independent agent invocations',async t=>{
 const api=fixture(t);await observe(api,'one','松鹤项目延期了，我今天很焦虑');
 await observe(api,'one','先写下一个卡点。编号：731864','assistant');
 const learned=await assistantAgent({phase:'native.learn',turnId:'one',heard:'松鹤项目延期了，我今天很焦虑',now},api);
 await assistantAgent({phase:'step',state:learned.state,heard:'松鹤项目延期了，我今天很焦虑',modelText:JSON.stringify({items:[],emotion:{labels:['anxiety'],basis:'explicit',quote:'我今天很焦虑',confidence:.7}}),now},api);
 const recalled=await assistantAgent({phase:'native.recall',heard:'松鹤项目，你之前建议和编号是什么',now},api);
 assert.match(recalled.instructions,/731864/);assert.match(recalled.instructions,/explicit-user-affect/);
 assert.match(recalled.instructions,/notUserFact/);
 const s=new Store(api.storage);assert.equal(s.data.assistantTurns.length,1);assert.equal(s.data.assistantTurns[0].emotionAssessment.basis,'explicit');assert.equal(s.data.records.filter(r=>r.kind==='emotion').length,1);s.close();
});
test('native projects do not share advice, format or historical affect',async t=>{
 const api=fixture(t);await observe(api,'one','松鹤项目，我今天很焦虑');await observe(api,'one','先理清卡点。编号：731864','assistant');
 const r=await assistantAgent({phase:'native.observe',role:'user',text:'竹海项目今天进展顺利，我很平静',turnId:'two',now},api);
 assert.doesNotMatch(r.instructions,/731864|先理清卡点/);assert.deepEqual(r.state.context.emotion.current.labels,['calm']);
 const plain=await assistantAgent({phase:'native.recall',heard:'松鹤项目下一步呢',now},api);assert.doesNotMatch(plain.instructions,/731864/);
});
test('native no-store prevents both sides and deletion prevents later learning',async t=>{
 const api=fixture(t);await observe(api,'private','不要保存本轮内容。松鹤项目，我今天很焦虑');await observe(api,'private','建议不保存','assistant');
 let s=new Store(api.storage);assert.equal(s.data.records.length,0);assert.equal(s.data.assistantTurns.length,0);s.close();
 await observe(api,'one','松鹤项目，我今天很焦虑');await observe(api,'one','先写下一个卡点','assistant');
 s=new Store(api.storage);s.forget('松鹤',now);s.save();s.close();
 const learn=await assistantAgent({phase:'native.learn',turnId:'one',heard:'松鹤项目，我今天很焦虑',now},api);assert.equal(learn.action,'finish');
 s=new Store(api.storage);assert.equal(s.data.records.length,0);assert.equal(s.data.assistantTurns.length,0);s.close();
});
test('split native project, affect and request survive new-call recall without chat history',async t=>{
 const api=fixture(t),callId='call-split';
 for(const [turnId,text] of [['p','松鹤项目的评审延期了'],['e','我今天很焦虑'],['r','请给一个小建议并自己生成编号'],['f','只聊天不交办工作']])await assistantAgent({phase:'native.observe',role:'user',callId,turnId,text,now},api);
 await assistantAgent({phase:'native.observe',role:'assistant',callId,turnId:'f',evidenceTurnIds:['p','e','r','f'],text:'先写下一个卡点。编号：731864',now},api);
 const continuation=await assistantAgent({phase:'native.recall',callId,heard:'当时你给的建议和编号是什么',now},api);assert.match(continuation.instructions,/731864/);
 await assistantAgent({phase:'native.end',callId,now},api);
 const r=await assistantAgent({phase:'native.recall',callId:'fresh',heard:'松鹤项目上一次通话给我的建议和编号是什么，当时什么情绪',now},api);
 assert.match(r.instructions,/731864/);assert.match(r.instructions,/explicit-user-affect/);
 const s=new Store(api.storage),turn=s.data.assistantTurns[0];assert.deepEqual(turn.emotionAssessment.labels,['anxiety']);assert.equal(turn.sourceIds.length,4);assert.deepEqual(s.recordTopics(s.index.get(turn.sourceIds[0])),['松鹤']);assert.equal(s.data.nativeCalls.length,0);s.close();
});
test('false missing-memory answer is replaced only by scoped, exact advice evidence',()=>{
 const heard='松鹤项目上一次通话的建议和编号是什么';
 const context={noStore:true,inferenceEvidence:{topicSelection:{resolved:['松鹤'],ambiguous:false},assistantTurns:[{text:'先整理卡点。编号：731864',sourceStatement:{id:'a',text:'松鹤项目'},supportingStatements:[{id:'e',text:'我今天很焦虑'}],emotionAssessment:{evidenceSourceId:'e'}}]}};
 const reply=groundedRecallReply('没有可查的记录，未知',context,heard);assert.match(reply,/731864/);assert.match(reply,/当时的原话/);assert.match(reply,/不代表你现在/);
 assert.equal(groundedRecallReply('未知',{inferenceEvidence:{assistantTurns:[]}},heard),'未知');
 assert.equal(groundedRecallReply('未知',context,'不要再提建议编号'),'未知');
});
test('native project switches, explicit boundaries and new sockets do not inherit prior affect',async t=>{
 const api=fixture(t);let turn=0;
 const say=(text,callId='one',at=now)=>assistantAgent({phase:'native.observe',role:'user',callId,turnId:String(++turn),text,now:at},api);
 await say('松鹤项目延期了');await say('我今天很焦虑');await say('竹海项目进展顺利');const calm=await say('我现在很平静');
 assert.doesNotMatch(calm.instructions,/松鹤/);
 const fresh=await say('我今天很难过','another');assert.doesNotMatch(fresh.instructions,/竹海|松鹤/);
 await say('换个话题');const reset=await say('我现在很难过');assert.doesNotMatch(reset.instructions,/竹海|松鹤/);
 await say('松鹤项目');const expired=await say('我今天很焦虑','one','2026-10-25T03:03:00Z');assert.doesNotMatch(expired.instructions,/松鹤/);
});
test('native no-store boundary is sticky across ASR fragments and survives agent process boundaries',async t=>{
 const api=fixture(t);await assistantAgent({phase:'native.observe',callId:'one',turnId:'a',role:'user',text:'不要保存本轮内容',now},api);
 await assistantAgent({phase:'native.observe',callId:'one',turnId:'b',role:'user',text:'松鹤项目我今天很焦虑',now},api);
 await assistantAgent({phase:'native.observe',callId:'one',turnId:'b',role:'assistant',text:'不保存的回复',now},api);
 const s=new Store(api.storage);assert.equal(s.data.records.length,0);assert.equal(s.data.assistantTurns.length,0);s.close();
});
test('emotion wording guard softens hypotheses but preserves self-report, quotes, code and numbers',()=>{
 const text='换谁都会觉得泄气。你肯定很焦虑。我完全懂你的感受。编号：731864';
 const fixed=calibrateEmotionReply(text,{basis:'inferred'});assert.doesNotMatch(fixed,/换谁都会|你肯定|完全懂/);assert.match(fixed,/可能/);assert.match(fixed,/731864/);
 assert.equal(calibrateEmotionReply(text,{basis:'explicit'}),text);
 const quoted='记录原话：“你肯定很焦虑”。`731864`';assert.equal(calibrateEmotionReply(quoted,{basis:'unknown'}),quoted);
});
test('accepted split affect is retrievable after hangup even without a completed assistant reply',async t=>{
 const api=fixture(t);
 await assistantAgent({phase:'native.observe',callId:'early',turnId:'p',role:'user',text:'松鹤项目评审延期',now},api);
 await assistantAgent({phase:'native.observe',callId:'early',turnId:'e',role:'user',text:'我今天很焦虑',now},api);
 await assistantAgent({phase:'native.end',callId:'early',now},api);
 const r=await assistantAgent({phase:'native.recall',callId:'new',heard:'松鹤项目上一次通话我说的情绪是什么',now},api);
 assert.match(r.instructions,/explicit-user-affect/);
});
test('out-of-order ASR cannot inherit or overwrite the newer project scope',async t=>{
 const api=fixture(t),callId='ordered';
 const say=(turnId,text,isolatedFragment=false)=>assistantAgent({phase:'native.observe',role:'user',callId,turnId,text,isolatedFragment,now},api);
 await say('a','松鹤项目延期');await say('b','竹海项目进展顺利');const late=await say('late','我今天很焦虑',true);assert.doesNotMatch(late.instructions,/竹海|松鹤/);
 const next=await say('next','我现在很平静');assert.match(next.instructions,/竹海/);assert.doesNotMatch(next.instructions,/松鹤/);
});
