'use strict';
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path'), vm = require('node:vm');
function fixture() {
  const foreign = {id: 'foreign-user-session', title: 'User conversation', messages: [], runs: []};
  const sessions = new Map([[foreign.id, foreign]]), allRuns = [], calls = [];
  let selected = foreign.id, sequence = 0, closed = false;
  const response = (status, value) => ({status: () => status, ok: () => status >= 200 && status < 300,
    json: async () => JSON.parse(JSON.stringify(value)), text: async () => JSON.stringify(value), headers: () => ({})});
  const request = {async fetch(url, options = {}) {
    const parsed = new URL(url), route = parsed.pathname, method = options.method || 'GET';calls.push({method, route});
    const input = typeof options.data === 'string' ? JSON.parse(options.data) : options.data;
    if (route === '/api/lock-state') return response(200, {locked: false, gen: 1});
    if (route === '/api/sessions' && method === 'POST') {
      const session = {id: 'owned-session-' + ++sequence, title: input.title, messages: [], runs: []};sessions.set(session.id, session);return response(201, session);
    }
    if (route === '/api/sessions' && method === 'GET') return response(200, [...sessions.values()].filter(session => !!session.archived === (parsed.searchParams.get('archived') === '1')));
    const match = /^\/api\/sessions\/([^/]+)(?:\/runs\/([^/]+)\/cancel)?$/.exec(route);
    if (match) {
      const session = sessions.get(match[1]);if (!session) return response(404, {error: 'not found'});
      if (method === 'GET') return response(200, session);
      assert.notEqual(session.id, foreign.id, 'cleanup cannot mutate a user session');
      if (method === 'DELETE') {sessions.delete(session.id);return response(200, {ok: true});}
      if (method === 'POST' && match[2]) {session.runs.find(run => run.id === match[2]).status = 'cancelled';return response(200, {ok: true});}
    }
    throw Error('unexpected fixture request: ' + method + ' ' + route);
  }};
  const page = {
    async goto() {}, async reload() {}, async waitForTimeout() {}, async close() {},
    async waitForFunction(_fn, id) {if (typeof id === 'string') selected = id;},
    async evaluate(fn, value) {
      const body = String(fn);
      if (body.includes('selectSession(')) {selected = value;return;}
      if (body.includes('localStorage.setItem')) return;
      if (body.includes('state?.session?.runs?.[0]?.status')) return {status: 'completed', toolUses: 0};
      if (body.includes('const run = state?.session?.runs?.[0]')) return {status: 'completed', live: '', tool: ''};
      if (body.includes('textContent?.trim')) return '';
      return false; // Intentional first UI assertion failure after starting a task.
    },
    async fill(_selector, text) {sessions.get(selected).messages.push(text);},
    async click() {const run = {id: 'owned-run-' + allRuns.length, status: 'running'};sessions.get(selected).runs.push(run);allRuns.push(run);}
  };
  const context = {request, async newPage() {return page;}, async addInitScript() {}, async route() {}, async unroute() {}, async close() {}};
  const browser = {async newPage() {return page;}, async newContext() {return context;}, async close() {closed = true;}};
  return {foreign, sessions, allRuns, calls, context, page, chromium: {async launch() {return browser;}}, get closed() {return closed;}};
}
async function runScript(name) {
  const f = fixture(), script = fs.readFileSync('scripts/' + name, 'utf8');
  const context = {require(id) {if (id === 'playwright-core') return {chromium: f.chromium};return require(path.resolve('scripts', id));},
    process: {env: {BASE_URL: 'http://owned.invalid', AIDE_TOKEN: 'owned-token', CHROME_PATH: '/owned/chrome'}, argv: ['node', name],
      exit() {throw Error('simulated process termination skips cleanup');}, exitCode: 0},
    console: {log() {}, error() {}}, setTimeout, clearTimeout};
  try {await vm.runInNewContext(script, context);} catch (_) {}
  assert.deepEqual(f.foreign.messages, [], name + ' must not type into the preselected user conversation');
  assert.deepEqual(f.foreign.runs, [], name + ' must not start provider work in user conversation');
  assert.equal(f.sessions.size, 1, name + ' must remove its sessions after assertion failure');
  assert.ok(f.allRuns.length > 0 && f.allRuns.every(run => run.status === 'cancelled'), name + ' must cancel started owned runs');
  assert.equal(f.closed, true, name + ' must close its browser in finally');
}
async function main() {
  await runScript('ui_queue_check.cjs');await runScript('ui_stream_check.cjs');
  const {createOwnedSessionScope} = require('./ui_acceptance_scope.cjs');
  {
    const f = fixture(), scope = createOwnedSessionScope(f.context.request, 'http://owned.invalid', 'owned-token');
    for (let i = 0; i < 7; i++) await scope.create('filler-' + i);
    await scope.cleanup();assert.equal(f.sessions.size, 1, 'all seven owned sessions must be deleted');
  }
  {
    const f = fixture(), fetch = f.context.request.fetch;
    let lost = false;
    f.context.request.fetch = async (url, options) => {
      const result = await fetch(url, options);
      if (!lost && options?.method === 'POST') {lost = true;throw Error('owned lost creation response');}
      return result;
    };
    const scope = createOwnedSessionScope(f.context.request, 'http://owned.invalid', 'owned-token');
    await assert.rejects(scope.create('lost'), /lost creation response/);
    assert.equal(f.sessions.size, 2);await scope.cleanup();assert.equal(f.sessions.size, 1, 'committed creation with lost response must be discovered and cleaned');
  }
  {
    const f = fixture(), scope = createOwnedSessionScope(f.context.request, 'http://owned.invalid', 'owned-token');
    const first = await scope.create('failing-delete');await scope.create('other');
    const fetch = f.context.request.fetch;
    f.context.request.fetch = (url, options) => options?.method === 'DELETE' && url.endsWith('/' + first.id)
      ? Promise.reject(Error('owned delete failure')) : fetch(url, options);
    await assert.rejects(scope.cleanup(), /cleanup incomplete/);assert.equal(f.sessions.size, 2, 'failed cleanup must continue deleting other owned sessions');
    assert.equal(f.sessions.has(f.foreign.id), true);
  }
  console.log('PASS both actual acceptance CLIs isolate selected session and cancel/delete owned data on injected post-start UI failure');
}
module.exports = {fixture};
if (require.main === module) main().catch(error => {console.error(error);process.exitCode = 1;});
