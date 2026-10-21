'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
class Element {
 constructor(tag,cls='',text=''){this.tag=tag;this.className=cls;this.textContent=text;this.children=[];this.attributes={};this.value='';this.hidden=false;this.listeners={};this.open=false;}
 append(...nodes){for(const n of nodes){this.children.push(n);n.parent=this;}}
 replaceChildren(...nodes){this.children=[];this.append(...nodes);}
 setAttribute(k,v){this.attributes[k]=v;}
 getAttribute(k){return this.attributes[k];}
 addEventListener(k,v){this.listeners[k]=v;}
 showModal(){this.open=true;}
 close(){this.open=false;this.listeners.close?.();}
 remove(){if(this.parent)this.parent.children=this.parent.children.filter(x=>x!==this);}
 focus(){}
 get dataset(){return this.data??={};}
}
const flush=()=>new Promise(r=>setImmediate(r));
async function fixture(privateStatus){
 const ids=Object.fromEntries(['plugin-market','plugin-panel','plugin-mode-installed','plugin-mode-market','plugin-count'].map(id=>[id,new Element('div')]));
 const body=new Element('body'),window={},calls=[],flags={locked:false,failInstall:false,version:'1.0.0',revision:'a'.repeat(40)};
 const plugin={id:'fixture',kind:'plugin',name:'<img src=x onerror=bad()>',description:'<script>bad()</script>',version:'2.0.0',category:'creation',platforms:['macos'],minAideVersion:'0.1.17.0 RC2',license:'MIT',permissions:'Read authorized files',sha256:'b'.repeat(64),bytes:10,trustNotice:'Trusted code executes in host',readme:'<iframe src=x>',source:'https://github.com/skyelan1999/aide',compatible:true,installedVersion:'1.0.0',updateAvailable:true};
 if(privateStatus){plugin.distribution='private';plugin.commerce={status:privateStatus,priceLabel:'价格待公布',publicKeys:{}};plugin.sha256='';plugin.bytes=0;}
 const snapshot=()=>({plugins:[{...plugin,installedVersion:flags.version}],workspace:'ws-a',workspaceRevision:7,revision:flags.revision,stale:true});
 const api=async(path,o={})=>{calls.push({path,...o});if(path==='/plugin-routing'){if(o.method==='PUT'){const data=JSON.parse(o.body);flags.autoInstall=data.autoInstall;}return {autoInstall:!!flags.autoInstall,generation:1};}if(path.startsWith('/plugin-routing/search?'))return {matches:[{id:'fixture',name:'Fixture',reasons:['test'],installed:false,autoInstallable:true}],autoInstall:!!flags.autoInstall};if(path.startsWith('/plugin-market?')||path==='/plugin-market')return snapshot();if(path.endsWith('/install')){if(flags.failInstall)throw Error('Rejected fixture');flags.version='2.0.0';return {ok:true};}throw Error(path);};
 const context={window,document:{getElementById:id=>ids[id],body,activeElement:new Element('button')},setInterval,clearInterval,setTimeout,AbortController,Blob,URL,crypto:require('node:crypto').webcrypto,console};
 vm.runInNewContext(fs.readFileSync('internal/server/web/plugin-market.js','utf8'),context);
 window.AidePluginMarket.mount({api,raw:()=>{},t:s=>s,el:(...args)=>new Element(...args),refreshInstalled:async()=>{},paintInstalled:()=>{},blocked:()=>flags.locked});
 ids['plugin-mode-market'].onclick();await flush();return {window,ids,body,flags,calls,plugin};
}
const walk=e=>[e,...e.children.flatMap(walk)];
(async()=>{
 const f=await fixture(),market=f.window.AidePluginMarket;
 assert(market.versionOlder('1.2.0','1.10.0'));assert(market.versionOlder('0.1.17.0 RC2','0.1.17.0 RC10'));assert(!market.versionOlder('1.0.0','1.0.0-beta1'));
 assert.equal(market.stateOf({compatible:false}),'不兼容');assert.equal(market.stateOf({compatible:true,newerInstalled:true}),'已安装较新版本');
 assert.equal(f.ids['plugin-market'].hidden,false);assert.equal(f.ids['plugin-mode-market'].attributes['aria-selected'],'true');
 let nodes=walk(f.ids['plugin-market']);assert(nodes.some(n=>n.textContent===f.plugin.name));assert(!nodes.some(n=>(n.innerHTML||'').includes('onerror')));assert(nodes.some(n=>String(n.textContent).includes('缓存目录')));
 const toggle=nodes.find(n=>n.type==='checkbox');assert.equal(toggle.checked,false);toggle.checked=true;await toggle.onchange();assert.equal(f.flags.autoInstall,true);toggle.checked=false;await toggle.onchange();assert.equal(f.flags.autoInstall,false);
 const input=nodes.find(n=>n.type==='search');input.value='no-match';input.oninput();assert(walk(f.ids['plugin-market']).some(n=>n.textContent==='没有匹配的插件'));input.value='fixture';input.oninput();
 input.value='test task';await walk(f.ids['plugin-market']).find(n=>n.textContent==='按需求匹配').onclick();assert(walk(f.ids['plugin-market']).some(n=>String(n.textContent).includes('Fixture · test')));input.value='fixture';input.oninput();
 walk(f.ids['plugin-market']).find(n=>n.textContent==='查看详情').onclick();let modal=f.body.children.at(-1);assert(modal.open);assert(walk(modal).some(n=>n.tag==='pre'&&n.textContent==='<iframe src=x>'));
 f.flags.revision='c'.repeat(40);await market.refresh();f.flags.failInstall=true;await walk(modal).find(n=>n.className==='primary').onclick();
 const sent=f.calls.find(n=>n.path.endsWith('/install'));assert.equal(JSON.parse(sent.body).revision,'a'.repeat(40),'install must keep reviewed catalog snapshot');assert(walk(modal).some(n=>n.textContent==='Rejected fixture'));assert(!walk(modal).find(n=>n.className==='primary').disabled);
 f.flags.failInstall=false;await walk(modal).find(n=>n.className==='primary').onclick();assert.equal(modal.open,false);assert.equal(f.flags.version,'2.0.0');
 f.ids['plugin-mode-installed'].onclick();assert.equal(f.ids['plugin-market'].hidden,true);
 const paid=await fixture('coming_soon');assert.equal(paid.window.AidePluginMarket.stateOf(paid.plugin),'付费筹备中');walk(paid.ids['plugin-market']).find(n=>n.textContent==='查看详情').onclick();const paidModal=paid.body.children.at(-1);assert(walk(paidModal).some(n=>String(n.textContent).includes('尚未开放销售')));assert(!walk(paidModal).some(n=>n.textContent==='下载 ZIP'||n.textContent==='安装'||n.tag==='input'));
 const licensed=await fixture('available');walk(licensed.ids['plugin-market']).find(n=>n.textContent==='查看详情').onclick();const licensedModal=licensed.body.children.at(-1);assert.equal(walk(licensedModal).filter(n=>n.type==='file').length,2);assert(walk(licensedModal).some(n=>n.textContent==='显示设备授权码'));
 console.log('PASS marketplace numeric versions, tab/filter states, literal untrusted text, review snapshot, failure retry and successful refresh');
})().catch(e=>{console.error(e);process.exitCode=1;});
