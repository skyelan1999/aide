'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),os=require('node:os'),crypto=require('node:crypto');
const {Store}=require('./store.cjs'),{projectTopics}=require('./cognition.cjs');
const now='2026-10-25T03:00:00Z';
function fixture(t){const directory=fs.mkdtempSync(path.join(os.tmpdir(),'mix-topic-'));t.after(()=>fs.rmSync(directory,{recursive:true,force:true}));return {directory,key:crypto.randomBytes(32).toString('base64'),scope:'a'.repeat(32)}}
function anchor(s){const c=s.begin('a','MIXPROBE-'+ 'a'.repeat(32)+'青铜项目，我今天很焦虑。请给个建议并自行生成六位编号。',now);s.rememberReply(c,'建议：只整理一个卡点。\n编号：482917',now);return c;}
test('shared markers and generic project concepts cannot cross named-project boundaries',t=>{
 const s=new Store(fixture(t)),a=anchor(s);s.add('decision','青铜项目先整理卡点',now,{sourceIds:[a.sourceId]});
 const b=s.context('MIXPROBE-'+ 'a'.repeat(32)+'紫檀项目现在不知道怎么办',now);assert.equal(b.inferenceEvidence.assistantTurns.length,0);assert.ok(!b.references.some(x=>x.id===a.sourceId||x.sourceIds.includes(a.sourceId)));s.close();
});
test('same-project evidence strips one-turn presentation, explicit original recall preserves it',t=>{
 const s=new Store(fixture(t));anchor(s);
 const normalContext=s.context('青铜项目下一步怎么办',now);assert.ok(!JSON.stringify(normalContext.references).includes('六位编号'));const ordinary=normalContext.inferenceEvidence;assert.equal(ordinary.assistantTurns.length,1);assert.match(ordinary.assistantTurns[0].text,/整理一个卡点/);assert.ok(!JSON.stringify(ordinary.assistantTurns).includes('482917'));assert.ok(!ordinary.assistantTurns[0].sourceStatement.text.includes('六位编号'));
 const explicit=s.context('青铜项目，你刚才的建议和编号是什么',now).inferenceEvidence;assert.ok(explicit.assistantTurns[0].text.includes('482917'));s.close();
});
test('vague recall with several known projects requests clarification, never the latest arbitrary row',t=>{
 const s=new Store(fixture(t));anchor(s);const b=s.begin('b','紫檀项目需要讨论进度',now);s.rememberReply(b,'先确认里程碑',now);const c=s.context('你刚才给的建议是什么',now).inferenceEvidence;assert.equal(c.topicSelection.ambiguous,true);assert.equal(c.assistantTurns.length,0);s.close();
});
test('host history is scoped as user/assistant pairs and old numbering is historical data',t=>{
 const s=new Store(fixture(t));anchor(s);const h=[{role:'user',content:'青铜项目，请给建议并附编号'},{role:'assistant',content:'只整理卡点。编号：482917'},{role:'user',content:'紫檀项目想讨论进展'},{role:'assistant',content:'先确认里程碑'}];const selected=s.history(h,'紫檀项目怎么办');assert.equal(selected.length,2);assert.ok(!JSON.stringify(selected).includes('482917'));
 const same=s.history(h,'青铜项目你刚才建议了什么');assert.ok(!JSON.stringify(same).includes('482917'));s.close();
});
test('deleting an evidence dependency also clears later assistant turns that used it',t=>{
 const s=new Store(fixture(t)),a=anchor(s),c=s.begin('c','青铜项目你刚才建议了什么',now);s.rememberReply(c,'整理卡点，482917',now);assert.equal(s.data.assistantTurns.length,2);s.management({operation:'delete',recordId:a.sourceId},now);assert.equal(s.data.assistantTurns.length,0);s.close();
});
test('unscoped stable preferences remain available while project-scoped ones stay local',t=>{
 const s=new Store(fixture(t)),a=anchor(s);const global=s.add('preference','希望回答简洁',now);s.add('preference','青铜项目优先使用蓝色',now,{sourceIds:[a.sourceId]});const b=s.context('紫檀项目，回答方式有什么偏好',now);assert.ok(b.references.some(r=>r.id===global.id));assert.ok(!b.references.some(r=>r.text.includes('青铜')));s.close();
});
test('project parsing supports exact named and quoted labels, never fuzzy aliases',()=>{
 assert.deepEqual(projectTopics('你刚才对青铜项目建议了什么'),['青铜']);assert.deepEqual(projectTopics('回想以前的合成天青项目'),['天青']);assert.deepEqual(projectTopics('这个项目'),[]);assert.deepEqual(projectTopics('项目“夜航”'),['夜航']);
});
