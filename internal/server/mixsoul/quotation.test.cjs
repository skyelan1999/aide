'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict');
const {nativeQuotation,assistantAgent}=require('./agent.cjs'),{presentationRecall}=require('./cognition.cjs'),{Store}=require('./store.cjs');
const fs=require('node:fs'),os=require('node:os'),path=require('node:path'),crypto=require('node:crypto');
function fixture(t){const directory=fs.mkdtempSync(path.join(os.tmpdir(),'mix-quote-'));t.after(()=>fs.rmSync(directory,{recursive:true,force:true}));return {storage:{directory,key:crypto.randomBytes(32).toString('base64'),scope:'b'.repeat(32)},settings:{}};}
const now='2026-10-25T03:00:00Z';
test('colloquial original wording chooses only a scoped unsolicited assistant quote',async t=>{
 const api=fixture(t);await assistantAgent({phase:'native.observe',role:'user',callId:'old',turnId:'one',text:'松鹤项目，我今天很焦虑',now},api);await assistantAgent({phase:'native.observe',role:'assistant',callId:'old',turnId:'one',text:'先确认新的评审窗口。',now},api);await assistantAgent({phase:'native.observe',role:'assistant',callId:'old',turnId:'one',text:'拿到新窗口后，再评估手头材料的准备节奏。',proactive:true,now},api);
 const heard='松鹤项目，上次我不再说话后，你自己主动补充的那一句原话是什么？';assert.ok(presentationRecall(heard));
 const r=await assistantAgent({phase:'native.observe',role:'user',callId:'fresh',turnId:'query',text:'不要保存本轮内容。'+heard,now},api);assert.equal(r.state.context.nativeQuotation.text,'拿到新窗口后，再评估手头材料的准备节奏。');assert.equal(r.state.context.nativeQuotation.selfReport,'我今天很焦虑');assert.match(r.instructions,/先一字不改/);
 const other=await assistantAgent({phase:'native.observe',role:'user',callId:'other',turnId:'another',text:'不要保存本轮内容。竹海项目上次你主动补充的原话是什么？',now},api);assert.equal(other.state.context.nativeQuotation,null);
 const ordinary=await assistantAgent({phase:'native.observe',role:'user',callId:'ordinary',turnId:'ordinary',text:'不要保存本轮内容。松鹤项目下一步怎么办？',now},api);assert.equal(ordinary.state.context.nativeQuotation,null);
 const s=new Store(api.storage);s.forget('松鹤');s.save();s.close();const gone=await assistantAgent({phase:'native.recall',callId:'gone',heard,now},api);assert.doesNotMatch(gone.instructions,/拿到新窗口后/);
});
test('ambiguous, sensitive or long historical quotes do not become a literal plan',()=>{
 const row={id:'a'.repeat(24),role:'assistant',notUserFact:true,text:'旧回答',sourceIds:['b'.repeat(24)],initiative:true};const c={inferenceEvidence:{topicSelection:{ambiguous:false},assistantTurns:[row]}};const q='松鹤项目以前你主动补充的原话是什么';assert.ok(nativeQuotation(c,q));
 for(const context of [{inferenceEvidence:{...c.inferenceEvidence,topicSelection:{ambiguous:true}}},{inferenceEvidence:{...c.inferenceEvidence,assistantTurns:[row,{...row,id:'c'.repeat(24)}]}},{inferenceEvidence:{...c.inferenceEvidence,assistantTurns:[{...row,text:'密码是合成材料'}]}},{inferenceEvidence:{...c.inferenceEvidence,assistantTurns:[{...row,text:'长'.repeat(121)}]}}])assert.equal(nativeQuotation(context,q),null);
});

test('ordinary recall replies do not push requested unsolicited evidence out of retrieval',async t=>{
 const api=fixture(t);await assistantAgent({phase:'native.observe',role:'user',callId:'seed',turnId:'seed',text:'松鹤项目，我今天很焦虑',now},api);await assistantAgent({phase:'native.observe',role:'assistant',callId:'seed',turnId:'seed',text:'拿到新窗口后，再评估手头材料的准备节奏。',proactive:true,now},api);
 for(let n=0;n<3;n++){await assistantAgent({phase:'native.observe',role:'user',callId:'q'+n,turnId:'q'+n,text:'松鹤项目上次主动补充的原话是什么',now},api);await assistantAgent({phase:'native.observe',role:'assistant',callId:'q'+n,turnId:'q'+n,text:'原话是拿到新窗口后，再评估手头材料的准备节奏。',now},api);}
 const r=await assistantAgent({phase:'native.observe',role:'user',callId:'fresh',turnId:'last',text:'不要保存本轮内容。松鹤项目上次主动补充的原话是什么',now},api);assert.equal(r.state.context.nativeQuotation.text,'拿到新窗口后，再评估手头材料的准备节奏。');
});

test('requesting the proactive reply and its preceding advice retains both evidence entries',async t=>{
 const api=fixture(t);await assistantAgent({phase:'native.observe',role:'user',callId:'seed',turnId:'seed',text:'松鹤项目，我今天很焦虑',now},api);for(const [text,proactive] of [['先确认评审时间。',false],['拿到时间后，再安排材料准备节奏。',true]])await assistantAgent({phase:'native.observe',role:'assistant',callId:'seed',turnId:'seed',text,proactive,now},api);
 const r=await assistantAgent({phase:'native.observe',role:'user',callId:'fresh',turnId:'query',text:'不要保存本轮内容。松鹤项目上次主动补充的原话，以及紧前那一句建议的原话是什么？',now},api);assert.equal(r.state.context.inferenceEvidence.assistantTurns.length,2);assert.equal(r.state.context.nativeQuotation,null);
});
