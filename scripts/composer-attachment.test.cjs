'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const test = require('node:test');
const vm = require('node:vm');
const {randomUUID} = require('node:crypto');

const source = require('./fixtures/app-source.cjs').readAppSource();
const start = source.indexOf('const COMPOSER_ATTACHMENT_DIR');
const end = source.indexOf("$('task-form').onsubmit", start);
assert.ok(start >= 0 && end > start);

function setup(respond = () => ({ok: true, json: async () => ({})})) {
  const taskForm = Object.assign(new EventTarget(), {
    dataset: {},
    classList: {add() {}, remove() {}},
  });
  const prompt = Object.assign(new EventTarget(), {
    value: '',
    selectionStart: 0,
    selectionEnd: 0,
    setRangeText(text, start, end) {
      this.value = this.value.slice(0, start) + text + this.value.slice(end);
    },
  });
  const state = {config: {workspaceId: 'workspace-a'}, session: {id: 'chat-a'}, token: 'fixture'};
  const requests = [], attachments = [], notices = [];
  const context = vm.createContext({
    $: id => id === 'task-form' ? taskForm : prompt,
    t: (key, ...args) => key.replace(/\{(\d+)\}/g, (_, index) => args[Number(index)]),
    toast: message => notices.push(message),
    action: fn => fn,
    state,
    lockScreen: {locked: false},
    recoveryScope: () => state.config.workspaceId,
    URLSearchParams, TextEncoder, File, Event, crypto: {randomUUID},
    attachFileEntry: ({path}, {root}) => attachments.push({path, root}),
    fetch: (url, options) => {
      const query = new URL(url, 'http://test.invalid').searchParams;
      requests.push({query, ...options});
      return respond();
    },
  });
  vm.runInContext(source.slice(start, end), context);
  function dispatch(type, transfer) {
    const paste = type === 'paste';
    const event = new Event(type, {cancelable: true});
    Object.defineProperty(event, paste ? 'clipboardData' : 'dataTransfer', {value: transfer});
    (paste ? prompt : taskForm).dispatchEvent(event);
    return event;
  }
  return {context, state, prompt, requests, attachments, notices, dispatch,
    drain: () => vm.runInContext('composerFileQueue', context)};
}

function fileItem(file) {
  return {kind: 'file', getAsFile: () => file};
}

test('ordinary and image files paste once via the browser FileList', async () => {
  const ui = setup();
  const files = [new File(['hello'], '报告.txt'), new File(['png'], '', {type: 'image/png'})];
  assert.equal(ui.dispatch('paste', {files, items: files.map(fileItem)}).defaultPrevented, true);
  await ui.drain();
  assert.equal(ui.requests.length, 2);
  assert.equal(ui.requests[0].body, files[0]);
  assert.equal(ui.requests[1].body.type, 'image/png');
  assert.ok(ui.requests.every(({query}) => query.get('workspaceId') === 'workspace-a'));
  assert.match(ui.attachments[0].path, /-报告\.txt$/);
  assert.match(ui.attachments[1].path, /-pasted-image-.+\.png$/);
  assert.ok(ui.attachments.every(({root}) => root === 'workspace'));
});

test('clipboard and drop both accept browser file items with an empty FileList', async () => {
  const ui = setup();
  const file = new File([], '空.txt', {type: 'text/plain'});
  for (const type of ['paste', 'drop']) {
    assert.equal(ui.dispatch(type, {files: [], items: [fileItem(file)]}).defaultPrevented, true);
  }
  await ui.drain();
  assert.equal(ui.attachments.length, 2);
  assert.ok(ui.requests.every(({body}) => body === file));
  assert.notEqual(ui.attachments[0].path, ui.attachments[1].path);
});

test('plain text is untouched and unavailable pasted or dropped files report an error', async () => {
  const ui = setup();
  assert.equal(ui.dispatch('paste', {getData: () => '/private/example.txt'}).defaultPrevented, false);
  assert.equal(ui.dispatch('drop', {types: ['text/plain']}).defaultPrevented, false);
  assert.equal(ui.dispatch('drop', {types: ['Files'], items: [fileItem(null)]}).defaultPrevented, true);
  assert.equal(ui.dispatch('paste', {types: ['Files'], items: [fileItem(null)], getData: () => ''}).defaultPrevented, true);
  await ui.drain();
  assert.equal(ui.requests.length, 0);
  assert.equal(ui.notices.length, 2);
  assert.ok(ui.notices.every(message => message.includes('未能读取附件')));
});

