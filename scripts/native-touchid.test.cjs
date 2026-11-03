'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync('internal/server/web/app.js', 'utf8');
const first = source.indexOf('function b64uToBuf(');
const last = source.indexOf("$('lock-form').onsubmit", first);
assert.ok(first >= 0 && last > first);
const nodes = Object.fromEntries(['touchid-unlock-btn','touchid-hint','lock-error'].map(id => [id, {
  hidden:false, disabled:false, textContent:'', classList:{toggle(){}}, setAttribute(){}
}]));
const fixture = {id:'fixture',rawId:'Zml4dHVyZQ',type:'public-key',response:{signature:'signed-fixture'}};
const start = {challenge:'Zml4dHVyZQ',rpId:'localhost',allowCredentials:[{type:'public-key',id:'Zml4dHVyZQ'}]};
const calls = [];
let dismissed = 0;
const context = {
  window:{}, location:{hostname:'localhost'}, state:{config:{webAuthnReady:true,hasPlatformCredential:true},token:'fixture-only'},
  lockScreen:{unlocking:false}, navigator:{credentials:{}}, Uint8Array, atob, btoa, t:x=>x, $:id=>nodes[id],
  api:async path=>{calls.push(path);return path==='/lock-state'?{gen:7}:start;},
  fetch:async (path,options)=>{calls.push({path,body:JSON.parse(options.body)});return {ok:true,json:async()=>({lockState:{locked:false,gen:7}})};},
  dismissAfterUnlock:async(scope,verified)=>{assert.equal(scope.gen,7);assert.equal(verified.locked,false);dismissed++;},
};
vm.createContext(context);
vm.runInContext(source.slice(first,last),context);
(async()=>{
  let nativeGet = 0, nativeCreate = 0;
  context.window.AideNativeBiometrics = {
    status:async()=>({available:true,credentialIds:[]}),
    get:async options=>{assert.equal(options,start);nativeGet++;return fixture;},
    create:async options=>{assert.equal(options,start);nativeCreate++;return fixture;},
  };
  await context.updateTouchIdButton();
  assert.equal(nodes['touchid-unlock-btn'].hidden,false);
  assert.equal(nodes['touchid-unlock-btn'].disabled,true);
  assert.match(nodes['touchid-hint'].textContent,/账户/);
  context.window.AideNativeBiometrics.status = async()=>({available:true,credentialIds:['fixture']});
  await context.updateTouchIdButton();
  assert.equal(nodes['touchid-unlock-btn'].disabled,false);
  assert.equal(await context.createPlatformCredential(start),fixture);
  assert.equal(nativeCreate,1);
  await context.unlockByTouchId();
  assert.equal(nativeGet,1);assert.equal(dismissed,1);
  const finish = calls.find(x=>x.path);
  assert.match(finish.path,/lockGen=7&unlockScreen=1/);
  assert.deepEqual(finish.body,fixture);
  calls.length=0;
  context.window.AideNativeBiometrics.get = async()=>{throw Object.assign(new Error('cancelled'),{name:'NotAllowedError'});};
  await context.unlockByTouchId();
  assert.equal(dismissed,1);assert.equal(calls.some(x=>x.path),false);
  assert.equal(context.lockScreen.unlocking,false);
  context.window.AideNativeBiometrics.get = async()=>fixture;
  context.fetch = async()=>({ok:false,json:async()=>({error:'signature rejected'})});
  await context.unlockByTouchId();
  assert.equal(dismissed,1);assert.equal(nodes['lock-error'].textContent,'signature rejected');
  context.window.AideNativeBiometrics.platform='windows';
  await context.updateTouchIdButton();
  assert.equal(nodes['touchid-unlock-btn'].textContent,'Windows Hello 解锁');
  context.window.AideNativeBiometrics.status=async()=>({available:true,credentialIds:[]});
  await context.updateTouchIdButton();
  assert.match(nodes['touchid-hint'].textContent,/Windows Hello/);
  context.location.hostname='foreign.test';
  assert.equal((await context.touchIdAvailable()).ok,false);
  context.location.hostname='localhost';context.state.config.webAuthnReady=false;
  assert.equal((await context.touchIdAvailable()).ok,false);
  context.state.config.webAuthnReady=true;
  delete context.window.AideNativeBiometrics;
  context.window.PublicKeyCredential=context.PublicKeyCredential={isUserVerifyingPlatformAuthenticatorAvailable:async()=>true};
  assert.equal((await context.touchIdAvailable()).ok,true);
  let browserCalls=0;
  context.navigator.credentials.get=async options=>{assert.ok(options.publicKey.challenge instanceof ArrayBuffer);browserCalls++;return {
    id:'browser',type:'public-key',rawId:new Uint8Array([1]).buffer,
    response:{clientDataJSON:new Uint8Array([2]).buffer,authenticatorData:new Uint8Array([3]).buffer,signature:new Uint8Array([4]).buffer,userHandle:null}
  };};
  assert.equal((await context.requestPlatformAssertion(start)).response.signature,'BA');
  assert.equal(browserCalls,1);
  context.navigator.credentials.get=async()=>null;
  assert.equal(await context.requestPlatformAssertion(start),null);
  console.log('PASS native routing, enrollment status, cancellation, server rejection, lock generation, browser fallback');
})().catch(error=>{console.error(error);process.exitCode=1;});
