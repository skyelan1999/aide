// Isolated mock acceptance for the voice panel lifecycle and send routing.
// Run: node scripts/test_voice_panel.cjs
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync('internal/server/web/app.js', 'utf8');
const voiceStart = source.indexOf('/* ── 语音小秘（Web Speech API 实时断句 + AI 甄别 + 直接发送）');
const voiceEnd = source.indexOf('/* ── 双向语音：', voiceStart);
const startFn = source.indexOf('async function voiceStart()', voiceEnd);
const startEnd = source.indexOf('// ===== 小秘语音导览', startFn);
assert.ok(voiceStart >= 0 && voiceEnd > voiceStart && startFn > voiceEnd && startEnd > startFn, 'voice source blocks found');

class FakeElement {
  constructor(id = '') {
    this.id = id;
    this.textContent = '';
    this.children = [];
    this.scrollHeight = 0;
    this.scrollTop = 0;
    this.classes = new Set();
    this.classList = {
      add: (...names) => names.forEach(n => this.classes.add(n)),
      remove: (...names) => names.forEach(n => this.classes.delete(n)),
      contains: name => this.classes.has(name),
      toggle: (name, force) => {
        const enabled = force === undefined ? !this.classes.has(name) : force;
        enabled ? this.classes.add(name) : this.classes.delete(name);
        return enabled;
      },
    };
  }
  append(...items) { this.children.push(...items); }
  replaceChildren(...items) { this.children = items; }
}

const ids = new Map();
const get = id => { if (!ids.has(id)) ids.set(id, new FakeElement(id)); return ids.get(id); };
const sentRuns = [];
const toasts = [];
let filterRelease;
let filterCalls = 0;
let micRelease;
let stoppedTracks = 0;
let rejectNextAudioTrack = true;
let streamSerial = 0;
function mockMicStream() {
  const id = ++streamSerial;
  const audioTrack = { id: 'selected-track-' + id, kind: 'audio', readyState: 'live' };
  return { getAudioTracks: () => [audioTrack], getTracks: () => [{ stop: () => stoppedTracks++ }] };
}
const recs = [];
class FakeRecognition {
  constructor() { recs.push(this); }
  start(...tracks) {
    if (tracks.length && rejectNextAudioTrack) { rejectNextAudioTrack = false; throw new Error('audioTrack overload unsupported'); }
    this.started = true;
    this.startTrack = tracks[0] || null;
  }
  stop() { this.stopped = true; }
  abort() { this.aborted = true; }
}
const state = {
  config: { configured: true, voiceInputDevice: 'mock-mic', voiceAssistantName: '小秘', voiceReplyEnabled: false },
  session: { id: 'session-1', kind: 'main', messages: [] },
  profiles: { strategy: 'manual', activeProfile: 'default' },
  queueMode: true, mode: 'chat', workflowPhase: '',
};
const context = {
  state,
  window: { SpeechRecognition: FakeRecognition, speechSynthesis: { getVoices: () => [] } },
  navigator: { mediaDevices: { getUserMedia: () => new Promise(resolve => { micRelease = resolve; }) } },
  $: get,
  el: (tag, cls = '', text = '') => { const e = new FakeElement(); e.tagName = tag; if (cls) e.classList.add(...cls.split(/\s+/)); e.textContent = text; return e; },
  t: s => s,
  toast: message => toasts.push(message),
  action: fn => fn,
  ttsCancel: () => {},
  clearTimeout, setTimeout,
  api: async (path, opts = {}) => {
    if (path === '/voice-filter') {
      filterCalls++;
      if (filterCalls === 1) return new Promise(resolve => { filterRelease = () => resolve({ action: 'send', text: '第一条指令', mode: 'queue' }); });
      if (opts.body.includes('第二条')) return { action: 'send', text: '第二条指令', mode: 'insert' };
      return { action: 'ignore', reason: '背景声' };
    }
    if (path.endsWith('/runs')) { sentRuns.push(JSON.parse(opts.body)); return {}; }
    throw new Error('Unexpected API path: ' + path);
  },
  selectSession: async () => {},
};
vm.createContext(context);
vm.runInContext(source.slice(voiceStart, voiceEnd) + '\nglobalThis.__voice = voice;', context, { filename: 'app.js voice pipeline' });
vm.runInContext(source.slice(startFn, startEnd) + '\nglobalThis.__voiceStart = voiceStart; globalThis.__voiceHardStop = voiceHardStop;', context, { filename: 'app.js voice lifecycle' });

const tick = () => new Promise(resolve => setImmediate(resolve));
function emit(rec, text) {
  rec.onresult({ resultIndex: 0, results: [{ isFinal: true, 0: { transcript: text } }] });
}

(async () => {
  // The panel must be visible before the selected microphone promise resolves.
  get('voice-btn').onclick();
  await tick();
  assert.equal(get('voice-panel').classList.contains('hidden'), false);
  assert.match(get('voice-status-text').textContent, /正在请求麦克风/);
  micRelease(mockMicStream());
  await tick();
  assert.equal(recs.at(-1).startTrack, null, 'unsupported audioTrack overload falls back to the system microphone');
  recs.at(-1).onerror({ error: 'not-allowed' });
  assert.equal(get('voice-panel').classList.contains('hidden'), false, 'permission failure keeps the panel open for retry');
  assert.match(get('voice-status-text').textContent, /权限被拒绝/);

  get('voice-btn').onclick();
  await tick();
  assert.match(get('voice-status-text').textContent, /正在请求麦克风/);
  const selectedStream = mockMicStream();
  micRelease(selectedStream);
  await tick();
  const rec = recs.at(-1);
  assert.equal(rec.startTrack, selectedStream.getAudioTracks()[0], 'supported start(audioTrack) receives the selected microphone');
  rec.onstart();
  assert.equal(context.__voice.listening, true);
  assert.match(get('voice-status-text').textContent, /聆听中/);
  rec.onresult({ resultIndex: 0, results: [{ isFinal: false, 0: { transcript: '实时转写' } }] });
  assert.match(get('voice-status-text').textContent, /实时转写/);

  emit(rec, '第一条。');
  await tick();
  assert.match(get('voice-queue-status').textContent, /待处理 1/);
  emit(rec, '第二条。');
  filterRelease();
  await tick(); await tick(); await tick();
  assert.deepEqual(sentRuns.map(x => x.queued), [true, false], 'queue and insert map to queued true/false');
  assert.match(get('voice-text').children.map(x => x.children[0].textContent).join(' '), /已发送/);

  emit(rec, '背景声音。');
  await tick(); await tick();
  assert.match(get('voice-text').children.map(x => x.children[0].textContent).join(' '), /已忽略/);

  context.__voiceHardStop();
  assert.equal(get('voice-panel').classList.contains('hidden'), true);
  assert.equal(stoppedTracks, 2, 'permission failure and hard stop release their microphone streams');

  state.session.kind = 'assistant';
  get('voice-btn').onclick();
  await tick();
  assert.equal(get('voice-panel').classList.contains('hidden'), false, 'assistant session uses the same panel path');
  micRelease(mockMicStream());
  await tick();
  recs.at(-1).onstart();
  get('voice-btn').onclick();
  assert.equal(get('voice-panel').classList.contains('hidden'), true, 'mic toggle stops and hides the assistant panel');
  assert.equal(stoppedTracks, 3);
  console.log('PASS: immediate panel, permission retry, selected audio track/fallback, main/assistant path, queue/insert, ignored log, and stop');
})().catch(error => { console.error(error); process.exitCode = 1; });
