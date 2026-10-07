'use strict';

// Narrow macOS desktop relay. It exposes only frontmost-app scoped screenshot,
// click, text entry and a small key allowlist. No shell or arbitrary file API.
const http = require('node:http');
const { spawn } = require('node:child_process');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const crypto = require('node:crypto');

const PORT = Number(process.env.AIDE_COMPUTER_BRIDGE_PORT || 17778);
const TOKEN = process.env.AIDE_COMPUTER_BRIDGE_TOKEN || '';
const BODY_LIMIT = 16 * 1024;
const MAX_TEXT = 2000;
const KEYS = Object.freeze({ ENTER: 'Return', TAB: 'tab', ESCAPE: 'Escape', BACKSPACE: 'delete', DELETE: 'forward delete', UP: 'up arrow', DOWN: 'down arrow', LEFT: 'left arrow', RIGHT: 'right arrow', SPACE: 'space' });

function appAllowed(name, rules) { return Array.isArray(rules) && rules.includes(name); }
function pointAllowed(x, y, width, height) { return Number.isInteger(x) && Number.isInteger(y) && x >= 0 && y >= 0 && x < width && y < height; }
function keyAllowed(key) { return Object.prototype.hasOwnProperty.call(KEYS, String(key || '').toUpperCase()); }

function run(command, args, input) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { stdio: ['pipe', 'pipe', 'pipe'], timeout: 10000 });
    let stdout = '', stderr = '';
    child.stdout.on('data', chunk => { stdout += chunk; if (stdout.length > 20000) child.kill(); });
    child.stderr.on('data', chunk => { stderr += chunk; if (stderr.length > 4000) child.kill(); });
    child.on('error', reject);
    child.on('close', code => code === 0 ? resolve(stdout.trim()) : reject(new Error((stderr || `${command} 退出码 ${code}`).slice(0, 500))));
    child.stdin.end(input);
  });
}

async function frontmostApp() {
  const script = 'tell application "System Events" to get name of first process whose frontmost is true';
  return run('/usr/bin/osascript', ['-e', script]);
}
async function assertApp(rules) {
  if (!Array.isArray(rules) || rules.length < 1 || rules.length > 50 || rules.some(value => typeof value !== 'string' || value.length > 120)) throw new Error('应用允许列表无效');
  const app = await frontmostApp();
  if (!appAllowed(app, rules)) throw new Error(`当前前台应用“${app}”不在允许列表中`);
  return app;
}
function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = []; let size = 0;
    req.on('data', chunk => { size += chunk.length; if (size > BODY_LIMIT) { reject(new Error('请求过大')); req.destroy(); return; } chunks.push(chunk); });
    req.on('end', () => { try { resolve(size ? JSON.parse(Buffer.concat(chunks).toString('utf8')) : {}); } catch (_) { reject(new Error('JSON 请求体无效')); } });
    req.on('error', reject);
  });
}
function reply(res, status, value) { const body = Buffer.from(JSON.stringify(value)); res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', 'Content-Length': body.length, 'Cache-Control': 'no-store' }); res.end(body); }

async function route(req, res) {
  if (TOKEN.length < 32) return reply(res, 503, { ok: false, error: 'Aide 电脑桥接令牌未配置' });
  const peer = String(req.socket.remoteAddress || '').replace(/^::ffff:/, '');
  const localOrDocker = peer === '::1' || peer.startsWith('127.') || peer.startsWith('192.168.65.') || /^172\.(1[6-9]|2\d|3[01])\./.test(peer);
  if (!localOrDocker) return reply(res, 403, { ok: false, error: '桥接服务仅接受本机和 Docker Desktop 私有网络请求' });
  if (req.headers.authorization !== `Bearer ${TOKEN}`) return reply(res, 401, { ok: false, error: '未授权' });
  const pathname = new URL(req.url, 'http://localhost').pathname;
  if (req.method === 'GET' && pathname === '/healthz') return reply(res, 200, { ok: true, service: 'aide-computer-bridge' });
  if (req.method !== 'POST' || !['/v1/snapshot', '/v1/click', '/v1/type', '/v1/key'].includes(pathname)) return reply(res, 404, { ok: false, error: '不支持的电脑操作' });
  let input; try { input = await readBody(req); } catch (error) { return reply(res, 400, { ok: false, error: error.message }); }
  try {
    const app = await assertApp(input.allowedApps);
    if (pathname === '/v1/snapshot') {
      const file = path.join(os.tmpdir(), `aide-screen-${crypto.randomUUID()}.png`);
      try { await run('/usr/sbin/screencapture', ['-x', file]); const bytes = await fs.readFile(file); return reply(res, 200, { ok: true, app, mimeType: 'image/png', imageBase64: bytes.toString('base64') }); }
      finally { await fs.rm(file, { force: true }).catch(() => {}); }
    }
    if (pathname === '/v1/click') {
      if (!Number.isInteger(input.x) || !Number.isInteger(input.y) || input.x < 0 || input.y < 0 || input.x > 20000 || input.y > 20000) throw new Error('屏幕坐标超出允许范围');
      const script = `tell application "System Events" to click at {${input.x}, ${input.y}}`;
      await run('/usr/bin/osascript', ['-e', script]);
      return reply(res, 200, { ok: true, app, clicked: true });
    }
    if (pathname === '/v1/type') {
      if (typeof input.text !== 'string' || input.text.length > MAX_TEXT) throw new Error('输入内容超过限制');
      const script = 'on run argv\n tell application "System Events" to keystroke (item 1 of argv)\nend run';
      await run('/usr/bin/osascript', ['-', input.text], script);
      return reply(res, 200, { ok: true, app, typed: true });
    }
    if (!keyAllowed(input.key)) throw new Error('不支持该按键');
    const key = KEYS[String(input.key).toUpperCase()];
    const script = `tell application "System Events" to key code ${({ Return: 36, tab: 48, Escape: 53, delete: 51, 'forward delete': 117, 'up arrow': 126, 'down arrow': 125, 'left arrow': 123, 'right arrow': 124, space: 49 })[key]}`;
    await run('/usr/bin/osascript', ['-e', script]);
    return reply(res, 200, { ok: true, app, key: input.key });
  } catch (error) { return reply(res, 502, { ok: false, error: String(error.message || error).slice(0, 500) }); }
}

if (require.main === module) {
  const server = http.createServer((req, res) => { void route(req, res); });
  server.headersTimeout = 5000; server.requestTimeout = 10000;
  server.listen(PORT, '0.0.0.0', () => console.log(`aide computer bridge listening on ${PORT}`));
}
module.exports = { appAllowed, pointAllowed, keyAllowed };
