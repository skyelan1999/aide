'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs'),path=require('node:path');
function fixture(response={protocol:1,url:'/plugin-workbench/'+'a'.repeat(64)}){
 const opened=[],nodes=[],events={},focus={count:0,focus(){this.count++;}};
 function node(tag){const n={tag,contentWindow:{messages:[],postMessage(data,target,ports){this.messages.push({data,target,ports});}},children:[],attributes:{},append(...a){this.children.push(...a);},setAttribute(k,v){this.attributes[k]=v;},focus(){},click(){this.clicked=true;},remove(){this.removed=true;}};nodes.push(n);return n;}
 let themeChange;const root={dataset:{theme:'light',palette:'blue'}};
 const document={documentElement:root,activeElement:focus,createElement:node,body:node('body'),addEventListener:(k,f)=>events[k]=f};let confirmResult=true;let interval,blocked=false,scope='one',enabled=true;const errors=[];
 class MessageChannel{constructor(){const make=()=>({received:[],closed:false,start(){},close(){this.closed=true;},postMessage(value){this.other.received.push(value);return this.other.onmessage?.({data:value});}});this.port1=make();this.port2=make();this.port1.other=this.port2;this.port2.other=this.port1;}}
 const urls=new Set();const urlAPI={createObjectURL(){const u='blob:test-'+urls.size;urls.add(u);return u;},revokeObjectURL(u){urls.delete(u);}};
 class MutationObserver{constructor(fn){themeChange=fn;}observe(){}disconnect(){themeChange=null;}}
 const window={confirm:()=>confirmResult,open:(...args)=>opened.push(args),addEventListener:(k,f)=>events[k]=f,removeEventListener:k=>delete events[k]},ctx={window,document,MutationObserver,getComputedStyle:()=>({getPropertyValue:()=>root.dataset.theme==='light'?'#f2f3f5':'#202328'}),requestAnimationFrame:fn=>(fn(),1),cancelAnimationFrame:()=>{},AbortController,MessageChannel,ArrayBuffer,Blob,URLSearchParams,encodeURIComponent,URL:urlAPI,setTimeout:()=>1,clearTimeout:()=>{},setInterval:f=>(interval=f,1),clearInterval:()=>{}};vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../internal/server/web/plugin-workbench.js'),'utf8'),ctx);
 return{confirm:value=>confirmResult=value,opened,root,themeChange:()=>themeChange?.(),controller:window.AidePluginWorkbench,urls,nodes,events,focus,errors,services:{api:async()=>response,t:x=>x,toast:x=>errors.push(x),blocked:()=>blocked,scope:()=>scope,enabled:()=>enabled},tick:()=>interval(),block:()=>blocked=true,changeScope:()=>scope='two',disable:()=>enabled=false};
}
const plugin={id:'test',enabled:true,workbench:{protocol:1,title:'Test'}};
test('workbench uses sandbox without same-origin or credentials and close unloads the frame',async()=>{const f=fixture();await f.controller.open(plugin,f.services);const frame=f.nodes.find(n=>n.tag==='iframe');assert.equal(frame.attributes.sandbox,'allow-scripts allow-downloads');assert.equal(frame.attributes.allow,'fullscreen *');assert.equal(frame.attributes.allowfullscreen,'');assert.equal(frame.src,'/plugin-workbench/'+'a'.repeat(64));f.controller.close();assert.equal(frame.src,'about:blank');assert.equal(f.focus.count,1);});
test('lock, scope changes, disabled plugins and Escape close the active workbench',async()=>{for(const action of ['block','changeScope','disable']){const f=fixture();await f.controller.open(plugin,f.services);f[action]();f.tick();assert.equal(f.nodes.find(n=>n.tag==='iframe').src,'about:blank');}const f=fixture();await f.controller.open(plugin,f.services);f.events.keydown({key:'Escape',preventDefault(){}});assert.equal(f.nodes.find(n=>n.tag==='iframe').src,'about:blank');});
test('workbench refuses arbitrary routes returned by a broken API',async()=>{for(const url of ['https://evil.invalid/','/api/config','/plugin-workbench/short']){const f=fixture({protocol:1,url});await f.controller.open(plugin,f.services);assert.equal(f.errors.length,1);assert.equal(f.nodes.find(n=>n.tag==='iframe').src,'about:blank');}});

