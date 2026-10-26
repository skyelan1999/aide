'use strict';
// Actual-source component acceptance. Mock APIs and microphone; no full workbench claim.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {chromium} = require('./browser-runtime/node_modules/playwright');
const {popupSource} = require('./test_reminder_popup.cjs');
const {recorderSource} = require('./test_voice_sample_duration.cjs');

async function main() {
  const out = process.env.AIDE_COMPONENT_EVIDENCE_DIR;
  assert.ok(out, 'AIDE_COMPONENT_EVIDENCE_DIR must name an owned evidence directory');
  fs.mkdirSync(out, {recursive: true});
  const app = fs.readFileSync('internal/server/web/app.js', 'utf8');
  const dialog = app.match(/<dialog id="reminder-alert"[\s\S]*?<\/dialog>/)?.[0];
  assert.ok(dialog, 'production reminder dialog found');
  const css = ['themes/dark/tokens.css', 'themes/light/tokens.css', 'style.css', 'macos.css', 'workbench.css']
    .map(file => fs.readFileSync('internal/server/web/' + file, 'utf8')).join('\n');
  const browser = await chromium.launch({executablePath: '/usr/bin/chromium', headless: true,
    args: ['--no-sandbox', '--disable-dev-shm-usage']});
  const checks = [], errors = [];
  let context;
  try {
    context = await browser.newContext();
    const page = await context.newPage();
    page.on('pageerror', error => errors.push(error.message));
    for (const [theme, width] of [['light', 1440], ['dark', 390]]) {
      await page.setViewportSize({width, height: 1000});
      await page.setContent('<html data-theme="' + theme + '"><head><meta charset="utf-8"></head><body style="display:block">' +
        '<main style="padding:16px;max-width:700px;margin:auto"><h1>组件验收 · 模拟数据</h1>' +
        '<button id="check-reminders">检查到期提醒</button><div id="voice-fixture"></div></main>' + dialog + '</body></html>');
      await page.addStyleTag({content: css});
      await page.evaluate(() => {
        window.$ = id => document.getElementById(id);
        window.el = (tag, cls = '', text = '') => {
          const element = document.createElement(tag);element.className = cls;element.textContent = text;return element;
        };
        window.t = (text, ...args) => text.replace(/\{(\d+)\}/g, (_, i) => args[Number(i)]);
        window.fmt = value => value;
        window.action = fn => fn;
        window.toast = () => {};
        window.refreshConfig = async () => {};
        window.state = {config: {}, token: 'owned-fixture-token'};
        window.fixture = {seen: [], markers: [], fail: true, uploads: [], samples: [], tracksStopped: 0, now: 1000};
        window.center = {messageUI: {allowReminderPopup: () => true}};
        window.popupBusy = false;
        window.api = async url => {
          if (url === '/reminders/due') return ['a', 'b', 'c'].filter(id => !fixture.seen.includes(id))
            .map(id => ({id, title: '模拟提醒 ' + id.toUpperCase(), priority: 'normal', dueAt: '2026-10-26 18:00'}));
          if (url.endsWith('/popup-seen')) {
            const id = url.split('/')[2];
            fixture.markers.push({id, open: $('reminder-alert').open, rows: $('reminder-alert-items').children.length});
            if (id === 'b' && fixture.fail) throw Error('owned marker failure');
            fixture.seen.push(id);return {};
          }
          if (url === '/voice-samples') return {samples: fixture.samples};
          throw Error('unexpected fixture API: ' + url);
        };
        Object.defineProperty(performance, 'now', {configurable: true, value: () => fixture.now});
        Object.defineProperty(navigator, 'mediaDevices', {configurable: true, value: {
          getUserMedia: async () => {fixture.now += 20000;return {getTracks: () => [{stop() {fixture.tracksStopped++;}}]};}
        }});
        window.MediaRecorder = class {
          constructor() {this.state = 'inactive';this.mimeType = 'audio/webm';fixture.recorder = this;}
          start() {this.state = 'recording';fixture.started = fixture.now;}
          stop() {this.state = 'inactive';fixture.stopped = fixture.now;}
          async deliver() {
            this.ondataavailable({data: new Blob(['owned-audio'], {type: this.mimeType})});
            await this.onstop();
          }
        };
        window.fetch = async (url, options) => {
          fixture.uploads.push({url, type: options.body.type, audio: await options.body.text()});
          fixture.samples.push({id: 'owned-sample', durationSec: Number(new URL(url, 'http://owned.invalid').searchParams.get('durationSec')),
            createdAt: '2026-10-26T18:00:00Z', bytes: options.body.size});
          return {ok: true, json: async () => ({id: 'owned-sample'})};
        };
      });
      await page.addScriptTag({content: popupSource() + '\n' + recorderSource()});
      await page.evaluate(() => {
        $('check-reminders').onclick = checkDuePopups;
        const close = () => {$('reminder-alert').close();popupBusy = false;};
        $('reminder-alert-close').onclick = close;$('reminder-alert-done').onclick = close;
        $('reminder-alert').addEventListener('close', () => {popupBusy = false;});
        $('voice-fixture').append(renderVoiceCloneControl());
      });
      await page.getByRole('button', {name: '检查到期提醒', exact: true}).click();
      await page.waitForFunction(() => fixture.markers.length === 3);
      assert.equal(await page.locator('#reminder-alert').evaluate(node => node.open), true);
      const markers = await page.evaluate(() => fixture.markers);
      assert.ok(markers.every(marker => marker.open && marker.rows === 3));
      await page.getByRole('button', {name: '知道了', exact: true}).focus();
      await page.screenshot({path: path.join(out, theme + '-reminders.png')});
      await page.keyboard.press('Enter');
      await page.evaluate(() => {fixture.fail = false;});
      await page.getByRole('button', {name: '检查到期提醒', exact: true}).click();
      await page.waitForFunction(() => fixture.seen.length === 3);
      assert.equal(await page.locator('#reminder-alert-items').textContent(), '模拟提醒 Bnormal 优先级 · 2026-10-26 18:00');
      await page.getByRole('button', {name: '知道了', exact: true}).click();
      await page.locator('#voice-fixture input[type=checkbox]').check();
      await page.getByRole('button', {name: '开始录音', exact: true}).click();
      await page.waitForFunction(() => fixture.recorder?.state === 'recording');
      await page.evaluate(() => {fixture.now = fixture.started + 40000;});
      await page.getByRole('button', {name: '停止并上传', exact: true}).click();
      await page.evaluate(async () => {fixture.now += 3500;await fixture.recorder.deliver();});
      await page.waitForFunction(() => document.querySelector('.voice-history-item')?.textContent.includes('40s'));
      const voice = await page.evaluate(() => ({uploads: fixture.uploads, tracksStopped: fixture.tracksStopped}));
      assert.equal(voice.uploads.length, 1);
      assert.equal(new URL(voice.uploads[0].url, 'http://owned.invalid').searchParams.get('durationSec'), '40');
      assert.equal(voice.uploads[0].type, 'audio/webm');assert.equal(voice.uploads[0].audio, 'owned-audio');
      assert.equal(voice.tracksStopped, 1);
      assert.equal(await page.getByRole('button', {name: '开始录音', exact: true}).isEnabled(), true);
      await page.screenshot({path: path.join(out, theme + '-voice.png')});
      checks.push({theme, width, result: 'PASS', scope: 'native dialog, pointer/keyboard, partial marker failure/retry; mocked 40s recording, permission/encoding exclusion, displayed duration'});
    }
    assert.deepEqual(errors, []);
    fs.writeFileSync(path.join(out, 'result.json'), JSON.stringify({checks, errors, fullWorkbench: false, realMicrophone: false}, null, 2) + '\n');
    console.log(JSON.stringify(checks));
  } finally {
    if (context) await context.close();
    await browser.close();
  }
}
main().catch(error => {console.error(error);process.exitCode = 1;});
