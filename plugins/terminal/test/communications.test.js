'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),os=require('node:os'),net=require('node:net'),dgram=require('node:dgram');
const {Sessions}=require('../sessions'),{definitions}=require('../communications');
const delay=ms=>new Promise(r=>setTimeout(r,ms));async function until(fn,label){for(let i=0;i<200;i++){if(await fn())return;await delay(25);}throw Error('Timeout '+label);}
function fixture(t){const root=fs.mkdtempSync(path.join(os.tmpdir(),'aide-terminal-comm-')),m=new Sessions();t.after(async()=>{await m.stop();fs.rmSync(root,{recursive:true,force:true});});return {root,m};}
async function read(m,root,id,text){let output='';await until(()=>{output=Buffer.from(m.poll(root,{id}).output.data,'base64').toString();return output.includes(text);},text);return output;}
test('all 35 legacy communication tools have collision-free Terminal names',()=>{const tools=definitions();assert.equal(tools.length,35);assert.equal(new Set(tools.map(t=>t.name)).size,35);for(const tool of tools)assert(tool.name.startsWith('terminal_')&&typeof tool.handler==='function');});
test('TCP client/server sessions preserve binary TX/RX, record logs, isolate handles and close listeners',async t=>{
 const {root,m}=fixture(t);const listener=(await m.create(root,{kind:'tcp-server',port:0,name:'Listener'})).session;assert.equal(listener.host,'127.0.0.1');assert(listener.port>0);
 const client=(await m.create(root,{kind:'tcp',host:'127.0.0.1',port:listener.port,name:'Client'})).session;await until(()=>m.list(root).sessions.length===3,'accepted peer');const accepted=m.list(root).sessions.find(s=>s.id!==listener.id&&s.id!==client.id);assert.equal(accepted.host,'127.0.0.1');assert(accepted.port>0);
 await m.send(root,{id:client.id,data:'00 ff 41 0d 0a',format:'hex'});await read(m,root,accepted.id,'00 ff 41 0d 0a');await m.send(root,{id:accepted.id,data:'reply\r\n'});await read(m,root,client.id,'72 65 70 6c 79 0d 0a');assert(m.logs(root,{id:client.id}).files.length);
 const second=fs.mkdtempSync(path.join(os.tmpdir(),'aide-terminal-scope-'));t.after(()=>fs.rmSync(second,{recursive:true,force:true}));await assert.rejects(m.commCall(second,{action:'tcp_send',arguments:{connectionId:client.resource,data:'wrong'}}),/工作区/);await assert.rejects(m.send(root,{id:client.id,data:'f',format:'hex'}),/完整字节/);
 await m.close(root,{id:listener.id});await until(()=>m.list(root).sessions.find(s=>s.id===accepted.id).status==='exited','peer closed');const probe=net.createServer();await new Promise((resolve,reject)=>probe.once('error',reject).listen(listener.port,'127.0.0.1',resolve));await new Promise(r=>probe.close(r));
});
test('UDP sends and receives from the same bound socket, preserving packet boundaries and bytes',async t=>{
 const {root,m}=fixture(t),peer=dgram.createSocket('udp4');await new Promise(r=>peer.bind(0,'127.0.0.1',r));t.after(()=>peer.close());const sources=[];peer.on('message',(data,remote)=>{sources.push(remote.port);peer.send(data,remote.port,remote.address);});
 const session=(await m.create(root,{kind:'udp',host:'127.0.0.1',port:peer.address().port,bindPort:0})).session;await m.send(root,{id:session.id,data:'000102ff',format:'hex'});await read(m,root,session.id,'RX');assert.deepEqual(sources,[session.bindPort]);const output=Buffer.from(m.poll(root,{id:session.id}).output.data,'base64').toString();assert(output.includes('TX')&&output.includes('00 01 02 ff'));await m.close(root,{id:session.id});assert.equal(m.list(root).sessions[0].status,'exited');
});
test('native serial runtime opens virtual ports, applies config, transfers hex/replay and frees handles',async t=>{
 const {root,m}=fixture(t);const pair=await m.commCall(root,{action:'serial_virtual_create'});const left=(await m.create(root,{kind:'serial',devicePath:pair.masterPath,baudRate:115200})).session;const right=(await m.create(root,{kind:'serial',devicePath:pair.slavePath,baudRate:115200})).session;
 assert.equal(left.baudRate,115200);await m.send(root,{id:left.id,data:'41 00 ff',format:'hex'});await read(m,root,right.id,'41 00 ff');await m.commCall(root,{action:'serial_replay',arguments:{path:pair.masterPath,frames:[{data:'REPLAY',delay:0}],speed:1}});await read(m,root,right.id,'REPLAY');await m.close(root,{id:left.id});await m.close(root,{id:right.id});await m.commCall(root,{action:'serial_virtual_destroy',arguments:{pairId:pair.pairId}});
 await assert.rejects(m.create(root,{kind:'serial',devicePath:path.join(root,'ordinary-file')}));
});
