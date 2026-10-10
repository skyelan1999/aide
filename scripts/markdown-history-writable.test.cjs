const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/markdown-history.js','utf8');
const start=source.indexOf('confirm.onclick=async()=>'),end=source.indexOf('\n       }catch(e)',start);
assert.ok(start>=0&&end>start);
async function run({failed=false,changed=false}={}){
 let observed,closed=false;const editor={readOnly:false,value:'body'},spec={path:'notes.md',source:'sftp',wsId:'A'};
 const c=vm.createContext({confirm:{},checkbox:{},editor,spec,blocked:()=>false,isDirty:()=>false,currentText:'body',getContext:()=>changed?{...spec,wsId:'B'}:spec,t:x=>x,api:async()=>{assert.equal(editor.readOnly,true);if(failed)throw Error('network');return {content:'restored'}},dialog:{open:true,close(){closed=true}},onRestored(){observed=editor.readOnly},v:{id:'000001'},plan:{identity:'id',files:[]},restoreAssets:true,error(){},changes:{append(){}},document:{createElement:()=>({})}});
 vm.runInContext(source.slice(start,end),c);await c.confirm.onclick();return {editor,observed,closed};
}
async function backup({changed=false,failed=false}={}){
 const start=source.indexOf('const backupArea='),end=source.indexOf('\n    dialog.append',start);
 const elements=[],calls=[],clicks=[];let closed=false;
 const spec={path:'notes.md',source:'sftp',wsId:'A'};
 const c=vm.createContext({document:{createElement(tag){const e={tag,append(){},click(){clicks.push(this.download)}};elements.push(e);return e}},query:()=>new URLSearchParams({path:spec.path}),spec,getContext:()=>changed?{...spec,wsId:'B'}:spec,request:1,epoch:1,blocked:()=>closed,dialog:{open:true},t:x=>x,error(){},URL:{createObjectURL:()=> 'blob:backup',revokeObjectURL(){}},Blob,Uint8Array,atob,setTimeout(fn){fn()},api:async q=>{calls.push(q);if(failed)throw Error('failed');return {versions:201,objects:202,objectBytes:123,unavailableAssets:1,base64:'UEs=',filename:'notes.md.history.zip'}}});
 vm.runInContext(source.slice(start,end),c);
 const storage=elements.find(e=>e.textContent==='历史存储统计'),download=elements.find(e=>e.textContent==='下载完整历史备份');
 await storage.onclick();await download.onclick();
 assert.equal(storage.disabled,false);assert.equal(download.disabled,false);
 assert.ok(calls[0].includes('/file/history/backup?'));assert.ok(calls[1].includes('archive=1'));
 assert.equal(clicks.length,changed||failed?0:1,'failed/stale scope must not download another workspace archive');
 if(clicks.length)assert.equal(clicks[0],'notes.md.history.zip');
 closed=true;await download.onclick();assert.equal(clicks.length,changed||failed?0:1,'locked UI must not download');
}
(async()=>{
 let r=await run();assert.equal(r.observed,false,'autosave activation must observe writable editor');assert.equal(r.closed,true);assert.equal(r.editor.readOnly,false);
 r=await run({failed:true});assert.equal(r.editor.readOnly,false,'failed restoration must release temporary readonly');assert.equal(r.observed,undefined);
 r=await run({changed:true});assert.equal(r.observed,undefined,'stale scope must not activate restored editor');
 await backup();await backup({changed:true});await backup({failed:true});
 console.log('Markdown history writable callback, error cleanup and stale context PASS (source VM)');
})().catch(e=>{console.error(e);process.exitCode=1});
