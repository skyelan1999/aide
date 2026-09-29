'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../internal/server/web/app.js'), 'utf8');
function excerpt(start, end) {
  const a = source.indexOf(start);
  const b = source.indexOf(end, a);
  assert.ok(a >= 0 && b > a, `missing ${start}`);
  return source.slice(a, b);
}
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return {promise, resolve, reject};
}
function node(id = '') {
  const classes = new Set();
  return {
    dataset: {sessionId: id, sessionTitle: id},
    classList: {
      add(c) { classes.add(c); }, remove(c) { classes.delete(c); },
      toggle(c, on) { if (on) classes.add(c); else classes.delete(c); },
      contains(c) { return classes.has(c); },
    },
    attributes: {}, setAttribute(k, v) { this.attributes[k] = v; },
    focus() { this.focused = true; }, textContent: '', value: '',
  };
}

async function testSessionSelection() {
  const s1 = node('s1'), s2 = node('s2'), s3 = node('s3');
  const assistant = node('assistant');
  const nodes = {'assistant-entry': assistant, conversation: node(), 'session-title': node(), prompt: node()};
  const requests = [], gets = new Map();
  const state = {session: {id:'s1',title:'s1'}, sessionJSON:'', pendingSessionId:'', live:{}, liveStable:{}, liveRound:{}, liveTool:{}, liveReasoning:{}, runPhase:{}};
  const context = {
    state, $: id => nodes[id],
    document: {querySelectorAll: selector => {
      assert.equal(selector, '.session-item[data-session-id]'); return [s1,s2,s3];
    }},
    api(url, options) {
      requests.push([url, options?.method || 'GET']);
      if (options?.method === 'PATCH') return Promise.resolve({});
      const d = deferred(); gets.set(url, d); return d.promise;
    },
    clearTimeout() {}, closeStream() {}, ttsCancel() {}, cancelContextPreview() {},
    refreshCompactInfo() {}, schedulePoll() {}, scheduleContextPreview() {},
    renderSession() {}, loadSessions() { requests.push(['list','GET']); return Promise.resolve(); },
    t: key => key,
  };
  vm.createContext(context);
  vm.runInContext(excerpt('const sessionSeq =', 'function closeStream()'), context);

  const first = context.selectSession('s2');
  assert.equal(s2.classList.contains('active'), true, 'selection must paint before GET');
  assert.equal(s2.classList.contains('loading'), true);
  assert.equal(nodes.conversation.attributes['aria-busy'], 'true');
  assert.deepEqual(requests, [['/sessions/s2','GET']], 'checked write must not precede GET');
  gets.get('/sessions/s2').resolve({id:'s2',title:'s2',runs:[]});
  await first;
  assert.equal(state.session.id, 's2');
  assert.equal(nodes.prompt.focused, true);
  assert.equal(s2.classList.contains('loading'), false);
  assert.equal(nodes.conversation.attributes['aria-busy'], 'false');
  assert.equal(requests.some(([url, method]) => url === '/sessions/s2' && method === 'PATCH'), true);

  const old = context.selectSession('s1');
  const latest = context.selectSession('s3');
  gets.get('/sessions/s3').resolve({id:'s3',title:'s3',runs:[]});
  await latest;
  gets.get('/sessions/s1').resolve({id:'s1',title:'s1',runs:[]});
  await old;
  assert.equal(state.session.id, 's3', 'stale response must not overwrite newest selection');

  const failed = context.selectSession('s1');
  gets.get('/sessions/s1').reject(new Error('offline'));
  await assert.rejects(failed, /offline/);
  assert.equal(state.session.id, 's3');
  assert.equal(s3.classList.contains('active'), true, 'failed GET must restore prior selection');
  assert.equal(nodes['session-title'].textContent, 's3');
}

async function testSendSubmission() {
  const send = node(), hint = node(), prompt = node(); prompt.value = 'test request';
  const conversation = node(); conversation.scrollHeight = 100; conversation.scrollTo = () => {};
  const nodes = {send, 'composer-hint':hint, prompt, conversation, 'task-form':node()};
  const state = {session:{id:'s1',kind:'normal'}, config:{configured:true}, previewOverLimit:false, submitting:false, attachments:[], profiles:{strategy:'auto'}, mode:'chat', workflowPhase:''};
  let posts = 0, refreshes = 0, cancels = 0;
  let run = deferred();
  const context = {
    state, $: id => nodes[id], t: key => key, xiaomiDictation:{active:false,starting:false},
    voice:{queuedOverride:null}, action: fn => fn, toast() {}, openSettings() {}, renderAttachments() {},
    cancelContextPreview() { cancels++; }, setSendMode() {},
    updateSendEnabled() { refreshes++; send.disabled = state.submitting || state.previewOverLimit; send.classList.toggle('submitting',state.submitting); },
    api(url, options) { assert.equal(url,'/sessions/s1/runs'); assert.equal(options.method,'POST'); posts++; return run.promise; },
    selectSession() { return Promise.resolve(); },
  };
  vm.createContext(context);
  vm.runInContext(excerpt("$('task-form').onsubmit =", "$('queue-toggle')?.addEventListener"), context);
  const event = {preventDefault() {}};
  const first = nodes['task-form'].onsubmit;
  assert.equal(typeof first, 'function');
  const pending = first(event);
  assert.equal(state.submitting, true, 'submit state must paint before POST resolves');
  assert.equal(send.disabled, true);
  assert.equal(posts, 1);
  await first(event);
  assert.equal(posts, 1, 'second click must not create another run');
  run.resolve({});
  await pending;
  assert.equal(state.submitting, false);
  assert.equal(send.disabled, false);
  assert.equal(prompt.value, '');
  assert.equal(cancels, 1);
  assert.ok(refreshes >= 2);

  prompt.value = 'retry draft';
  run = deferred();
  const failed = first(event);
  run.reject(new Error('rejected'));
  await assert.rejects(failed, /rejected/);
  assert.equal(prompt.value, 'retry draft', 'failed POST must retain draft');
  assert.equal(state.submitting, false);
  assert.equal(send.disabled, false);
}

(async () => {
  await testSessionSelection();
  await testSendSubmission();
  console.log('ui click latency PASS');
})().catch(error => { console.error(error); process.exitCode = 1; });
