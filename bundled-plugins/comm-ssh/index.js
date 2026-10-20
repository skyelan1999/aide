'use strict';
/* comm-ssh 常驻守护插件（aide 协议 v1.2 daemon，见 docs/plugins/comm-ssh.md）。
 *
 * 基于 ssh2 库（随插件打包 node_modules），提供：
 *   - SSH 客户端：密码/公钥认证、exec 命令执行
 *   - SFTP：列目录、下载、上传
 *   - 本地端口转发（本地监听 → 经 SSH 隧道到 remoteHost:remotePort）
 *   - 模拟 SSH 服务端：可脚本化命令应答，用于端到端测试与演练
 *
 * 安全模型（见文档 §安全模型）：
 *   - manifest enabled:false，须用户显式开启才会被拉起；
 *   - 所有监听套接字（模拟服务端、隧道本地端口）仅绑 127.0.0.1（AIDE_DAEMON_BIND）；
 *   - 密码/私钥按连接调用临时传入，仅驻留进程内存，关闭即丢弃；
 *     结果/事件/日志中绝不回显凭证正文（keyId 为上层 vault 引用占位）；
 *   - 流量事件只上报方向与字节数，不上报载荷正文。
 */
const net = require('net');
const fs = require('fs');
const os = require('os');
const path = require('path');
const crypto = require('crypto');

/* SFTP 状态码（ssh2 约定） */
const STATUS_OK = 0;
const STATUS_EOF = 1;
const STATUS_FAILURE = 4;

let ssh2;
try {
  ssh2 = require('ssh2');
} catch (err) {
  // 依赖缺失时仍允许加载（surface 聚合），真正调用时给出可读错误。
  ssh2 = null;
}
const { Client, Server } = ssh2 || {};

/* ---------------- 状态 ---------------- */

const clients = new Map();        // connectionId -> { client, sftp }
const tunnels = new Map();        // tunnelId -> { server, connectionId }
const simulateServers = new Map(); // serverId -> { server, rules, sessions, authPassword, expectedPassword, publickey }

let ctx = null;
let counters = { c: 0, t: 0, s: 0 };

const MAX_OUT = 256 * 1024;       // stdout/stderr 单次最大保留字节

function emit(type, fields) {
  try { if (ctx && typeof ctx.emit === 'function') ctx.emit(type, fields || {}); } catch (_) {}
}
function nextId(prefix) {
  counters[prefix] = (counters[prefix] || 0) + 1;
  return prefix + '-' + counters[prefix] + '-' + Date.now().toString(36);
}
function loopback() {
  return process.env.AIDE_DAEMON_BIND || '127.0.0.1';
}
function ensureDeps() {
  if (!ssh2 || !Client || !Server) {
    throw new Error('ssh2 依赖未安装：请在 plugins/comm-ssh 内执行 npm install ssh2');
  }
}
function requireConn(connectionId) {
  const rec = clients.get(String(connectionId || ''));
  if (!rec) throw new Error('SSH 连接不存在或已关闭: ' + connectionId);
  return rec;
}
function truncateStr(s) {
  if (s.length <= MAX_OUT) return s;
  return s.slice(0, MAX_OUT) + '…[truncated ' + s.length + ' bytes]';
}

/* ---------------- 1) ssh_connect ---------------- */

