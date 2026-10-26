'use strict';
// Actual Chromium + current transfer UI, isolated HTTP API fixtures. Backend
// snapshot consistency and restart behavior are covered by TestIssue78.
const assert = require('node:assert/strict');
const http = require('node:http');
const fs = require('node:fs/promises');
const path = require('node:path');
const { chromium } = require('./browser-runtime/node_modules/playwright');
async function main() {
  const created = [], jobs = new Map(), requested = [];
  const source = await fs.readFile(path.join(__dirname, '../internal/server/web/file-transfers.js'));
  const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, 'http://fixture.local');
    if (url.pathname === '/file-transfers.js') { res.setHeader('Content-Type', 'application/javascript; charset=utf-8'); return res.end(source); }
    if (url.pathname === '/api/file/transfers' && req.method === 'POST') {
      let body = ''; for await (const part of req) body += part;
      const input = JSON.parse(body); created.push(input);
      const job = { ...input, id: 'snapshot-' + created.length, status: 'complete', size: 16, completed: 16, tops: [{ source: input.paths[0] }] };
      jobs.set(job.id, job); res.setHeader('Content-Type', 'application/json'); return res.end(JSON.stringify(job));
    }
    if (url.pathname === '/api/file/transfers') { res.setHeader('Content-Type', 'application/json'); return res.end(JSON.stringify([...jobs.values()])); }
    if (url.pathname.startsWith('/api/file/transfers/')) { res.setHeader('Content-Type', 'application/json'); return res.end(JSON.stringify(jobs.get(url.pathname.split('/').pop()))); }
    if (url.pathname === '/api/file/stream') {
      requested.push(url.search); const job = jobs.get(url.searchParams.get('job'));
      if (!job) { res.statusCode = 400; return res.end('mutable URL rejected'); }
      const name = job.rawDownload ? 'device.bin' : 'device.bin.zip';
      res.setHeader('Content-Type', 'application/octet-stream'); res.setHeader('Content-Disposition', `attachment; filename="${name}"`);
      return res.end(job.rawDownload ? 'raw-snapshot-body' : 'zip-snapshot-body');
    }
    res.setHeader('Content-Type', 'text/html; charset=utf-8');
    res.end(`<!doctype html><html lang="zh"><button id="refresh-files">Refresh</button>
      <button id="raw">Download file</button><button id="archive">Download ZIP</button>
      <script>window.notices=[];window.AideAsyncAction={wrap:(fn,onError)=>(...args)=>Promise.resolve().then(()=>fn(...args)).catch(onError)};</script>
      <script src="/file-transfers.js"></script><script>
      const services={t:s=>s,scope:()=>"owned-workspace",blocked:()=>false,token:()=>"owned-token",target:()=>({root:"workspace",writable:true}),base:()=>".",notice:s=>notices.push(s),refresh:async()=>{},api:async(url,options)=>{const r=await fetch("/api"+url,options);if(!r.ok)throw Error(r.status);return r.json();}};
      Object.defineProperty(window,"showSaveFilePicker",{value:undefined,configurable:true});
      AideTransfers.mount(services);
      document.querySelector('#raw').onclick=()=>AideTransfers.download({name:"device.bin",path:"nested/device.bin"},false,{root:"workspace"});
      document.querySelector('#archive').onclick=()=>AideTransfers.download({name:"device.bin",path:"nested/device.bin"},true,{root:"workspace"});
      </script></html>`);
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  let browser;
  try {
    browser = await chromium.launch({ headless: true, ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {}) });
    const context = await browser.newContext({ acceptDownloads: true });
    const page = await context.newPage();
    const errors = []; page.on('pageerror', error => errors.push(error.message));
    await page.goto(`http://127.0.0.1:${server.address().port}/`);
    for (const width of [1280, 390]) {
      await page.setViewportSize({ width, height: 800 });
      for (const raw of [true, false]) {
        const waiting = page.waitForEvent('download');
        await page.click(raw ? '#raw' : '#archive');
        const download = await waiting;
        assert.equal(download.suggestedFilename(), raw ? 'device.bin' : 'device.bin.zip');
        assert.equal(await fs.readFile(await download.path(), 'utf8'), raw ? 'raw-snapshot-body' : 'zip-snapshot-body');
        const input = created.at(-1);
        assert.equal(input.rawDownload, raw); assert.deepEqual(input.paths, ['nested/device.bin']);
        assert.equal(input.workspaceId, 'owned-workspace');
        assert.match(requested.at(-1), /job=snapshot-/); assert(!requested.at(-1).includes('path='));
        assert.equal(await page.locator('.file-transfer-task').last().locator('small').first().textContent(), raw ? '下载' : '打包下载');
        await page.evaluate(() => document.querySelector('dialog').close());
      }
    }
    assert.deepEqual(errors, []);
    await context.close();
    console.log('PASS REAL Chromium: raw/ZIP snapshot job creation, original filename/content, desktop/narrow layout, immutable job URLs and task labels');
  } finally {
    if (browser) await browser.close();
    server.closeAllConnections(); await new Promise(resolve => server.close(resolve));
  }
}
main().catch(error => { console.error(error); process.exitCode = 1; });
