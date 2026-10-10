const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const source = fs.readFileSync('internal/server/web/app.js', 'utf8');
function extract(name) {
  const match = new RegExp('^(?:async )?function ' + name + '\\(', 'm').exec(source);
  assert.ok(match, `Missing product function: ${name}`);
  const start = match.index, lineEnd = source.indexOf('\n', start);
  const end = source.slice(start, lineEnd).endsWith('}') ? lineEnd : source.indexOf('\n}', lineEnd) + 2;
  assert.ok(end > start, `Missing function boundary: ${name}`);
  return source.slice(start, end);
}
const functions = ['composerAutoReview', 'syncComposerApproval', 'refreshApprovalPolicy', 'setComposerAutoReview', 'setupGlobalEvents'].map(extract).join('\n');
let server = {enabled: false, revision: 1};
let puts = 0, failPut = false, failGet = false, holdPut = null;
function client() {
  const listeners = new Map(), notices = [], selected = [];
  let renders = 0, polls = 0;
  const state = {approvalPolicy: null, approvalModeBusy: false, session: null, token: 'fixture'};
  const context = vm.createContext({state, JSON, encodeURIComponent,
    api: async (path, options) => {
      assert.equal(path, '/approval-policy');
      if (options?.method === 'PUT') {
        puts++;
        if (holdPut) await holdPut;
        if (failPut) throw new Error('fixture save failed');
        server = {enabled: JSON.parse(options.body).enabled, revision: server.revision + 1};
      } else if (failGet) throw new Error('fixture refresh failed');
      return {...server};
    },
    refreshStrategyUI: () => renders++,
    selectSession: async id => selected.push(id), schedulePoll: () => polls++,
    toast: value => notices.push(value), t: value => value,
    scheduleSessionsRefresh: () => {},
    EventSource: class { addEventListener(name, handler) { listeners.set(name, handler); } }
  });
  vm.runInContext('let sessionsEv = null;\n' + functions, context);
  return {state, listeners, notices, selected, run: expression => vm.runInContext(expression, context), metrics: () => ({renders, polls})};
}
(async () => {
  const first = client(), second = client();
  assert.equal(first.run('composerAutoReview()'), false);
  await first.run('refreshApprovalPolicy()');
  first.run('setupGlobalEvents()'); second.run('setupGlobalEvents()');
  assert.ok(second.listeners.has('approval-policy-changed'));
  first.state.session = {id: 'one', runs: [{autoReview: false}]};
  await first.run('setComposerAutoReview(true)');
  assert.equal(first.run('composerAutoReview()'), true);
  assert.deepEqual(first.selected, ['one']);
  assert.equal(first.metrics().polls, 1);
  await second.listeners.get('approval-policy-changed')();
  assert.equal(second.run('composerAutoReview()'), true);
  for (const id of ['two', 'one', null]) {
    second.state.session = id ? {id, runs: [{autoReview: false, status: 'completed'}]} : null;
    assert.equal(second.run('composerAutoReview()'), true, 'Session/run must not override global policy');
  }
  const restarted = client();
  await restarted.run('refreshApprovalPolicy()');
  assert.equal(restarted.run('composerAutoReview()'), true);
  const before = puts;
  await first.run('setComposerAutoReview(true)');
  assert.equal(puts, before, 'Identical selection must not write');
  let release; holdPut = new Promise(resolve => { release = resolve; });
  const pending = first.run('setComposerAutoReview(false)');
  assert.equal(first.state.approvalModeBusy, true);
  await first.run('setComposerAutoReview(false)');
  assert.equal(puts, before + 1, 'Busy selection must not duplicate writes');
  release(); await pending; holdPut = null;
  assert.equal(first.state.approvalModeBusy, false);
  await second.listeners.get('approval-policy-changed')();
  assert.equal(second.run('composerAutoReview()'), false);
  const current = {...second.state.approvalPolicy};
  const actualServer = server;
  server = {enabled: true, revision: current.revision - 1};
  await second.run('refreshApprovalPolicy()');
  assert.equal(second.run('composerAutoReview()'), false, 'Stale GET must be ignored');
  server = actualServer;
  failPut = true;
  await first.run('setComposerAutoReview(true)');
  assert.equal(first.run('composerAutoReview()'), false);
  assert.equal(first.state.approvalModeBusy, false);
  assert.ok(first.notices.includes('fixture save failed'));
  failGet = true;
  await first.run('setComposerAutoReview(true)');
  assert.equal(first.run('composerAutoReview()'), false);
  assert.equal(first.state.approvalModeBusy, false);
  failPut = failGet = false;
  server = {enabled: true, revision: server.revision + 1};
  await second.listeners.get('open')();
  // The reconnect listener deliberately does not return its promise.
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(second.run('composerAutoReview()'), true);
  assert.ok(first.metrics().renders > 0);
  console.log('PASS: global policy, cross-session/SSE sync, fresh client, busy writes, stale GET, failed save/refresh and reconnect');
})().catch(error => { console.error(error); process.exitCode = 1; });

// Review has its own lifetime after model generation. A closed model stream
// must not strand the UI at "reviewing" until the user refreshes the page.
(async () => {
  function pollClient(status = 'awaiting_approval', approvalState = 'reviewing') {
    let timer, streamOpens = 0, renders = 0, refreshes = 0;
    const state = {session: {id: 'review-session', runs: [{id: 'review-run', status, approvalState}]}, runPhase: {}, streamRetryAt: 0};
    const c = vm.createContext({state, clearTimeout(){timer = null;}, setTimeout(fn){timer = fn; return 1;},
      openStream(){streamOpens++;}, closeStream(){}, Date, getSessionWindow: async () => c.next,
      adoptSessionIfChanged(s){state.session=s; return true;}, renderSession(){renders++;},
      reconcileAvatarRun(){}, api: async()=>{}, loadSessions:async()=>{refreshes++;},
      loadFiles:async()=>{}, scheduleTitleSync(){}, maybeAutoNarrate(){}});
    vm.runInContext(extract('schedulePoll'), c);
    return {state,c,run:()=>vm.runInContext('schedulePoll()',c),tick:()=>timer(),metrics:()=>({timer:!!timer,streamOpens,renders,refreshes})};
  }
  for (const next of [
    {id:'review-session',runs:[{id:'review-run',status:'completed',applied:true}]},
    {id:'review-session',runs:[{id:'review-run',status:'awaiting_approval',approvalState:'manual'}]}
  ]) {
    const p=pollClient();p.c.next=next;p.run();
    assert.equal(p.metrics().timer,true);assert.equal(p.metrics().streamOpens,0,'review does not reopen generation SSE');
    await p.tick();assert.equal(p.state.session,next);assert.equal(p.metrics().renders,1);
    assert.equal(p.metrics().refreshes,1);assert.equal(p.metrics().timer,false,'terminal/manual review stops polling');
  }
  const p=pollClient();p.c.getSessionWindow=async()=>{throw Error('transient outage');};p.run();await p.tick();
  assert.equal(p.metrics().timer,true,'failed read must retain review reconciliation');
  const approved=pollClient('awaiting_approval','approved');approved.run();
  assert.equal(approved.metrics().timer,true,'approval does not prove disk apply finished');
  const manual=pollClient('awaiting_approval','manual');manual.run();assert.equal(manual.metrics().timer,false);
  console.log('PASS: file-review poll survives model completion, approved/manual transitions and transient outage');
})().catch(error => { console.error(error); process.exitCode = 1; });
