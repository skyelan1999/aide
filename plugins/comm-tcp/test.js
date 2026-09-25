'use strict';
/* comm-tcp 集成测试：直接 require 插件模块、构造假 ctx、驱动各 handler（容器内 node 直接跑）。
 *
 * 运行（容器内，/workspace 挂载仓库根）：
 *   docker run --rm --network=host -v "$PWD":/workspace -w /workspace node:24-bookworm-slim node plugins/comm-tcp/test.js
 * 全部用 127.0.0.1 本地回环，不依赖外网。
 */
const net = require('net');

const mod = require('./index.js');
const events = [];
const tools = new Map();
const fakeCtx = {
  logger: { info() {}, warn() {}, error() {} },
  provide() {}, slot() {}, effect() {}, on() {},
  emit(type, fields) { events.push(Object.assign({ type }, fields)); },
  tool(def) { tools.set(def.name, def); },
};
mod.apply(fakeCtx);
if (typeof mod.start === 'function') mod.start();
const call = (name, args) => tools.get(name).handler(args || {});
const wait = (ms) => new Promise(r => setTimeout(r, ms));
const nextTraffic = (connId, dir, timeout = 2000) => new Promise((resolve, reject) => {
  const t0 = Date.now();
  const iv = setInterval(() => {
    const hit = events.find(e => e.type === 'traffic' && e.connectionId === connId && (!dir || e.direction === dir));
    if (hit) { clearInterval(iv); resolve(hit); }
    else if (Date.now() - t0 > timeout) { clearInterval(iv); reject(new Error('等待 traffic 超时: ' + connId + ' ' + dir)); }
  }, 10);
});
const nextEvent = (subtype, timeout = 2000) => new Promise((resolve, reject) => {
  const t0 = Date.now();
  const iv = setInterval(() => {
    const hit = events.find(e => e.subtype === subtype);
    if (hit) { clearInterval(iv); resolve(hit); }
    else if (Date.now() - t0 > timeout) { clearInterval(iv); reject(new Error('等待 event 超时: ' + subtype)); }
  }, 10);
});

let pass = 0, fail = 0; const failures = [];
function ok(cond, name) { if (cond) { pass++; console.log('  ✓', name); } else { fail++; failures.push(name); console.log('  ✗', name); } }

// 起一个原始 TCP server，收到 data 时存入 received
function rawServer(port, onData) {
  const received = [];
  const server = net.createServer((sock) => {
    sock.on('data', (b) => {
      received.push(b.toString());
      if (onData) onData(sock, b);
    });
  });
  return new Promise((resolve) => server.listen(port, '127.0.0.1', () => resolve({ server, received })));
}

