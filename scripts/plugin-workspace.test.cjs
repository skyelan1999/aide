'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),os=require('node:os'),path=require('node:path'),cp=require('node:child_process');
const host=fs.readFileSync(path.join(__dirname,'../internal/server/plugin_host.js'),'utf8');
test('plugin binary chunks are bounded, workspace-scoped and preserve exact bytes',()=>{
 const temp=fs.mkdtempSync(path.join(os.tmpdir(),'aide-plugin-chunks-'));
 try{
  const plugins=path.join(temp,'plugins'),plugin=path.join(plugins,'fixture'),project=path.join(temp,'project');fs.mkdirSync(plugin,{recursive:true});fs.mkdirSync(project);
  fs.writeFileSync(path.join(project,'bytes.bin'),Buffer.from([0,255,65,0,128]));fs.writeFileSync(path.join(project,'empty.bin'),Buffer.alloc(0));fs.writeFileSync(path.join(temp,'outside.bin'),Buffer.from([42]));fs.symlinkSync(path.join(temp,'outside.bin'),path.join(project,'escape.bin'));
  fs.writeFileSync(path.join(plugin,'manifest.json'),JSON.stringify({id:'fixture',main:'index.js'}));
  fs.writeFileSync(path.join(plugin,'index.js'),`module.exports={apply(ctx){ctx.tool({name:'chunk',handler:(args,api)=>({capabilities:api.capabilities,chunk:api.readFileChunk(args.path,args.options)})});}}`);
  function run(workspace,file,options){const out=path.join(temp,'out.json'),req={plugin:'fixture',tool:'chunk',args:{path:file,options},workspace,enabledPlugins:[{id:'fixture',main:'index.js'}]};cp.execFileSync(process.execPath,['-e',host,'call',plugins,JSON.stringify(req),out]);return JSON.parse(fs.readFileSync(out,'utf8'));}
  const first=run(project,'bytes.bin',{offset:0,length:2});assert.equal(first.ok,true);assert(first.result.capabilities.includes('plugin-file-chunks-v1'));assert.deepEqual(Buffer.from(first.result.chunk.dataBase64,'base64'),Buffer.from([0,255]));assert.deepEqual([first.result.chunk.offset,first.result.chunk.nextOffset,first.result.chunk.size,first.result.chunk.eof],[0,2,5,false]);
  const last=run(project,'bytes.bin',{offset:2,length:4});assert.deepEqual(Buffer.from(last.result.chunk.dataBase64,'base64'),Buffer.from([65,0,128]));assert.deepEqual([last.result.chunk.nextOffset,last.result.chunk.eof],[5,true]);
  assert.deepEqual(run(project,'empty.bin').result.chunk,{dataBase64:'',offset:0,nextOffset:0,size:0,eof:true});
  for(const [root,file,options] of [[project,'../outside.bin',{}],[project,'escape.bin',{}],[project,'.',{}],[project,'bytes.bin',{offset:-1}],[project,'bytes.bin',{offset:6}],[project,'bytes.bin',{length:524289}],[project,'bytes.bin',{length:0}],['','bytes.bin',{}]])assert.equal(run(root,file,options).ok,false);
 }finally{fs.rmSync(temp,{recursive:true,force:true});}
});
test('plugin helper uses bound project, rejects traversal and symlink escape, and denies empty remote root',()=>{
 const temp=fs.mkdtempSync(path.join(os.tmpdir(),'aide-plugin-root-'));try{
 const plugins=path.join(temp,'plugins'),plugin=path.join(plugins,'fixture'),project=path.join(temp,'project');fs.mkdirSync(plugin,{recursive:true});fs.mkdirSync(project);fs.writeFileSync(path.join(project,'file.txt'),'PROJECT');fs.writeFileSync(path.join(temp,'outside.txt'),'PRIVATE');fs.symlinkSync(path.join(temp,'outside.txt'),path.join(project,'escape.txt'));
 fs.writeFileSync(path.join(plugin,'manifest.json'),JSON.stringify({id:'fixture',main:'index.js'}));fs.writeFileSync(path.join(plugin,'index.js'),`module.exports={apply(ctx){ctx.tool({name:'probe',handler:(args,api)=>api.readFile(args.path)});}}`);
 function run(workspace,p){const out=path.join(temp,'out.json'),req={plugin:'fixture',tool:'probe',args:{path:p},workspace,enabledPlugins:[{id:'fixture',main:'index.js'}]};cp.execFileSync(process.execPath,['-e',host,'call',plugins,JSON.stringify(req),out]);return JSON.parse(fs.readFileSync(out,'utf8'));}
 assert.equal(run(project,'file.txt').result,'PROJECT');assert.equal(run(project,'../outside.txt').ok,false);assert.equal(run(project,'escape.txt').ok,false);assert.equal(run('','file.txt').ok,false);
 }finally{fs.rmSync(temp,{recursive:true,force:true});}
});
test('daemon helper binds each overlapping call root independently without global chdir',async()=>{
 const temp=fs.mkdtempSync(path.join(os.tmpdir(),'aide-daemon-root-'));let child;
 try{
  const plugin=path.join(temp,'index.js'),a=path.join(temp,'a'),b=path.join(temp,'b');fs.mkdirSync(a);fs.mkdirSync(b);fs.writeFileSync(path.join(a,'file.txt'),'A');fs.writeFileSync(path.join(b,'file.txt'),'B');
  fs.writeFileSync(plugin,`module.exports={apply(ctx){ctx.tool({name:'probe',handler:async(args,api)=>{await new Promise(r=>setTimeout(r,args.delay));return {text:api.readFile('file.txt'),root:api.workspaceRoot};}});}}`);
  child=cp.spawn(process.execPath,['-e',host,'daemon',plugin],{stdio:['pipe','pipe','pipe']});child.stderr.resume();
  const responses=await new Promise((resolve,reject)=>{const got={},timer=setTimeout(()=>reject(Error('daemon timeout')),5000),rl=require('node:readline').createInterface({input:child.stdout});child.once('error',reject);rl.on('line',line=>{let msg;try{msg=JSON.parse(line);}catch{return;}
   if(msg.params?.subtype==='ready')for(const [id,workspace,delay]of [[1,a,100],[2,b,0]])child.stdin.write(JSON.stringify({jsonrpc:'2.0',id,method:'tool.call',params:{tool:'probe',args:{delay},workspace}})+'\n');
   if(msg.id){got[msg.id]=msg;if(got[1]&&got[2]){clearTimeout(timer);rl.close();resolve(got);}}
  });});
  assert.equal(responses[1].result.text,'A');assert.equal(responses[2].result.text,'B');assert.equal(responses[1].result.root,a);assert.equal(responses[2].result.root,b);
 }finally{if(child){child.stdin.end();child.kill();}fs.rmSync(temp,{recursive:true,force:true});}
});
