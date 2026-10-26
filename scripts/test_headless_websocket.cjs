'use strict';
// Real Chromium + local WebSocket peers; no user profile or external requests.
const assert = require('node:assert/strict');
const http = require('node:http');
const path = require('node:path');
const { spawn } = require('node:child_process');
const { wsServer: WebSocketServer } = require('./browser-runtime/node_modules/playwright-core/lib/utilsBundle.js');
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
async function listen(server, host) {
  await new Promise(resolve => server.listen(0, host, resolve));
  return server.address().port;
}
async function main() {
  const outside = http.createServer();
  const sockets = new WebSocketServer({ server: outside });
  let connections = 0, messages = 0;
  sockets.on('connection', socket => {
    connections++;
    socket.on('message', message => { messages++; socket.send(message.toString()); });
  });
  const outsidePort = await listen(outside, '127.0.0.2');
  const fixture = http.createServer((_, res) => {
    res.setHeader('Content-Type', 'text/html');
    res.end(`<!doctype html><title>WebSocket scope fixture</title>
      <p id="result">connecting</p><button id="send">Send again</button>
      <script>
        const socket = new WebSocket('ws://127.0.0.2:${outsidePort}/channel');
        socket.onopen = () => socket.send('initial');
        socket.onmessage = event => document.querySelector('#result').textContent = event.data;
        socket.onclose = () => document.querySelector('#result').textContent = 'closed';
        document.querySelector('#send').onclick = () => {
          if (socket.readyState === WebSocket.OPEN) socket.send('after-policy-change');
          else document.querySelector('#result').textContent = 'closed';
        };
      </script>`);
  });
  const fixturePort = await listen(fixture, '127.0.0.1');
  const reservation = http.createServer();
  const port = await listen(reservation, '127.0.0.1');
  await new Promise(resolve => reservation.close(resolve));
  const token = 'websocket-scope-fixture-' + 'x'.repeat(40);
  // An optional system Chromium makes the pinned bridge testable on a cloud
  // executor without installing another browser. The bridge source is unmodified.
  const bootstrap = `const {chromium}=require(process.argv[1]);
    if(process.env.CHROME_PATH){const launch=chromium.launch.bind(chromium);
      chromium.launch=options=>launch({...options,executablePath:process.env.CHROME_PATH});}
    require(process.argv[2]);`;
  const bridge = spawn(process.execPath, ['-e', bootstrap,
    path.join(__dirname, 'browser-runtime/node_modules/playwright'),
    path.join(__dirname, 'headless-browser-bridge.js')], {
    env: { ...process.env, AIDE_BROWSER_BRIDGE_TOKEN: token, AIDE_HEADLESS_BRIDGE_PORT: String(port) },
    stdio: ['ignore', 'ignore', 'inherit'],
  });
  const headers = { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' };
  const base = `http://127.0.0.1:${port}`;
  const post = async (route, allowedHosts, extra = {}) => {
    const response = await fetch(base + route, { method: 'POST', headers, body: JSON.stringify({ allowedHosts, ...extra }) });
    const value = await response.json();
    assert.equal(response.status, 200, JSON.stringify(value));
    return value;
  };
  const rules = ['127.0.0.1'];
  const both = ['127.0.0.1', '127.0.0.2'];
  const url = `http://127.0.0.1:${fixturePort}/`;
  try {
    let ready = false;
    for (let n = 0; n < 50; n++) {
      try { ready = (await fetch(base + '/healthz', { headers })).ok; } catch (_) {}
      if (ready) break;
      await pause(100);
    }
    assert(ready, 'real Chromium bridge must start');
    await post('/v1/navigate', rules, { url });
    for (let n = 0; n < 30; n++) {
      const result = await post('/v1/snapshot', rules);
      if (/closed|initial/.test(result.text)) break;
      await pause(50);
    }
    assert.equal(connections, 0, 'out-of-scope WebSocket must be blocked before its handshake');
    await post('/v1/navigate', both, { url });
    let echoed = false;
    for (let n = 0; n < 30; n++) {
      echoed = (await post('/v1/snapshot', both)).text.includes('initial');
      if (echoed) break;
      await pause(50);
    }
    assert(echoed, 'allowed WebSocket must exchange frames with the real server');
    assert.equal(connections, 1);
    assert.equal(messages, 1);
    await post('/v1/snapshot', rules);
    await post('/v1/click', rules, { selector: '#send' });
    await pause(100);
    assert.equal(messages, 1, 'narrowed policy must stop frames on the previously allowed socket');
    for (let n = 0; n < 30 && sockets.clients.size; n++) await pause(50);
    assert.equal(sockets.clients.size, 0, 'narrowed policy must close the upstream socket');
    await post('/v1/navigate', rules, { url });
    await pause(100);
    assert.equal(connections, 1, 'new context must retain WebSocket admission checks');
    await post('/v1/navigate', both, { url });
    for (let n = 0; n < 30 && messages < 2; n++) await pause(50);
    assert.equal(messages, 2);
    const rejected = await fetch(base + '/v1/snapshot', {
      method: 'POST', headers, body: JSON.stringify({ allowedHosts: ['example.invalid'] }),
    });
    assert.equal(rejected.status, 502, 'snapshot of a removed page host must fail');
    for (let n = 0; n < 30 && sockets.clients.size; n++) await pause(50);
    assert.equal(sockets.clients.size, 0, 'even a rejected snapshot must apply the narrowed network scope');
    console.log('PASS REAL Chromium: WebSocket handshake scope, allowed bidirectional frames, live allowlist revocation and upstream cleanup');
  } finally {
    const exited = new Promise(resolve => bridge.once('exit', resolve));
    bridge.kill('SIGTERM');
    if (bridge.exitCode === null) await exited;
    for (const socket of sockets.clients) socket.terminate();
    await new Promise(resolve => sockets.close(resolve));
    fixture.closeAllConnections(); outside.closeAllConnections();
    await Promise.all([new Promise(resolve => fixture.close(resolve)), new Promise(resolve => outside.close(resolve))]);
  }
}
main().catch(error => { console.error(error); process.exitCode = 1; });
