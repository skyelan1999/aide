// Isolated browser check using the current main-page CSP. All requests are
// fulfilled locally; this is not a full DOCX or Draw.io exploit reproduction.
const { chromium } = require('playwright-core');
const fs = require('fs');
(async () => {
  const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH || '/usr/bin/chromium', headless: true, args: ['--no-sandbox'] });
  try {
    const page = await browser.newPage();
    const server = fs.readFileSync('internal/server/server.go', 'utf8');
    const match = server.match(/"default-src 'self'; script-src 'self' 'wasm-unsafe-eval'[^"\n]+/);
    if (!match) throw Error('Main-page CSP extraction anchor changed');
    const csp = match[0].slice(1);
    await page.route('**/*', route => route.fulfill({
      status: 200, contentType: 'text/html',
      headers: { 'Content-Security-Policy': csp },
      body: route.request().url() === 'http://aide.test/'
        ? '<!doctype html><a id="bad" href="javascript:window.__executed=1">link</a><style>body{--docx-probe:allowed}</style><iframe src="/frame"></iframe>'
        : '<!doctype html><p>Isolated frame fixture</p>'
    }));
    await page.goto('http://aide.test/');
    await page.click('#bad');
    const result = await page.evaluate(() => ({
      scriptExecuted: window.__executed === 1,
      inlineStyleApplied: getComputedStyle(document.body).getPropertyValue('--docx-probe') === 'allowed'
    }));
    await page.evaluate(() => { document.querySelector('iframe').src = 'http://other.test/'; });
    await page.waitForTimeout(200);
    result.externalFrameLoaded = page.frames().some(frame => frame.url().startsWith('http://other.test/'));
    console.log(JSON.stringify(result));
    if (result.scriptExecuted || result.inlineStyleApplied || result.externalFrameLoaded) throw Error('Unexpected CSP behavior');
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
