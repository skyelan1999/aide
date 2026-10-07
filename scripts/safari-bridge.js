'use strict';

// Small macOS host relay for Safari's W3C WebDriver. It exposes only the
// bounded browser operations used by plugins/browser-control, never the raw
// WebDriver endpoint. The listener is reachable from Docker Desktop, so every
// request requires the per-install token managed by scripts/aide.sh.
const http = require('node:http');
const { spawn } = require('node:child_process');

const PORT = Number(process.env.AIDE_SAFARI_BRIDGE_PORT || 17777);
const DRIVER_PORT = Number(process.env.AIDE_SAFARI_DRIVER_PORT || 4444);
const TOKEN = process.env.AIDE_BROWSER_BRIDGE_TOKEN || '';
const BODY_LIMIT = 64 * 1024;
const TEXT_LIMIT = 30000;
let driverProcess;
let sessionId = '';
let queue = Promise.resolve();

function allowedHost(host, rules) {
  const normalized = String(host || '').toLowerCase().replace(/\.$/, '');
  return Array.isArray(rules) && rules.some(rule => {
    const value = String(rule || '').trim().toLowerCase().replace(/\.$/, '');
    if (value.startsWith('*.')) return normalized.endsWith(value.slice(1)) && normalized !== value.slice(2);
    return normalized === value;
  });
}

function safeURL(raw, rules) {
  let url;
  try { url = new URL(String(raw || '')); } catch (_) { throw new Error('无效网页 URL'); }
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) throw new Error('只允许不含账号信息的 HTTP/HTTPS URL');
  if (!allowedHost(url.hostname, rules)) throw new Error('网站不在插件允许列表中');
  return url.href;
}

