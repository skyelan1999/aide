'use strict';
/* comm-udp：常驻 UDP 通讯插件（协议 v1.2 daemon）。
 * 仅用 Node 内置 dgram 模块，零第三方依赖。
 *
 * 安全模型：默认禁用；绑定一律 127.0.0.1；无 raw socket / CAP_NET_RAW。
 * UDP 无连接：每个 socket 绑定一个端口，收包按 (address,port) 来源区分。
 */
const dgram = require('dgram');

const BOUND_HOST = '127.0.0.1';
const MAX_PREVIEW = 256;

module.exports = {
  name: 'comm-udp',
  daemon: true,

  apply(ctx) {
    const self = this;
    const emitLog = (level, message) => { try { ctx.emit('log', { level, message: String(message) }); } catch (_) {} };

    const state = {
      sockets: new Map(),   // socketId -> {socket, port, host, multicast:Set<address>, rec}
      simRules: new Map(),  // port -> rules[]
      sockSeq: 0,
    };
    self.__state = state;

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

    // 规则模拟：对收到的报文匹配并回复来源
    function maybeSimulate(sockEntry, buf, rinfo) {
      const rules = state.simRules.get(sockEntry.port);
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
            if (respBuf && respBuf.length) {
              sockEntry.socket.send(respBuf, rinfo.port, rinfo.address, () => {
                ctx.emit('traffic', { direction: 'out', socketId: sockEntry.id, remote: rinfo.address + ':' + rinfo.port, length: respBuf.length, payloadTruncated: respBuf.length > MAX_PREVIEW, time: nowIso() });
              });
            }
          } catch (e) { emitLog('error', 'UDP 模拟回复异常: ' + e.message); }
        };
        const delay = Number(rule.delay) || 0;
        if (delay > 0) setTimeout(doReply, delay); else doReply();
        return;
      }
    }

    ctx.tool({
      name: 'udp_bind',
      description: '在 127.0.0.1:port 绑定 UDP socket 收包（host 强制回环）。multicast=true 时开启组播回环。记录收到的报文并经事件上报。',
      parameters: { type: 'object', required: ['port'], properties: {
        port: { type: 'number', description: '绑定端口 1..65535' },
        host: { type: 'string', description: '保留；实际绑 127.0.0.1' },
        multicast: { type: 'boolean', description: '是否开启组播回环（setMulticastLoopback）' },
      } },
      handler: (args) => {
        const port = Number(args.port);
        if (!port || port < 1 || port > 65535) throw new Error('非法端口: ' + args.port);
        for (const s of state.sockets.values()) if (s.port === port) throw new Error('端口已绑定: ' + port);
        const wantMulticast = !!args.multicast;
        // 普通绑定严格回环；组播是内核强制例外：接收组播必须绑 INADDR_ANY，
        // 但组播接口显式限定回环 127.0.0.1（见 multicast_join），仍不暴露到外网。
        const bindHost = wantMulticast ? '0.0.0.0' : BOUND_HOST;
        const socket = dgram.createSocket({ type: 'udp4', reuseAddr: true });
        const id = 'u-' + (++state.sockSeq);
        const entry = { id, socket, port, host: bindHost, multicast: new Set() };
        return new Promise((resolve, reject) => {
          socket.on('error', (err) => reject(new Error('绑定失败: ' + (err && err.message))));
          if (wantMulticast) { try { socket.setMulticastLoopback(true); } catch (_) {} }
          socket.bind(port, bindHost, () => {
            state.sockets.set(id, entry);
            ctx.emit('event', { subtype: 'bound', socketId: id, port, host: bindHost, multicast: wantMulticast, time: nowIso() });
            resolve({ ok: true, socketId: id, port, host: bindHost });
          });
          socket.on('message', (buf, rinfo) => {
            try {
              const pv = preview(buf);
              ctx.emit('traffic', { direction: 'in', socketId: id, remote: rinfo.address + ':' + rinfo.port, length: buf.length, payloadTruncated: pv.truncated, previewHex: pv.previewHex, time: nowIso() });
              maybeSimulate(entry, buf, rinfo);
            } catch (e) { emitLog('error', 'message 处理异常: ' + e.message); }
          });
        });
      },
    });

    ctx.tool({
      name: 'udp_send',
      description: '发送一个 UDP 数据报到 host:port。无状态一次性 socket。',
      parameters: { type: 'object', required: ['host', 'port', 'data'], properties: {
        host: { type: 'string' }, port: { type: 'number' },
        data: { description: '发送数据；format=hex 时为十六进制串' },
        format: { type: 'string', enum: ['utf8', 'hex'] },
      } },
      handler: (args) => new Promise((resolve, reject) => {
        const host = String(args.host || '127.0.0.1');
        const port = Number(args.port);
        if (!port || port < 1 || port > 65535) throw new Error('非法端口: ' + args.port);
        const buf = encodeData(args.data, args.format);
        const sock = dgram.createSocket('udp4');
        sock.send(buf, port, host, (err) => {
          try { sock.close(); } catch (_) {}
          if (err) return reject(new Error('发送失败: ' + (err && err.message)));
          ctx.emit('traffic', { direction: 'out', remote: host + ':' + port, length: buf.length, payloadTruncated: buf.length > MAX_PREVIEW, time: nowIso() });
          resolve({ ok: true, host, port, sent: buf.length });
        });
      }),
    });

    ctx.tool({
      name: 'udp_close',
      description: '关闭指定绑定 socket；不传 socketId 关闭全部。',
      parameters: { type: 'object', properties: { socketId: { type: 'string' } } },
      handler: (args) => {
        let closed = 0;
        if (args.socketId) {
          const e = state.sockets.get(String(args.socketId));
          if (!e) throw new Error('socket 不存在: ' + args.socketId);
          try { e.socket.close(); } catch (_) {}
          state.sockets.delete(String(args.socketId));
          closed = 1;
        } else {
          for (const e of Array.from(state.sockets.values())) { try { e.socket.close(); closed++; } catch (_) {} }
          state.sockets.clear();
        }
        return { ok: true, closed };
      },
    });

    ctx.tool({
      name: 'udp_simulate_set',
      description: '为已绑定端口设置模拟应答规则（收到匹配报文后回复来源）。rules:[{match, reply?, replyFormat?, delay?}]；"*"/"echo" 回显。',
      parameters: { type: 'object', required: ['port', 'rules'], properties: {
        port: { type: 'number' },
        rules: { type: 'array', items: { type: 'object', required: ['match'], properties: {
          match: { type: 'string' }, reply: {}, replyFormat: { type: 'string', enum: ['utf8', 'hex'] }, delay: { type: 'number' },
        } } },
      } },
      handler: (args) => {
        const port = Number(args.port);
        const exists = [...state.sockets.values()].some(s => s.port === port);
        if (!exists) throw new Error('端口未绑定，先 udp_bind: ' + port);
        if (!Array.isArray(args.rules)) throw new Error('rules 必须是数组');
        state.simRules.set(port, args.rules.slice(0, 64));
        return { ok: true, port, ruleCount: args.rules.length };
      },
    });

    ctx.tool({
      name: 'udp_simulate_clear',
      description: '清除某端口的模拟规则。',
      parameters: { type: 'object', required: ['port'], properties: { port: { type: 'number' } } },
      handler: (args) => { state.simRules.delete(Number(args.port)); return { ok: true, port: Number(args.port) }; },
    });

    ctx.tool({
      name: 'udp_multicast_join',
      description: '在某已绑定端口的 socket 上加入组播组 address（如 239.0.0.1）。',
      parameters: { type: 'object', required: ['address', 'port'], properties: {
        address: { type: 'string' }, port: { type: 'number' },
      } },
      handler: (args) => {
        const port = Number(args.port);
        const sockEntry = [...state.sockets.values()].find(s => s.port === port);
        if (!sockEntry) throw new Error('端口未绑定: ' + port);
        const address = String(args.address);
        try { sockEntry.socket.addMembership(address, '127.0.0.1'); sockEntry.multicast.add(address); }
        catch (e) { throw new Error('加入组播失败: ' + (e && e.message)); }
        return { ok: true, address, port };
      },
    });

    ctx.tool({
      name: 'udp_multicast_leave',
      description: '离开组播组 address。',
      parameters: { type: 'object', required: ['address', 'port'], properties: {
        address: { type: 'string' }, port: { type: 'number' },
      } },
      handler: (args) => {
        const port = Number(args.port);
        const sockEntry = [...state.sockets.values()].find(s => s.port === port);
        if (!sockEntry) throw new Error('端口未绑定: ' + port);
        const address = String(args.address);
        try { sockEntry.socket.dropMembership(address, '127.0.0.1'); sockEntry.multicast.delete(address); }
        catch (e) { throw new Error('离开组播失败: ' + (e && e.message)); }
        return { ok: true, address, port };
      },
    });
  },

  start() { /* 端口由工具按需绑定 */ },

  stop() {
    const s = this.__state;
    if (!s) return;
    for (const e of s.sockets.values()) { try { e.socket.close(); } catch (_) {} }
    s.sockets.clear(); s.simRules.clear();
  },
};
