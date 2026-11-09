'use strict';
const assert = require('node:assert/strict');
const test = require('node:test');
const timeline = require('../internal/server/web/session-timeline.js');

test('interleaves prompt, interjection and answer segments by real time', () => {
  const run = {
    id: 'run-1', prompt: '先看结构', created: '2026-11-09T10:00:00Z',
    steers: [{content: '先别改后端', queued: false, at: '2026-11-09T10:02:00Z'}],
    steps: [{name: 'chat', outputSegments: [{id: 's1', content: '第一段', at: '2026-11-09T10:01:00Z'}]}],
  };
  const kinds = timeline.entries(run).map(entry => entry.kind);
  assert.deepEqual(kinds, ['prompt', 'answer', 'steer'], '插话必须落在它实际发生的时刻，而不是整轮回答之后');
});

test('places an interjection between two answer segments', () => {
  const run = {
    id: 'run-2', prompt: '开始', created: '2026-11-09T10:00:00Z',
    steers: [{content: '换个方向', queued: false, at: '2026-11-09T10:01:30Z'}],
    steps: [{name: 'chat', outputSegments: [
      {id: 'a', content: '前一段', at: '2026-11-09T10:01:00Z'},
      {id: 'b', content: '后一段', at: '2026-11-09T10:02:00Z'},
    ]}],
  };
  const items = timeline.entries(run);
  assert.deepEqual(items.map(entry => entry.kind), ['prompt', 'answer', 'steer', 'answer']);
  assert.equal(items[2].content, '换个方向');
  assert.equal(items[3].content, '后一段');
});

test('keeps queued messages out of the timeline', () => {
  const run = {id: 'run-3', prompt: '任务', created: '2026-11-09T10:00:00Z',
    steers: [{content: '排队项', queued: true, at: '2026-11-09T10:05:00Z'}], steps: []};
  assert.deepEqual(timeline.entries(run).map(entry => entry.kind), ['prompt']);
});

test('falls back to run creation time for legacy records without timestamps', () => {
  const run = {id: 'run-4', prompt: '旧任务', created: '2026-11-09T10:00:00Z',
    steers: [{content: '旧插话', queued: false, at: ''}],
    steps: [{name: 'chat', outputSegments: [{id: 'legacy', content: '旧回答', at: ''}]}]};
  const items = timeline.entries(run);
  assert.deepEqual(items.map(entry => entry.kind), ['prompt', 'steer', 'answer'], '缺时间时仍保持输入在前、回答在后');
  assert.equal(items[0].at, Date.parse('2026-11-09T10:00:00Z'));
});

test('accepts epoch milliseconds as well as backend timestamps', () => {
  const ms = 1789000000000;
  assert.equal(timeline.timeOf(ms), ms);
  assert.notEqual(timeline.clock(ms), '');
});

test('parses RFC3339 nanosecond timestamps used by the backend', () => {
  const nano = '2026-11-09T14:58:57.357238482Z';
  assert.equal(timeline.timeOf(nano), Date.parse('2026-11-09T14:58:57.357Z'));
  assert.notEqual(timeline.clock(nano), '');
});

test('formats a local clock label and stays empty for invalid values', () => {
  const iso = '2026-11-09T10:20:00Z';
  const expected = new Date(Date.parse(iso));
  const label = timeline.clock(iso);
  assert.equal(label, String(expected.getHours()).padStart(2, '0') + ':' + String(expected.getMinutes()).padStart(2, '0'));
  assert.equal(timeline.clock(''), '');
  assert.equal(timeline.clock('not-a-date'), '');
});
