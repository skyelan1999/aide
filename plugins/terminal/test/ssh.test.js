'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),os=require('node:os');
const {generateKeyPairSync}=require('node:crypto');
const {Server}=require('ssh2'),{Sessions}=require('../sessions');
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
async function readUntil(manager,root,id,pattern){let out='';for(let i=0;i<250;i++){out=Buffer.from(manager.poll(root,{id}).output.data,'base64').toString();if(pattern.test(out))return out;await delay(30);}throw Error('Missing '+pattern+' in '+out);}
test('real OpenSSH verifies host key, authenticates interactively and keeps credentials out of logs',async t=>{
  const root=fs.mkdtempSync(path.join(os.tmpdir(),'aide-terminal-ssh-')),manager=new Sessions(),peers=new Set();
  const hostKey=generateKeyPairSync('rsa',{modulusLength:2048,privateKeyEncoding:{type:'pkcs1',format:'pem'},publicKeyEncoding:{type:'pkcs1',format:'pem'}}).privateKey;
  const password='temporary-credential-'+Date.now();
  let auth=0,windows=0;
  const server=new Server({hostKeys:[hostKey]},client=>{
    peers.add(client);client.on('error',()=>{});client.on('close',()=>peers.delete(client));
    client.on('authentication',ctx=>{if(ctx.method==='password'&&ctx.username==='device'&&ctx.password===password){auth++;ctx.accept();}else ctx.reject(['password']);});
    client.on('ready',()=>client.on('session',accept=>{
      const session=accept();session.on('pty',accept=>accept());session.on('window-change',(accept)=>{windows++;accept?.();});
      session.on('shell',accept=>{
        const stream=accept();stream.write('DEVICE_READY\r\n$ ');let line='';
        stream.on('data',data=>{for(const char of data.toString()){
          if(char==='\r'||char==='\n'){if(line==='exit'){stream.exit(0);stream.end();}else{stream.write('\r\nDEVICE_RESULT:'+line+'\r\n$ ');line='';}}
          else if(char==='\x03'){line='';stream.write('^C\r\n$ ');}else{line+=char;stream.write(char);}
        }});
      });
    }));
  });
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  t.after(async()=>{await manager.stop();for(const client of peers)client.end();await new Promise(resolve=>server.close(resolve));fs.rmSync(root,{recursive:true,force:true});});
  const rec=(await manager.create(root,{kind:'ssh',name:'SSH device',host:'127.0.0.1',port:server.address().port,username:'device'})).session;
  await readUntil(manager,root,rec.id,/fingerprint|authenticity/);
  assert.equal(auth,0,'untrusted host cannot authenticate before confirmation');
  manager.send(root,{id:rec.id,data:'yes\r'});await readUntil(manager,root,rec.id,/password:/);
  manager.send(root,{id:rec.id,data:password+'\r'});await readUntil(manager,root,rec.id,/DEVICE_READY/);assert.equal(auth,1);
  manager.resize(root,{id:rec.id,cols:95,rows:32});manager.send(root,{id:rec.id,data:'device status\r'});
  const output=await readUntil(manager,root,rec.id,/DEVICE_RESULT:device status/);assert(!output.includes(password));
  const log=manager.logs(root,{id:rec.id,index:0});assert(!Buffer.from(log.data,'base64').includes(Buffer.from(password)));
  const meta=fs.readFileSync(path.join(root,'.aide/terminal-workbench',rec.id+'.json'),'utf8');assert(!meta.includes(password));assert(!meta.includes('password'));
  assert(fs.readFileSync(path.join(root,'.aide/terminal-workbench/known_hosts'),'utf8').includes('ssh-rsa'));assert(windows>=1);
  manager.send(root,{id:rec.id,data:'exit\r'});for(let i=0;i<60&&manager.list(root).sessions[0].status==='running';i++)await delay(30);
  assert.equal(manager.list(root).sessions[0].status,'exited');
});
