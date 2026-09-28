'use strict';
/* comm-tcp：常驻 TCP 通讯插件（协议 v1.2 daemon）。
 * 仅用 Node 内置 net 模块，零第三方依赖。
 *
 * 安全模型：
 *   - 安装后默认禁用（manifest enabled:false），须用户显式开启。
 *   - 所有监听/代理一律只绑回环 127.0.0.1，不暴露局域网/公网。
 *   - 不使用 raw socket / CAP_NET_RAW，不做抓包。
 *
 * 事件：收发包 ctx.emit('traffic', {direction, connectionId, recordingId, length, previewHex, payloadTruncated})；
 *       连接建立/关闭 ctx.emit('event', {subtype:'connected'|'closed'|'error', ...})。
 */
const net = require('net');

const BOUND_HOST = '127.0.0.1';        // 强制回环
const MAX_PREVIEW = 256;               // 流量事件附带的载荷预览上限（字节）

module.exports = {
  name: 'comm-tcp',
  daemon: true,

  apply(ctx) {
    const self = this;
    const emitLog = (level, message) => { try { ctx.emit('log', { level, message: String(message) }); } catch (_) {} };

    /* ---------------- 内部状态（挂到 self，供 start/stop 访问） ---------------- */
    const state = {
      listeners: new Map(),   // port -> {server, host, conns:Set<connId>, recording}
      connections: new Map(), // connId -> {socket, role, listenerPort, recordingId, remote, closed}
      proxies: new Map(),     // proxyId -> {server, listenPort, targetHost, targetPort}
      simRules: new Map(),    // port -> rules[]
      recordings: [],         // {id, port, role, frames:[{seq,dir,dataHex,t}]}
      connSeq: 0, proxySeq: 0, recSeq: 0,
    };
    self.__state = state;

    /* ---------------- 工具函数 ---------------- */
    function encodeData(data, format) {
      if (Buffer.isBuffer(data)) return data;
      if (data == null) return Buffer.alloc(0);
      if (format === 'hex') return Buffer.from(String(data), 'hex');
      return Buffer.from(String(data), 'utf8');
    }
    function preview(buf) {
      const truncated = buf.length > MAX_PREVIEW;
      const slice = truncated ? buf.subarray(0, MAX_PREVIEW) : buf;
      return { previewHex: slice.toString('hex'), truncated };
    }
    function nowIso() { return new Date().toISOString(); }
    function newRecording(port, role) {
      const rec = { id: 'rec-' + (++state.recSeq), port, role, frames: [] };
      state.recordings.push(rec);
      return rec;
    }
    function recordFrame(rec, dir, buf) {
      if (!rec) return;
      rec.frames.push({ seq: rec.frames.length, dir, dataHex: buf.toString('hex'), t: Date.now() });
    }
    function recById(id) { return state.recordings.find(r => r.id === id) || null; }

    // 注册一条 socket 连接，挂到 listener/recording，并装上报钩子
    function registerSocket(socket, opts) {
      const connectionId = 'c-' + (++state.connSeq);
      const rec = opts.recording || null;
      const entry = {
        socket, connectionId,
        role: opts.role,               // server | client | proxy-front
        listenerPort: opts.listenerPort != null ? opts.listenerPort : null,
        recordingId: rec ? rec.id : null,
        remote: socket.remoteAddress ? socket.remoteAddress + ':' + socket.remotePort : (opts.remote || ''),
        closed: false,
      };
      state.connections.set(connectionId, entry);
      if (opts.listenerConns) opts.listenerConns.add(connectionId);

      ctx.emit('event', { subtype: 'connected', connectionId, role: opts.role, listenerPort: entry.listenerPort, remote: entry.remote, recordingId: entry.recordingId, time: nowIso() });

      socket.on('data', (buf) => {
        try {
          const pv = preview(buf);
          ctx.emit('traffic', { direction: 'in', connectionId, recordingId: entry.recordingId, length: buf.length, payloadTruncated: pv.truncated, previewHex: pv.previewHex, time: nowIso() });
          recordFrame(rec, 'c2s', buf);
          if (entry.listenerPort != null) maybeSimulate(entry, buf);
        } catch (e) { emitLog('error', 'data 处理异常: ' + e.message); }
      });
      socket.on('error', (err) => {
        ctx.emit('event', { subtype: 'error', connectionId, message: String(err && err.message), time: nowIso() });
      });
      socket.on('close', () => {
        if (entry.closed) return;
        entry.closed = true;
        state.connections.delete(connectionId);
        if (opts.listenerConns) opts.listenerConns.delete(connectionId);
        ctx.emit('event', { subtype: 'closed', connectionId, role: opts.role, remote: entry.remote, time: nowIso() });
      });
      return entry;
    }

    function emitOut(entry, buf) {
      const pv = preview(buf);
      ctx.emit('traffic', { direction: 'out', connectionId: entry.connectionId, recordingId: entry.recordingId, length: buf.length, payloadTruncated: pv.truncated, previewHex: pv.previewHex, time: nowIso() });
      if (entry.recordingId) recordFrame(recById(entry.recordingId), 's2c', buf);
    }

    /* ---- 规则模拟 ---- */
    function maybeSimulate(entry, buf) {
      const rules = state.simRules.get(entry.listenerPort);
      if (!rules || !rules.length) return;
      const utf8 = buf.toString('utf8');
      const hex = buf.toString('hex');
      for (const rule of rules) {
        if (!rule || typeof rule !== 'object') continue;
        const m = rule.match;
        let matched;
        if (m === '*' || m === 'echo' || m === '') matched = true;
        else if (typeof m === 'string') matched = utf8.includes(m) || hex.includes(String(m).toLowerCase());
        else continue;
        if (!matched) continue;

        const doReply = () => {
          try {
            const respBuf = (m === '*' || m === 'echo') ? buf : encodeData(rule.reply, rule.replyFormat);
            if (respBuf && respBuf.length) { entry.socket.write(respBuf); emitOut(entry, respBuf); }
            if (rule.disconnect) setTimeout(() => { try { entry.socket.end(); } catch (_) {} }, 30);
          } catch (e) { emitLog('error', '模拟回复异常: ' + e.message); }
        };
        const delay = Number(rule.delay) || 0;
        if (delay > 0) setTimeout(doReply, delay); else doReply();
        return;
      }
    }

    /* ---------------- 工具注册 ---------------- */

    ctx.tool({
      name: 'tcp_listen',
      description: '在 127.0.0.1 监听 TCP 端口（host 强制回环，不可配到外网）。记录新连接与双向报文预览，新连接与收发包经事件上报。',
      parameters: { type: 'object', required: ['port'], properties: {
        port: { type: 'number', description: '监听端口 1..65535' },
        host: { type: 'string', description: '保留；实际一律绑 127.0.0.1' },
      } },
      handler: (args) => {
        const port = Number(args.port);
        if (!port || port < 1 || port > 65535) throw new Error('非法端口: ' + args.port);
        if (state.listeners.has(port)) throw new Error('端口已被监听: ' + port);
        return new Promise((resolve, reject) => {
          const server = net.createServer((socket) => {
            try {
              const lis = state.listeners.get(port);
              registerSocket(socket, { role: 'server', listenerPort: port, listenerConns: lis.conns, recording: lis.recording });
            } catch (e) { emitLog('error', 'accept 异常: ' + e.message); }
          });
          server.on('error', (err) => reject(new Error('监听失败: ' + (err && err.message))));
          server.listen(port, BOUND_HOST, () => {
            const rec = newRecording(port, 'server');
            state.listeners.set(port, { server, host: BOUND_HOST, conns: new Set(), recording: rec });
            resolve({ ok: true, port, host: BOUND_HOST, recordingId: rec.id, connections: 0 });
          });
        });
      },
    });

    ctx.tool({
      name: 'tcp_connect',
      description: '作为客户端连接 host:port，返回 connectionId 供 send/close 使用。',
      parameters: { type: 'object', required: ['host', 'port'], properties: {
        host: { type: 'string' }, port: { type: 'number' },
      } },
      handler: (args) => {
        const host = String(args.host || '127.0.0.1');
        const port = Number(args.port);
        if (!port || port < 1 || port > 65535) throw new Error('非法端口: ' + args.port);
        return new Promise((resolve, reject) => {
          const socket = net.createConnection({ host, port }, () => {
            const rec = newRecording(port, 'client');
            const entry = registerSocket(socket, { role: 'client', remote: host + ':' + port, recording: rec });
            resolve({ ok: true, connectionId: entry.connectionId, host, port, recordingId: rec.id });
          });
          socket.once('error', (err) => reject(new Error('连接失败: ' + (err && err.message))));
          socket.setTimeout(10000, () => { socket.destroy(); reject(new Error('连接超时')); });
        });
      },
    });

    ctx.tool({
      name: 'tcp_send',
      description: '向指定连接发送数据。data 为字符串，format 默认 utf8，可选 hex。',
      parameters: { type: 'object', required: ['connectionId', 'data'], properties: {
        connectionId: { type: 'string' },
        data: { description: '发送数据；format=hex 时为十六进制串' },
        format: { type: 'string', enum: ['utf8', 'hex'] },
      } },
      handler: (args) => {
        const entry = state.connections.get(String(args.connectionId));
        if (!entry) throw new Error('连接不存在或已关闭: ' + args.connectionId);
        const buf = encodeData(args.data, args.format);
        entry.socket.write(buf);
        emitOut(entry, buf);
        return { ok: true, connectionId: entry.connectionId, sent: buf.length };
      },
    });

    ctx.tool({
      name: 'tcp_close',
      description: '关闭指定连接；不传 connectionId 关闭全部连接（不停 listener/proxy）。',
      parameters: { type: 'object', properties: { connectionId: { type: 'string' } } },
      handler: (args) => {
        let closed = 0;
        if (args.connectionId) {
          const entry = state.connections.get(String(args.connectionId));
          if (!entry) throw new Error('连接不存在: ' + args.connectionId);
          try { entry.socket.end(); } catch (_) {}
          closed = 1;
        } else {
          for (const entry of Array.from(state.connections.values())) { try { entry.socket.end(); closed++; } catch (_) {} }
        }
        return { ok: true, closed };
      },
    });

    ctx.tool({
      name: 'tcp_proxy_start',
      description: '在 127.0.0.1:listenPort 起本地代理，转发到 targetHost:targetPort，录制双向流量。无需特权/CAP_NET_RAW。',
      parameters: { type: 'object', required: ['listenPort', 'targetHost', 'targetPort'], properties: {
        listenPort: { type: 'number' }, targetHost: { type: 'string' }, targetPort: { type: 'number' },
      } },
      handler: (args) => {
        const listenPort = Number(args.listenPort);
        const targetHost = String(args.targetHost || '127.0.0.1');
        const targetPort = Number(args.targetPort);
        if (!listenPort || listenPort < 1 || listenPort > 65535) throw new Error('非法 listenPort');
        if (!targetPort || targetPort < 1 || targetPort > 65535) throw new Error('非法 targetPort');
        for (const p of state.proxies.values()) if (p.listenPort === listenPort) throw new Error('本地端口已被代理占用: ' + listenPort);
        if (state.listeners.has(listenPort)) throw new Error('本地端口已被监听占用: ' + listenPort);
        const proxyId = 'proxy-' + (++state.proxySeq);
        return new Promise((resolve, reject) => {
          const server = net.createServer((front) => {
            const back = net.createConnection({ host: targetHost, port: targetPort });
            const rec = newRecording(listenPort, 'proxy');
            const frontEntry = registerSocket(front, { role: 'proxy-front', recording: rec });
            // front -> back（c2s 已由 registerSocket 记录）：仅转发
            front.on('data', (buf) => { try { back.write(buf); } catch (_) {} });
            back.on('data', (buf) => {
              recordFrame(rec, 's2c', buf);
              ctx.emit('traffic', { direction: 'in', connectionId: frontEntry.connectionId, recordingId: rec.id, length: buf.length, payloadTruncated: buf.length > MAX_PREVIEW, time: nowIso() });
              try { front.write(buf); } catch (_) {}
            });
            const teardown = () => { try { front.destroy(); } catch (_) {} try { back.destroy(); } catch (_) {} };
            back.on('error', teardown);
            front.on('error', teardown);
            back.on('close', teardown);
          });
          server.on('error', (err) => reject(new Error('代理监听失败: ' + (err && err.message))));
          server.listen(listenPort, BOUND_HOST, () => {
            state.proxies.set(proxyId, { server, listenPort, targetHost, targetPort });
            resolve({ ok: true, proxyId, listenPort, host: BOUND_HOST, targetHost, targetPort });
          });
        });
      },
    });

    ctx.tool({
      name: 'tcp_proxy_stop',
      description: '停止指定代理并释放本地端口。',
      parameters: { type: 'object', required: ['proxyId'], properties: { proxyId: { type: 'string' } } },
      handler: (args) => {
        const p = state.proxies.get(String(args.proxyId));
        if (!p) throw new Error('代理不存在: ' + args.proxyId);
        try { p.server.close(); } catch (_) {}
        state.proxies.delete(String(args.proxyId));
        return { ok: true, proxyId: args.proxyId };
      },
    });

    ctx.tool({
      name: 'tcp_simulate_set',
      description: '为已监听端口设置模拟应答规则。rules:[{match, reply?, replyFormat?, delay?, disconnect?}]。match 子串匹配收到的 utf8/hex；"*"/"echo" 原样回显；delay 毫秒后回复；disconnect 回复后断连。',
      parameters: { type: 'object', required: ['port', 'rules'], properties: {
        port: { type: 'number' },
        rules: { type: 'array', items: { type: 'object', required: ['match'], properties: {
          match: { type: 'string' }, reply: {}, replyFormat: { type: 'string', enum: ['utf8', 'hex'] },
          delay: { type: 'number' }, disconnect: { type: 'boolean' },
        } } },
      } },
      handler: (args) => {
        const port = Number(args.port);
        if (!state.listeners.has(port)) throw new Error('端口未监听，先 tcp_listen: ' + port);
        if (!Array.isArray(args.rules)) throw new Error('rules 必须是数组');
        state.simRules.set(port, args.rules.slice(0, 64));
        return { ok: true, port, ruleCount: args.rules.length };
      },
    });

    ctx.tool({
      name: 'tcp_simulate_clear',
      description: '清除某端口的模拟规则。',
      parameters: { type: 'object', required: ['port'], properties: { port: { type: 'number' } } },
      handler: (args) => { state.simRules.delete(Number(args.port)); return { ok: true, port: Number(args.port) }; },
    });

    ctx.tool({
      name: 'tcp_replay',
      description: '把某次录制中客户端(c2s)方向报文按时间顺序重放到 targetHost:targetPort。speed 默认 1（原速）；0 为尽快发送。',
      parameters: { type: 'object', required: ['recordingId', 'targetHost', 'targetPort'], properties: {
        recordingId: { type: 'string' }, targetHost: { type: 'string' }, targetPort: { type: 'number' }, speed: { type: 'number' },
      } },
      handler: (args) => new Promise((resolve, reject) => {
        const rec = recById(String(args.recordingId));
        if (!rec) throw new Error('录制不存在: ' + args.recordingId);
        const host = String(args.targetHost || '127.0.0.1');
        const port = Number(args.targetPort);
        const speed = Number(args.speed == null ? 1 : args.speed);
        const c2s = rec.frames.filter(f => f.dir === 'c2s');
        if (!c2s.length) { resolve({ ok: true, sentFrames: 0, note: '无 c2s 帧可回放' }); return; }
        const socket = net.createConnection({ host, port }, () => {
          let idx = 0, respBytes = 0;
          socket.on('data', (b) => { respBytes += b.length; });
          const sendNext = () => {
            if (idx >= c2s.length) {
              try { socket.end(); } catch (_) {}
              resolve({ ok: true, recordingId: rec.id, sentFrames: c2s.length, sentBytes: c2s.reduce((n, f) => n + f.dataHex.length / 2, 0), responseBytes: respBytes });
              return;
            }
            const frame = c2s[idx++];
            socket.write(Buffer.from(frame.dataHex, 'hex'));
            let wait = 0;
            if (speed > 0 && idx < c2s.length) wait = Math.max(0, (c2s[idx].t - frame.t) / speed);
            setTimeout(sendNext, Math.min(wait, 5000));
          };
          sendNext();
        });
        socket.once('error', (err) => reject(new Error('回放连接失败: ' + (err && err.message))));
        socket.setTimeout(15000, () => { socket.destroy(); reject(new Error('回放超时')); });
      }),
    });

    ctx.tool({
      name: 'tcp_fuzz',
      description: '对 host:port 发送边界/异常报文（超长、畸形、慢字节、半开连接），记录每次连通与响应，不崩溃。',
      parameters: { type: 'object', required: ['host', 'port'], properties: {
        host: { type: 'string' }, port: { type: 'number' },
        count: { type: 'number', description: '探测连接数，默认 8，上限 32' },
        payloads: { type: 'array', description: '自定义 payload 字符串数组；缺省用内置边界用例' },
      } },
      handler: (args) => new Promise((resolve) => {
        const host = String(args.host || '127.0.0.1');
        const port = Number(args.port);
        const count = Math.min(Math.max(Number(args.count) || 8, 1), 32);
        const builtins = [
          { name: 'overlong-64k', data: 'A'.repeat(65536) },
          { name: 'nul-prefix', data: '\x00\x00\x00\x00HELLO' },
          { name: 'http-noise', data: 'GET / HTTP/1.1\r\nHost: x\r\n\r\n' },
          { name: 'binary-junk', data: Buffer.from([0xff, 0xfe, 0x00, 0x01, 0x7f, 0x80]).toString('binary') },
          { name: 'slow-bytes', data: 'SLOW', slow: true },
          { name: 'half-open', data: '', half: true },
        ];
        const payloads = Array.isArray(args.payloads) && args.payloads.length
          ? args.payloads.map((p, i) => ({ name: 'custom-' + i, data: String(p) }))
          : builtins;
        const results = [];
        const probeOne = (spec) => new Promise((res) => {
          let settled = false;
          const out = { name: spec.name, connected: false, responded: false, bytes: 0, error: null };
          const done = (v) => { if (!settled) { settled = true; res(v); } };
          const sock = net.createConnection({ host, port }, () => {
            out.connected = true;
            if (spec.half) { try { sock.end(); } catch (_) {} }
            else if (spec.slow) {
              let i = 0;
              const step = () => { if (i >= spec.data.length) return done(out); try { sock.write(spec.data[i++]); } catch (_) {} setTimeout(step, 120); };
              step();
            } else { try { sock.write(spec.data); } catch (e) { out.error = String(e.message); } }
          });
          sock.on('data', (b) => { out.responded = true; out.bytes += b.length; });
          sock.on('error', (e) => { out.error = String(e && e.message); done(out); });
          sock.on('close', () => done(out));
          sock.setTimeout(3000, () => { try { sock.destroy(); } catch (_) {} done(out); });
        });
        (async () => {
          for (let i = 0; i < count; i++) results.push(await probeOne(payloads[i % payloads.length]));
          resolve({ ok: true, host, port, probes: results.length, survived: results.filter(r => r.connected && !r.error).length, results });
        })();
      }),
    });
  },

  start() {
    // 端口由工具按需开启；此处无需预绑定
  },

  stop() {
    const s = this.__state;
    if (!s) return;
    for (const l of s.listeners.values()) { try { l.server.close(); } catch (_) {} }
    for (const p of s.proxies.values()) { try { p.server.close(); } catch (_) {} }
    for (const c of s.connections.values()) { try { c.socket.destroy(); } catch (_) {} }
    s.listeners.clear(); s.proxies.clear(); s.connections.clear(); s.simRules.clear();
  },
};
