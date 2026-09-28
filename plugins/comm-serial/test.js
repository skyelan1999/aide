'use strict';
/* comm-serial 插件集成测试：直接 require 插件模块、构造协议 v1.2 假 ctx、驱动各 handler。
 *
 * 运行（容器内，/workspace 挂载仓库根）：
 *   docker run --rm -v "$PWD":/workspace -w /workspace node:24-bookworm-slim \
 *     bash -c "cd plugins/comm-serial && npm install --no-audit --no-fund && apt-get update -qq && apt-get install -y -qq socat >/dev/null && node test.js"
 *
 * 无 socat 时插件会自动降级到 python3 pty 桥接；两者都不可用则虚拟串口用例记 NOT_RUN 并通过。
 * 真实串口硬件不在容器内，相关用例恒为 NOT_RUN。
 */

const mod = require('./index.js');
const tools = new Map();
const events = [];
const fakeCtx = {
  logger: { info() {}, warn() {}, error() {} },
  tool(def) { tools.set(def.name, def); },
  provide() {}, slot() {},
  emit(type, fields) { events.push(Object.assign({ type }, fields)); },
};
mod.apply(fakeCtx);
const call = (name, args) => tools.get(name).handler(args || {});
const state = mod.__state;

let pass = 0, fail = 0, notRun = 0;
const failures = [];
function ok(cond, name) {
  if (cond) { pass++; console.log('  ✓', name); }
  else { fail++; failures.push(name); console.log('  ✗', name); }
}
function skip(name, why) { notRun++; console.log('  - NOT_RUN', name + '（' + why + '）'); }

/* 轮询等待端口收到期望字节 */
function waitIncoming(path, expectStr, timeoutMs) {
  return new Promise((resolve) => {
    const t0 = Date.now();
    const iv = setInterval(() => {
      const st = state.ports.get(path);
      const text = st ? Buffer.concat(st.incoming).toString('utf8') : '';
      if (text.includes(expectStr) || Date.now() - t0 > (timeoutMs || 2000)) {
        clearInterval(iv);
        resolve(text);
      }
    }, 20);
  });
}

