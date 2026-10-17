'use strict';
const assert=require('node:assert/strict'),vm=require('node:vm'),fs=require('node:fs');
const source=fs.readFileSync('internal/server/web/workbench-background.js','utf8');
const settings=fs.readFileSync('internal/server/web/settings-init.js','utf8');
const flush=async()=>{for(let i=0;i<10;i++)await Promise.resolve();};
class Node {
 constructor(tag='div'){this.tagName=tag.toUpperCase();this.dataset={};this.attrs={};this.children=[];this.isConnected=true;this.files=[];this.plays=0;this.pauses=0;}
 setAttribute(k,v){this.attrs[k]=String(v);}removeAttribute(k){delete this.attrs[k];}hasAttribute(k){return k in this.attrs;}
 toggleAttribute(k,on){on?this.setAttribute(k,''):this.removeAttribute(k);}
 append(...n){this.children.push(...n);}prepend(n){this.children.unshift(n);}replaceChildren(...n){this.children=n;}
 remove(){this.isConnected=false;}play(){this.plays++;return Promise.resolve();}pause(){this.pauses++;}load(){}
 set src(v){this.setAttribute('src',v);}get src(){return this.attrs.src;}
}
function setup(initial='off'){
 const root=new Node(),body=new Node(),listeners={},subscriptions=[],urls=[],revoked=[],events={};
 root.style={setProperty:(key,value)=>root.attrs[key]=value};
 let strength=22;
 let mode=initial,asset,fail=false,locked=false;
 const mediaQuery={matches:false,addEventListener(){}};
 const document={documentElement:root,body,hidden:false,createElement:t=>new Node(t),querySelector:()=>locked?{}:null,querySelectorAll:()=>[],addEventListener:(k,f)=>listeners[k]=f};
 const database={transaction(){const tx={objectStore(){return {
  get(){const q={};queueMicrotask(()=>{q.result=asset;tx.oncomplete();});return q;},
  put(value){const q={};queueMicrotask(()=>{if(fail){tx.error=new Error('quota');tx.onabort();}else{asset=value;tx.oncomplete();}});return q;},
  delete(){const q={};queueMicrotask(()=>{asset=undefined;tx.oncomplete();});return q;}
 };}};return tx;},close(){}};
 const indexedDB={open(){const request={};queueMicrotask(()=>{request.result=database;request.onsuccess();});return request;}};
 const window={indexedDB,matchMedia:()=>mediaQuery,addEventListener:(k,f)=>events[k]=f,aideUI:{get:key=>key==='backgroundStrength'?strength:mode,set:(key,value)=>{if(key==='backgroundStrength')strength=value;else mode=value;subscriptions.forEach(f=>f());},subscribe:f=>subscriptions.push(f)}};
 const sandbox={window,document,indexedDB,URL:{createObjectURL:b=>{const u='blob:'+urls.length;urls.push(u);return u;},revokeObjectURL:u=>revoked.push(u)},MutationObserver:class{constructor(f){this.callback=f;}observe(){}},console};
 vm.runInNewContext(source,sandbox);
 return {root,body,window,document,events,listeners,urls,revoked,mediaQuery,set asset(v){asset=v;},get asset(){return asset;},set fail(v){fail=v;},set locked(v){locked=v;},setMode:v=>window.aideUI.set('background',v),layer:()=>body.children[0],render:()=>window.AideBackground.renderSettings(),update:()=>subscriptions.forEach(f=>f())};
}
(async()=>{
 let h=setup();assert.equal(h.root.dataset.background,'off');assert.equal(h.urls.length,0);assert.equal(h.body.children.length,1);
 // The bundled choice works without browser storage and does not replace the user's uploaded asset.
 const bundled=setup('whale');let bundledVideo=bundled.layer().children[0];
 assert.equal(bundledVideo.src,'/backgrounds/whale-girl.mp4');assert.equal(bundledVideo.muted,true);assert.equal(bundledVideo.loop,true);assert.equal(bundledVideo.playsInline,true);assert.equal(bundled.urls.length,0);
 bundled.asset={blob:{},name:'keep.webm',type:'video/webm'};
 bundled.document.hidden=true;bundled.listeners.visibilitychange();assert.ok(bundledVideo.pauses>0);
 bundled.document.hidden=false;bundled.locked=true;bundled.update();assert.ok(bundled.root.hasAttribute('data-background-paused'));
 bundled.locked=false;bundled.update();assert.ok(bundledVideo.plays>0);
 bundled.setMode('local');await flush();assert.equal(bundled.asset.name,'keep.webm');
 bundled.setMode('whale');assert.deepEqual(bundled.revoked,['blob:0']);bundled.setMode('off');assert.deepEqual(bundled.revoked,['blob:0'],'static URLs must not be treated as object URLs');
 assert.equal(bundled.asset.name,'keep.webm');
 bundled.setMode('whale');await bundled.render().children[3].onclick();assert.equal(bundled.asset,undefined);assert.equal(bundled.root.dataset.background,'whale','deleting an upload must not disable the built-in background');assert.equal(bundled.layer().children.length,1);
 h.setMode('aurora');assert.equal(h.root.dataset.background,'aurora');assert.equal(h.urls.length,0);
 const glows=h.layer().children.slice();assert.equal(glows.length,2);h.update();assert.deepEqual(h.layer().children,glows,'preference refresh must not accumulate animated layers');
 h.document.hidden=true;h.listeners.visibilitychange();assert.ok(h.root.hasAttribute('data-background-paused'));h.document.hidden=false;h.listeners.visibilitychange();assert.equal(h.root.hasAttribute('data-background-paused'),false);
 h.asset={blob:{},name:'clip.webm',type:'video/webm'};h.setMode('local');await flush();let video=h.layer().children[0];assert.equal(video.tagName,'VIDEO');assert.equal(video.muted,true);assert.ok(video.src);assert.ok(video.plays>0);
 const strengthPanel=h.render(),range=strengthPanel.children[6].children[1];
 range.value='40';range.oninput();await flush();assert.equal(h.root.attrs['--wb-background-strength'],'0.4');assert.equal(h.layer().children[0],video);assert.equal(h.urls.length,1);assert.equal(strengthPanel.children[6].children[2].textContent,'40%');
 h.document.hidden=true;h.listeners.visibilitychange();assert.ok(video.pauses>0);assert.ok(h.root.hasAttribute('data-background-paused'));
 h.document.hidden=false;h.locked=true;h.update();assert.ok(h.root.hasAttribute('data-background-paused'));
 h.locked=false;h.root.dataset.motion='reduced';h.events['aide:motion']();assert.ok(h.root.hasAttribute('data-background-paused'));
 h.root.dataset.motion='full';h.events['aide:motion']();assert.equal(h.root.hasAttribute('data-background-paused'),false);
 h.setMode('off');assert.equal(h.layer().children.length,0);assert.equal(video.src,undefined);assert.deepEqual(h.revoked,['blob:0']);
 // Fast switch must not attach a late IndexedDB read to another mode.
 h.setMode('local');h.setMode('aurora');await flush();assert.deepEqual(h.layer().children.map(node=>node.tagName),['SPAN','SPAN'],'late local media must not attach over aurora');
 // A video loaded while locked receives its source and can resume when unlocked.
 h.locked=true;h.setMode('local');await flush();video=h.layer().children[0];assert.ok(video.src);assert.equal(video.plays,0);
 h.locked=false;h.update();assert.ok(video.plays>0);
 // GIFs are actually unloaded when reduced motion is active.
 h.setMode('off');h.asset={blob:{},name:'motion.gif',type:'image/gif'};h.setMode('local');await flush();let image=h.layer().children[0];assert.ok(image.src);
 h.mediaQuery.matches=true;h.update();assert.equal(image.src,undefined);h.mediaQuery.matches=false;h.update();assert.ok(image.src);
 h.events.pagehide();assert.equal(h.layer().children.length,0);h.events.pageshow();await flush();assert.equal(h.layer().children.length,1);
 // Save failure retains the old media/preference; invalid/oversized types never write.
 const panel=h.render(),picker=panel.children[2].children[1];h.fail=true;const oldAsset=h.asset;
 picker.files=[{name:'new.webm',type:'video/webm',size:10}];await picker.onchange();assert.equal(h.asset,oldAsset);assert.equal(h.root.dataset.background,'local');assert.match(panel.children[4].textContent,/原素材/);
 picker.files=[{type:'image/svg+xml',size:10}];await picker.onchange();assert.equal(h.asset,oldAsset);
 picker.files=[{type:'video/mp4',size:31*1024*1024}];await picker.onchange();assert.equal(h.asset,oldAsset);
 h.fail=false;picker.files=[{name:'new.webm',type:'video/webm',size:10}];await picker.onchange();await flush();assert.equal(h.asset.name,'new.webm');assert.equal(h.layer().children.length,1);
 await panel.children[3].onclick();assert.equal(h.asset,undefined);assert.equal(h.root.dataset.background,'off');assert.equal(h.layer().children.length,0);
 panel.isConnected=false;const nextPanel=h.render();panel.children[4].textContent='detached';h.setMode('aurora');assert.equal(panel.children[4].textContent,'detached');assert.equal(nextPanel.children[1].children[1].attrs['aria-pressed'],'true');
 // Persisted settings, invalid defaults, and cross-tab normalization use aideUI.
 const attrs={},store={'aide.ui':JSON.stringify({background:'aurora',theme:'light'})},ws={localStorage:{getItem:k=>store[k]??null,setItem:(k,v)=>store[k]=v,removeItem:k=>delete store[k]},navigator:{language:'en'},matchMedia:()=>({matches:false,addEventListener(){}}),dispatchEvent(){},addEventListener:(k,f)=>ws[k]=f};
 vm.runInNewContext(settings,{window:ws,document:{documentElement:{setAttribute:(k,v)=>attrs[k]=v}},Event:class{}});
 assert.equal(ws.aideUI.get('background'),'aurora');ws.aideUI.set('background','nonsense');assert.equal(ws.aideUI.get('background'),'off');
 ws.aideUI.set('background','whale');assert.equal(ws.aideUI.get('background'),'whale');assert.equal(JSON.parse(store['aide.ui']).background,'whale');
 ws.storage({key:'aide.ui',newValue:JSON.stringify({background:'whale'})});assert.equal(ws.aideUI.get('background'),'whale');
 assert.equal(ws.aideUI.get('backgroundStrength'),22);ws.aideUI.set('backgroundStrength',90);assert.equal(ws.aideUI.get('backgroundStrength'),50);ws.aideUI.set('backgroundStrength',NaN);assert.equal(ws.aideUI.get('backgroundStrength'),22);ws.aideUI.set('backgroundStrength',-1);assert.equal(ws.aideUI.get('backgroundStrength'),10);
 ws.storage({key:'aide.ui',newValue:JSON.stringify({background:'local',backgroundStrength:36})});assert.equal(ws.aideUI.get('backgroundStrength'),36);
 ws.storage({key:'aide.ui',newValue:JSON.stringify({background:'local'})});assert.equal(ws.aideUI.get('background'),'local');ws.storage({key:'aide.ui',newValue:'{}'});assert.equal(ws.aideUI.get('background'),'off');
 console.log('PASS background lifecycle, stale reads, local save/rollback, validation, release, motion, lock and preference persistence');
})().catch(e=>{console.error(e);process.exitCode=1;});