const bridgePlugin={...plugin,workbench:{...plugin.workbench,tools:['own_tool']}};
const bridgeResponse={protocol:1,url:'/plugin-workbench/'+'a'.repeat(64),bridge:{protocol:1,instanceId:'a'.repeat(64),tools:['own_tool']}};
test('MessagePort only connects to the active frame and never carries parent instance/auth',async()=>{
 const f=fixture(bridgeResponse);await f.controller.open(bridgePlugin,f.services);const frame=f.nodes.find(n=>n.tag==='iframe');f.events.message({source:{},data:{type:'aide.workbench.ready',protocol:1}});assert.equal(frame.contentWindow.messages.length,0);f.events.message({source:frame.contentWindow,data:{type:'aide.workbench.ready',protocol:1}});const message=frame.contentWindow.messages[0];assert.equal(message.data.type,'aide.workbench.connect');assert(!JSON.stringify(message.data).includes('instanceId'));const port=message.ports[0];const calls=[];f.services.api=async(url,options)=>{calls.push({url,options});return {requestId:'call1',status:'result',result:{ok:true}};};await port.postMessage({protocol:1,requestId:'bad',method:'tool.call',tool:'other'});assert.equal(calls.length,0);assert.equal(port.received.at(-1).status,'error');await port.postMessage({protocol:1,requestId:'call1',method:'tool.call',tool:'own_tool',arguments:{x:1}});assert.equal(calls.length,1);assert.equal(JSON.parse(calls[0].options.body).tool,'own_tool');assert.equal(port.received.at(-1).result.ok,true);await port.postMessage({protocol:1,requestId:'call1',method:'tool.call',tool:'own_tool'});assert.equal(calls.length,1);f.controller.close();assert.equal(port.other.closed,true);assert(port.received.some(x=>x.event==='scope.revoked'));
});
test('closing a workbench drops late tool replies and proposal approval is absent from the port',async()=>{
 const f=fixture(bridgeResponse);await f.controller.open(bridgePlugin,f.services);const frame=f.nodes.find(n=>n.tag==='iframe');f.events.message({source:frame.contentWindow,data:{type:'aide.workbench.ready',protocol:1}});const port=frame.contentWindow.messages[0].ports[0];let finish;f.services.api=async(url)=>url.endsWith('/call')?new Promise(resolve=>finish=resolve):{};
 await port.postMessage({protocol:1,requestId:'approve1',method:'approve',proposalId:'x'});assert.equal(port.received.at(-1).status,'error');const pending=port.postMessage({protocol:1,requestId:'late1',method:'tool.call',tool:'own_tool'});f.controller.close();finish({requestId:'late1',status:'result',result:'must drop'});await pending;assert(!port.received.some(x=>x.requestId==='late1'));
});

const exportMessage=(bytes=new ArrayBuffer(3),name='experiment.json',requestId='export_1')=>({type:'aide.workbench.export',protocol:1,pluginId:'test',requestId,name,bytes});
test('workbench export requires actual active frame and a separate parent save click; cleanup revokes data',async()=>{
 const f=fixture();await f.controller.open(plugin,f.services);const frame=f.nodes.find(n=>n.tag==='iframe');f.events.message({source:{},data:exportMessage()});assert.equal(f.urls.size,0);
 f.events.message({source:frame.contentWindow,data:exportMessage()});assert.equal(f.urls.size,1);assert.equal(f.nodes.filter(n=>n.tag==='a').length,0);assert.equal(frame.contentWindow.messages.at(-1).data.status,'ready');
 const save=f.nodes.find(n=>n.tag==='button'&&n.textContent?.startsWith('保存文件'));save.onclick();assert(f.nodes.find(n=>n.tag==='a').clicked);assert.equal(frame.contentWindow.messages.at(-1).data.status,'requested');f.controller.close();assert.equal(f.urls.size,0);
});
test('workbench export rejects traversal, foreign plugin, oversized data and replaces pending export; revocation prevents save',async()=>{
 const f=fixture();await f.controller.open(plugin,f.services);const frame=f.nodes.find(n=>n.tag==='iframe'),send=data=>f.events.message({source:frame.contentWindow,data});
 for(const name of ['../evil.json','a/b.json','a\\b.json','bad\n.json','.hidden'])send(exportMessage(undefined,name));send({...exportMessage(),pluginId:'other'});send(exportMessage(new ArrayBuffer(16*1024*1024+1)));assert.equal(f.urls.size,0);
 send(exportMessage());send(exportMessage(undefined,'second.json','export_2'));assert.equal(frame.contentWindow.messages.at(-1).data.status,'ready');assert.equal(f.urls.size,1);assert.equal(frame.contentWindow.messages.at(-1).data.requestId,'export_2');
 const save=f.nodes.find(n=>n.tag==='button'&&n.textContent?.startsWith('保存文件'));f.changeScope();save.onclick();assert.equal(f.nodes.filter(n=>n.tag==='a').length,0);f.tick();assert.equal(f.urls.size,0);
});

