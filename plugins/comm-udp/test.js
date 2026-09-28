'use strict';
/* comm-udp 集成测试：直接 require 插件模块、构造假 ctx、驱动各 handler（容器内 node 直接跑）。
 *
 * 运行（容器内，/workspace 挂载仓库根）：
 *   docker run --rm --network=host -v "$PWD":/workspace -w /workspace node:24-bookworm-slim node plugins/comm-udp/test.js
 * 全部用 127.0.0.1 本地回环（组播用本地回环地址 239.0.0.1），不依赖外网。
 */
const dgram = require('dgram');

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
const lastInTraffic = () => [...events].reverse().find(e => e.type === 'traffic' && e.direction === 'in');

let pass = 0, fail = 0; const failures = [];
function ok(cond, name) { if (cond) { pass++; console.log('  ✓', name); } else { fail++; failures.push(name); console.log('  ✗', name); } }

// 起一个原始 UDP socket，收到报文时 push 到 received
function rawUdp(port) {
  const received = [];
  const sock = dgram.createSocket({ type: 'udp4', reuseAddr: true });
  return new Promise((resolve) => sock.bind(port, '127.0.0.1', () => {
    sock.on('message', (b, rinfo) => received.push({ text: b.toString(), rinfo }));
    resolve({ sock, received });
  }));
}

(async () => {
  /* == 1. bind + echo 模拟 == */
  console.log('== 1. udp_bind + echo 模拟应答 ==');
  let boundSockId;
  {
    const PORT = 43101;
    const b = await call('udp_bind', { port: PORT });
    ok(b.ok && b.host === '127.0.0.1', 'udp_bind 绑 127.0.0.1, socketId=' + b.socketId);
    boundSockId = b.socketId;
    await call('udp_simulate_set', { port: PORT, rules: [{ match: '*' }] });
    // 用原始 socket 发数据并等回显
    const cli = dgram.createSocket('udp4');
    const echo = await new Promise((res, reject) => {
      const t0 = Date.now();
      cli.on('message', (msg) => res(msg.toString()));
      setInterval(() => { if (Date.now() - t0 > 2000) reject(new Error('echo 超时')); }, 20);
      cli.send('hello-udp', PORT, '127.0.0.1');
    });
    ok(echo === 'hello-udp', 'echo 规则原样回显, got=' + JSON.stringify(echo));
    cli.close();
    await wait(50);
    const tr = lastInTraffic();
    ok(tr && tr.socketId === boundSockId && tr.length === 9, '收包 traffic(in) 上报, length=' + (tr && tr.length));
  }

  /* == 2. udp_send 发送到原始接收端 == */
  console.log('== 2. udp_send 发送 ==');
  {
    const PORT = 43102;
    const rx = await rawUdp(PORT);
    await call('udp_send', { host: '127.0.0.1', port: PORT, data: 'packet-from-plugin' });
    await wait(150);
    ok(rx.received.length === 1 && rx.received[0].text === 'packet-from-plugin', '原始接收端收到插件发送的报文: ' + JSON.stringify(rx.received.map(r => r.text)));
    rx.sock.close();
  }

  /* == 3. 匹配回复（非 echo） == */
  console.log('== 3. 规则匹配回复 ==');
  {
    const PORT = 43103;
    await call('udp_bind', { port: PORT });
    await call('udp_simulate_set', { port: PORT, rules: [{ match: 'ping', reply: 'pong' }] });
    const cli = dgram.createSocket('udp4');
    const rep = await new Promise((res, reject) => {
      const t0 = Date.now();
      cli.on('message', (msg) => res(msg.toString()));
      const iv = setInterval(() => { if (Date.now() - t0 > 2000) { clearInterval(iv); reject(new Error('匹配回复超时')); } }, 20);
      cli.send('xxxpingxxx', PORT, '127.0.0.1');
    });
    ok(rep === 'pong', '匹配 "ping" 回复 "pong", got=' + JSON.stringify(rep));
    cli.close();
    await call('udp_simulate_clear', { port: PORT });
  }

  /* == 4. 组播（本地回环） == */
  console.log('== 4. 组播 join/leave ==');
  {
    const PORT = 43104;
    const GROUP = '239.0.0.1';
    const b = await call('udp_bind', { port: PORT, multicast: true });
    const join = await call('udp_multicast_join', { address: GROUP, port: PORT });
    ok(join.ok, '加入组播组 ' + GROUP);
    // 发送组播数据报到 group:port（绑定临时端口并把组播出口限定回环）
    const sender = dgram.createSocket({ type: 'udp4', reuseAddr: true });
    await new Promise(res => sender.bind(0, '127.0.0.1', res));
    sender.setMulticastLoopback(true);
    sender.setMulticastInterface('127.0.0.1');
    const before = events.length;
    await new Promise((res) => sender.send('mcast', PORT, GROUP, res));
    await wait(200);
    const mcastHit = events.slice(before).some(e => e.type === 'traffic' && e.direction === 'in' && e.length === 5);
    ok(mcastHit, '绑定 socket 收到组播报文');
    const leave = await call('udp_multicast_leave', { address: GROUP, port: PORT });
    ok(leave.ok, '离开组播组');
    sender.close();
  }

  /* == 5. udp_close / stop() 释放 == */
  console.log('== 5. udp_close / stop() 释放端口 ==');
  {
    const PORT = 43105;
    const b = await call('udp_bind', { port: PORT });
    const closed = call('udp_close', { socketId: b.socketId });
    ok(closed.closed === 1, 'udp_close 关闭 1 个 socket');
    // stop() 应关闭剩余所有 socket（含 43101/43103/43104）
    mod.stop();
    await wait(100);
    // 同端口应可再次绑定
    let freed = false;
    try {
      const s = dgram.createSocket({ type: 'udp4', reuseAddr: true });
      await new Promise((resolve, reject) => { s.on('error', reject); s.bind(PORT, '127.0.0.1', resolve); });
      s.close(); freed = true;
    } catch (_) { freed = false; }
    ok(freed, 'stop() 后 UDP 端口可重新绑定（已释放）');
  }

  console.log('\n========================================');
  console.log('通过 ' + pass + ' / ' + (pass + fail));
  if (fail) { console.log('失败: ' + failures.join('; ')); process.exit(1); }
  console.log('ALL_TESTS_PASS');
  process.exit(0);
})().catch((e) => { console.error('TEST_FATAL', e); process.exit(1); });
