'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const {readAppSource, sourceFunction} = require('./fixtures/app-source.cjs');

(async () => {
  const source = readAppSource();
  const core = fs.readFileSync(path.join(__dirname, '../internal/server/web/app-core.js'), 'utf8');
  assert.match(core, /historyLimit: 12/, 'new selections start with a small history window');
  const loadEarlier = sourceFunction(source, 'loadEarlierSessionHistory');
  const loadNearTop = sourceFunction(source, 'loadEarlierHistoryNearTop');
  let resolveLoad, loads = 0;
  const button = {disabled:false, attributes:{}, textContent:'', setAttribute(name,value){this.attributes[name]=value;}};
  const conversation = {scrollTop:500};
  const state = {session:{id:'long-session',hasOlder:true},historyLimit:12,historyScroll:false};
  const context = {
    state,
    SESSION_HISTORY_PAGE:12,
    sessionHistoryLoading:false,
    $:id => id === 'timeline' ? {querySelector:selector => selector === '.load-older-history' ? button : null} : conversation,
    t:value => value,
    action:fn => fn,
    selectSession:async id => { loads++; assert.equal(id,'long-session'); return new Promise(resolve => { resolveLoad=resolve; }); },
  };
  vm.createContext(context);
  vm.runInContext(`${loadEarlier}\n${loadNearTop}`, context);
  const first = context.loadEarlierSessionHistory();
  await context.loadEarlierSessionHistory();
  assert.equal(loads,1,'overlapping top sentinel and button loads share one request');
  assert.equal(state.historyLimit,24);
  assert.equal(state.historyScroll,true);
  assert.equal(button.disabled,true);
  assert.equal(button.attributes['aria-busy'],'true');
  resolveLoad();
  await first;
  assert.equal(context.sessionHistoryLoading,false);
  conversation.scrollTop = 120;
  const nearTop = context.loadEarlierHistoryNearTop();
  assert.equal(loads,2,'reaching the top automatically requests the next page');
  resolveLoad();
  await new Promise(resolve => setImmediate(resolve));
  state.session.hasOlder = false;
  conversation.scrollTop = 0;
  context.loadEarlierHistoryNearTop();
  assert.equal(loads,2,'exhausted history makes no further request');

  const conversationCSS = fs.readFileSync(path.join(__dirname, '../internal/server/web/conversation.css'), 'utf8');
  const workbenchCSS = fs.readFileSync(path.join(__dirname, '../internal/server/web/workbench.css'), 'utf8');
  assert.match(conversationCSS, /#timeline>\.run,#timeline>\.assistant-msg\{content-visibility:auto;contain-intrinsic-size:auto 720px\}/);
  assert.match(workbenchCSS, /\.session-item\{[^}]*content-visibility:auto;contain-intrinsic-size:auto 56px/);
  console.log('PASS: bounded first page, deduplicated automatic earlier-history loading and offscreen rendering containment');
})().catch(error => { console.error(error); process.exitCode=1; });
