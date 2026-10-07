'use strict';

const assert = require('node:assert/strict');
const http = require('node:http');
const { spawn } = require('node:child_process');
const path = require('node:path');
const { isAllowedHost, validateURL } = require('../plugins/browser-control/index.js')._test;

async function freePort() {
  const server = http.createServer();
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address();
  await new Promise(resolve => server.close(resolve));
  return port;
}

async function main() {
  assert.equal(isAllowedHost('docs.example.com', ['*.example.com']), true);
  assert.equal(isAllowedHost('example.org', ['example.com']), false);
  assert.equal(validateURL('https://docs.example.com/a', ['*.example.com']), 'https://docs.example.com/a');
  assert.throws(() => validateURL('file:///etc/passwd', ['*']), /HTTP\/HTTPS/);
  assert.throws(() => validateURL('https://user:pass@example.com', ['example.com']), /HTTP\/HTTPS/);
  assert.throws(() => validateURL('https://evil.example/', ['example.com']), /允许列表/);

  const driverPort = await freePort();
  const bridgePort = await freePort();
  const token = 'test-token-' + 'x'.repeat(40);
  let currentUrl = 'https://example.com/';
  let clickCount = 0;
  let filledText = '';
  const fakeDriver = http.createServer(async (req, res) => {
    const chunks = [];
    for await (const chunk of req) chunks.push(chunk);
    const body = chunks.length ? JSON.parse(Buffer.concat(chunks).toString()) : {};
    const send = value => { res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ value })); };
    if (req.method === 'GET' && req.url === '/status') return send({ ready: true });
    if (req.method === 'POST' && req.url === '/session') return send({ sessionId: 'fake-session' });
    if (req.method === 'POST' && req.url === '/session/fake-session/url') { currentUrl = body.url; return send(null); }
    if (req.method === 'POST' && req.url === '/session/fake-session/execute/sync') {
      if (body.script.includes('location.href')) return send({ url: currentUrl, title: 'Aide browser test', text: 'mock page text' });
      if (body.script.includes('e.click()')) { clickCount++; return send(true); }
      if (body.script.includes('setter.call')) { filledText = body.args[1]; return send(true); }
    }
    res.writeHead(404); res.end('{}');
  });
  await new Promise(resolve => fakeDriver.listen(driverPort, '127.0.0.1', resolve));
  const bridge = spawn(process.execPath, [path.join(__dirname, 'safari-bridge.js')], {
    env: { ...process.env, AIDE_BROWSER_BRIDGE_TOKEN: token, AIDE_SAFARI_BRIDGE_PORT: String(bridgePort), AIDE_SAFARI_DRIVER_PORT: String(driverPort) },
    stdio: ['ignore', 'ignore', 'inherit'],
  });
  const base = `http://127.0.0.1:${bridgePort}`;
  const headers = { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' };
  try {
    let healthy = false;
    for (let i = 0; i < 50; i++) {
      try { healthy = (await fetch(base + '/healthz', { headers })).ok; } catch (_) {}
      if (healthy) break;
      await new Promise(resolve => setTimeout(resolve, 50));
    }
    assert.equal(healthy, true, 'bridge should start');
    const unauth = await fetch(base + '/healthz');
    assert.equal(unauth.status, 401, 'bridge must reject missing bearer token');
    const post = async (route, body) => fetch(base + route, { method: 'POST', headers, body: JSON.stringify(body) });
    let response = await post('/v1/navigate', { url: 'https://evil.example/', allowedHosts: ['example.com'] });
    assert.equal(response.status, 502, 'bridge must reject disallowed host');
    assert.equal(currentUrl, 'https://example.com/', 'rejected navigation must not reach WebDriver');
    response = await post('/v1/navigate', { url: 'https://example.com/docs', allowedHosts: ['example.com'] });
    assert.equal(response.status, 200);
    let result = await response.json();
    assert.equal(result.url, 'https://example.com/docs');
    assert.equal(result.title, 'Aide browser test');
    assert.equal(result.text, 'mock page text');
    response = await post('/v1/click', { selector: 'button#open', allowedHosts: ['example.com'] });
    assert.equal(response.status, 200);
    assert.equal(clickCount, 1);
    response = await post('/v1/fill', { selector: 'input[name=q]', text: 'needle', allowedHosts: ['example.com'] });
    assert.equal(response.status, 200);
    assert.equal(filledText, 'needle');
    console.log('PASS browser allowlist, bridge auth, navigation, snapshot, click, fill');
  } finally {
    bridge.kill('SIGTERM');
    fakeDriver.close();
  }
}

main().catch(error => { console.error(error); process.exitCode = 1; });
