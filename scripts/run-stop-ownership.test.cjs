const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const src=fs.readFileSync('internal/server/web/app.js','utf8'),start=src.indexOf('const stopRequests=new Set();'),end=src.indexOf('// ── 全局提醒中心',start);
assert(start>=0&&end>start);
const calls=[],toasts=[],ctx={state:{session:{id:'A',runs:[{id:'rA',status:'running'}]},config:{workspaceId:'one'}},sessionSeq:{value:1},lockScreen:{locked:false},recoveryScope:()=>ctx.state.config.workspaceId,t:s=>s,toast:s=>toasts.push(s),setSendMode(){},refreshSessionSoon:()=>ctx.refreshes++,schedulePoll:()=>ctx.polls++,refreshes:0,polls:0,api:path=>new Promise((resolve,reject)=>calls.push({path,resolve,reject}))};vm.createContext(ctx);vm.runInContext(src.slice(start,end),ctx);
(async()=>{
 const a=ctx.state.session,p=ctx.stopRunById('rA');await ctx.stopRunById('rA');assert.equal(calls.length,1,'pending stop must be single flight');
 ctx.state.session={id:'B',runs:[{id:'rB',status:'running'}]};calls[0].resolve({ok:true});await p;assert.equal(toasts.length,0);assert.equal(ctx.refreshes,0,'late stop acknowledgement must not refresh another chat');assert.equal(calls[0].path,'/sessions/A/runs/rA/cancel');
 ctx.state.session=a;const q=ctx.stopRunById('rA');calls[1].reject(Error('offline'));await q;assert.equal(toasts.at(-1),'offline','stop failures must be visible');
 const retry=ctx.stopRunById('rA');calls[2].resolve({ok:true});await retry;assert.equal(toasts.at(-1),'已请求停止');assert.equal(ctx.refreshes,1);assert.equal(ctx.polls,1,'successful stop reconciles server state even if SSE is lost');assert.equal(a.runs[0].status,'running','ack alone must not invent a terminal result');
 const scoped=ctx.stopRunById('rA');ctx.state.config.workspaceId='two';calls[3].reject(Error('old workspace'));await scoped;assert.equal(toasts.length,2);
 ctx.state.config.workspaceId='one';ctx.state.pendingSessionId='B';await ctx.stopRunById('rA');assert.equal(calls.length,4);ctx.state.pendingSessionId='';ctx.lockScreen.locked=true;await ctx.stopRunById('rA');assert.equal(calls.length,4);
 console.log('Run stop ownership PASS: single flight, captured target, navigation/workspace/lock guards, visible failures, immediate reconciliation, no invented terminal');
})().catch(e=>{console.error(e);process.exitCode=1;});