(async () => {
  console.log('== 1. serial_list 无真实设备时返回数组、不报错 ==');
  {
    const r = await call('serial_list');
    ok(Array.isArray(r.ports), 'serial_list 返回数组（长度 ' + r.ports.length + '）');
  }

  console.log('== 2. 创建虚拟串口对 ==');
  let pair = null;
  try {
    pair = await call('serial_virtual_create');
    ok(pair && pair.pairId && pair.masterPath && pair.slavePath,
      '虚拟对创建成功 backend=' + pair.backend + ' ' + pair.masterPath + ' <-> ' + pair.slavePath);
  } catch (e) {
    skip('虚拟串口回环', '无可用后端（socat/python3 均缺失）：' + e.message);
  }

  if (pair) {
    console.log('== 3. 打开两端并做 utf8 回环 ==');
    await call('serial_open', { path: pair.masterPath, baudRate: 115200 });
    await call('serial_open', { path: pair.slavePath, baudRate: 115200 });
    ok(state.ports.has(pair.masterPath) && state.ports.has(pair.slavePath),
      '两端均已打开（ports Map 含两条）');
    const mst = state.ports.get(pair.masterPath);
    ok(mst.config.baudRate === 115200 && mst.config.dataBits === 8 && mst.config.stopBits === 1 && mst.config.parity === 'none',
      '串口参数回显正确（baudRate=115200,dataBits=8,stopBits=1,parity=none）');

    await call('serial_write', { path: pair.masterPath, data: 'hello-serial\n', format: 'utf8' });
    const slaveText = await waitIncoming(pair.slavePath, 'hello-serial', 3000);
    ok(slaveText.includes('hello-serial'), 'master 写入 utf8，slave 收到（实测: "' + slaveText.trim() + '"）');

    console.log('== 4. hex 格式回环 ==');
    await call('serial_write', { path: pair.slavePath, data: 'deadbeef', format: 'hex' });
    const masterBuf = await new Promise((resolve) => {
      const t0 = Date.now();
      const iv = setInterval(() => {
        const st = state.ports.get(pair.masterPath);
        const hex = st ? Buffer.concat(st.incoming).toString('hex') : '';
        if (hex.includes('deadbeef') || Date.now() - t0 > 3000) { clearInterval(iv); resolve(hex); }
      }, 20);
    });
    ok(masterBuf.includes('deadbeef'), 'slave 写 hex(deadbeef)，master 收到对应字节（hex: ' + masterBuf + '）');

    console.log('== 5. traffic 事件上报（in/out，长度计数，不含正文） ==');
    const outEvt = events.find((e) => e.type === 'traffic' && e.direction === 'out');
    const inEvt = events.find((e) => e.type === 'traffic' && e.direction === 'in');
    ok(outEvt && outEvt.length > 0 && outEvt.payloadTruncated === true, 'out 流量事件带长度且 payloadTruncated=true');
    ok(inEvt && inEvt.length > 0 && inEvt.payloadTruncated === true, 'in 流量事件带长度且 payloadTruncated=true');

    console.log('== 6. serial_replay 按帧重放 ==');
    const recId = 'rec-1';
    state.recordings.set(recId, [
      { delay: 0, data: 'FRAME-A' },
      { delay: 50, data: 'FRAME-B' },
    ]);
    const rep = await call('serial_replay', { path: pair.masterPath, recordingId: recId, speed: 2 });
    ok(rep.ok && rep.frames === 2 && rep.bytesSent === ('FRAME-A' + 'FRAME-B').length,
      'replay 重放 2 帧共 ' + rep.bytesSent + ' 字节');
    const slaveReplay = await waitIncoming(pair.slavePath, 'FRAME-A', 3000);
    ok(slaveReplay.includes('FRAME-A') && slaveReplay.includes('FRAME-B'), 'slave 收到重放两帧');

    console.log('== 7. serial_close 释放端口 ==');
    await call('serial_close', { path: pair.masterPath });
    await new Promise((r) => setTimeout(r, 200));
    ok(!state.ports.has(pair.masterPath), 'close 后 master 从 ports Map 移除');
    let reopen = false;
    try {
      await call('serial_open', { path: pair.masterPath });
      reopen = state.ports.has(pair.masterPath);
      await call('serial_close', { path: pair.masterPath });
    } catch (_) {}
    ok(reopen, '关闭后可重新打开（端口已释放）');

    console.log('== 8. serial_virtual_destroy 清理 ==');
    await call('serial_virtual_destroy', { pairId: pair.pairId });
    await new Promise((r) => setTimeout(r, 300));
    ok(!state.virtualPairs.has(pair.pairId), 'destroy 后虚拟对从 Map 移除');
  } else {
    skip('串口参数回环', '无虚拟对');
    skip('hex 回环', '无虚拟对');
    skip('traffic 事件', '无虚拟对');
    skip('replay 重放', '无虚拟对');
    skip('close/reopen', '无虚拟对');
    skip('virtual_destroy', '无虚拟对');
  }

  console.log('== 9. stop() 清理所有端口与虚拟对 ==');
  if (pair) {
    try { await call('serial_virtual_create'); } catch (_) {}
  }
  await mod.stop();
  ok(state.ports.size === 0 && state.virtualPairs.size === 0, 'stop() 后 ports/virtualPairs 均清空');

  console.log('\n========================================');
  console.log('通过 ' + pass + ' / ' + (pass + fail) + '，NOT_RUN ' + notRun);
  if (fail) { console.log('失败用例: ' + failures.join('; ')); process.exit(1); }
  console.log('ALL_TESTS_PASS');
})().catch((e) => {
  console.error('测试异常:', e);
  process.exit(1);
});