async function driver(path, method = 'GET', body) {
  const response = await fetch(`http://127.0.0.1:${DRIVER_PORT}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(30000),
  });
  const result = await response.json().catch(() => ({}));
  if (!response.ok || result.value?.error) throw new Error(String(result.value?.message || result.error || `Safari WebDriver HTTP ${response.status}`).slice(0, 500));
  return result.value;
}

async function ensureSession() {
  if (sessionId) return;
  if (!driverProcess || driverProcess.exitCode !== null) {
    driverProcess = spawn('/usr/bin/safaridriver', ['--port', String(DRIVER_PORT)], { stdio: 'ignore', detached: true });
    driverProcess.unref();
  }
  const startedAt = Date.now();
  let lastError;
  while (Date.now() - startedAt < 8000) {
    try {
      await driver('/status');
      const value = await driver('/session', 'POST', { capabilities: { alwaysMatch: { browserName: 'safari' } } });
      sessionId = value.sessionId || value.session?.sessionId || '';
      if (!sessionId) throw new Error('Safari WebDriver 未返回 sessionId');
      return;
    } catch (error) {
      lastError = error;
      await new Promise(resolve => setTimeout(resolve, 250));
    }
  }
  throw new Error(`无法启动 Safari WebDriver：${lastError?.message || '超时'}。请在 macOS Safari 开发者设置中启用“允许远程自动化”。`);
}

async function currentPage(rules) {
  await ensureSession();
  const base = `/session/${encodeURIComponent(sessionId)}`;
  const value = await driver(`${base}/execute/sync`, 'POST', {
    script: 'return {url: location.href, title: document.title || "", text: document.body ? document.body.innerText : ""};',
    args: [],
  });
  const url = String(value?.url || '');
  const parsed = new URL(url);
  if (!allowedHost(parsed.hostname, rules)) throw new Error('当前页面不在允许列表中，已拒绝读取');
  return { url, title: String(value.title || '').slice(0, 500), text: String(value.text || '').slice(0, TEXT_LIMIT) };
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let size = 0;
    req.on('data', chunk => {
      size += chunk.length;
      if (size > BODY_LIMIT) { reject(new Error('请求过大')); req.destroy(); return; }
      chunks.push(chunk);
    });
    req.on('end', () => {
      try { resolve(size ? JSON.parse(Buffer.concat(chunks).toString('utf8')) : {}); }
      catch (_) { reject(new Error('JSON 请求体无效')); }
    });
    req.on('error', reject);
  });
}

function reply(res, status, value) {
  const body = Buffer.from(JSON.stringify(value));
  res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', 'Content-Length': body.length, 'Cache-Control': 'no-store' });
  res.end(body);
}

async function route(req, res) {
  if (TOKEN.length < 32) return reply(res, 503, { ok: false, error: 'Aide Safari 桥接令牌未配置' });
  const peer = String(req.socket.remoteAddress || '').replace(/^::ffff:/, '');
  const dockerDesktopPeer = peer === '::1' || peer.startsWith('127.') || peer.startsWith('192.168.65.') || /^172\.(1[6-9]|2\d|3[01])\./.test(peer);
  if (!dockerDesktopPeer) return reply(res, 403, { ok: false, error: '桥接服务只接受本机和 Docker Desktop 私有网络请求' });
  if (req.headers.authorization !== `Bearer ${TOKEN}`) return reply(res, 401, { ok: false, error: '未授权' });
  const path = new URL(req.url, 'http://localhost').pathname;
  if (req.method === 'GET' && path === '/healthz') return reply(res, 200, { ok: true, service: 'aide-safari-bridge', driver: 'Safari WebDriver', session: Boolean(sessionId) });
  if (req.method !== 'POST' || !['/v1/snapshot', '/v1/navigate', '/v1/click', '/v1/fill'].includes(path)) return reply(res, 404, { ok: false, error: '不支持的浏览器操作' });
  let input;
  try { input = await readBody(req); } catch (error) { return reply(res, 400, { ok: false, error: error.message }); }
  const rules = input.allowedHosts;
  if (!Array.isArray(rules) || rules.length > 100) return reply(res, 403, { ok: false, error: '网站允许列表无效' });
  try {
    const result = await (queue = queue.catch(() => {}).then(async () => {
      await ensureSession();
      const base = `/session/${encodeURIComponent(sessionId)}`;
      if (path === '/v1/navigate') {
        const target = safeURL(input.url, rules);
        await driver(`${base}/url`, 'POST', { url: target });
        const page = await currentPage(rules);
        return { ok: true, ...page };
      }
      if (path === '/v1/click') {
        const selector = String(input.selector || '');
        if (!selector.trim() || selector.length > 500) throw new Error('CSS 选择器必须为 1–500 字符');
        await currentPage(rules);
        const value = await driver(`${base}/execute/sync`, 'POST', { script: 'const e=document.querySelector(arguments[0]); if(!e) throw new Error("未找到目标"); e.click(); return true;', args: [selector] });
        return { ok: true, clicked: Boolean(value), page: await currentPage(rules) };
      }
      if (path === '/v1/fill') {
        const selector = String(input.selector || ''), text = String(input.text || '');
        if (!selector.trim() || selector.length > 500 || text.length > 4000) throw new Error('输入目标或文本超出允许长度');
        await currentPage(rules);
        const value = await driver(`${base}/execute/sync`, 'POST', { script: 'const e=document.querySelector(arguments[0]); if(!e) throw new Error("未找到输入目标"); if(!(e instanceof HTMLInputElement||e instanceof HTMLTextAreaElement||e.isContentEditable)) throw new Error("目标不是可编辑控件"); e.focus(); if(e.isContentEditable) e.textContent=arguments[1]; else { const setter=Object.getOwnPropertyDescriptor(e instanceof HTMLTextAreaElement?HTMLTextAreaElement.prototype:HTMLInputElement.prototype,"value").set; setter.call(e,arguments[1]); } e.dispatchEvent(new Event("input",{bubbles:true})); e.dispatchEvent(new Event("change",{bubbles:true})); return true;', args: [selector, text] });
        return { ok: true, filled: Boolean(value), page: await currentPage(rules) };
      }
      return { ok: true, ...await currentPage(rules) };
    }));
    return reply(res, 200, result);
  } catch (error) {
    return reply(res, 502, { ok: false, error: String(error.message || error).slice(0, 500) });
  }
}

const server = http.createServer((req, res) => { void route(req, res); });
server.headersTimeout = 5000;
server.requestTimeout = 10000;
server.listen(PORT, '0.0.0.0', () => console.log(`aide Safari bridge listening on ${PORT}`));

module.exports = { allowedHost, safeURL };