function handleConnect(args) {
  ensureDeps();
  return new Promise((resolve, reject) => {
    const host = String(args.host || '').trim();
    if (!host) return reject(new Error('缺少必填参数 host'));
    const port = Number(args.port) || 22;
    const username = String(args.username || '').trim();
    if (!username) return reject(new Error('缺少必填参数 username'));
    let authType = String(args.authType || 'password');
    const cfg = {
      host,
      port,
      username,
      readyTimeout: Number(args.timeout) || 15000,
      keepaliveInterval: 30000,
    };
    if (authType === 'publickey') {
      // 私钥由调用方（持有解锁 vault 的上层）解析后传入；本插件不持久化、不回显。
      if (!args.privateKey) return reject(new Error('publickey 认证需要 privateKey（keyId 须由上层 vault 解析后注入）'));
      cfg.privateKey = String(args.privateKey);
      if (args.passphrase) cfg.passphrase = String(args.passphrase);
    } else {
      if (!args.password) return reject(new Error('password 认证需要 password（keyId 须由上层 vault 解析后注入）'));
      cfg.password = String(args.password);
      authType = 'password';
    }
    const conn = new Client();
    const cid = nextId('c');
    let settled = false;
    conn.on('ready', () => {
      if (settled) return; settled = true;
      clients.set(cid, { client: conn, sftp: null });
      emit('event', { subtype: 'connected', message: `SSH 已连接 ${username}@${host}:${port}` });
      resolve({ connectionId: cid, host, port, username, authType });
    });
    conn.on('error', (err) => {
      if (settled) return; settled = true;
      reject(new Error('SSH 连接失败: ' + (err.level ? err.level + ' ' : '') + err.message));
    });
    conn.on('close', () => {
      clients.delete(cid);
      emit('event', { subtype: 'disconnected', message: `SSH 连接关闭 ${cid}` });
    });
    try { conn.connect(cfg); } catch (e) { settled = true; reject(e); }
  });
}

/* ---------------- 2) ssh_exec ---------------- */

function handleExec(args) {
  ensureDeps();
  const rec = requireConn(args.connectionId);
  return new Promise((resolve, reject) => {
    const command = String(args.command || '');
    if (!command.trim()) return reject(new Error('缺少必填参数 command'));
    rec.client.exec(command, (err, stream) => {
      if (err) return reject(new Error('exec 失败: ' + err.message));
      let stdout = '', stderr = '', exitCode = null, signal = null;
      emit('traffic', { direction: 'out', length: Buffer.byteLength(command), payloadTruncated: true });
      stream.on('close', (code, sig) => {
        exitCode = code == null ? null : code;
        signal = sig || null;
        resolve({ stdout: truncateStr(stdout), stderr: truncateStr(stderr), exitCode, signal: signal || undefined });
      });
      stream.on('data', (d) => { stdout += d.toString('utf8'); });
      stream.stderr.on('data', (d) => { stderr += d.toString('utf8'); });
      const timeout = Number(args.timeout) || 30000;
      const timer = setTimeout(() => { try { stream.signal('KILL'); } catch (_) {} }, timeout);
      stream.on('close', () => clearTimeout(timer));
    });
  });
}

/* ---------------- SFTP ---------------- */

function openSftp(connectionId) {
  const rec = requireConn(connectionId);
  if (rec.sftp) return Promise.resolve(rec.sftp);
  return new Promise((resolve, reject) => {
    rec.client.sftp((err, sftp) => {
      if (err) return reject(new Error('SFTP 打开失败: ' + err.message));
      rec.sftp = sftp;
      resolve(sftp);
    });
  });
}

function handleSftpList(args) {
  ensureDeps();
  return openSftp(args.connectionId).then((sftp) => new Promise((resolve, reject) => {
    const dir = String(args.path || '.');
    sftp.readdir(dir, (err, list) => {
      if (err) return reject(new Error('SFTP 列目录失败: ' + err.message));
      const entries = list.map((e) => {
        const a = e.attrs || {};
        return {
          name: e.filename,
          type: a.isDirectory && a.isDirectory() ? 'dir' : 'file',
          size: a.size || 0,
          modifyTime: a.mtime ? new Date(a.mtime * 1000).toISOString() : undefined,
        };
      });
      resolve({ path: dir, entries });
    });
  }));
}

function handleSftpGet(args) {
  ensureDeps();
  return openSftp(args.connectionId).then((sftp) => new Promise((resolve, reject) => {
    const remotePath = String(args.remotePath || '');
    const localPath = String(args.localPath || '');
    if (!remotePath || !localPath) return reject(new Error('缺少 remotePath/localPath'));
    sftp.fastGet(remotePath, localPath, (err) => {
      if (err) return reject(new Error('SFTP 下载失败: ' + err.message));
      const bytes = require('fs').statSync(localPath).size;
      emit('traffic', { direction: 'in', length: bytes, payloadTruncated: true });
      resolve({ remotePath, localPath, bytes });
    });
  }));
}

