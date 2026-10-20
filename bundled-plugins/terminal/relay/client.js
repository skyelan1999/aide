'use strict';
const tls=require('node:tls'),net=require('node:net'),crypto=require('node:crypto'),fs=require('node:fs'),path=require('node:path');
function keyBytes(value){let bytes;try{bytes=Buffer.from(String(value||''),'base64');}catch(_){}if(!bytes||bytes.length!==32||bytes.toString('base64')!==value)throw Error('中继密钥必须是32字节密钥的 Base64 文本，请生成或粘贴有效密钥');return bytes;}
function identity(key){const seed=crypto.createHmac('sha256',key).update('aide-terminal-relay-certificate-v1').digest();return crypto.createPublicKey(crypto.createPrivateKey({key:Buffer.concat([Buffer.from('302e020100300506032b657004220420','hex'),seed]),format:'der',type:'pkcs8'})).export({type:'spki',format:'der'});}
async function open(config,mode='open'){
 const key=keyBytes(config.key),expected=identity(key);
 return new Promise((resolve,reject)=>{
  // Self-signed relay identity is authenticated with an exact public-key pin
  // derived from the shared 256-bit key BEFORE any application data is sent.
  const socket=tls.connect({host:config.host,port:config.port,minVersion:'TLSv1.3',rejectUnauthorized:false});
  let buffer=Buffer.alloc(0),step=0,finished=false;
  const fail=error=>{if(finished)return;finished=true;clearTimeout(timer);socket.destroy();reject(error);};
  const timer=setTimeout(()=>fail(Error('中继连接或认证超时')),12000);
  socket.once('error',fail);socket.once('end',()=>fail(Error('中继连接已关闭')));
  socket.once('secureConnect',()=>{try{const peer=socket.getPeerX509Certificate(),actual=peer?.publicKey.export({type:'spki',format:'der'});if(!actual||actual.length!==expected.length||!crypto.timingSafeEqual(actual,expected))throw Error('中继身份不匹配，请检查地址与加密密钥');socket.on('data',header);}catch(error){fail(error);}});
  const header=data=>{try{buffer=Buffer.concat([buffer,data]);while(buffer.includes(10)){const at=buffer.indexOf(10);if(at>4096)throw Error('中继响应过大');const message=JSON.parse(buffer.subarray(0,at));buffer=buffer.subarray(at+1);if(step++===0){if(message.version!==1||typeof message.challenge!=='string'||!/^[A-Za-z0-9+/]{43}=$/.test(message.challenge))throw Error('无效中继握手');const proof=crypto.createHmac('sha256',key).update('aide-relay-v1:'+message.challenge+':'+mode+':'+config.id).digest('hex');socket.write(JSON.stringify({id:config.id,mode,proof})+'\n');}else{if(!message.ok||message.id!==config.id||message.kind!==config.kind)throw Error(message.error||'中继配置不匹配');finished=true;clearTimeout(timer);socket.removeListener('data',header);socket.removeListener('error',fail);socket.pause();if(buffer.length)socket.unshift(buffer);if(mode==='probe'){socket.destroy();resolve({online:true});}else resolve(socket);return;}}if(buffer.length>4096)throw Error('中继响应过大');}catch(error){fail(error);}};
 });
}
function script(config){const template=fs.readFileSync(path.join(__dirname,'agent.py'),'utf8');return template.replace('__CONFIG_JSON__',()=>JSON.stringify(JSON.stringify(config)));}
async function tunnel(config,onStatus){const active=new Set();const server=net.createServer(async local=>{active.add(local);local.on('error',()=>{});try{const remote=await open(config);active.add(remote);remote.on('error',()=>local.destroy());remote.on('close',()=>{active.delete(remote);local.destroy();});local.on('close',()=>{active.delete(local);remote.destroy();});local.pipe(remote).pipe(local);remote.resume();onStatus?.(true);}catch(_){onStatus?.(false);local.destroy();}});await new Promise((resolve,reject)=>{server.once('error',reject);server.listen(0,'127.0.0.1',resolve);});return {port:server.address().port,close:()=>{for(const socket of active)socket.destroy();server.close();}};}
module.exports={open,script,tunnel,keyBytes};
