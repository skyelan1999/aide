const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/markdown-history.js','utf8');
const start=source.indexOf('confirm.onclick=async()=>'),end=source.indexOf('\n       }catch(e)',start);
assert.ok(start>=0&&end>start);
async function run({failed=false,changed=false,lateChanged=false,lockedAfter=false,closedAfter=false,journal=false,selectedAfter=false}={}){
 let observed,closed=false,locked=false,drift=changed,shouldFail=failed;const messages=[],errors=[],editor={readOnly:false,value:'body'},spec={path:'notes.md',source:'sftp',wsId:'A'};
 const c=vm.createContext({confirm:{},checkbox:{},editor,spec,failure:null,blocked:()=>locked,isDirty:()=>false,currentText:'body',getContext:()=>drift?{...spec,wsId:'B'}:spec,t:x=>x,api:async()=>{assert.equal(editor.readOnly,true);drift=lateChanged;locked=lockedAfter;if(selectedAfter)c.selection=2;if(closedAfter)c.dialog.open=false;if(shouldFail){shouldFail=false;const e=Error('SSH connection unavailable');if(journal)e.data={journal:'restore-test-1',files:[{path:'diagram.svg',applied:true},{path:'notes.md',applied:false}]};throw e;}return {content:'restored'}},dialog:{open:true,close(){closed=true;this.open=false}},onRestored(){observed=editor.readOnly},v:{id:'000001'},pick:1,selection:1,plan:{identity:'id',files:[]},restoreAssets:true,error(e){errors.push(e.message)},changes:{append(...nodes){messages.push(...nodes)}},document:{createElement:()=>({attributes:{},setAttribute(k,v){this.attributes[k]=v},remove(){const i=messages.indexOf(this);if(i>=0)messages.splice(i,1)}})}});
 vm.runInContext(source.slice(start,end),c);await c.confirm.onclick();return {editor,observed,closed,messages,errors,confirm:c.confirm,checkbox:c.checkbox,retry:()=>c.confirm.onclick()};
}
async function backup({changed=false,failed=false,manager=false}={}){
 const start=source.indexOf('const backupArea='),end=source.indexOf('\n    dialog.append',start);
 const elements=[],calls=[],clicks=[];let closed=false,managerOptions,rendered=0;
 const window=manager?{AideMarkdownHistoryManager:{render(options){managerOptions=options;rendered++;return {};}}}:{};
 const spec={path:'notes.md',source:'sftp',wsId:'A'};
 const c=vm.createContext({window,document:{createElement(tag){const e={tag,append(){},click(){clicks.push(this.download)}};elements.push(e);return e}},query:()=>new URLSearchParams({path:spec.path}),spec,getContext:()=>changed?{...spec,wsId:'B'}:spec,request:1,epoch:1,blocked:()=>closed,dialog:{open:true},t:x=>x,error(){},URL:{createObjectURL:()=> 'blob:backup',revokeObjectURL(){}},Blob,Uint8Array,atob,setTimeout(fn){fn()},api:async q=>{calls.push(q);if(failed)throw Error('failed');return {versions:201,objects:202,objectBytes:123,unavailableAssets:1,base64:'UEs=',filename:'notes.md.history.zip'}}});
 vm.runInContext(source.slice(start,end),c);
 assert.equal(rendered,manager?1:0);
 if(manager){assert.equal(managerOptions.spec,spec);assert.equal(managerOptions.isActive(),!changed);}
 const storage=elements.find(e=>e.textContent==='历史存储统计'),download=elements.find(e=>e.textContent==='下载完整历史备份');
 await storage.onclick();await download.onclick();
 assert.equal(storage.disabled,false);assert.equal(download.disabled,false);
 assert.ok(calls[0].includes('/file/history/backup?'));assert.ok(calls[1].includes('archive=1'));
 assert.equal(clicks.length,changed||failed?0:1,'failed/stale scope must not download another workspace archive');
 if(clicks.length)assert.equal(clicks[0],'notes.md.history.zip');
 closed=true;if(manager)assert.equal(managerOptions.isActive(),false,'locked/detached history must block management');await download.onclick();assert.equal(clicks.length,changed||failed?0:1,'locked UI must not download');
}
(async()=>{
 let r=await run();assert.equal(r.observed,false,'autosave activation must observe writable editor');assert.equal(r.closed,true);assert.equal(r.editor.readOnly,false);
 r=await run({failed:true});assert.equal(r.editor.readOnly,false,'failed restoration must release temporary readonly');assert.equal(r.observed,undefined);
 assert.equal(r.messages.length,1,'connection failure must persist inside the restore dialog without a journal');
 assert.equal(r.messages[0].attributes.role,'alert');assert.ok(r.messages[0].textContent.includes('SSH connection unavailable'));
 assert.equal(r.confirm.disabled,false);assert.equal(r.checkbox.disabled,false);await r.retry();assert.equal(r.messages.length,0,'retry clears prior failure');
 r=await run({failed:true,journal:true});assert.ok(r.messages[0].textContent.includes('restore-test-1'));assert.ok(r.messages[0].textContent.includes('diagram.svg'));assert.ok(!r.messages[0].textContent.includes('notes.md'),'unapplied files must not be reported applied');
 for(const options of [{lateChanged:true},{lockedAfter:true},{closedAfter:true},{selectedAfter:true}]){r=await run({failed:true,...options});assert.equal(r.messages.length,0,'late failures must not expose old workspace details');assert.equal(r.errors.length,0,'late failures must not leak via global toast');}

 r=await run({changed:true});assert.equal(r.observed,undefined,'stale scope must not activate restored editor');
 r=await run({selectedAfter:true});assert.equal(r.observed,undefined,'late success must not apply another selected version');assert.equal(r.closed,false);
 await backup();await backup({changed:true});await backup({failed:true});await backup({manager:true});await backup({manager:true,changed:true});
 console.log('Markdown history writable callback, error cleanup and stale context PASS (source VM)');
})().catch(e=>{console.error(e);process.exitCode=1});
