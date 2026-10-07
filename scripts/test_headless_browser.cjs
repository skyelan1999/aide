'use strict';
// Real Chromium integration, using a disposable bridge and local test page.
const assert = require('node:assert/strict');
const http = require('node:http');
const { spawn } = require('node:child_process');
const path = require('node:path');
async function listen(server) { await new Promise(r => server.listen(0, '127.0.0.1', r)); return server.address().port; }
async function main() {
  const fixture = http.createServer((req, res) => {
    if (req.url === '/manual.pdf') { res.setHeader('Content-Type', 'application/pdf'); return res.end('%PDF-1.4\nfixture'); }
    if (req.url === '/denied.pdf') { res.writeHead(302, { Location: 'https://example.com/secret.pdf' }); return res.end(); }
    if (req.url === '/oversized.pdf') { res.setHeader('Content-Length', 33 * 1024 * 1024); return res.end(); }
    if (req.url === '/fake.pdf') return res.end('<html>not a PDF</html>');
    if (req.url === '/late-links') return res.end(Array.from({ length: 150 }, (_, i) => `<a href="/nav${i}">Navigation</a>`).join('') + '<a href="/manual.pdf">Manual</a>');
    if (req.url === '/transition') return res.end('<script>location.replace("/late-links")</script>');
    if (req.url === '/redirect') { res.writeHead(302, { Location: 'https://example.com/' }); return res.end(); }
    res.setHeader('Content-Type', 'text/html');
    res.end('<title>Aide headless fixture</title><h1>READY</h1><input id="q"><button id="go" onclick="document.querySelector(\'#result\').textContent=\'RESULT:\'+document.querySelector(\'#q\').value">Apply</button><p id="result">WAITING</p><a href="/more">More</a><div style="height:3000px">Long page</div>');
  });
  const fixturePort = await listen(fixture);
  const reservation = http.createServer(); const port = await listen(reservation); await new Promise(r => reservation.close(r));
  const token = 'headless-test-' + 'x'.repeat(40);
  const bridge = spawn(process.execPath, [path.join(__dirname, 'headless-browser-bridge.js')], {
    env: { ...process.env, AIDE_BROWSER_BRIDGE_TOKEN: token, AIDE_HEADLESS_BRIDGE_PORT: String(port) }, stdio: ['ignore', 'ignore', 'inherit'],
  });
  const base = `http://127.0.0.1:${port}`;
  const headers = { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' };
  const post = async (route, args) => {
    const response = await fetch(base + route, { method: 'POST', headers, body: JSON.stringify({ allowedHosts: ['127.0.0.1'], ...args }) });
    return { status: response.status, value: await response.json() };
  };
  try {
    let ready = false;
    for (let i = 0; i < 50; i++) {
      try { ready = (await fetch(base + '/healthz', { headers })).ok; } catch (_) {}
      if (ready) break; await new Promise(r => setTimeout(r, 100));
    }
    assert(ready, 'Chromium must actually start');
    assert.equal((await fetch(base + '/healthz')).status, 401);
    const url = `http://127.0.0.1:${fixturePort}/`;
    let result = await post('/v1/read', { url });
    assert.equal(result.status, 200); assert.match(result.value.text, /READY/); assert.equal(result.value.links[0].text, 'More');
    assert.equal((await post('/v1/snapshot', {})).status, 502, 'read must close its isolated page');
    assert.equal((await post('/v1/navigate', { url })).status, 200);
    assert.equal((await post('/v1/fill', { selector: '#q', text: 'aide-real-test' })).status, 200);
    assert.equal((await post('/v1/click', { selector: '#go' })).status, 200);
    result = await post('/v1/snapshot', {}); assert.match(result.value.text, /RESULT:aide-real-test/);
    assert.equal((await post('/v1/scroll', { direction: 'down' })).status, 200);
    assert.equal((await post('/v1/snapshot', { allowedHosts: ['example.com'] })).status, 502);
    assert.equal((await post('/v1/navigate', { url: 'https://example.com/' })).status, 502);
    assert.equal((await post('/v1/read', { url: url + 'redirect' })).status, 502, 'redirect must be blocked before outside request');
    assert.equal((await post('/v1/read', { url: 'file:///etc/passwd' })).status, 502);
    result = await post('/v1/read', { url: url + 'late-links' });
    assert.equal(result.value.links[0].text, 'Manual', 'PDF beyond old 100-link cutoff must be exposed');
    result = await post('/v1/read', { url: url + 'transition' });
    assert.equal(result.status, 200); assert.match(result.value.url, /late-links/);
    result = await post('/v1/read', { url: url + 'manual.pdf' });
    assert.equal(result.status, 200); assert.equal(Buffer.from(result.value.pdfBase64, 'base64').subarray(0, 5).toString(), '%PDF-');
    for (const name of ['denied', 'oversized', 'fake']) assert.equal((await post('/v1/read', { url: url + name + '.pdf' })).status, 502);
    console.log('PASS REAL Chromium: original controls, late PDF links, page transition, PDF bytes, PDF redirect scope/size/magic denial');
  } finally {
    bridge.kill('SIGTERM'); await new Promise(r => bridge.once('exit', r));
    await new Promise(r => fixture.close(r));
  }
}
main().catch(error => { console.error(error); process.exitCode = 1; });
