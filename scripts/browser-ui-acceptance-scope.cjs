'use strict';
const assert = require('node:assert/strict'), http = require('node:http');
const {chromium} = require('./browser-runtime/node_modules/playwright');
const {runOwnedUI} = require('./ui_acceptance_scope.cjs');
async function main() {
  const foreign = {id: 'foreign-session', title: 'Owned fixture user conversation', messages: [], runs: []};
  const before = JSON.stringify(foreign), sessions = new Map([[foreign.id, foreign]]), runs = [], leakedWrites = [];
  let next = 0;
  const server = http.createServer(async (req, res) => {
    const parsed = new URL(req.url, 'http://owned.invalid');
    const json = (status, data) => {res.writeHead(status, {'Content-Type': 'application/json'});res.end(JSON.stringify(data));};
    const parts = [];for await (const chunk of req) parts.push(chunk);
    const input = parts.length ? JSON.parse(Buffer.concat(parts)) : {};
    if (parsed.pathname === '/') {
      res.writeHead(200, {'Content-Type': 'text/html'});
      res.end('<!doctype html><meta charset="utf-8"><title>Owned UI isolation fixture</title><textarea id="prompt"></textarea><button id="send">send</button><script>' +
        'let state={session:null};async function api(path,options={}){const r=await fetch("/api"+path,options);const d=await r.json();if(!r.ok)throw Error(d.error||"fixture request failed");return d;}' +
        'async function selectSession(id){state.session=await api("/sessions/"+encodeURIComponent(id));}' +
        'document.querySelector("#send").onclick=async()=>{await api("/sessions/"+state.session.id+"/runs",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({prompt:document.querySelector("#prompt").value})});};</script>');return;
    }
    if (parsed.pathname === '/api/lock-state') return json(200, {locked: false, gen: 1});
    if (parsed.pathname === '/api/sessions' && req.method === 'POST') {
      const session = {id: 'test-' + ++next, title: input.title, messages: [], runs: []};sessions.set(session.id, session);return json(201, session);
    }
    if (parsed.pathname === '/api/sessions' && req.method === 'GET') return json(200, [...sessions.values()].filter(session => !!session.archived === (parsed.searchParams.get('archived') === '1')));
    if (parsed.pathname === '/api/export') return json(200, {sessions: [...sessions.values()], count: sessions.size});
    const match = /^\/api\/sessions\/([^/]+)(?:\/runs(?:\/([^/]+)\/cancel)?)?$/.exec(parsed.pathname);
    if (match) {
      const session = sessions.get(match[1]);if (!session) return json(404, {error: 'not found'});
      if (req.method === 'GET') return json(200, session);
      if (session.id === foreign.id) {leakedWrites.push(req.url);return json(500, {error: 'foreign write reached server'});}
      if (req.method === 'DELETE') {sessions.delete(session.id);return json(200, {ok: true});}
      if (req.method === 'PATCH') {session.archived = !!input.archived;return json(200, session);}
      if (match[2]) {session.runs.find(run => run.id === match[2]).status = 'cancelled';return json(200, {ok: true});}
      if (req.method === 'POST') {const run = {id: 'run-' + runs.length, status: 'running'};session.runs.push(run);session.messages.push(input.prompt);runs.push(run);return json(202, run);}
    }
    leakedWrites.push(req.url);return json(500, {error: 'unexpected global write'});
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const base = 'http://127.0.0.1:' + server.address().port;
  try {
    await assert.rejects(runOwnedUI({chromium, base, token: 'owned-fixture-token', chrome: '/usr/bin/chromium', label: 'scope-browser'}, async ({page, scope, session}) => {
      const denied = await page.evaluate(async () => {
        const statuses = [];
        for (const [path, method] of [['/sessions/foreign-session', 'DELETE'], ['/settings', 'PUT'], ['/sessions/archived/all', 'DELETE']]) {
          statuses.push((await fetch('/api' + path, {method, body: '{}'})).status);
        }
        return statuses;
      });
      assert.deepEqual(denied, [403, 403, 403]);
      const created = await page.evaluate(async () => {
        const ids = [];
        for (let i = 0; i < 6; i++) ids.push((await api('/sessions', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({title: window.__aideUITestRunTag + 'fill-' + i})})).id);
        await api('/sessions/' + ids[0], {method: 'PATCH', body: JSON.stringify({archived: true})});
        return {ids, list: await api('/sessions'), exported: await api('/export')};
      });
      assert.equal(created.ids.length, 6);assert.equal(scope.owned.size, 7);
      assert.ok(created.list.every(item => item.id !== foreign.id));assert.ok(created.exported.sessions.every(item => item.id !== foreign.id));
      await page.fill('#prompt', 'owned isolated browser task');await page.click('#send');
      await page.waitForFunction(async id => (await api('/sessions/' + id)).runs.length === 1, session.id);
      throw Error('owned intentional UI assertion failure');
    }), /owned intentional UI assertion failure/);
    assert.equal(sessions.size, 1);assert.equal(JSON.stringify(foreign), before);
    assert.equal(runs.length, 1);assert.equal(runs[0].status, 'cancelled');assert.deepEqual(leakedWrites, []);
    console.log('PASS real Chromium scoped primary+6fillers, owned archived/export view, denied foreign/global/bulk mutations, finally cancel/delete after assertion failure; fixture user untouched');
  } finally {await new Promise(resolve => server.close(resolve));}
}
main().catch(error => {console.error(error);process.exitCode = 1;});