(async () => {
  /* == 1. echo 模拟：监听 + echo 规则，客户端收发一致 == */
  console.log('== 1. echo 模拟应答 ==');
  {
    const PORT = 42101;
    const lis = await call('tcp_listen', { port: PORT });
    ok(lis.ok && lis.host === '127.0.0.1', 'tcp_listen 绑 127.0.0.1 成功, recordingId=' + lis.recordingId);
    await call('tcp_simulate_set', { port: PORT, rules: [{ match: '*' }] }); // echo
    const cli = net.createConnection({ host: '127.0.0.1', port: PORT });
    const echoed = await new Promise((resolve) => {
      cli.on('data', (b) => resolve(b.toString()));
      cli.write('hello-echo');
    });
    ok(echoed === 'hello-echo', 'echo 规则原样回显, got=' + JSON.stringify(echoed));
    cli.end();
  }

  /* == 2. 客户端 connect/send 收发（对端 echo server） == */
  console.log('== 2. tcp_connect / tcp_send 双向收发 ==');
  {
    const ECHO_PORT = 42102;
    const backend = await rawServer(ECHO_PORT, (sock, b) => sock.write('BACK:' + b));
    const conn = await call('tcp_connect', { host: '127.0.0.1', port: ECHO_PORT });
    ok(conn.ok && /^c-\d+$/.test(conn.connectionId), 'tcp_connect 返回 connectionId=' + conn.connectionId);
    const sent = call('tcp_send', { connectionId: conn.connectionId, data: 'ping-1' });
    ok(sent.sent === 6, 'tcp_send 写入 6 字节, got=' + sent.sent);
    const tr = await nextTraffic(conn.connectionId, 'in', 2000);
    ok(tr.length > 0 && tr.payloadTruncated === false, '收到对端回包 traffic(in), length=' + tr.length);
    backend.server.close();
  }

  /* == 3. 代理录制：proxy 转发双向流量并落 recording == */
  console.log('== 3. tcp_proxy_start 录制双向流量 ==');
  let proxyRecId = null;
  {
    const BACK_PORT = 42103;
    const PROXY_PORT = 42104;
    const backend = await rawServer(BACK_PORT, (sock, b) => sock.write('ACK:' + b));
    const proxy = await call('tcp_proxy_start', { listenPort: PROXY_PORT, targetHost: '127.0.0.1', targetPort: BACK_PORT });
    ok(proxy.ok && /^proxy-\d+$/.test(proxy.proxyId), 'proxy 启动 proxyId=' + proxy.proxyId);
    // 记录代理前的事件数，取新 recording
    const cli = net.createConnection({ host: '127.0.0.1', port: PROXY_PORT });
    const reply = await new Promise((res) => { cli.on('data', (b) => res(b.toString())); cli.write('req-through-proxy'); });
    ok(reply === 'ACK:req-through-proxy', '代理转发并由后端回包, reply=' + JSON.stringify(reply));
    cli.end();
    // 录制在 proxy 内 newRecording，经 connected 事件带 recordingId；取最新一条
    const connEv = events.filter(e => e.subtype === 'connected' && e.role === 'proxy-front').pop();
    proxyRecId = connEv.recordingId;
    ok(proxyRecId && proxyRecId.startsWith('rec-'), '代理产生 recordingId=' + proxyRecId);
    await call('tcp_proxy_stop', { proxyId: proxy.proxyId });
    backend.server.close();
  }

  /* == 4. 规则模拟：匹配回复 / 延迟 / 断连 == */
  console.log('== 4. 规则模拟（匹配/延迟/断连） ==');
  {
    const PORT = 42105;
    await call('tcp_listen', { port: PORT });
    await call('tcp_simulate_set', { port: PORT, rules: [
      { match: 'foo', reply: 'bar' },
      { match: 'slow', reply: 'late', delay: 250 },
      { match: 'bye', reply: 'gone', disconnect: true },
    ]});
    // 匹配回复
    let c1 = net.createConnection({ host: '127.0.0.1', port: PORT });
    const r1 = await new Promise((res) => { c1.on('data', (b) => res(b.toString())); c1.write('xxxfoo'); });
    ok(r1 === 'bar', '匹配 "foo" 回复 "bar", got=' + JSON.stringify(r1));
    c1.end();
    // 延迟
    let c2 = net.createConnection({ host: '127.0.0.1', port: PORT });
    const t0 = Date.now();
    const r2 = await new Promise((res) => { c2.on('data', (b) => res(b.toString())); c2.write('slow'); });
    const el = Date.now() - t0;
    ok(r2 === 'late' && el >= 200, '延迟 250ms 后回复 "late", 耗时=' + el + 'ms');
    c2.end();
    // 断连：发送 bye，先收到回复 "gone"，随后连接被断开
    let c3 = net.createConnection({ host: '127.0.0.1', port: PORT });
    const gotReply = await new Promise((res) => { c3.once('data', (b) => res(b.toString())); c3.write('bye'); });
    const waitClose = await new Promise((res) => { c3.once('close', () => res(true)); setTimeout(() => res(false), 1000); });
    ok(gotReply === 'gone', '断连前先回复 "gone", got=' + JSON.stringify(gotReply));
    ok(waitClose === true, 'disconnect=true 回复后连接关闭');
    await call('tcp_simulate_clear', { port: PORT });
  }

  /* == 5. 录制回放：把代理录制的 c2s 帧重放到新目标 == */
  console.log('== 5. tcp_replay 回放 ==');
  {
    const TARGET_PORT = 42106;
    const seen = [];
    const target = await rawServer(TARGET_PORT, (sock, b) => seen.push(b.toString()));
    ok(proxyRecId, '使用上一步代理录制 ' + proxyRecId);
    const rep = await call('tcp_replay', { recordingId: proxyRecId, targetHost: '127.0.0.1', targetPort: TARGET_PORT, speed: 0 });
    ok(rep.ok && rep.sentFrames >= 1, '回放发送 c2s 帧数=' + rep.sentFrames);
    ok(seen.includes('req-through-proxy'), '新目标收到回放报文: ' + JSON.stringify(seen));
    target.server.close();
  }

  /* == 6. fuzz 不崩 == */
  console.log('== 6. tcp_fuzz 边界报文不崩 ==');
  {
    const PORT = 42107;
    const backend = await rawServer(PORT, (sock) => sock.end()); // 收到即断
    const fz = await call('tcp_fuzz', { host: '127.0.0.1', port: PORT, count: 6 });
    ok(fz.ok && fz.probes === 6, 'fuzz 跑 6 个探测, probes=' + fz.probes);
    ok(Array.isArray(fz.results) && fz.results.length === 6, '返回逐项结果数组');
    ok(fz.results.some(r => r.name === 'overlong-64k'), '含超长用例 overlong-64k');
    backend.server.close();
  }

  /* == 7. 关闭与端口释放 == */
  console.log('== 7. tcp_close / stop() 释放端口 ==');
  {
    const PORT = 42108;
    await call('tcp_listen', { port: PORT });
    // stop() 应关闭所有 listener/connection
    mod.stop();
    await wait(150);
    // 同端口应可再次绑定（说明已释放）
    let freed = false;
    try {
      const s = net.createServer();
      await new Promise((resolve, reject) => {
        s.on('error', reject);
        s.listen(PORT, '127.0.0.1', resolve);
      });
      s.close();
      freed = true;
    } catch (_) { freed = false; }
    ok(freed, 'stop() 后端口可重新绑定（已释放）');
  }

  console.log('\n========================================');
  console.log('通过 ' + pass + ' / ' + (pass + fail));
  if (fail) { console.log('失败: ' + failures.join('; ')); process.exit(1); }
  console.log('ALL_TESTS_PASS');
  process.exit(0);
})().catch((e) => { console.error('TEST_FATAL', e); process.exit(1); });
