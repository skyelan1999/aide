const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/app.js','utf8');
const code=source.slice(source.indexOf('let assistantResultRefreshBusy=false;'),source.indexOf('function setupGlobalEvents()'));
(async()=>{
 let renders=0;const draft='正在输入的草稿';let resolve;
 const loaded={id:'assistant-a',kind:'assistant',messages:[{role:'assistant',content:'测试结果已返回'}]};
 const hooks={state:{session:{id:'assistant-a',kind:'assistant'},sessionJSON:'old'},lockScreen:{locked:false},sessionSeq:{value:1},getSessionWindow:async()=>loaded,sessionRevision:s=>JSON.stringify(s),renderSession(){renders++;},draft};
 const ctx=vm.createContext(hooks);vm.runInContext(code,ctx);
 await ctx.refreshAssistantResults();assert.equal(renders,1);assert.equal(hooks.draft,draft);assert.equal(hooks.state.session.messages.length,1);
 hooks.state.sessionJSON='before';hooks.getSessionWindow=()=>new Promise(r=>resolve=r);
 const pending=ctx.refreshAssistantResults();hooks.state.session={id:'other-project',kind:''};hooks.sessionSeq.value++;resolve(loaded);await pending;assert.equal(hooks.state.session.id,'other-project');assert.equal(renders,1);
 console.log('PASS: result refresh keeps composer draft; stale response cannot switch projects');
})().catch(e=>{console.error(e);process.exitCode=1;});
