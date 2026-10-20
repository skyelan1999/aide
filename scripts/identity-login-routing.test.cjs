const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const app=fs.readFileSync('internal/server/web/app.js','utf8');
const start=app.indexOf('async function api(path,'),end=app.indexOf('\nfunction action(',start);
assert.ok(start>=0&&end>start,'extract only the API helper, independently of action options');
const source=app.slice(start,end);
(async()=>{
 for(const path of ['/auth/verify','/account/verify-password','/webauthn/assertion/start','/sessions/assistant-id/unlock-assistant','/sessions']){
  for(const invalidToken of [false,true]){
   const dialog={open:false,count:0,showModal(){this.open=true;this.count++;}};
   const state={token:'fixture-token'};
   const context={state,$:()=>dialog,t:s=>s,window:{},fetch:async()=>({ok:false,status:401,headers:{get:key=>key==='X-Aide-Auth-Required'&&invalidToken?'token':null},json:async()=>({error:'fixture verification failure'})})};
   vm.createContext(context);vm.runInContext(source,context);
   await assert.rejects(context.api(path,{method:'POST'}),e=>e.status===401);
   assert.equal(dialog.open,invalidToken||path==='/sessions',`${path}: biometric/password failures stay local; invalid access tokens open login`);
   assert.equal(state.token,'fixture-token','verification failure must preserve access token');
  }
 }
 console.log('PASS identity routing: failed reauthentication stays in its own dialog; expired access tokens still require login');
})().catch(e=>{console.error(e);process.exitCode=1;});