test('theme follows host only after a bound frame requests it and stops on close',async()=>{
 const f=fixture();await f.controller.open(plugin,f.services);const frame=f.nodes.find(n=>n.tag==='iframe');
 const ready={type:'aide.workbench.theme.ready',protocol:1};
 f.events.message({source:{},data:ready});assert.equal(frame.contentWindow.messages.length,0);
 f.events.message({source:frame.contentWindow,data:ready});let m=frame.contentWindow.messages.at(-1).data;
 assert.equal(m.theme,'light');assert.equal(m.colors.bg,'#f2f3f5');assert(!JSON.stringify(m).includes('token'));
 f.root.dataset.theme='dark';f.root.dataset.palette='comfort';f.themeChange();m=frame.contentWindow.messages.at(-1).data;assert.equal(m.theme,'dark');assert.equal(m.palette,'comfort');
 f.controller.close();const count=frame.contentWindow.messages.length;f.themeChange();assert.equal(frame.contentWindow.messages.length,count);
});

test('idle renewal supports more than 128 calls without exposing rotated keys or retrying tools',async()=>{
 const f=fixture({...bridgeResponse,bridge:{...bridgeResponse.bridge}});await f.controller.open(bridgePlugin,f.services);
 const frame=f.nodes.find(n=>n.tag==='iframe');f.events.message({source:frame.contentWindow,data:{type:'aide.workbench.ready',protocol:1,requestIds:'monotonic'}});const port=frame.contentWindow.messages[0].ports[0];
 let renewed=0,called=0;
 f.services.api=async(url,options)=>{if(url.endsWith('/renew')){renewed++;return {protocol:1,instanceId:'b'.repeat(64)};}if(url.endsWith('/call')){called++;if(called>100)assert(url.includes('b'.repeat(64)));return {requestId:JSON.parse(options.body).requestId,status:'result',result:{ok:true}};}return {};};
 for(let i=0;i<140;i++)await port.postMessage({protocol:1,requestId:'r_'+(i+1),method:'tool.call',tool:'own_tool'});
 await port.postMessage({protocol:1,requestId:'r_1',method:'tool.call',tool:'own_tool'});assert.equal(called,140);assert.equal(renewed,1);assert(!JSON.stringify(port.received).includes('b'.repeat(64)));
 f.controller.close();
});
test('daemon detach closes the view immediately and drains one dispatched call; revocation still aborts',async()=>{
 for(const force of [false,true]){
  const plugin={...bridgePlugin,daemon:true,workbench:{...bridgePlugin.workbench,closeBehavior:'detach'}};
  const f=fixture({...bridgeResponse,bridge:{...bridgeResponse.bridge}});await f.controller.open(plugin,f.services);const frame=f.nodes.find(n=>n.tag==='iframe');f.events.message({source:frame.contentWindow,data:{type:'aide.workbench.ready',protocol:1}});const port=frame.contentWindow.messages[0].ports[0];
  let finish,signal,deleted=0;f.services.api=(url,options)=>{if(url.endsWith('/call')){signal=options.signal;return new Promise(resolve=>finish=resolve);}deleted++;return Promise.resolve({});};
  const pending=port.postMessage({protocol:1,requestId:'pending_detach',method:'tool.call',tool:'own_tool'});
  if(force){f.block();f.tick();}else f.controller.close();
  assert.equal(frame.src,'about:blank');assert.equal(signal.aborted,force);assert.equal(deleted,force?1:0);
  finish({requestId:'pending_detach',status:'result',result:{ok:true}});await pending;await Promise.resolve();assert(signal.aborted);assert.equal(deleted,1);assert(!port.received.some(x=>x.requestId==='pending_detach'));
 }
});




test('page exit revokes its own bridge and unloads the sandbox',async()=>{const f=fixture(bridgeResponse);const calls=[];const api=f.services.api;f.services.api=async(url,options)=>{calls.push({url,options});return api(url,options);};await f.controller.open(bridgePlugin,f.services);f.events.pagehide();assert.equal(f.nodes.find(n=>n.tag==='iframe').src,'about:blank');const deleted=calls.find(c=>c.url.endsWith('/'+ 'a'.repeat(64)));assert.equal(deleted.options.method,'DELETE');assert.equal(deleted.options.keepalive,true);});