test('long pasted text retains its original content when upload fails', async () => {
  const ui = setup(async () => ({ok: false, json: async () => ({error: 'upload failed'})}));
  const text = '长文本'.repeat(3000);
  assert.equal(ui.dispatch('paste', {getData: () => text}).defaultPrevented, true);
  await ui.drain();
  assert.match(ui.requests[0].query.get('path'), /粘贴文本-.+\.md$/);
  assert.equal(ui.prompt.value, text);
});

test('clipboard image formats retain their real extension instead of defaulting to PNG', async () => {
  const ui = setup();
  const files = [
    new File(['jpeg'], '', {type: 'image/jpeg'}),
    new File(['gif'], '', {type: 'image/gif'}),
    new File(['webp'], '', {type: 'image/webp'}),
    new File(['tiff'], '', {type: 'image/tiff'}),
  ];
  ui.dispatch('paste', {files, items: files.map(fileItem), getData: () => ''});
  await ui.drain();
  assert.deepEqual(ui.requests.map(({body}) => body.type), ['image/jpeg', 'image/gif', 'image/webp', 'image/tiff']);
  assert.match(ui.attachments[0].path, /\.jpg$/);
  assert.match(ui.attachments[1].path, /\.gif$/);
  assert.match(ui.attachments[2].path, /\.webp$/);
  assert.match(ui.attachments[3].path, /\.tiff$/);
});

test('browser-decodable TIFF is converted to a real PNG before upload', async () => {
  const ui = setup();
  let closed = false;
  ui.context.createImageBitmap = async () => ({width: 2, height: 2, close: () => { closed = true; }});
  ui.context.document = {createElement: tag => {
    assert.equal(tag, 'canvas');
    return {width: 0, height: 0, getContext: () => ({drawImage() {}}),
      toBlob: resolve => resolve(new Blob(['real png'], {type: 'image/png'}))};
  }};
  ui.dispatch('drop', {files: [new File(['tiff'], 'scan.tiff', {type: 'image/tiff'})], types: ['Files']});
  await ui.drain();
  assert.equal(closed, true);
  assert.equal(ui.requests[0].body.type, 'image/png');
  assert.match(ui.attachments[0].path, /scan\.png$/);
});

test('mixed rich text and image paste preserves text and the 20000-byte attachment rule', async () => {
  const short = setup();
  const image = new File(['png'], 'image.png', {type: 'image/png'});
  short.dispatch('paste', {files: [image], items: [fileItem(image)], getData: () => 'formatted text'});
  await short.drain();
  assert.equal(short.prompt.value, 'formatted text');
  assert.equal(short.attachments.length, 1);

  const boundary = setup();
  assert.equal(boundary.dispatch('paste', {getData: () => 'a'.repeat(20000)}).defaultPrevented, false);
  await boundary.drain();
  assert.equal(boundary.requests.length, 0);

  const long = setup();
  const text = '长'.repeat(20001);
  long.dispatch('paste', {files: [image], items: [fileItem(image)], getData: () => text});
  await long.drain();
  assert.equal(long.requests.length, 2);
  assert.match(long.attachments[0].path, /粘贴文本-.+\.md$/);
  assert.match(long.attachments[1].path, /image\.png$/);
  assert.match(long.prompt.value, /长文本已转为附件/);
  assert.notEqual(long.prompt.value, text);
});

for (const [target, switchOwner] of [
  ['workspace', state => { state.config.workspaceId = 'workspace-b'; }],
  ['conversation', state => { state.pendingSessionId = 'chat-b'; }],
]) {
  test('switching ' + target + ' stops queued uploads from attaching to a different draft', async () => {
    let finish, started;
    const upload = new Promise(resolve => { finish = resolve; });
    const uploading = new Promise(resolve => { started = resolve; });
    const ui = setup(() => { started(); return upload; });
    const files = [new File(['first'], 'first.txt'), new File(['second'], 'second.txt')];
    ui.dispatch('drop', {files, types: ['Files']});
    await uploading;
    switchOwner(ui.state);
    finish({ok: true, json: async () => ({})});
    await ui.drain();
    assert.equal(ui.requests.length, 1);
    assert.equal(ui.requests[0].query.get('workspaceId'), 'workspace-a');
    assert.equal(ui.attachments.length, 0);
  });
}
