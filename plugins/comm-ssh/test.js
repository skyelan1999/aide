'use strict';
/* comm-ssh 插件端到端测试：直接 require 插件模块、构造假 ctx、用 ssh2 模拟服务端做本地回环。
 *
 * 运行（容器内，/workspace 挂载仓库根；ssh2 已 npm install 到本目录 node_modules）：
 *   docker run --rm -v "$PWD":/workspace -w /workspace/plugins/comm-ssh node:24-bookworm-slim node test.js
 * 本地等价：node plugins/comm-ssh/test.js
 *
 * 全部流量走 127.0.0.1，不依赖外网 SSH。
 */
const fs = require('fs');
const os = require('os');
const path = require('path');
const net = require('net');
const crypto = require('crypto');

const mod = require('./index.js');
const tools = new Map();
const emitted = [];
const fakeCtx = {
  logger: { info() {}, warn() {}, error() {} },
  tool(def) { tools.set(def.name, def); },
  emit(type, fields) { emitted.push(Object.assign({ type }, fields || {})); },
  provide() {}, slot() {},
};
mod.apply(fakeCtx);
const call = (name, args) => tools.get(name).handler(args || {});

/* ---- 断言 ---- */
let pass = 0, fail = 0;
const failures = [];
function ok(cond, name) {
  if (cond) { pass++; console.log('  ✓', name); }
  else { fail++; failures.push(name); console.log('  ✗', name); }
}
async function expectReject(name, fn) {
  try { await fn(); ok(false, name + '（未抛错）'); }
  catch (_) { ok(true, name); }
}

/* 等待一次 TCP 连接成功/失败 */
function tcpProbe(port, timeoutMs = 1500) {
  return new Promise((resolve) => {
    const sock = net.connect(port, '127.0.0.1');
    let done = false;
    const finish = (up) => { if (done) return; done = true; sock.destroy(); resolve(up); };
    sock.on('connect', () => finish(true));
    sock.on('error', () => finish(false));
    setTimeout(() => finish(false), timeoutMs);
  });
}

const TMP = fs.mkdtempSync(path.join(os.tmpdir(), 'comm-ssh-test-'));