function handleSftpPut(args) {
  ensureDeps();
  return openSftp(args.connectionId).then((sftp) => new Promise((resolve, reject) => {
    const localPath = String(args.localPath || '');
    const remotePath = String(args.remotePath || '');
    if (!localPath || !remotePath) return reject(new Error('缺少 localPath/remotePath'));
    sftp.fastPut(localPath, remotePath, (err) => {
      if (err) return reject(new Error('SFTP 上传失败: ' + err.message));
      const bytes = require('fs').statSync(localPath).size;
      emit('traffic', { direction: 'out', length: bytes, payloadTruncated: true });
      resolve({ localPath, remotePath, bytes });
    });
  }));
}

/* ---------------- 6/7) 端口转发 ---------------- */

function handleTunnelStart(args) {
  ensureDeps();
  const rec = requireConn(args.connectionId);
  return new Promise((resolve, reject) => {
    const localPort = Number(args.localPort);
    const remoteHost = String(args.remoteHost || '');
    const remotePort = Number(args.remotePort);
    if (!Number.isInteger(localPort) || localPort < 0 || !remoteHost || !remotePort) {
      return reject(new Error('缺少 localPort/remoteHost/remotePort'));
    }
    const bind = loopback(); // 隧道本地监听仅绑回环
    const server = net.createServer((socket) => {
      rec.client.forwardOut('127.0.0.1', localPort, remoteHost, remotePort, (err, stream) => {
        if (err) { socket.destroy(); return; }
        socket.pipe(stream);
        stream.pipe(socket);
      });
    });
    server.once('error', (e) => reject(new Error('隧道监听失败: ' + e.message)));
    server.listen(localPort, bind, () => {
      const tid = nextId('t');
      tunnels.set(tid, { server, connectionId: args.connectionId });
      const actualPort = server.address().port;
      emit('event', { subtype: 'tunnel-up', message: `隧道 127.0.0.1:${actualPort} -> ${remoteHost}:${remotePort}` });
      resolve({ tunnelId: tid, localPort: actualPort, remoteHost, remotePort, bind });
    });
  });
}

function handleTunnelStop(args) {
  const tid = String(args.tunnelId || '');
  const rec = tunnels.get(tid);
  if (!rec) return Promise.resolve({ ok: true, tunnelId: tid, closed: false });
  return new Promise((resolve) => {
    rec.server.close(() => {
      tunnels.delete(tid);
      emit('event', { subtype: 'tunnel-down', message: '隧道关闭 ' + tid });
      resolve({ ok: true, tunnelId: tid, closed: true });
    });
  });
}

/* ---------------- 8) ssh_close ---------------- */

function handleClose(args) {
  if (args.connectionId) {
    const rec = clients.get(String(args.connectionId));
    if (rec) { try { rec.client.end(); } catch (_) {} clients.delete(String(args.connectionId)); }
    return Promise.resolve({ closed: [String(args.connectionId)] });
  }
  const closed = [];
  for (const [cid, rec] of clients) { try { rec.client.end(); } catch (_) {} closed.push(cid); }
  clients.clear();
  return Promise.resolve({ closed });
}

/* ---------------- 9-11) 模拟 SSH 服务端 ---------------- */

function generateHostKey() {
  return crypto.generateKeyPairSync('rsa', {
    modulusLength: 2048,
    publicKeyEncoding: { type: 'spki', format: 'pem' },
    privateKeyEncoding: { type: 'pkcs1', format: 'pem' },
  }).privateKey;
}

/* 把 fs.Stats 转成 ssh2 服务端需要的 attrs。 */
function attrsFromStat(stat) {
  return {
    mode: stat.mode,
    uid: stat.uid,
    gid: stat.gid,
    size: stat.size,
    atime: Math.floor(stat.atimeMs / 1000),
    mtime: Math.floor(stat.mtimeMs / 1000),
  };
}

