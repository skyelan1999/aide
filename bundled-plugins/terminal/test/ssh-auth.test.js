'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),os=require('node:os'),path=require('node:path'),{spawn}=require('node:child_process'),{generateKeyPairSync}=require('node:crypto');
const {Server,utils}=require('ssh2'),{Sessions}=require('../sessions');
const delay=ms=>new Promise(r=>setTimeout(r,ms));
async function until(fn,label){for(let i=0;i<250;i++){if(await fn())return;await delay(25);}throw Error('Timeout '+label);}
async function output(m,c,id,pattern){let text='';await until(()=>{text=Buffer.from(m.poll(c,{id}).output.data,'base64').toString();return pattern.test(text);},pattern);return text;}
function pair(passphrase){return generateKeyPairSync('rsa',{modulusLength:2048,privateKeyEncoding:{type:'pkcs1',format:'pem',...(passphrase?{cipher:'aes-256-cbc',passphrase}:{})},publicKeyEncoding:{type:'pkcs1',format:'pem'}}).privateKey;}
async function device(t,key,passphrase){
 const pub=utils.parseKey(key,passphrase),host=pair(),clients=new Set(),state={auth:0,commands:[]};
 const server=new Server({hostKeys:[host]},client=>{clients.add(client);client.on('error',()=>{});client.on('close',()=>clients.delete(client));client.on('authentication',ctx=>{if(ctx.method==='publickey'&&ctx.username==='device'&&ctx.key.data.equals(pub.getPublicSSH())&&(!ctx.signature||pub.verify(ctx.blob,ctx.signature,ctx.hashAlgo))){if(ctx.signature)state.auth++;ctx.accept();}else ctx.reject(['publickey']);});client.on('ready',()=>client.on('session',accept=>{const session=accept();session.on('pty',accept=>accept());session.on('shell',accept=>{const stream=accept();stream.write('KEY_AUTH_READY\r\n');stream.on('data',b=>stream.write('ECHO:'+b));});session.on('exec',(accept,reject,info)=>{state.commands.push(info.command);const stream=accept();stream.write('WORKSPACE_READY\r\n');stream.on('data',b=>stream.write('REMOTE:'+b));});}));});
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));t.after(()=>{for(const c of clients)c.end();server.close();});return {port:server.address().port,host,state};
}
for(const encrypted of [false,true])test('real OpenSSH memory-backed '+(encrypted?'encrypted key and interactive passphrase':'reference key')+' authenticates without persisting key bytes',{timeout:20000},async t=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'aide-terminal-key-')),m=new Sessions(),secret='test-passphrase-'+Date.now(),key=pair(encrypted?secret:undefined),d=await device(t,key,encrypted?secret:undefined),context={storageRoot:root,mode:'local',identityMaterial:Buffer.from(key).toString('base64')};t.after(async()=>{await m.stop();fs.rmSync(root,{recursive:true,force:true});});
 const rec=(await m.create(context,{kind:'ssh',host:'127.0.0.1',port:d.port,username:'device',auth:'key',identitySource:'device-keys',identityFile:'board.pem'})).session;
 await output(m,context,rec.id,/authenticity|fingerprint/);m.send(context,{id:rec.id,data:'yes\r'});
 if(encrypted){await output(m,context,rec.id,/passphrase/);m.send(context,{id:rec.id,data:secret+'\r'});}
 await output(m,context,rec.id,/KEY_AUTH_READY/);assert.equal(d.state.auth,1);m.send(context,{id:rec.id,data:'hello\r'});await output(m,context,rec.id,/ECHO:hello/);
 const dir=path.join(root,'.aide/terminal-workbench');for(const name of fs.readdirSync(dir)){const b=fs.readFileSync(path.join(dir,name));assert(!b.includes(Buffer.from(key)));assert(!b.includes(Buffer.from(secret)));assert(!b.includes(Buffer.from(context.identityMaterial)));}assert.equal(rec.identitySource,'device-keys');assert.equal(rec.auth,'key');
});
test('SSH workspace borrows authenticated master, quotes remote cwd, retains logs and fails closed after master exit',{timeout:20000},async t=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'aide-terminal-remote-')),key=pair(),d=await device(t,key),m=new Sessions();let master;
 t.after(async()=>{await m.stop();master?.kill();fs.rmSync(root,{recursive:true,force:true});});
 const keyFile=path.join(root,'identity'),known=path.join(root,'known_hosts'),sock=path.join(root,'ssh.sock');fs.writeFileSync(keyFile,key,{mode:0o600});fs.writeFileSync(known,'[127.0.0.1]:'+d.port+' '+utils.parseKey(d.host).getPublicSSH().toString('base64').replace(/^/,'ssh-rsa ')+'\n',{mode:0o600});
 master=spawn('ssh',['-F','/dev/null','-M','-N','-S',sock,'-o','StrictHostKeyChecking=yes','-o','UserKnownHostsFile='+known,'-o','IdentitiesOnly=yes','-i',keyFile,'-p',String(d.port),'-l','device','127.0.0.1'],{stdio:'ignore'});await until(()=>fs.existsSync(sock),'SSH master');
 const context={storageRoot:root,mode:'ssh',remote:{host:'127.0.0.1',port:d.port,username:'device',path:"/remote/project's files",socket:sock}};
 const rec=(await m.create(context,{name:'Workspace',cwd:'src'})).session;await output(m,context,rec.id,/WORKSPACE_READY/);assert.equal(rec.kind,'workspace');assert.equal(d.state.auth,1,'no second authentication');assert.equal(d.state.commands[0],"cd -- '/remote/project'\"'\"'s files/src' && exec /bin/bash --noprofile --norc -i");m.send(context,{id:rec.id,data:'status\r'});await output(m,context,rec.id,/REMOTE:status/);assert(m.logs(context,{id:rec.id}).files.length);assert.equal(m.list(context).workspace.mode,'ssh');
 await assert.rejects(m.create(context,{cwd:'../escape'}),/目录/);master.kill();await until(()=>!fs.existsSync(sock),'master exit');const failed=(await m.create(context,{})).session;await until(()=>m.list(context).sessions.find(r=>r.id===failed.id).status!=='running','closed without fallback');assert.equal(d.state.auth,1,'missing master must not authenticate again');
});
