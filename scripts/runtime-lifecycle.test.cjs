'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
for (const refusedClose of [false, true]) {
  const events = [], listeners = {}, writes = [];
  let content = 'private conversation', disposed = 0;
  const document = {title: 'private project', documentElement: {replaceChildren() {content = ''; writes.push('erase');}}};
  const window = {addEventListener(name, fn) {listeners[name] = fn;}, dispatchEvent(event) {events.push(event.type);},
    stop() {writes.push('stop');}, close() {writes.push('close'); if (refusedClose) throw Error('user tab');},
    location: {replace(url) {assert.equal(content, '', 'erase synchronously before browser navigation');writes.push(url);}}};
  vm.runInNewContext(fs.readFileSync('internal/server/web/runtime-lifecycle.js', 'utf8'), {window, document, Event: class {constructor(type) {this.type = type;}}});
  window.AideRuntimeLifecycle.shutdown(() => {disposed++;});
  assert.equal(content, ''); assert.equal(document.title, ''); assert.equal(disposed, 1);
  assert.deepEqual(events, ['aide:runtime-stopping']);
  assert.deepEqual(writes, ['stop', 'erase', 'close', 'about:blank']);
  window.AideRuntimeLifecycle.shutdown(() => {throw Error('duplicate cleanup');});
  content = 'browser cache restored private content';listeners.pageshow();assert.equal(content, '');
}
console.log('PASS shutdown erases private content before close/navigation, survives refused close and prevents cached-page restoration');

// Exercise the production SSE registration and disposal callback as well. A
// failed optional helper must not leave the other streams/voice/token active.
for (const helperFailure of [false, true]) {
  const called = [], listeners = {}, state = {token:'synthetic', session:{id:'private'}, file:{path:'private'},
    stream:{close(){called.push('stream');if(helperFailure)throw Error('disconnected');}},
    commandAbort:{abort(){called.push('command');}}};
  const window = {AideRuntimeLifecycle:{shutdown(dispose){dispose();called.push('erase');}},
    aideAssistantCall:{end(){called.push('call');}},aideAssistantLive:{stop(){called.push('live');}}};
  const storage = new Map([['aide-token','synthetic'],['draft','preserved']]);
  const source = fs.readFileSync('internal/server/web/app.js','utf8');
  const start = source.indexOf('function setupGlobalEvents() {');
  const end = source.indexOf('\n\n/*',start);
  vm.runInNewContext('let sessionsEv=null;'+source.slice(start,end)+'\nsetupGlobalEvents();', {
    state,window,localStorage:{removeItem(key){storage.delete(key);}},
    voiceHardStop(){called.push('voice');},
    EventSource:class {constructor(url){assert.match(url,/access_token=synthetic$/);}addEventListener(name,fn){listeners[name]=fn;}close(){called.push('events');}},
  });
  listeners['runtime-stopping']();
  assert.equal(state.token,'');assert.equal(state.session,null);assert.equal(state.file,null);
  assert.equal(storage.has('aide-token'),false);assert.equal(storage.get('draft'),'preserved');
  assert.deepEqual(called,['events','stream','command','call','live','voice','erase']);
}
console.log('PASS production SSE stop callback clears authentication and cancels streams/calls/voice despite a helper failure');