test('workbench stays embedded without new-tab controller',async()=>{const f=fixture();assert.equal(f.controller.openTab,undefined);await f.controller.open(plugin,f.services);assert(f.nodes.some(n=>n.tag==='iframe'));f.controller.close();});

test('file selection hints contain only validated source paths and no credentials',async()=>{
 const f=fixture(bridgeResponse);f.services.selection={left:{source:'workspace',path:'docs/a.txt',directory:false,token:'secret'},right:{source:'source:ref',path:'b.txt',directory:false},settings:{password:'secret'}};
 await f.controller.open(bridgePlugin,f.services);const frame=f.nodes.find(n=>n.tag==='iframe');f.events.message({source:frame.contentWindow,data:{type:'aide.workbench.ready',protocol:1}});
 const m=frame.contentWindow.messages[0].data;assert.equal(m.selection.right.source,'source:ref');assert(!JSON.stringify(m).includes('secret'));assert(!JSON.stringify(m).includes('settings'));
 for(const bad of ['../x','/etc/passwd','a\\b','x\n'])assert.equal(f.controller.selection({left:{source:'workspace',path:bad}}),undefined);
 assert.equal(f.controller.selection({left:{source:'https://example.com',path:'a'}}),undefined);f.controller.close();
});

test('file bridge rejects iframe commit; only trusted parent Save publishes and emits saved',async()=>{
 const p={id:'fixture',name:'File viewer',enabled:true,workbench:{protocol:1,title:'Files',tools:[],fileHandler:{extensions:['.obj'],editable:true}}};
 const f=fixture({protocol:1,url:'/plugin-workbench/'+'b'.repeat(64),bridge:{protocol:1,instanceId:'c'.repeat(64),tools:[],document:true}});f.services.document={path:'model.obj',root:'workspace'};f.services.handlers=[p];
 await f.controller.open(p,f.services);const frame=f.nodes.find(n=>n.tag==='iframe');f.events.message({source:frame.contentWindow,data:{type:'aide.workbench.ready',protocol:1}});const port=frame.contentWindow.messages[0].ports[0],calls=[];
 assert(frame.contentWindow.messages[0].data.capabilities.includes('document.read'));
 f.services.api=async(url,options)=>{calls.push({url,body:JSON.parse(options.body)});return{status:'result',requestId:calls.at(-1).body.requestId,result:calls.at(-1).body.method==='document.save.commit'?{saved:true,version:'new-version'}:{savePending:true}};};
 await port.postMessage({protocol:1,requestId:'attempt',method:'document.save.commit'});assert.equal(calls.length,0);assert.equal(port.received.at(-1).status,'error');
 await port.postMessage({protocol:1,requestId:'ready',method:'document.save.request',arguments:{}});assert.equal(calls.length,1);
 const save=f.nodes.find(n=>n.tag==='button'&&n.textContent==='保存到原文件');assert.equal(save.hidden,false);await save.onclick({isTrusted:false});assert.equal(calls.length,1);await save.onclick({isTrusted:true});assert.equal(calls.length,2);assert.equal(calls.at(-1).body.method,'document.save.commit');assert(port.received.some(v=>v.event==='document.saved'));assert.equal(save.hidden,true);f.controller.close();
});
test('leaving a dirty file viewer can be cancelled before removing the frame',async()=>{
 const f=fixture({...bridgeResponse,bridge:{...bridgeResponse.bridge,document:true}});f.services.document={path:'model.obj',root:'workspace'};f.services.handlers=[bridgePlugin];await f.controller.open(bridgePlugin,f.services);const frame=f.nodes.find(n=>n.tag==='iframe');f.events.message({source:frame.contentWindow,data:{type:'aide.workbench.ready',protocol:1}});const port=frame.contentWindow.messages[0].ports[0];await port.postMessage({protocol:1,requestId:'dirty',method:'document.dirty',arguments:{dirty:true}});f.confirm(false);assert.equal(f.controller.prepareFileOpen(),false);assert.notEqual(frame.src,'about:blank');f.confirm(true);assert.equal(f.controller.prepareFileOpen(),true);assert.equal(frame.src,'about:blank');
});

