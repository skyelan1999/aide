'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {chromium}=require('./browser-runtime/node_modules/playwright');
const {base,cube}=require('./test_obj_preview_bounds.cjs');
const origin='https://aide-obj-owned.test';
const host=`<!doctype html><meta charset="utf-8"><title>Owned OBJ component fixture</title><style>body{margin:0}iframe{width:100%;height:98vh;border:0}</style><script>
window.ownedText='';window.ownedEditable=true;window.ownedFailures=0;window.ownedSaved=[];window.ownedMethods=[];
addEventListener('message',event=>{
 if(event.source!==document.querySelector('iframe')?.contentWindow||event.data?.type!=='aide.workbench.ready')return;
 window.ownedPort?.close();const channel=new MessageChannel();window.ownedPort=channel.port1;
 const bytes=new TextEncoder().encode(ownedText);let chunks=[];
 channel.port1.onmessage=event=>{const m=event.data;ownedMethods.push(m.method);let result={};
  if(m.method==='document.info')result={size:bytes.length,version:'owned-v1',editable:ownedEditable,path:'owned-fixture.obj'};
  else if(m.method==='document.read'){const b=bytes.subarray(m.arguments.offset,m.arguments.offset+524288);let binary='';for(let i=0;i<b.length;i+=8192)binary+=String.fromCharCode(...b.subarray(i,i+8192));result={data:btoa(binary)};}
  else if(m.method==='document.error')ownedFailures++;
  else if(m.method==='document.save.begin')chunks=[];
  else if(m.method==='document.save.chunk')chunks.push(Uint8Array.from(atob(m.arguments.data),c=>c.charCodeAt(0)));
  else if(m.method==='document.save.request'){const total=chunks.reduce((n,c)=>n+c.length,0),data=new Uint8Array(total);let offset=0;for(const c of chunks){data.set(c,offset);offset+=c.length;}ownedSaved.push(new TextDecoder().decode(data));}
  else if(m.method!=='document.dirty')throw Error('Unexpected fixture method '+m.method);
  channel.port1.postMessage({requestId:m.requestId,status:'ok',result});
  if(m.method==='document.save.request')setTimeout(()=>channel.port1.postMessage({event:'document.saved',result:{version:'owned-v2'}}),0);
 };
 event.source.postMessage({type:'aide.workbench.connect',protocol:1,pluginId:'mesh-file-preview',tools:[],capabilities:[]},'${origin}',[channel.port2]);
});
</script>`;
async function main(){
 const out=process.env.AIDE_COMPONENT_EVIDENCE_DIR;assert.ok(out);fs.mkdirSync(out,{recursive:true});
 const browser=await chromium.launch({executablePath:'/usr/bin/chromium',headless:true,args:['--no-sandbox','--disable-dev-shm-usage']});let context;
 const checks=[],errors=[];
 try{
  context=await browser.newContext();const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
  await context.addInitScript(()=>{if(window===parent)return;window.ownedSegments=0;const line=CanvasRenderingContext2D.prototype.lineTo;CanvasRenderingContext2D.prototype.lineTo=function(){ownedSegments++;return line.apply(this,arguments);};});
  await context.route('**/*',route=>{const u=new URL(route.request().url());if(u.origin!==origin)return route.abort();return route.fulfill({contentType:'text/html',body:u.pathname==='/'?host:fs.readFileSync('examples/file-preview-plugin/viewer.html')});});
  await page.goto(origin);
  async function open(text,editable=true){
   await page.evaluate(({text,editable})=>{ownedText=text;ownedEditable=editable;document.querySelector('iframe')?.remove();const f=document.createElement('iframe');f.src='/viewer.html';f.title='Actual generated OBJ preview';document.body.append(f);},{text,editable});
   const handle=await page.locator('iframe').elementHandle(),frame=await handle.contentFrame();await handle.dispose();return frame;
  }
  for(const [theme,width] of [['light',1440],['dark',390]]){
   await page.setViewportSize({width,height:1000});await page.emulateMedia({colorScheme:theme});
   let frame=await open(cube);await frame.getByRole('status').filter({hasText:'可编辑'}).waitFor();
   assert.ok(await frame.evaluate(()=>vertices.length>0&&edges.length>0&&ownedSegments===edges.length));
   const before=await frame.evaluate(()=>ownedSegments);
   await frame.getByRole('slider').focus();await frame.getByRole('slider').press('ArrowRight');
   assert.ok(await frame.evaluate(()=>ownedSegments)>before,'actual keyboard rotation redraws');
   await page.screenshot({path:path.join(out,theme+'-normal.png')});
   await frame.getByRole('textbox',{name:'OBJ 源文件'}).fill(base+'f '+'1 '.repeat(2049));
   await frame.getByRole('status').filter({hasText:'超过预览限制'}).waitFor();
   assert.equal(await frame.getByRole('button',{name:'准备保存修改'}).isDisabled(),true);
   assert.equal(await frame.evaluate(()=>vertices.length+edges.length),0);
   await page.screenshot({path:path.join(out,theme+'-rejected.png')});
   const edited=cube+'\n# owned component edit\n';await frame.getByRole('textbox',{name:'OBJ 源文件'}).fill(edited);
   await frame.getByRole('button',{name:'准备保存修改'}).click();await frame.getByRole('status',{exact:true}).filter({hasText:'已保存'}).waitFor();
   assert.equal(await page.evaluate(()=>ownedSaved.at(-1)),edited,'real bundled bridge serializes edited bytes to mock host');
   checks.push({case:'normal load, keyboard rotation, reject edit, clear geometry, recover edit and bridge save',theme,width,result:'PASS'});
   const failures=await page.evaluate(()=>ownedFailures);
   frame=await open(base+'l 1 2 1\n'.repeat(50000)+'l 1 2\n');
   await frame.getByRole('status').filter({hasText:'超过预览限制'}).waitFor();await page.waitForFunction(n=>ownedFailures===n+1,failures);
   assert.equal(await frame.evaluate(()=>vertices.length+edges.length),0);assert.equal(await frame.getByRole('button').isDisabled(),true);
   checks.push({case:'aggregate edge limit on initial read reports preview failure',theme,result:'PASS'});
   frame=await open(cube,false);await frame.getByRole('status').filter({hasText:'只读'}).waitFor();
   assert.equal(await frame.getByRole('textbox').getAttribute('readonly'),'');assert.equal(await frame.getByRole('button').isDisabled(),true);
   checks.push({case:'read-only bridge retains restrictions',theme,result:'PASS'});
  }
  assert.deepEqual(errors,[]);fs.writeFileSync(path.join(out,'result.json'),JSON.stringify({checks,errors,browser:browser.version(),actualGeneratedViewer:true,actualBundledClient:true,mockHost:true,fullWorkbench:false},null,2)+'\n');console.log(JSON.stringify(checks));
 }finally{if(context)await context.close();await browser.close();}
}
main().catch(e=>{console.error(e.stack);process.exitCode=1;});
