'use strict';
const {test}=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),os=require('node:os'),path=require('node:path'),crypto=require('node:crypto');
const {target,verify,noLinks,main,managerRequest}=require('./codex-host-control.cjs');
test('six supported targets, no unsupported fallback',()=>{for(const p of ['win32','darwin','linux'])for(const a of ['x64','arm64'])assert.match(target(p,a),/^codex-bridge-/);assert.throws(()=>target('win32','ia32'));assert.throws(()=>target('unknown','x64'));});
test('checksum rejects absent and changed binary',()=>{const d=fs.mkdtempSync(path.join(os.tmpdir(),'host-check-'));try{const f=path.join(d,target());fs.writeFileSync(f,'fixture');const hash=crypto.createHash('sha256').update('fixture').digest('hex'),s=hash+'  '+path.basename(f);assert.equal(verify(f,s),hash);assert.throws(()=>verify(f,''));fs.writeFileSync(f,'tampered');assert.throws(()=>verify(f,s));}finally{fs.rmSync(d,{recursive:true});}});
test('rejects symlink state without modifying destination',()=>{const d=fs.mkdtempSync(path.join(os.tmpdir(),'host-link-'));try{fs.symlinkSync(os.tmpdir(),path.join(d,'link'));assert.throws(()=>noLinks(path.join(d,'link','metadata')));}finally{fs.rmSync(d,{recursive:true});}});
test('arbitrary commands and incomplete launch arguments rejected',async()=>{await assert.rejects(main(['exec','--root','/tmp']));await assert.rejects(main(['start','--root','relative']));await assert.rejects(main(['start','--root','/tmp','--command','whoami']));});

const http = require('node:http');

test('manager control preserves the approved authority and refuses denial', async () => {
  const token = 'a'.repeat(64);
  const server = http.createServer((request, response) => {
    if (request.headers.host !== 'host.docker.internal:17888' ||
        request.headers.authorization !== 'Bearer ' + token ||
        request.headers['x-manager-owner-ui'] !== '1') {
      response.writeHead(403).end();
      return;
    }
    if (request.method === 'GET' && request.url === '/api/status')
      return response.end(JSON.stringify({ service: 'aide-codex-bridge-manager' }));
    if (request.method === 'POST' && request.url === '/api/quit')
      return response.end(JSON.stringify({ closing: true }));
    response.writeHead(404).end();
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  try {
    const request = (route, method, key = token) => managerRequest('127.0.0.1', server.address().port, route, method, key);
    assert.deepEqual(await request('status', 'GET'), { service: 'aide-codex-bridge-manager' });
    assert.deepEqual(await request('quit', 'POST'), { closing: true });
    await assert.rejects(request('quit', 'POST', 'b'.repeat(64)), /HTTP 403/);
  } finally {
    await new Promise(resolve => server.close(resolve));
  }
});