/* SSH_FXF 打开位掩码 → fs.openSync 模式字符串。 */
function flagsToMode(flags) {
  const READ = 0x1, WRITE = 0x2, APPEND = 0x40, CREAT = 0x8, TRUNC = 0x10;
  const f = Number(flags) || 0;
  if (f & APPEND) return (f & WRITE) ? 'a' : 'a+';
  if (f & WRITE) return (f & READ) ? 'w+' : 'w';
  return 'r'; // CREAT/TRUNC 对只读打开无意义
}

/* 为模拟服务端挂一个最小可用的 SFTP 子系统，后端为 sftpRoot 目录（仅测试/演练）。 */
function setupSftp(sftp, sftpRoot) {
  const handles = new Map(); // handle -> {type:'file'|'dir', value}
  let nextHandle = 1;
  const root = path.resolve(sftpRoot);
  const safe = (p) => {
    const abs = path.normalize(path.join(root, String(p || '.')));
    if (abs !== root && !abs.startsWith(root + path.sep)) throw new Error('sftp 路径越界');
    return abs;
  };
  sftp.on('OPEN', (reqid, filename, flags) => {
    let fd;
    try {
      fd = fs.openSync(safe(filename), flagsToMode(flags));
    } catch (e) { return sftp.status(reqid, STATUS_FAILURE, e.message); }
    const handle = Buffer.from('f' + nextHandle++);
    handles.set(handle.toString(), { type: 'file', value: fd });
    sftp.handle(reqid, handle);
  });
  sftp.on('READ', (reqid, handle, offset, length) => {
    const rec = handles.get(handle.toString());
    if (!rec || rec.type !== 'file') return sftp.status(reqid, STATUS_FAILURE, 'bad handle');
    const buf = Buffer.alloc(length);
    let n = 0;
    try { n = fs.readSync(rec.value, buf, 0, length, offset); }
    catch (e) { return sftp.status(reqid, STATUS_FAILURE, e.message); }
    if (n > 0) sftp.data(reqid, buf.slice(0, n));
    else sftp.status(reqid, STATUS_EOF);
  });
  sftp.on('WRITE', (reqid, handle, offset, data) => {
    const rec = handles.get(handle.toString());
    if (!rec || rec.type !== 'file') return sftp.status(reqid, STATUS_FAILURE, 'bad handle');
    try { fs.writeSync(rec.value, data, 0, data.length, offset); }
    catch (e) { return sftp.status(reqid, STATUS_FAILURE, e.message); }
    sftp.status(reqid, STATUS_OK);
  });
  sftp.on('CLOSE', (reqid, handle) => {
    const rec = handles.get(handle.toString());
    if (rec) { try { fs.closeSync(rec.value); } catch (_) {} handles.delete(handle.toString()); }
    sftp.status(reqid, STATUS_OK);
  });
  sftp.on('OPENDIR', (reqid, dirname) => {
    let entries;
    try { entries = fs.readdirSync(safe(dirname), { withFileTypes: true }); }
    catch (e) { return sftp.status(reqid, STATUS_FAILURE, e.message); }
    const handle = Buffer.from('d' + nextHandle++);
    handles.set(handle.toString(), { type: 'dir', value: entries });
    sftp.handle(reqid, handle);
  });
  sftp.on('READDIR', (reqid, handle) => {
    const rec = handles.get(handle.toString());
    if (!rec || rec.type !== 'dir') return sftp.status(reqid, STATUS_EOF);
    const entries = rec.value;
    if (rec.index === undefined) rec.index = 0;
    if (rec.index >= entries.length) return sftp.status(reqid, STATUS_EOF);
    const batch = entries.slice(rec.index, rec.index + 50).map((e) => {
      const abs = path.join(root, e.name);
      let stat;
      try { stat = fs.statSync(abs); } catch (_) { stat = { mode: e.isDirectory() ? 16877 : 33188, size: 0, uid: 0, gid: 0, atimeMs: 0, mtimeMs: 0 }; }
      return { filename: e.name, longname: '-rw-r--r-- 1 user group 0 Jan 1 00:00 ' + e.name, attrs: attrsFromStat(stat) };
    });
    rec.index += batch.length;
    sftp.name(reqid, batch);
  });
  sftp.on('STAT', (reqid, p) => {
    try { sftp.attrs(reqid, attrsFromStat(fs.statSync(safe(p)))); }
    catch (e) { sftp.status(reqid, STATUS_FAILURE, e.message); }
  });
  sftp.on('REALPATH', (reqid, p) => {
    sftp.name(reqid, [{ filename: safe(p || '.'), attrs: {} }]);
  });
  // 其余子操作：最小演练不实现，回失败即可。
  for (const ev of ['LSTAT', 'FSTAT', 'SETSTAT', 'FSETSTAT', 'REMOVE', 'RMDIR', 'READLINK', 'SYMLINK', 'RENAME', 'MKDIR']) {
    sftp.on(ev, (reqid) => sftp.status(reqid, STATUS_FAILURE, ev + ' not implemented in mock'));
  }
}

