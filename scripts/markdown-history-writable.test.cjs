const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const source=fs.readFileSync('internal/server/web/markdown-history.js','utf8');
const start=source.indexOf('confirm.onclick=async()=>'),end=source.indexOf('\n       }catch(e)',start);
assert.ok(start>=0&&end>start);
async function run({failed=false,changed=false}={}){
 let observed,closed=false;const editor={readOnly:false,value:'body'},spec={path:'notes.md',source:'sftp',wsId:'A'};
 const c=vm.createContext({confirm:{},checkbox:{},editor,spec,blocked:()=>false,isDirty:()=>false,currentText:'body',getContext:()=>changed?{...spec,wsId:'B'}:spec,t:x=>x,api:async()=>{assert.equal(editor.readOnly,true);if(failed)throw Error('network');return {content:'restored'}},dialog:{open:true,close(){closed=true}},onRestored(){observed=editor.readOnly},v:{id:'000001'},plan:{identity:'id',files:[]},restoreAssets:true,error(){},changes:{append(){}},document:{createElement:()=>({})}});
 vm.runInContext(source.slice(start,end),c);await c.confirm.onclick();return {editor,observed,closed};
}
(async()=>{
 let r=await run();assert.equal(r.observed,false,'autosave activation must observe writable editor');assert.equal(r.closed,true);assert.equal(r.editor.readOnly,false);
 r=await run({failed:true});assert.equal(r.editor.readOnly,false,'failed restoration must release temporary readonly');assert.equal(r.observed,undefined);
 r=await run({changed:true});assert.equal(r.observed,undefined,'stale scope must not activate restored editor');
 console.log('Markdown history writable callback, error cleanup and stale context PASS (source VM)');
})().catch(e=>{console.error(e);process.exitCode=1});