async function main() {
  console.log('== 0. 工具已注册（11 个）==');
  const expectedTools = ['ssh_connect','ssh_exec','ssh_sftp_list','ssh_sftp_get','ssh_sftp_put',
    'ssh_tunnel_start','ssh_tunnel_stop','ssh_close','ssh_simulate_start','ssh_simulate_stop','ssh_simulate_set_rule'];
  ok(expectedTools.every(t => tools.has(t)), '注册了全部 11 个工具: ' + [...tools.keys()].join(','));

  /* 1. 模拟服务端 + 密码连接 + exec 规则应答 */
  console.log('== 1. 模拟服务端 / 密码连接 / exec 规则 ==');
  const sim1 = await call('ssh_simulate_start', { port: 0, expectedPassword: 'secret' });
  ok(sim1.serverId && sim1.port > 0, '模拟服务端启动，端口 ' + sim1.port);
  await call('ssh_simulate_set_rule', { serverId: sim1.serverId, rules: [
    { commandMatch: 'hello', reply: 'hi-from-mock', exitCode: 0 },
  ]});
  const c1 = await call('ssh_connect', { host: '127.0.0.1', port: sim1.port, username: 'tester', authType: 'password', password: 'secret' });
  ok(c1.connectionId && c1.authType === 'password', '密码连接成功: ' + c1.connectionId);
  const r1 = await call('ssh_exec', { connectionId: c1.connectionId, command: 'hello' });
  ok(r1.stdout === 'hi-from-mock' && r1.exitCode === 0, '规则应答 stdout/exitCode 正确: [' + r1.stdout + '/' + r1.exitCode + ']');
  const r1b = await call('ssh_exec', { connectionId: c1.connectionId, command: 'no-such-cmd' });
  ok(r1b.exitCode === 127, '无规则命令返回 exitCode=127');

  /* 2. 认证失败（错误密码） */
  console.log('== 2. 认证失败正确报错 ==');
  await expectReject('错误密码被拒绝', () => call('ssh_connect', { host: '127.0.0.1', port: sim1.port, username: 'tester', authType: 'password', password: 'WRONG' }));

  /* 3. SFTP：上传→列目录→下载回，内容一致 */
  console.log('== 3. SFTP put/list/get 往返一致 ==');
  const localUp = path.join(TMP, 'local-up.txt');
  const payload = 'hello-sftp-' + Date.now();
  fs.writeFileSync(localUp, payload);
  const put = await call('ssh_sftp_put', { connectionId: c1.connectionId, localPath: localUp, remotePath: '/up.txt' });
  ok(put.bytes === payload.length, '上传 bytes=' + put.bytes);
  const list = await call('ssh_sftp_list', { connectionId: c1.connectionId, path: '/' });
  ok(list.entries.some(e => e.name === 'up.txt' && e.type === 'file'), '列目录含 up.txt: ' + JSON.stringify(list.entries.map(e=>e.name)));
  const localDown = path.join(TMP, 'local-down.txt');
  const get = await call('ssh_sftp_get', { connectionId: c1.connectionId, remotePath: '/up.txt', localPath: localDown });
  ok(get.bytes === payload.length, '下载 bytes 一致');
  ok(fs.readFileSync(localDown, 'utf8') === payload, '下载内容与上传一致');

  /* 4. 端口转发：经隧道回环到一个 echo 服务 */
  console.log('== 4. 本地端口转发/隧道端到端 ==');
  let echoBuf = '';
  const echoSrv = net.createServer((s) => { s.on('data', (d) => s.end(d)); });
  await new Promise((res) => echoSrv.listen(0, '127.0.0.1', res));
  const echoPort = echoSrv.address().port;
  const tun = await call('ssh_tunnel_start', { connectionId: c1.connectionId, localPort: 0, remoteHost: '127.0.0.1', remotePort: echoPort });
  ok(tun.tunnelId && tun.localPort > 0, '隧道建立，本地端口 ' + tun.localPort);
  const echoed = await new Promise((resolve, reject) => {
    const s = net.connect(tun.localPort, '127.0.0.1');
    let data = '';
    s.on('connect', () => s.write('ping-via-tunnel'));
    s.on('data', (d) => { data += d.toString(); s.end(); });
    s.on('end', () => resolve(data));
    s.on('error', reject);
    setTimeout(() => reject(new Error('隧道超时')), 3000);
  });
  ok(echoed === 'ping-via-tunnel', '经隧道 echo 回包一致: ' + echoed);
  await call('ssh_tunnel_stop', { tunnelId: tun.tunnelId });
  const afterStop = await tcpProbe(tun.localPort);
  ok(afterStop === false, '隧道停止后本地端口已释放');
  echoSrv.close();

  /* 5. 规则：delay 与 regex */
  console.log('== 5. 模拟规则 delay / regex ==');
  await call('ssh_simulate_set_rule', { serverId: sim1.serverId, rules: [
    { commandMatch: '^greet', regex: true, reply: 'greeted' },
  ]});
  const r5 = await call('ssh_exec', { connectionId: c1.connectionId, command: 'greet-abc' });
  ok(r5.stdout === 'greeted', '正则规则命中: ' + r5.stdout);

  /* 6. 关闭连接 */
  console.log('== 6. ssh_close 关闭连接 ==');
  await call('ssh_close', { connectionId: c1.connectionId });
  let closedErr = false;
  try { await call('ssh_exec', { connectionId: c1.connectionId, command: 'x' }); }
  catch (_) { closedErr = true; }
  ok(closedErr, '关闭后再用该 connectionId 报错');

  /* 7. 私钥认证：生成测试密钥，模拟服务端开公钥模式 */
  console.log('== 7. 公钥（私钥）认证 ==');
  const kp = crypto.generateKeyPairSync('rsa', {
    modulusLength: 2048,
    publicKeyEncoding: { type: 'spki', format: 'pem' },
    privateKeyEncoding: { type: 'pkcs1', format: 'pem' },
  });
  const sim2 = await call('ssh_simulate_start', { port: 0, publickey: true, authPassword: false });
  await call('ssh_simulate_set_rule', { serverId: sim2.serverId, rules: [
    { commandMatch: 'whoami', reply: 'key-user', exitCode: 0 },
  ]});
  const c2 = await call('ssh_connect', { host: '127.0.0.1', port: sim2.port, username: 'keyuser', authType: 'publickey', privateKey: kp.privateKey });
  ok(c2.authType === 'publickey', '私钥认证连接成功');
  const r7 = await call('ssh_exec', { connectionId: c2.connectionId, command: 'whoami' });
  ok(r7.stdout === 'key-user' && r7.exitCode === 0, '公钥会话 exec 正常: ' + r7.stdout);
  await expectReject('公钥模式下密码登录被拒', () => call('ssh_connect', { host: '127.0.0.1', port: sim2.port, username: 'keyuser', authType: 'password', password: 'x' }));

  /* 8. 停止模拟服务端后端口释放 */
  console.log('== 8. ssh_simulate_stop 释放端口 ==');
  await call('ssh_close', { connectionId: c2.connectionId });
  const stopRes = await call('ssh_simulate_stop', { serverId: sim2.serverId });
  ok(stopRes.stopped === true, 'sim2 已停止');
  const sim2PortOpen = await tcpProbe(sim2.port, 800);
  ok(sim2PortOpen === false, 'sim2 端口停止后不再监听');

  /* 9. 事件上报不含凭证正文 */
  console.log('== 9. 事件/流量上报不泄露凭证 ==');
  const dumped = JSON.stringify(emitted);
  ok(!dumped.includes('secret') && !dumped.includes('BEGIN'), '事件载荷不含密码/私钥片段');
  ok(emitted.some(e => e.type === 'traffic'), '存在 traffic 流量事件');

  /* 清理 */
  await call('ssh_simulate_stop', { serverId: sim1.serverId }).catch(() => {});
  fs.rmSync(TMP, { recursive: true, force: true });
  mod.stop && mod.stop();

  console.log('\n========================================');
  console.log('通过 ' + pass + ' / ' + (pass + fail));
  if (fail) { console.log('失败用例: ' + failures.join('; ')); process.exit(1); }
  console.log('ALL_TESTS_PASS');
}

main().catch((e) => { console.error('TEST_FATAL', e); process.exit(1); });