function matchRule(rules, command) {
  for (const r of rules || []) {
    if (!r) continue;
    if (r.regex) {
      try { if (new RegExp(r.commandMatch).test(command)) return r; } catch (_) {}
    } else if (r.commandMatch === command || r.commandMatch === '*') {
      return r;
    }
  }
  return null;
}

function handleSimStart(args) {
  ensureDeps();
  return new Promise((resolve, reject) => {
    const port = Number(args.port);
    if (!Number.isInteger(port) || port < 0) return reject(new Error('缺少必填参数 port（>=0，0 表示随机端口）'));
    const bind = loopback(); // 模拟服务端仅绑回环
    const hostKey = args.hostKey ? String(args.hostKey) : generateHostKey();
    let sim;
    const srv = new Server({ hostKeys: [hostKey] }, (client, info) => {
      client.on('authentication', (ctxAuth) => {
        if (ctxAuth.method === 'password') {
          if (!sim.authPassword) return ctxAuth.reject(['publickey']);
          if (sim.expectedPassword && ctxAuth.password !== sim.expectedPassword) return ctxAuth.reject();
          return ctxAuth.accept();
        }
        if (ctxAuth.method === 'publickey') {
          if (!sim.publickey) return ctxAuth.reject(['password']);
          return ctxAuth.accept(); // 演练态：接受任意合法私钥
        }
        ctxAuth.reject(['password', 'publickey']);
      });
      client.on('ready', () => {
        emit('event', { subtype: 'sim-connected', message: `模拟 SSH 服务端收到连接 (${info.ip})` });
        client.on('session', (accept) => {
          const session = accept();
          session.on('sftp', (acceptSftp) => setupSftp(acceptSftp(), sim.sftpRoot));
          session.on('exec', (acceptCh, rejectCh, infoExec) => {
            const req = acceptCh();
            const command = String(infoExec.command || '');
            sim.sessions.push({ command, at: new Date().toISOString() });
            const rule = matchRule(sim.rules, command);
            const reply = () => {
              try {
                if (rule) {
                  req.write(String(rule.reply || ''));
                  if (rule.stderr) req.stderr.write(String(rule.stderr));
                  req.exit(typeof rule.exitCode === 'number' ? rule.exitCode : 0);
                } else {
                  req.write('');
                  req.exit(127);
                }
              } catch (_) {}
              try { req.close(); } catch (_) {}
            };
            const delay = Number(rule && rule.delay) || 0;
            if (delay > 0) setTimeout(reply, delay); else reply();
          });
        });
        // direct-tcpip：让隧道测试可端到端（把 dest 连接透传到本机目标）
        client.on('tcpip', (accept, rejectTcp, infoTcp) => {
          const up = net.connect(infoTcp.destPort, infoTcp.destIP);
          up.on('error', () => { try { rejectTcp(); } catch (_) {} });
          up.on('connect', () => {
            const ch = accept();
            ch.pipe(up); up.pipe(ch);
          });
        });
      });
      client.on('end', () => emit('event', { subtype: 'sim-disconnected', message: '模拟 SSH 客户端断开' }));
    });
    const sftpRoot = args.sftpRoot
      ? String(args.sftpRoot)
      : fs.mkdtempSync(path.join(os.tmpdir(), 'comm-ssh-sftp-'));
    sim = {
      id: nextId('s'),
      server: srv,
      rules: [],
      sessions: [],
      sftpRoot,
      authPassword: args.authPassword !== false,
      expectedPassword: args.expectedPassword ? String(args.expectedPassword) : '',
      publickey: args.publickey === true,
    };
    srv.once('error', (e) => reject(new Error('模拟服务端启动失败: ' + e.message)));
    srv.listen(port, bind, () => {
      simulateServers.set(sim.id, sim);
      const actualPort = srv.address().port;
      emit('event', { subtype: 'sim-listening', message: `模拟 SSH 服务端监听 ${bind}:${actualPort}` });
      resolve({ serverId: sim.id, port: actualPort, host: bind, password: sim.authPassword, publickey: sim.publickey, sftpRoot: sim.sftpRoot });
    });
  });
}

