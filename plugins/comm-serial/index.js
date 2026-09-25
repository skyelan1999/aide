'use strict';
/* aide 插件协议 v1.2 daemon 插件：comm-serial（串口通讯）
 *
 * 基于 serialport（@serialport/bindings-cpp）提供真实串口的常驻读写；
 * 另提供虚拟串口对（pty）用于在无硬件时测试/模拟设备。
 *
 * 工具：
 *   serial_list              列出可用串口（无设备时返回空数组，不报错）
 *   serial_open              打开并配置一个串口
 *   serial_write             向串口写数据（utf8|hex）
 *   serial_close             关闭指定/全部串口
 *   serial_virtual_create     创建一对互联的虚拟串口（pty），返回两个可打开的路径
 *   serial_virtual_destroy   销毁虚拟串口对
 *   serial_replay            向已打开串口按帧重放数据
 *
 * 事件：收发流量 ctx.emit('traffic', {direction,length,path,payloadTruncated:true})；
 *       打开/关闭/错误 ctx.emit('event', {subtype,message})。默认不上报报文正文。
 *
 * 虚拟串口后端优先级：socat（若在 PATH） > python3 标准库 os.openpty 桥接 > 报错。
 */
const { spawn } = require('child_process');
const fs = require('fs');

/* ---------- 状态（daemon 常驻内存） ---------- */
const ports = new Map();        // path -> { port, config, incoming:[Buffer], incomingBytes }
const virtualPairs = new Map(); // pairId -> { proc, backend, masterPath, slavePath }
const recordings = new Map();    // recordingId -> frames[]（进程内，不落盘）
let pairSeq = 0;

/* serialport 原生模块懒加载：缺失时 daemon 仍可启动，首次调用串口工具才报错。 */
let _SerialPort = null;
let _bindingError = null;
function loadSerialport() {
  if (_SerialPort) return _SerialPort;
  if (_bindingError) throw _bindingError;
  try {
    _SerialPort = require('serialport').SerialPort;
    return _SerialPort;
  } catch (e) {
    _bindingError = new Error('serialport 原生模块不可用：' + String(e && e.message));
    throw _bindingError;
  }
}

/* ---------- 小工具 ---------- */
function toInt(v, dflt) {
  const n = Number(v);
  return Number.isFinite(n) && n > 0 ? Math.trunc(n) : dflt;
}
function pick(v, allowed, dflt) {
  return allowed.includes(v) ? v : dflt;
}
function which(bin) {
  return new Promise((resolve) => {
    let out = '';
    let p;
    try { p = spawn('sh', ['-c', 'command -v ' + bin]); } catch (_) { return resolve(''); }
    if (!p.stdout) return resolve('');
    p.stdout.on('data', (d) => { out += d; });
    p.on('error', () => resolve(''));
    p.on('close', () => resolve(out.trim()));
  });
}
function mustPort(path) {
  const st = ports.get(path);
  if (!st) throw new Error('串口未打开: ' + path);
  return st;
}
/* 收包缓冲上限 64KB：超出丢最旧。生产事件只上报长度，不发正文。 */
function pushIncoming(st, chunk) {
  st.incoming.push(Buffer.from(chunk));
  st.incomingBytes += chunk.length;
  while (st.incomingBytes > 65536 && st.incoming.length) {
    const drop = st.incoming.shift();
    st.incomingBytes -= drop.length;
  }
}

/* ---------- Python 标准库 pty 桥接脚本（socat 不可用时的降级后端） ----------
 * 创建两个 pty 并双向中继：s1<->m1, s2<->m2；两个 slave 路径经 stdout 前两行打印。
 * SerialPort 打开 slave 路径即可：写 s1 端 → m1 读到 → 中继到 m2 → s2 端可读。 */
const PY_BRIDGE = [
  'import os, sys, select, tty',
  'm1, s1 = os.openpty()',
  'm2, s2 = os.openpty()',
  'for s in (s1, s2):',
  '    try: tty.setraw(s)',
  '    except Exception: pass',
  'sys.stdout.write(os.ttyname(s1) + "\\n" + os.ttyname(s2) + "\\n")',
  'sys.stdout.flush()',
  'while True:',
  '    r, _, _ = select.select([m1, m2], [], [])',
  '    if m1 in r:',
  '        d = os.read(m1, 4096)',
  '        if not d: break',
  '        os.write(m2, d)',
  '    if m2 in r:',
  '        d = os.read(m2, 4096)',
  '        if not d: break',
  '        os.write(m1, d)',
].join('\n');

/* ---------- ctx 注入点（apply 时绑定） ---------- */
let ctx = null;
function emitTraffic(direction, length, path) {
  if (ctx) ctx.emit('traffic', { direction, length, path, payloadTruncated: true });
}
function emitEvent(subtype, message) {
  if (ctx) ctx.emit('event', { subtype: String(subtype), message: String(message) });
}