test('mobile collection is opt-in and cannot invoke undeclared methods or disclose parent credentials',async()=>{
 for(const enabled of [false,true]){
  const f=fixture({...bridgeResponse,bridge:{...bridgeResponse.bridge,mobileCapture:enabled}});await f.controller.open(bridgePlugin,f.services);const frame=f.nodes.find(n=>n.tag==='iframe');f.events.message({source:frame.contentWindow,data:{type:'aide.workbench.ready',protocol:1}});const handshake=frame.contentWindow.messages[0],port=handshake.ports[0],calls=[];assert.equal(handshake.data.capabilities.includes('capture'),enabled);assert(!JSON.stringify(handshake.data).includes('instanceId'));
  f.services.api=async(url,options)=>{calls.push(JSON.parse(options.body));return {requestId:'capture_1',status:'result',result:{id:'synthetic'}};};await port.postMessage({protocol:1,requestId:'capture_1',method:'capture.control',arguments:{action:'status',id:'synthetic'}});assert.equal(calls.length,enabled?1:0);await port.postMessage({protocol:1,requestId:'capture_2',method:'capture.admin'});assert.equal(calls.length,enabled?1:0);f.controller.close();
 }
});

test('employee and location capabilities require an explicit compatible capture protocol',async()=>{
 for(const version of [undefined,2,3]){const f=fixture({...bridgeResponse,bridge:{...bridgeResponse.bridge,mobileCapture:true,mobileCaptureVersion:version}});await f.controller.open(bridgePlugin,f.services);const frame=f.nodes.find(n=>n.tag==='iframe');f.events.message({source:frame.contentWindow,data:{type:'aide.workbench.ready',protocol:1}});const caps=frame.contentWindow.messages[0].data.capabilities;assert.equal(caps.includes('capture.locations'),version>=2);assert.equal(caps.includes('capture.person-binding'),version>=3);f.controller.close();}
});

test('review preserves iframe draft and bridge, returns on approved save, and still revokes on scope loss',async()=>{
 const f=fixture(bridgeResponse);let reviewed='';f.services.reviewProposal=async id=>{reviewed=id;};await f.controller.open(bridgePlugin,f.services);const frame=f.nodes.find(n=>n.tag==='iframe'),host=f.nodes.find(n=>n.tag==='section');f.events.message({source:frame.contentWindow,data:{type:'aide.workbench.ready',protocol:1}});const port=frame.contentWindow.messages[0].ports[0];f.services.api=async()=>({status:'proposal_pending',proposalId:'save-one',sessionId:'session-one'});await port.postMessage({protocol:1,requestId:'save1',method:'tool.call',tool:'own_tool'});const review=f.nodes.find(n=>n.textContent==='查看待审批提案'),back=f.nodes.find(n=>n.textContent==='返回插件工作台');await review.onclick();assert.equal(reviewed,'session-one');assert.equal(host.hidden,true);assert.equal(back.hidden,false);assert(!port.closed);assert.notEqual(frame.src,'about:blank');f.services.api=async()=>({result:{status:'done',applied:true}});await f.tick();assert.equal(host.hidden,false);assert.equal(back.hidden,true);assert(port.received.some(x=>x.event==='proposal.changed'&&x.result.applied));f.changeScope();await f.tick();assert.equal(frame.src,'about:blank');assert.equal(back.removed,true);
});

test('manual project save writes directly and never permits iframe commit',async()=>{
 const f=fixture({...bridgeResponse,bridge:{...bridgeResponse.bridge}});await f.controller.open(bridgePlugin,f.services);
 const frame=f.nodes.find(n=>n.tag==='iframe');f.events.message({source:frame.contentWindow,data:{type:'aide.workbench.ready',protocol:1}});const port=frame.contentWindow.messages[0].ports[0],calls=[];
 f.services.api=async(url,options)=>{calls.push(JSON.parse(options.body));return {hash:'saved-hash'};};
 await port.postMessage({protocol:1,requestId:'direct_commit',method:'file.save.commit',arguments:{path:'x.pp',content:'bad',hash:''}});assert.equal(calls.length,0);
 await port.postMessage({protocol:1,requestId:'manual_save',method:'file.save',arguments:{path:'x.pp',content:'good',hash:'original'}});
 assert.equal(f.nodes.some(n=>n.className==='plugin-native-save-card'),false);assert.equal(calls.length,1);assert.equal(calls[0].method,'file.save.commit');assert.equal(calls[0].arguments.hash,'original');assert.equal(port.received.at(-1).result.saved,true);assert.equal(port.received.at(-1).result.hash,'saved-hash');
 await port.postMessage({protocol:1,requestId:'manual_save',method:'file.save',arguments:{path:'x.pp',content:'replay',hash:'original'}});assert.equal(calls.length,1);
});
