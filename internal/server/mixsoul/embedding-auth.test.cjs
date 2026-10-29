'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict');
const {embedding}=require('./embedding-client.cjs');
test('local embedding bearer is bounded, sent only to literal loopback, and never in payload',async t=>{
 const http=require('node:http'),token='a'.repeat(64);let calls=0;
 const server=http.createServer((req,res)=>{calls++;assert.equal(req.headers.authorization,'Bearer '+token);let raw='';req.on('data',x=>raw+=x);req.on('end',()=>{assert.ok(!raw.includes(token));res.end(JSON.stringify({data:[{index:0,embedding:[1,0]}]}));});});
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));t.after(()=>new Promise(resolve=>server.close(resolve)));
 const endpoint='http://127.0.0.1:'+server.address().port+'/v1/embeddings';
 assert.deepEqual(await embedding(endpoint,'fixture',['synthetic'],token),[[1,0]]);
 await assert.rejects(()=>embedding(endpoint,'fixture',['synthetic'],'bad\r\nvalue'));
 await assert.rejects(()=>embedding('http://host.docker.internal/','fixture',['synthetic'],token));
 assert.equal(calls,1);
});
test('MIXSoul exposes followup cleanup only when the host provides it',async t=>{
 const fs=require('node:fs'),os=require('node:os'),path=require('node:path'),crypto=require('node:crypto'),{assistantAgent}=require('./agent.cjs');
 const directory=fs.mkdtempSync(path.join(os.tmpdir(),'mix-follow-'));t.after(()=>fs.rmSync(directory,{recursive:true,force:true}));const api={storage:{directory,key:crypto.randomBytes(32).toString('base64'),scope:'c'.repeat(32)},settings:{emotionEnabled:false}};
 const out=await assistantAgent({phase:'begin',heard:'清理全部跟进',turnId:'test',hostTools:['clear_followups','run_shell'],history:[]},api);
 assert.ok(out.tools.includes('clear_followups'));assert.ok(!out.tools.includes('run_shell'));
});
