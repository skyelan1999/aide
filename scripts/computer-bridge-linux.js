'use strict';

// X11 host bridge for Aide. The bridge only targets the current foreground
// window and exposes screenshot, click, text entry and a small key allowlist.
const http = require('node:http');
const { execFile } = require('node:child_process');
const { promisify } = require('node:util');
const execFileAsync = promisify(execFile);

const PORT = Number(process.env.AIDE_COMPUTER_BRIDGE_PORT || 17778);
const TOKEN = process.env.AIDE_COMPUTER_BRIDGE_TOKEN || '';
const BODY_LIMIT = 16 * 1024;
const MAX_IMAGE = 2 * 1024 * 1024;
const KEYS = Object.freeze({ ENTER: 'Return', TAB: 'Tab', ESCAPE: 'Escape', BACKSPACE: 'BackSpace', DELETE: 'Delete', UP: 'Up', DOWN: 'Down', LEFT: 'Left', RIGHT: 'Right', SPACE: 'space' });

function peerAllowed(address) {
  const ip = String(address || '').replace(/^::ffff:/, '');
  return ip === '::1' || ip.startsWith('127.') || ip.startsWith('192.168.65.') || /^172\.(1[6-9]|2\d|3[01])\./.test(ip) || /^172\.1[6-9]\./.test(ip);
}
function reply(res, status, value) {
  const body = Buffer.from(JSON.stringify(value));
  res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', 'Content-Length': body.length, 'Cache-Control': 'no-store' });
  res.end(body);
}
function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = []; let size = 0;
    req.on('data', chunk => { size += chunk.length; if (size > BODY_LIMIT) { reject(new Error('请求过大')); req.destroy(); return; } chunks.push(chunk); });
    req.on('end', () => { try { resolve(size ? JSON.parse(Buffer.concat(chunks).toString('utf8')) : {}); } catch (_) { reject(new Error('JSON 请求体无效')); } });
    req.on('error', reject);
  });
}
async function command(file, args, options = {}) {
  const result = await execFileAsync(file, args, { timeout: 8000, maxBuffer: 4 * 1024 * 1024, encoding: options.encoding === 'buffer' ? null : 'utf8', windowsHide: true });
  return result.stdout;
}
async function foreground() {
  const id = String(await command('xdotool', ['getwindowfocus'])).trim();
  if (!/^0x[0-9a-f]+$|^[0-9]+$/i.test(id)) throw new Error('无法识别当前前台窗口');
  const pid = String(await command('xdotool', ['getwindowpid', id])).trim();
  if (!/^[1-9][0-9]*$/.test(pid)) throw new Error('无法识别当前前台应用');
  const title = String(await command('xdotool', ['getwindowname', id])).trim().slice(0, 240);
  let process = '';
  try { process = String(await command('cat', [`/proc/${pid}/comm`])).trim().slice(0, 120); } catch (_) { }
  if (!process) throw new Error('无法读取当前前台应用名称');
  return { id, pid, title, process };
}
function allowed(target, rules) {
  return Array.isArray(rules) && rules.length > 0 && rules.length <= 50 && rules.every(x => typeof x === 'string' && x.trim().length > 0 && x.length <= 120) && (rules.includes(target.process) || rules.includes(target.title));
}
async function assertAllowed(rules) {
  const target = await foreground();
  if (!allowed(target, rules)) throw new Error(`当前前台应用“${target.process}”不在允许列表中`);
  return target;
}
async function handle(req, res) {
  if (TOKEN.length < 32) return reply(res, 503, { ok: false, error: 'Aide 电脑桥接令牌未配置' });
  if (!peerAllowed(req.socket.remoteAddress)) return reply(res, 403, { ok: false, error: '桥接服务仅接受本机和 Docker 私有网络请求' });
  if (req.headers.authorization !== `Bearer ${TOKEN}`) return reply(res, 401, { ok: false, error: '未授权' });
  const route = new URL(req.url, 'http://localhost').pathname;
  if (req.method === 'GET' && route === '/healthz') return reply(res, 200, { ok: true, pid: process.pid, service: 'aide-linux-computer-bridge', platform: 'linux-x11', accessibilityInspection: false, permissions: { screenRecording: { required: false, granted: true, supported: true }, accessibility: { required: false, granted: false, supported: false } } });
  if (req.method !== 'POST' || !['/v1/inspect', '/v1/snapshot', '/v1/click', '/v1/type', '/v1/key'].includes(route)) return reply(res, 404, { ok: false, error: '不支持的电脑操作' });
  let input; try { input = await readBody(req); } catch (error) { return reply(res, 400, { ok: false, error: error.message }); }
  try {
    const target = await assertAllowed(input.allowedApps);
    if (route === '/v1/inspect') return reply(res, 501, { ok: false, error: 'Linux X11 桥接暂不提供辅助功能控件树；请使用 computer_snapshot 截图。' });
    if (route === '/v1/snapshot') {
      const image = await command('import', ['-window', target.id, 'png:-'], { encoding: 'buffer' });
      if (!Buffer.isBuffer(image) || image.length < 8 || image.length > MAX_IMAGE || image.subarray(0, 8).toString('hex') !== '89504e470d0a1a0a') throw new Error('截图无效或超过 2 MiB');
      const after = await foreground();
      if (after.pid !== target.pid || after.id !== target.id) throw new Error('前台应用已改变，截图未返回');
      return reply(res, 200, { ok: true, app: target.process, mimeType: 'image/png', imageBase64: image.toString('base64') });
    }
    if (input.approved !== true) throw new Error('电脑操作需要 Aide 确认');
    const current = await assertAllowed(input.allowedApps);
    if (current.id !== target.id || current.pid !== target.pid) throw new Error('前台应用已改变，操作未执行');
    if (route === '/v1/click') {
      if (!Number.isInteger(input.x) || !Number.isInteger(input.y) || input.x < 0 || input.y < 0 || input.x > 20000 || input.y > 20000) throw new Error('屏幕坐标超出允许范围');
      await command('xdotool', ['mousemove', '--sync', String(input.x), String(input.y), 'click', '1']);
      return reply(res, 200, { ok: true, app: target.process, clicked: true });
    }
    if (route === '/v1/type') {
      if (typeof input.text !== 'string' || Array.from(input.text).length > 2000) throw new Error('输入内容超过限制');
      await command('xdotool', ['type', '--clearmodifiers', '--delay', '1', '--', input.text]);
      return reply(res, 200, { ok: true, app: target.process, typed: true });
    }
    const key = String(input.key || '').toUpperCase();
    if (!Object.prototype.hasOwnProperty.call(KEYS, key)) throw new Error('不支持该按键');
    await command('xdotool', ['key', '--clearmodifiers', KEYS[key]]);
    return reply(res, 200, { ok: true, app: target.process, key });
  } catch (error) { return reply(res, 502, { ok: false, error: String(error.message || error).slice(0, 500) }); }
}

if (require.main === module) {
  if (process.env.XDG_SESSION_TYPE !== 'x11' || !process.env.DISPLAY) throw new Error('Aide Linux Computer Bridge requires an X11 desktop session');
  const server = http.createServer((req, res) => { void handle(req, res); });
  server.headersTimeout = 5000; server.requestTimeout = 10000;
  server.listen(PORT, '0.0.0.0', () => console.log(`Aide Linux computer bridge listening on ${PORT} (X11)`));
}

module.exports = { peerAllowed, allowed };