function attachPort(path, port, config) {
  const st = { port, config, incoming: [], incomingBytes: 0 };
  ports.set(path, st);
  port.on('data', (buf) => {
    pushIncoming(st, buf);
    emitTraffic('in', buf.length, path);
  });
  port.on('open', () => emitEvent('open', path + ' opened'));
  port.on('error', (err) => emitEvent('error', path + ': ' + (err && err.message)));
  port.on('close', () => {
    emitEvent('close', path + ' closed');
    ports.delete(path);
  });
}

async function closePort(path) {
  const st = ports.get(path);
  if (!st) return;
  await new Promise((res) => {
    try { st.port.close(() => res()); } catch (_) { res(); }
  });
  ports.delete(path);
}

/* ---------- 虚拟串口对后端 ---------- */
/* 就绪判定：symlink 已存在并指向一个真实字符设备（tty）。 */
function ttyReady(p) {
  try { return fs.existsSync(p) && fs.statSync(p).isCharacterDevice(); }
  catch (_) { return false; }
}

function socatPair(linkA, linkB) {
  return new Promise((resolve, reject) => {
    let proc;
    try {
      proc = spawn('socat', [
        'pty,raw,echo=0,link=' + linkA,
        'pty,raw,echo=0,link=' + linkB,
      ]);
    } catch (e) { return reject(e); }
    proc.stderr && proc.stderr.resume();   // 排空 stderr，避免管道阻塞
    let settled = false;
    const done = (err, val) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      clearInterval(poll);
      err ? reject(err) : resolve(val);
    };
    // 轮询文件系统判定就绪（不依赖 socat 日志格式）。
    const poll = setInterval(() => {
      if (ttyReady(linkA) && ttyReady(linkB)) {
        const pairId = 'vp-' + (++pairSeq);
        virtualPairs.set(pairId, { proc, backend: 'socat', masterPath: linkA, slavePath: linkB });
        done(null, { pairId, masterPath: linkA, slavePath: linkB, backend: 'socat' });
      }
    }, 50);
    const timer = setTimeout(() => done(new Error('socat 超时未创建 pty')), 8000);
    proc.on('error', (e) => done(e));
    proc.on('exit', (code) => {
      for (const [id, pair] of virtualPairs) {
        if (pair.proc === proc) virtualPairs.delete(id);
      }
      if (!settled) done(new Error('socat 异常退出 code=' + code));
    });
  });
}

function pythonPtyPair(pyBin) {
  return new Promise((resolve, reject) => {
    let proc;
    try { proc = spawn(pyBin, ['-c', PY_BRIDGE]); } catch (e) { return reject(e); }
    let buf = '';
    let settled = false;
    const done = (err, val) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      err ? reject(err) : resolve(val);
    };
    const timer = setTimeout(() => done(new Error('python pty 桥接超时')), 5000);
    proc.stdout.on('data', (chunk) => {
      buf += chunk.toString();
      const lines = buf.split('\n');
      if (lines.length >= 3) {
        const a = (lines[0] || '').trim();
        const b = (lines[1] || '').trim();
        if (a && b) {
          const pairId = 'vp-' + (++pairSeq);
          virtualPairs.set(pairId, { proc, backend: 'python-pty', masterPath: a, slavePath: b });
          done(null, { pairId, masterPath: a, slavePath: b, backend: 'python-pty' });
        }
      }
    });
    proc.on('error', (e) => done(e));
    proc.on('exit', () => {
      for (const [id, pair] of virtualPairs) {
        if (pair.proc === proc) virtualPairs.delete(id);
      }
    });
  });
}

async function createVirtualPair() {
  const linkDir = '/tmp';
  const tag = process.pid + '-' + (++pairSeq);
  if (await which('socat')) {
    return socatPair(linkDir + '/aide-comm-serial-' + tag + '-a', linkDir + '/aide-comm-serial-' + tag + '-b');
  }
  const py = await which('python3') || await which('python');
  if (py) return pythonPtyPair(py);
  throw new Error('无虚拟串口后端：请安装 socat 或 python3（测试容器内可 apt-get install -y socat）');
}

