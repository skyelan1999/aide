const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
class Element {
 constructor(tag='div'){this.tag=tag;this.children=[];this.attrs={};this.events={};this.open=true;this._text='';this._value=undefined;this.classList={toggle(){}};}
 set textContent(v){this._text=String(v);this.children=[];}get textContent(){return this._text+this.children.map(n=>n.textContent).join('');}
 append(...items){this.children.push(...items);}replaceChildren(...items){this._text='';this.children=[...items];}
 setAttribute(k,v){this.attrs[k]=v;}addEventListener(k,fn){this.events[k]=fn;}
 get value(){return this._value??this.children[0]?.value??'';}set value(v){this._value=v;}
 get selectedOptions(){return this.children.filter(c=>c.value===this.value);}
 querySelector(selector){return walk(this).find(n=>selector==='button[type="submit"]'&&n.tag==='button'&&n.type==='submit')||null;}
 requestSubmit(){this.pending=this.onsubmit?.({preventDefault(){}});return this.pending;}
 close(){this.open=false;this.events.close?.();}remove(){}
}
function walk(n){return n.children.flatMap(c=>[c,...walk(c)]);}
const history=fs.readFileSync('internal/server/web/markdown-history.js','utf8');
function setup({hasSnapshots=false,active=true}={}){
 const requests=[],dialog=new Element('dialog'),snapshotPanel=new Element(),editor={value:'unsaved <tag>\nsecond'},scope={active};
 const c={window:{},document:{createElement:tag=>new Element(tag),createTextNode:text=>{const e=new Element();e.textContent=text;return e;}},URLSearchParams,AbortController,TextEncoder,Uint32Array,requestAnimationFrame:fn=>fn()};vm.createContext(c);
 vm.runInContext(history.slice(history.indexOf(' function lineDiff('),history.indexOf(' function bind(')),c);
 vm.runInContext(fs.readFileSync('internal/server/web/git-history.js','utf8'),c);
 const api=(path,options)=>new Promise((resolve,reject)=>requests.push({path,options,resolve,reject}));
 c.window.AideGitHistory.mount({dialog,snapshotPanel,hasSnapshots,spec:{path:'src/文件 [x].txt',source:'local-one',root:'workspace'},editor,api,t:(s,...args)=>s.replace(/\{(\d+)\}/g,(_,n)=>args[n]),isActive:()=>scope.active,onTabChange(){},lineDiff:c.lineDiff,renderDiff:c.renderDiff,syncComparison:c.syncComparison});
 return {requests,dialog,snapshotPanel,editor,scope,c};
}
const meta={available:true,head:'a'.repeat(40),versions:[{id:'b'.repeat(40),short:'bbbbbbb',subject:'<img src=x>',created:'2026-10-14T00:00:00Z'}]};
const result={available:true,before:{exists:true,content:'saved\nsecond'},after:{exists:true,content:'disk\nsecond'},diff:'-saved\n+disk'};
const flush=()=>new Promise(setImmediate);
(async()=>{
 const f=setup();f.requests[0].resolve(meta);await flush();assert.equal(f.requests.length,2,'text-file Git view compares automatically');
 assert.equal(new URLSearchParams(f.requests[1].path.split('?')[1]).get('source'),'local-one');assert.equal(new URLSearchParams(f.requests[1].path.split('?')[1]).get('to'),'worktree');
 f.requests[1].resolve(result);await flush();const pres=walk(f.dialog).filter(n=>n.tag==='pre');assert.match(pres[1].textContent,/unsaved <tag>/,'draft pane never substitutes disk text');assert.ok(!walk(f.dialog).some(n=>n.tag==='summary'),'disk patch is not mislabeled as draft');
 const selects=walk(f.dialog).filter(n=>n.tag==='select'),form=walk(f.dialog).find(n=>n.tag==='form');selects[1].value='worktree';selects[1].onchange();form.requestSubmit();
 const tabs=f.dialog.children[0];tabs.children[0].onclick();assert.equal(f.requests.at(-1).options.signal.aborted,true,'switching source cancels pending comparison');tabs.children[1].onclick();assert.equal(form.children.at(-1).disabled,false,'returning to Git can compare again');
 f.requests.at(-1).resolve({...result,after:{exists:true,content:'stale'}});await flush();assert.ok(!walk(f.dialog).some(n=>n.tag==='pre'),'late response cannot repaint after tab change');
 form.requestSubmit();f.requests.at(-1).resolve(result);await flush();assert.ok(walk(f.dialog).some(n=>n.tag==='summary'&&n.textContent==='Unified diff'));assert.match(walk(f.dialog).filter(n=>n.tag==='pre')[1].textContent,/disk/);
 f.scope.active=false;const count=f.requests.length;form.requestSubmit();assert.equal(f.requests.length,count,'no new compare for stale context');
 const stale=setup();stale.scope.active=false;stale.requests[0].resolve(meta);await flush();assert.ok(!walk(stale.dialog).some(n=>n.tag==='form'),'late metadata cannot cross contexts');
 const absent=setup({hasSnapshots:true});absent.requests[0].resolve({available:false,reason:'No Git'});await flush();assert.equal(absent.dialog.children[0].hidden,true);assert.equal(absent.snapshotPanel.hidden,false,'existing snapshots survive no Git');
 const closed=setup();closed.dialog.close();closed.requests[0].resolve(meta);await flush();assert.equal(closed.requests.length,1,'closed dialog does not compare');
 const large=setup();large.editor.value='x\n'.repeat(7000);large.requests[0].resolve(meta);await flush();large.requests[1].resolve(result);await flush();assert.match(large.dialog.textContent,/不逐行着色/);assert.equal(walk(large.dialog).filter(n=>n.tag==='pre')[1].children.length,0,'large file is not expanded to thousands of row nodes');
 // The normal text-file history entry appears only after a scoped Git probe.
 const probeRequests=[];const button=new Element('button');let spec={path:'code.go',wsId:'A',root:'workspace'};
 const c={window:{AideGitHistory:{detect:()=>new Promise(resolve=>probeRequests.push(resolve))}},URLSearchParams};vm.createContext(c);vm.runInContext(history,c);const binding=c.window.AideMarkdownHistory.bind({button,editor:{value:''},getContext:()=>spec,api(){},t:x=>x,error(){}});binding.refresh();assert.equal(button.hidden,true);spec={...spec,wsId:'B'};binding.refresh();probeRequests[0]({available:true});await flush();assert.equal(button.hidden,true,'stale probe cannot reveal wrong workspace entry');probeRequests[1]({available:true});await flush();assert.equal(button.hidden,false);
 console.log('Git history UI: draft/disk distinction, scopes, cancellation, stale replies, existing snapshots, large text and generic text-file entry passed');
})().catch(e=>{console.error(e);process.exitCode=1;});