function handleSimStop(args) {
  const sid = String(args.serverId || '');
  const sim = simulateServers.get(sid);
  if (!sim) return Promise.resolve({ ok: true, serverId: sid, stopped: false });
  return new Promise((resolve) => {
    sim.server.close(() => {
      simulateServers.delete(sid);
      resolve({ ok: true, serverId: sid, stopped: true, recordedSessions: sim.sessions.length });
    });
  });
}

function handleSimSetRule(args) {
  const sid = String(args.serverId || '');
  const sim = simulateServers.get(sid);
  if (!sim) throw new Error('模拟服务端不存在: ' + sid);
  const rules = Array.isArray(args.rules) ? args.rules : [];
  for (const r of rules) {
    if (!r || typeof r.commandMatch !== 'string') throw new Error('每条 rule 必须含 commandMatch');
  }
  sim.rules = rules;
  return Promise.resolve({ serverId: sid, count: rules.length });
}

/* ---------------- 插件导出 ---------------- */

module.exports = {
  name: 'comm-ssh',
  daemon: true,
  apply(c) {
    ctx = c;
    c.tool({
      name: 'ssh_connect',
      description: '建立 SSH 连接。密码/私钥由上层（持有解锁 vault 的编排层）解析后临时传入，本插件不持久化、不回显。返回 connectionId。',
      parameters: {
        type: 'object',
        required: ['host', 'username'],
        properties: {
          host: { type: 'string' },
          port: { type: 'number', description: '默认 22' },
          authType: { type: 'string', enum: ['password', 'publickey'], description: '默认 password' },
          username: { type: 'string' },
          password: { type: 'string', description: '密码认证时使用（不回显）' },
          privateKey: { type: 'string', description: '公钥认证时的 PEM 私钥（由 vault 解析后注入，不回显）' },
          passphrase: { type: 'string', description: '加密私钥的口令' },
          keyId: { type: 'string', description: 'vault 凭证引用占位；由上层解析后替换为 password/privateKey' },
          timeout: { type: 'number', description: '建连超时 ms，默认 15000' },
        },
      },
      handler: (args) => handleConnect(args || {}),
    });
    c.tool({
      name: 'ssh_exec',
      description: '在已连接会话上执行命令，返回 {stdout, stderr, exitCode, signal?}。',
      parameters: {
        type: 'object', required: ['connectionId', 'command'],
        properties: {
          connectionId: { type: 'string' },
          command: { type: 'string' },
          timeout: { type: 'number', description: '默认 30000 ms，超时发 KILL' },
        },
      },
      handler: (args) => handleExec(args || {}),
    });
    c.tool({
      name: 'ssh_sftp_list',
      description: 'SFTP 列目录，返回 entries[{name,type,size,modifyTime}]。',
      parameters: {
        type: 'object', required: ['connectionId', 'path'],
        properties: { connectionId: { type: 'string' }, path: { type: 'string' } },
      },
      handler: (args) => handleSftpList(args || {}),
    });
    c.tool({
      name: 'ssh_sftp_get',
      description: 'SFTP 下载远端文件到本地路径（fastGet）。',
      parameters: {
        type: 'object', required: ['connectionId', 'remotePath', 'localPath'],
        properties: {
          connectionId: { type: 'string' },
          remotePath: { type: 'string' },
          localPath: { type: 'string' },
        },
      },
      handler: (args) => handleSftpGet(args || {}),
    });
    c.tool({
      name: 'ssh_sftp_put',
      description: 'SFTP 上传本地文件到远端路径（fastPut）。',
      parameters: {
        type: 'object', required: ['connectionId', 'localPath', 'remotePath'],
        properties: {
          connectionId: { type: 'string' },
          localPath: { type: 'string' },
          remotePath: { type: 'string' },
        },
      },
      handler: (args) => handleSftpPut(args || {}),
    });
    c.tool({
      name: 'ssh_tunnel_start',
      description: '本地端口转发：在 127.0.0.1:localPort 监听，经 SSH 隧道转发到 remoteHost:remotePort。返回 tunnelId。',
      parameters: {
        type: 'object', required: ['connectionId', 'localPort', 'remoteHost', 'remotePort'],
        properties: {
          connectionId: { type: 'string' },
          localPort: { type: 'number' },
          remoteHost: { type: 'string' },
          remotePort: { type: 'number' },
        },
      },
      handler: (args) => handleTunnelStart(args || {}),
    });
    c.tool({
      name: 'ssh_tunnel_stop',
      description: '停止指定隧道并释放本地端口。',
      parameters: {
        type: 'object', required: ['tunnelId'],
        properties: { tunnelId: { type: 'string' } },
      },
      handler: (args) => handleTunnelStop(args || {}),
    });
    c.tool({
      name: 'ssh_close',
      description: '关闭指定连接；不传 connectionId 关闭全部。',
      parameters: {
        type: 'object',
        properties: { connectionId: { type: 'string' } },
      },
      handler: (args) => handleClose(args || {}),
    });
    c.tool({
      name: 'ssh_simulate_start',
      description: '在 127.0.0.1 启动一个可脚本化的模拟 SSH 服务端（测试/演练用）。默认接受任意密码；可用 expectedPassword 限定、publickey:true 开启公钥模式。',
      parameters: {
        type: 'object', required: ['port'],
        properties: {
          port: { type: 'number' },
          hostKey: { type: 'string', description: 'PEM 私钥；缺省自动生成临时 RSA host key' },
          authPassword: { type: 'boolean', description: '是否允许密码登录，默认 true' },
          expectedPassword: { type: 'string', description: '若设置，仅该密码可登录；否则接受任意密码' },
          publickey: { type: 'boolean', description: '开启公钥登录（演练态接受任意合法私钥）' },
        },
      },
      handler: (args) => handleSimStart(args || {}),
    });
    c.tool({
      name: 'ssh_simulate_stop',
      description: '停止模拟 SSH 服务端并释放端口。',
      parameters: {
        type: 'object', required: ['serverId'],
        properties: { serverId: { type: 'string' } },
      },
      handler: (args) => handleSimStop(args || {}),
    });
    c.tool({
      name: 'ssh_simulate_set_rule',
      description: '为模拟服务端设置命令应答规则：[{commandMatch, reply, exitCode?, stderr?, delay?, regex?}]。commandMatch 精确匹配或 "*" 通配；regex:true 时按正则匹配。',
      parameters: {
        type: 'object', required: ['serverId', 'rules'],
        properties: {
          serverId: { type: 'string' },
          rules: { type: 'array', items: { type: 'object' } },
        },
      },
      handler: (args) => handleSimSetRule(args || {}),
    });
    c.provide && c.provide('comm-ssh');
  },
  start() {
    // 常驻进程启动即就绪；连接/隧道/模拟服务端按需创建。
  },
  stop() {
    for (const [, rec] of clients) { try { rec.client.end(); } catch (_) {} }
    clients.clear();
    for (const [, rec] of tunnels) { try { rec.server.close(); } catch (_) {} }
    tunnels.clear();
    for (const [, sim] of simulateServers) { try { sim.server.close(); } catch (_) {} }
    simulateServers.clear();
  },
};
