'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
class Node {
 constructor(tag){this.tagName=tag.toUpperCase();this.classList={contains:()=>false};this.children=[];this.attrs={};this.hidden=false;this.open=false;this.textContent='';}
 append(...nodes){this.children.push(...nodes);}replaceChildren(...nodes){this.children=nodes;}
 setAttribute(k,v){this.attrs[k]=v;}showModal(){this.open=true;}close(){this.open=false;}focus(){}
}
const flush=async()=>{for(let i=0;i<12;i++)await Promise.resolve();};
function fixture({previous=null,storageDenied=false,navigationType='reload'}={}){
 const events={},documentEvents={},timers=new Map(),local=new Map(),session=new Map(),requests=[],copied=[];let next=0,locked=false,health={status:'ok',service:'aide',instanceId:'one'},failure=false,observer;
 if(previous)session.set('aide.crash.tab.v1',JSON.stringify(previous));
 const storage=map=>({getItem:k=>{if(storageDenied)throw Error('denied');return map.get(k)||null;},setItem:(k,v)=>{if(storageDenied)throw Error('denied');map.set(k,v);}});
 const body=new Node('body'),document={body:null,hidden:false,readyState:'loading',createElement:t=>new Node(t),querySelector:()=>locked?{}:null,querySelectorAll:()=>[new Node('div')],addEventListener:(k,f)=>documentEvents[k]=f};
 const window={performance:{getEntriesByType:()=>[{type:navigationType}]},crypto:{randomUUID:()=>`test-event-${++next}`},addEventListener:(k,f)=>events[k]=f};
 const location={href:'https://localhost:9999/',origin:'https://localhost:9999'};
 const navigator={onLine:true,clipboard:{writeText:async text=>copied.push(text)}};
 vm.runInNewContext(fs.readFileSync('internal/server/web/crash-monitor.js','utf8'),{window,document,location,navigator,localStorage:storage(local),sessionStorage:storage(session),URL,Date,Math,JSON,Map,Number,String,AbortController,MutationObserver:class{constructor(f){observer=f;}observe(){}},setTimeout:(fn,ms)=>{const id=++next;timers.set(id,{fn,ms});return id;},clearTimeout:id=>timers.delete(id),fetch:async(path,options)=>{requests.push({path,options});if(failure)throw Error('unreachable');return {ok:true,json:async()=>health};}});
 const find=(id,node=body)=>node.id===id?node:node.children.map(c=>find(id,c)).find(Boolean);
 const metadata=()=>JSON.parse(local.get('aide.crash.metadata.v1')||'[]');
 const runTimer=async ms=>{for(const [id,t]of [...timers])if(t.ms===ms){timers.delete(id);t.fn();}await flush();};
 return {window,document,events,documentEvents,body,local,session,requests,copied,find,metadata,navigator,ready:async()=>{document.body=body;documentEvents.DOMContentLoaded();await flush();},runTimer,set health(v){health=v;},set failure(v){failure=v;},set locked(v){locked=v;observer?.();},error:(message='boom',line=12)=>events.error({target:window,message,filename:'https://localhost:9999/app.js?access_token=private',lineno:line,colno:3,error:{stack:'Error: '+message+'\n at https://localhost:9999/app.js?access_token=private:12:3'}})};
}
(async()=>{
 const f=fixture();f.error('Bearer secret-value api_key=sk-private-secret <img src=x>');
 assert.equal(f.metadata().length,1,'early bootstrap error is recorded before DOM ready');
 assert.equal(f.find('aide-crash-notice'),undefined);
 const serialized=f.local.get('aide.crash.metadata.v1');assert.ok(!serialized.includes('message')&&!serialized.includes('stack')&&!serialized.includes('secret')&&!serialized.includes('access_token'));
 await f.ready();assert.equal(f.find('aide-crash-notice').hidden,false);f.window.AideCrash.open();
 const dialog=f.find('aide-crash-dialog'),detail=dialog.children[2];assert.equal(dialog.open,true);assert.match(detail.textContent,/AIDE-WEB-RUNTIME/);assert.ok(detail.textContent.includes('https://localhost:9999/app.js:12:3'),'URL secret removal must preserve stack line and column');assert.match(detail.textContent,/\[redacted\]/);assert.ok(!detail.textContent.includes('secret-value')&&!detail.textContent.includes('sk-private')&&!detail.textContent.includes('access_token='));assert.ok(detail.textContent.includes('<img src=x>'),'exception is plain text, never inserted as markup');
 await dialog.children[4].onclick();assert.equal(f.copied[0],detail.textContent);
 f.error('Bearer secret-value api_key=sk-private-secret <img src=x>');assert.equal(f.metadata().length,1);assert.equal(f.metadata()[0].count,2);
 f.events.unhandledrejection({reason:{name:'AbortError',message:'expected cancel'}});assert.equal(f.metadata().length,1);
 f.events.error({target:{tagName:'IMG',src:'https://localhost:9999/missing.png'}});assert.equal(f.metadata().length,1);
 f.events.error({target:{tagName:'SCRIPT',src:'https://localhost:9999/settings-init.js'}});assert.equal(f.metadata().at(-1).code,'AIDE-WEB-ASSET');
 f.events.error({target:{tagName:'LINK',rel:'stylesheet',href:'https://localhost:9999/workbench.css'}});assert.equal(f.metadata().at(-1).file,'workbench.css');
 f.events.unhandledrejection({reason:new Error('unhandled work')});assert.equal(f.metadata().at(-1).code,'AIDE-WEB-PROMISE');assert.equal(f.metadata().at(-1).file,'','unknown source must not become an invented undefined filename');
 for(let i=0;i<30;i++)f.error('error '+i,i+1);assert.equal(f.metadata().length,12,'reports are bounded');assert.equal(f.body.children.filter(n=>n.id==='aide-crash-notice').length,1);assert.equal(f.body.children.filter(n=>n.id==='aide-crash-dialog').length,1);
 f.window.AideCrash.open();f.locked=true;assert.equal(dialog.open,false);assert.equal(f.find('aide-crash-notice').hidden,true);f.locked=false;assert.equal(f.find('aide-crash-notice').hidden,false);
 const typed=fixture();await typed.ready();const panic={code:'AIDE-SERVER-PANIC',incidentId:'a'.repeat(32)};typed.window.AideCrash.reportServer(panic);typed.events.unhandledrejection({reason:{message:'unhandled typed response',data:panic}});assert.equal(typed.metadata().length,1,'one server incident must not turn into a second promise incident');
 const h=fixture();await h.ready();h.failure=true;await h.runTimer(30000);await h.runTimer(30000);assert.equal(h.metadata().length,0,'one or two failed probes do not claim a crash');await h.runTimer(30000);assert.equal(h.metadata().at(-1).code,'AIDE-SERVICE-UNREACHABLE');await h.runTimer(30000);assert.equal(h.metadata().length,1,'ongoing outage does not add repeated notices');
 h.failure=false;await h.runTimer(30000);h.window.AideCrash.open();assert.match(h.find('aide-crash-dialog').children[2].textContent,/服务连接已恢复/);
 h.health={status:'ok',service:'aide',instanceId:'two'};await h.runTimer(30000);assert.equal(h.metadata().at(-1).code,'AIDE-SERVICE-RESTARTED');await h.runTimer(30000);assert.equal(h.metadata().length,2);
 h.document.hidden=true;h.documentEvents.visibilitychange();const before=h.requests.length;await h.runTimer(30000);assert.equal(h.requests.length,before,'hidden tabs do not probe');
 h.document.hidden=false;h.navigator.onLine=false;h.failure=true;h.documentEvents.visibilitychange();await flush();for(let i=0;i<4;i++)await h.runTimer(30000);assert.equal(h.metadata().length,2,'browser offline is not a server crash');
 assert.ok(h.requests.every(r=>r.path==='/healthz'&&r.options.credentials==='omit'&&!r.options.headers),'health probes send no credentials or model request');
 h.events.pagehide();assert.equal(JSON.parse(h.session.get('aide.crash.tab.v1')).active,false);const old=h.requests.length;await h.runTimer(30000);assert.equal(h.requests.length,old);
 const unclean=fixture({previous:{active:true,at:'2026-10-16T00:00:00Z'}});assert.equal(unclean.metadata()[0].code,'AIDE-WEB-UNCLEAN');assert.equal(unclean.metadata()[0].message,undefined);
 const empty=fixture();await empty.ready();empty.window.AideCrash.open();assert.equal(empty.find('aide-crash-dialog').open,true);assert.equal(empty.find('aide-crash-dialog').children[4].disabled,true);
 const cloned=fixture({previous:{active:true,at:'2026-10-16T00:00:00Z'},navigationType:'navigate'});assert.equal(cloned.metadata().length,0,'new tab inherited storage must not be reported as an unclean exit');
 const denied=fixture({storageDenied:true});await denied.ready();denied.error();assert.equal(denied.find('aide-crash-notice').hidden,false,'storage failure does not disable in-memory diagnostics');
 console.log('PASS early errors, safe metadata, redacted copy, bounded single UI, abort/image filtering, lock protection, outage threshold/recovery/instance change and background/offline lifecycle');
})().catch(e=>{console.error(e);process.exitCode=1;});