/* ---------- 插件对象 ---------- */
module.exports = {
  name: 'comm-serial',
  daemon: true,

  apply(c) {
    ctx = c;

    c.tool({
      name: 'serial_list',
      description: '列出系统可用串口（path/manufacturer/serialNumber/vendorId/productId）；无设备或无权限时返回空数组',
      handler: async () => {
        try {
          const SerialPort = loadSerialport();
          const list = await SerialPort.list();
          return {
            ports: list.map((p) => ({
              path: p.path,
              manufacturer: p.manufacturer || null,
              serialNumber: p.serialNumber || null,
              vendorId: p.vendorId || null,
              productId: p.productId || null,
            })),
          };
        } catch (e) {
          return { ports: [], error: String(e && e.message) };
        }
      },
    });

    c.tool({
      name: 'serial_open',
      description: '打开并配置串口。参数：path, baudRate(默认9600), dataBits(5-8,默认8), stopBits(1|1.5|2,默认1), parity(none|even|odd,默认none), flowControl(默认false)',
      handler: async (args) => {
        const SerialPort = loadSerialport();
        const path = String((args && args.path) || '').trim();
        if (!path) throw new Error('path 必填');
        if (ports.has(path)) throw new Error('已打开: ' + path);
        const config = {
          baudRate: toInt(args.baudRate, 9600),
          dataBits: pick(toInt(args.dataBits, 8), [5, 6, 7, 8], 8),
          stopBits: pick(Number(args.stopBits) || 1, [1, 1.5, 2], 1),
          parity: pick(String(args.parity || 'none').toLowerCase(), ['none', 'even', 'odd', 'mark', 'space'], 'none'),
          flowControl: !!args.flowControl,
          autoOpen: true,
        };
        const port = new SerialPort({ path, ...config });
        attachPort(path, port, config);
        await new Promise((resolve, reject) => {
          port.once('open', resolve);
          port.once('error', reject);
        });
        emitEvent('open', path + ' baudRate=' + config.baudRate);
        return { ok: true, path, config };
      },
    });

    c.tool({
      name: 'serial_write',
      description: '向已打开串口写数据。参数：path, data(字符串), format(utf8|hex,默认utf8)',
      handler: async (args) => {
        const path = String((args && args.path) || '');
        const st = mustPort(path);
        const format = args.format === 'hex' ? 'hex' : 'utf8';
        const data = Buffer.from(String((args && args.data) ?? ''), format);
        if (data.length === 0) throw new Error('data 为空');
        await new Promise((resolve, reject) => {
          st.port.write(data, (err) => (err ? reject(err) : resolve()));
        });
        emitTraffic('out', data.length, path);
        return { ok: true, path, bytesWritten: data.length };
      },
    });

    c.tool({
      name: 'serial_close',
      description: '关闭串口。传 path 关闭单个，不传则关闭全部已打开串口',
      handler: async (args) => {
        const targets = (args && args.path) ? [String(args.path)] : Array.from(ports.keys());
        for (const p of targets) await closePort(p);
        return { ok: true, closed: targets };
      },
    });

    c.tool({
      name: 'serial_virtual_create',
      description: '创建一对互联的虚拟串口(pty)，返回 {pairId, masterPath, slavePath, backend}；用于测试/模拟设备，无需硬件',
      handler: async () => createVirtualPair(),
    });

    c.tool({
      name: 'serial_virtual_destroy',
      description: '销毁虚拟串口对。参数：pairId；会一并关闭两端已打开的串口',
      handler: async (args) => {
        const pairId = String((args && args.pairId) || '');
        const pair = virtualPairs.get(pairId);
        if (!pair) throw new Error('未知 pairId: ' + pairId);
        for (const p of [pair.masterPath, pair.slavePath]) await closePort(p);
        try { pair.proc.kill('SIGTERM'); } catch (_) {}
        virtualPairs.delete(pairId);
        return { ok: true, pairId };
      },
    });

    c.tool({
      name: 'serial_replay',
      description: '向已打开串口按帧重放数据。参数：path, frames[{delay(ms), data, format?}], speed(倍速,默认1), recordingId(可选，复用进程内录制)',
      handler: async (args) => {
        const path = String((args && args.path) || '');
        const st = mustPort(path);
        const speed = Math.max(0.05, Number((args && args.speed)) || 1);
        let frames = args && args.frames;
        if (!frames && args && args.recordingId) frames = recordings.get(String(args.recordingId));
        if (!Array.isArray(frames) || !frames.length) throw new Error('frames 或 recordingId 必填且非空');
        let sent = 0;
        for (const f of frames) {
          const delay = (Number(f.delay) || 0) / speed;
          if (delay > 0) await new Promise((r) => setTimeout(r, delay));
          const buf = Buffer.from(String(f.data ?? ''), f.format === 'hex' ? 'hex' : 'utf8');
          await new Promise((res, rej) => st.port.write(buf, (e) => (e ? rej(e) : res())));
          sent += buf.length;
          emitTraffic('out', buf.length, path);
        }
        return { ok: true, path, bytesSent: sent, frames: frames.length };
      },
    });
  },

  start() { /* 懒加载：串口在 serial_open 时才打开 */ },

  async stop() {
    for (const p of Array.from(ports.keys())) await closePort(p);
    for (const [, pair] of virtualPairs) {
      try { pair.proc.kill('SIGTERM'); } catch (_) {}
    }
    virtualPairs.clear();
  },

  /* 测试/调试句柄：暴露进程内状态（不经过工具 RPC，仅供容器内测试脚本读取）。 */
  __state: { ports, virtualPairs, recordings },
};
