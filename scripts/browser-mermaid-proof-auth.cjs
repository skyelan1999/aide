'use strict';
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const {spawn} = require('node:child_process');
const {chromium} = require('./browser-runtime/node_modules/playwright');
async function main() {
  const out = process.env.AIDE_COMPONENT_EVIDENCE_DIR;
  assert.ok(out, 'set owned AIDE_COMPONENT_EVIDENCE_DIR');fs.mkdirSync(out, {recursive: true});
  const proofPath = path.resolve(out, 'browser-proof.json');
  assert.ok(!fs.existsSync(proofPath), 'use a fresh owned proof path');
  const server = spawn('python3', ['-u', 'scripts/fixtures/serve-mermaid-csp.py', '--port', '0', '--proof', proofPath], {stdio: ['ignore', 'pipe', 'pipe']});
  let browser, context, stderr = '';
  server.stderr.on('data', data => {stderr = (stderr + data).slice(-4000);});
  try {
    const base = await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(Error('owned proof server startup timeout')), 10000);
      server.once('error', error => {clearTimeout(timer);reject(error);});
      server.stdout.once('data', data => {clearTimeout(timer);resolve(String(data).trim());});
      server.once('exit', code => {clearTimeout(timer);reject(Error('owned proof server exited: ' + code));});
    });
    assert.match(base, /^http:\/\/127\.0\.0\.1:\d+\/$/);
    browser = await chromium.launch({executablePath: '/usr/bin/chromium', headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage']});
    context = await browser.newContext({viewport: {width: 1440, height: 1100}});
    const page = await context.newPage(), errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const saved = page.waitForResponse(response => response.url() === base + 'proof' && response.request().method() === 'POST');
    await page.goto(base, {waitUntil: 'load'});
    const response = await saved;assert.equal(response.status(), 204);
    await page.waitForFunction(() => document.querySelector('#result')?.textContent.startsWith('PASS'));
    const proof = JSON.parse(fs.readFileSync(proofPath));
    assert.equal(proof.status, 'pass');assert.equal(proof.strictCSP, true);assert.equal(proof.detachedCleanup, true);
    assert.equal(proof.checks.length, 3);assert.ok(proof.checks.every(check => check.diagrams === 4));
    await page.screenshot({path: path.join(out, 'mermaid-light.png'), fullPage: true});
    await page.getByRole('button', {name: '切换明暗'}).click();
    await page.waitForFunction(() => document.documentElement.dataset.theme === 'dark' && document.querySelectorAll('svg').length === 4);
    await page.screenshot({path: path.join(out, 'mermaid-dark.png'), fullPage: true});
    assert.deepEqual(errors, []);
    console.log('PASS real Mermaid vendor/browser strict-CSP rendering, light/dark, authenticated proof204 and receipt, repeated render and detached sheet cleanup');
  } catch (error) {console.error(stderr);throw error;}
  finally {
    if (context) await context.close();if (browser) await browser.close();
    if (server.exitCode === null) {
      const exited = new Promise(resolve => server.once('exit', resolve));server.kill('SIGTERM');await exited;
    }
  }
}
main().catch(error => {console.error(error);process.exitCode = 1;});
