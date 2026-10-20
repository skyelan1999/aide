'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),os=require('node:os'),path=require('node:path');
const {Sessions,HISTORY,LOG_BYTES,LOG_FILES}=require('../sessions');
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
async function waitFor(manager,root,id,pattern,timeout=5000){
  const end=Date.now()+timeout;let output='';
  while(Date.now()<end){output=Buffer.from(manager.poll(root,{id,offset:0}).output.data,'base64').toString();if(pattern.test(output))return output;await delay(30);}
  throw Error('Expected output '+pattern+'; received '+output);
}
function fixture(t){const root=fs.mkdtempSync(path.join(os.tmpdir(),'aide-terminal-')),manager=new Sessions();t.after(async()=>{await manager.stop();fs.rmSync(root,{recursive:true,force:true});});return{root,manager};}
test('real PTY isolates sessions, resizes, interrupts, survives UI reconnection and persists logs',async t=>{
  const {root,manager}=fixture(t);
  const a=(await manager.create(root,{name:'设备 A'})).session,b=(await manager.create(root,{name:'设备 B'})).session;
  manager.send(root,{id:a.id,data:"export AIDE_TEST_DEVICE=A; printf '\\nDEVICE_%s\\n' \"$AIDE_TEST_DEVICE\"\r"});
  await waitFor(manager,root,a.id,/DEVICE_A/);
  manager.send(root,{id:b.id,data:"printf '\\nOTHER_%s\\n' \"${AIDE_TEST_DEVICE:-empty}\"\r"});
  await waitFor(manager,root,b.id,/OTHER_empty/);
  manager.resize(root,{id:a.id,cols:91,rows:37});manager.send(root,{id:a.id,data:'stty size\r'});
  await waitFor(manager,root,a.id,/37 91/);
  manager.send(root,{id:a.id,data:'sleep 20\r'});await delay(100);manager.send(root,{id:a.id,data:'\x03'});
  manager.send(root,{id:a.id,data:"printf '\\nINTERRUPTED_OK\\n'\r"});await waitFor(manager,root,a.id,/INTERRUPTED_OK/);
  // Poll cursors belong to the UI, not session lifetime. A new view can read from zero.
  assert.equal(manager.list(root).sessions.filter(s=>s.status==='running').length,2);
  const out=await waitFor(manager,root,a.id,/DEVICE_A/);assert(!out.includes('OTHER_empty'));
  const files=manager.logs(root,{id:a.id}).files;assert(files.length>0);
  const log=manager.logs(root,{id:a.id,index:0,offset:0});assert(Buffer.from(log.data,'base64').toString().includes('DEVICE_A'));
  const dir=path.join(root,'.aide/terminal-workbench');assert.equal(fs.statSync(dir).mode&0o777,0o700);assert.equal(fs.statSync(path.join(dir,a.id+'.log')).mode&0o777,0o600);
  assert.throws(()=>manager.remove(root,{id:a.id}),/先关闭/);
  await manager.close(root,{id:a.id});assert.equal(manager.list(root).sessions.find(s=>s.id===a.id).status,'exited');
  assert.throws(()=>manager.send(root,{id:a.id,data:'x'}),/会话已结束/);
  await manager.stop();const restored=new Sessions();t.after(()=>restored.stop());
  assert.equal(restored.list(root).sessions.length,2);assert(Buffer.from(restored.poll(root,{id:a.id}).output.data,'base64').toString().includes('DEVICE_A'));
  assert(restored.logs(root,{id:a.id}).files.length);restored.remove(root,{id:a.id});assert(!restored.list(root).sessions.some(s=>s.id===a.id));
});
test('working directory and session identities stay bound to the real workspace',async t=>{
  const {root,manager}=fixture(t),other=fs.mkdtempSync(path.join(os.tmpdir(),'aide-terminal-other-'));t.after(()=>fs.rmSync(other,{recursive:true,force:true}));
  fs.mkdirSync(path.join(root,'.aide'),{mode:0o755});fs.chmodSync(path.join(root,'.aide'),0o755);
  const a=(await manager.create(root,{})).session;assert.equal(fs.statSync(path.join(root,'.aide')).mode&0o777,0o755,'preserve existing shared metadata directory permissions');
  assert.throws(()=>manager.poll(other,{id:a.id}),/不存在/);
  await assert.rejects(manager.create(root,{cwd:'..'}),/路径/);
  fs.symlinkSync(other,path.join(root,'outside'));await assert.rejects(manager.create(root,{cwd:'outside'}),/路径越界/);
  await assert.rejects(manager.create(root,{kind:'ssh',host:'-oProxyCommand=bad',username:'user'}),/主机名/);
  await assert.rejects(manager.create(root,{kind:'ssh',host:'localhost',username:'bad\nname'}),/文本/);
  await assert.rejects(manager.create(root,{kind:'ssh',host:'localhost',username:'user',identityFile:'outside/key'}));
  assert.throws(()=>manager.resize(root,{id:a.id,cols:10000}),/数字/);
  assert.throws(()=>manager.send(root,{id:a.id,data:'x'.repeat(16385)}),/16KiB/);
  fs.symlinkSync(other,path.join(root,'.aide/terminal-workbench',a.id+'.log'));
  manager.output(manager.space(root),manager.get(manager.space(root),a.id),Buffer.from('secret'));
  assert(manager.get(manager.space(root),a.id).logError);assert.equal(fs.readdirSync(other).filter(x=>x==='secret').length,0);
});
test('disk log rotation, bounded replay, pause, snapshot identity and restart metadata',async t=>{
  const {root,manager}=fixture(t),space=manager.space(root),id='a'.repeat(24);
  const rec={id,name:'rotation',kind:'local',cwd:'.',status:'running',logging:true,createdAt:new Date().toISOString(),history:Buffer.alloc(0),offset:0,base:0,worker:null};space.records.set(id,rec);manager.persist(space,rec);
  const block=Buffer.alloc(8192,65);
  for(let i=0;i<(LOG_BYTES/8192)*(LOG_FILES+2);i++)manager.output(space,rec,block);
  const files=manager.logs(root,{id}).files;assert.equal(files.length,LOG_FILES);assert(files.every(f=>f.bytes<=LOG_BYTES));assert(rec.history.length<=HISTORY);
  const tail=manager.poll(root,{id,offset:0}).output;assert.equal(tail.reset,true);assert.equal(Buffer.from(tail.data,'base64').length,65536);
  const before=manager.logs(root,{id,index:0});manager.output(space,rec,block);
  await assert.rejects(async()=>manager.logs(root,{id,index:0,identity:before.identity}),/轮转/);
  manager.logging(root,{id,enabled:false});const size=manager.logs(root,{id,index:0}).bytes;manager.output(space,rec,Buffer.from('not logged'));assert.equal(manager.logs(root,{id,index:0}).bytes,size);
  // An interrupted process is displayed as interrupted, never reconnected automatically.
  rec.status='running';manager.persist(space,rec);const restarted=new Sessions();t.after(()=>restarted.stop());assert.equal(restarted.list(root).sessions[0].status,'interrupted');assert.equal(restarted.get(restarted.space(root),id).worker,null);
});
test('storage refuses symlinks rather than writing outside the workspace',t=>{
  const {root,manager}=fixture(t),outside=fs.mkdtempSync(path.join(os.tmpdir(),'aide-terminal-store-'));t.after(()=>fs.rmSync(outside,{recursive:true,force:true}));fs.symlinkSync(outside,path.join(root,'.aide'));assert.throws(()=>manager.list(root),/符号链接/);assert.equal(fs.readdirSync(outside).length,0);
});
test('daemon death ends the running shell job instead of leaving it detached',async t=>{
  const {spawn}=require('node:child_process'),root=fs.mkdtempSync(path.join(os.tmpdir(),'aide-terminal-death-'));
  const command='i=0; while :; do echo $((i+=1)) > .heartbeat; sleep .1; done\r';
  const code=`const {Sessions}=require(${JSON.stringify(path.resolve(__dirname,'../sessions'))});(async()=>{const m=new Sessions(),r=await m.create(process.argv[1],{});m.send(process.argv[1],{id:r.session.id,data:${JSON.stringify(command)}});console.log('READY');setInterval(()=>{},1000);})();`;
  const env={...process.env};delete env.NODE_TEST_CONTEXT;
  const node=spawn(process.execPath,['-e',code,root],{env,stdio:['pipe','pipe','pipe']});let report='',stderr='';node.stdout.on('data',chunk=>report+=chunk);node.stderr.on('data',chunk=>stderr+=chunk);
  t.after(()=>{node.kill('SIGKILL');fs.rmSync(root,{recursive:true,force:true});});
  const heartbeat=path.join(root,'.heartbeat');
  for(let i=0;i<200&&!fs.existsSync(heartbeat);i++)await delay(20);
  assert(fs.existsSync(heartbeat),'shell heartbeat started: '+stderr);
  const first=fs.readFileSync(heartbeat,'utf8');await delay(300);assert.notEqual(fs.readFileSync(heartbeat,'utf8'),first,'job is running');
  node.kill('SIGKILL');await delay(800);const ended=fs.readFileSync(heartbeat,'utf8');await delay(600);
  assert.equal(fs.readFileSync(heartbeat,'utf8'),ended,'shell job must stop when daemon pipes close');
});
