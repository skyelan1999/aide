'use strict';
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
class Element {
  constructor(tag){this.tagName=tag;this.children=[];this.textContent='';this.attrs={};this.dataset={};this.flags=new Set();this.classList={toggle:(k,on)=>on?this.flags.add(k):this.flags.delete(k),add:k=>this.flags.add(k)};}
  append(...children){for(const c of children){this.children.push(c);if(c&&typeof c==='object')c.parentNode=this;}}
  replaceChildren(...children){this.children=[];this.append(...children);}
  setAttribute(k,v){this.attrs[k]=v;}
}
const scope={window:{},document:{createElement:tag=>new Element(tag)},Date,Error};
vm.runInNewContext(fs.readFileSync('internal/server/web/message-notifications.js','utf8'),scope);
const n=scope.window.AideMessageNotifications;
const clone=x=>JSON.parse(JSON.stringify(x));
const config={revision:4,settings:{badge:'number',categories:{chats:true,groups:true,contacts:true,files:true,reminders:true},reminderScope:'all',popup:true,sound:false,desktop:false,includeMuted:false},conversations:{}};
const all=e=>[e,...e.children.filter(x=>typeof x==='object').flatMap(all)];
const event={preventDefault(){}};
(async()=>{
  const now=Date.now(),key='a'.repeat(64);
  const c=clone(config);c.conversations[key]={mode:'mute',until:new Date(now+1000).toISOString()};
  assert.equal(n.mode(c,key,now),'mute');assert.equal(n.mode(c,key,now+1000),'default');
  assert.equal(n.mode(c,'other',now),'default');c.conversations[key].until='invalid';assert.equal(n.mode(c,key,now),'default');
  c.conversations[key]={mode:'hidden'};assert.equal(n.channels(c,key,'chats',now).popup,undefined);
  c.conversations[key]={mode:'mute'};c.settings.includeMuted=true;assert.equal(n.channels(c,key,'chats',now).popup,undefined,'including muted counts must not restore alerts');
  delete c.conversations[key];assert.equal(n.channels(c,key,'chats',now).popup,true);c.settings.categories.chats=false;assert.equal(n.channels(c,key,'chats',now).popup,true,'badge category switch must not silently change alert channels');
  const burst=Array.from({length:500},(_,i)=>({key,id:String(i),kind:'chats'}));assert.equal(n.batches(config,burst).length,1);assert.equal(n.batches(config,burst)[0].count,500);assert.equal(n.batches(config,burst,key).length,0);const quiet=clone(config);quiet.conversations[key]={mode:'mute'};assert.equal(n.batches(quiet,burst).length,0);
  const state={visible:true,active:true,locked:false,bottom:true,connected:true};assert.equal(n.canRead(state),true);
  for(const k of ['visible','active','bottom','connected'])assert.equal(n.canRead({...state,[k]:false}),false,k);
  assert.equal(n.canRead({...state,locked:true}),false,'locked view must not acknowledge');
  const badge=new Element('span');n.badge(badge,150,'number','150 unread');assert.equal(badge.textContent,'99+');assert.equal(badge.attrs['aria-label'],'150 unread');assert.equal(badge.dataset.count,'150');
  n.badge(badge,3,'dot','3 unread',true);assert.equal(badge.textContent,'');assert.equal(badge.flags.has('is-muted'),true);assert.equal(badge.flags.has('is-dot'),true);
  n.badge(badge,3,'off');assert.equal(badge.flags.has('hidden'),true);n.badge(badge,0);assert.equal(badge.flags.has('hidden'),true);
  const host=new Element('div');let saved=null;
  const form=n.settingsForm({host,config:clone(config),tr:x=>x,labelFor:k=>k,save:async value=>{saved=clone(value);}});
  const selects=all(form).filter(e=>e.tagName==='select');selects[0].value='off';selects[1].value='due';
  await form.onsubmit(event);assert.equal(saved.settings.badge,'off');assert.equal(saved.settings.reminderScope,'due');assert.equal(saved.settings.categories.groups,true);assert.equal(saved.settings.sound,false);
  const details=n.ruleControl({key,config:clone(config),tr:x=>x,save:async value=>{saved=clone(value);}});
  const ruleForm=all(details).find(e=>e.tagName==='form');const controls=all(ruleForm).filter(e=>e.tagName==='select');controls[0].value='mute';controls[1].value='1';
  await ruleForm.onsubmit(event);assert.equal(saved.key,key);assert.equal(saved.mode,'mute');assert.ok(Date.parse(saved.until)>now+3500000&&Date.parse(saved.until)<Date.now()+3610000);
  const failed=n.ruleControl({key,config:clone(config),tr:x=>x,save:async()=>{throw new Error('conflict');}});await all(failed).find(e=>e.tagName==='form').onsubmit(event);assert.ok(all(failed).some(e=>e.attrs.role==='alert'&&e.textContent==='conflict'));
  let permissionCalls=0,persistCalls=0;scope.window.Notification={requestPermission:async()=>{permissionCalls++;return'denied';}};
  const denied=n.settingsForm({host,config:clone(config),tr:x=>x,labelFor:k=>k,save:async()=>{persistCalls++;}});
  const inputs=all(denied).filter(e=>e.tagName==='input');inputs[inputs.length-1].checked=true;
  assert.equal(permissionCalls,0,'mount must not request permission');await denied.onsubmit(event);assert.equal(permissionCalls,1);assert.equal(persistCalls,0);assert.equal(inputs[inputs.length-1].checked,false);
  console.log('PASS notification modes, expiry, global channel precedence, read visibility gates, accessible badges, settings/rule saves and denied permission/error handling');
})().catch(error=>{console.error(error);process.exitCode=1;});
