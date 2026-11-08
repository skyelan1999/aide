'use strict';
const assert = require('node:assert/strict');
const test = require('node:test');
const fs = require('node:fs');
const path = require('node:path');
const review = require('../internal/server/web/session-review.js');

test('uses recorded orchestration timing and preserves the task status', () => {
  const result = review.build({
    id: 'session-1', title: 'Check', created: '2026-11-08T00:00:00Z', updated: '2026-11-08T00:02:00Z',
    runsTotal: 3, sessionUsage: {total: 120},
    runs: [{id: 'run-3', status: 'completed', created: '2026-11-08T00:01:00Z', model: 'test-model',
      workspaceMode: 'local', toolUses: [{}, {}], orchestrationDropped: 4,
      orchestrationEvents: [{kind: 'model_request', stage: 'chat', startedAt: '2026-11-08T00:01:05Z', completedAt: '2026-11-08T00:01:15Z', elapsedMillis: 10000, outcome: 'completed'}],
    }],
  });
  assert.equal(result.total, 3);
  assert.equal(result.turns[0].number, 3);
  assert.equal(result.status, 'completed');
  assert.equal(result.toolsLoaded, 2);
  assert.equal(result.turns[0].lastEvent, Date.parse('2026-11-08T00:01:15Z'));
  assert.equal(result.turns[0].ended, undefined, 'task end must not be inferred from its final event');
  assert.equal(result.turns[0].dropped, 4);
  assert.equal(result.hasOlder, true);
});

test('older request snapshots contribute only metadata, not sensitive bodies', () => {
  const secret = 'a-secret-prompt-and-tool-arguments';
  const result = review.build({runs: [{id: 'old', status: 'failed', created: '2026-11-08T00:00:00Z',
    requestSnapshots: [{purpose: 'chat', at: '2026-11-08T00:00:02Z', completedAt: '2026-11-08T00:00:04Z',
      elapsedMillis: 2000, outcome: 'failed', messages: [{content: secret}], body: secret, tools: [secret]}]}]});
  assert.equal(result.turns[0].events[0].source, 'snapshot');
  assert.equal(result.turns[0].events[0].elapsed, 2000);
  assert.equal(JSON.stringify(result).includes(secret), false);
});

test('empty and running sessions do not acquire fabricated timing', () => {
  const empty = review.build({kind: 'assistant', messages: [{content: 'hello'}]});
  assert.equal(empty.status, 'chat');
  assert.equal(empty.first, null);
  const running = review.build({runs: [{id: 'active', status: 'running', created: '2026-11-08T00:00:00Z'}]});
  assert.equal(running.status, 'running');
  assert.equal(running.turns[0].lastEvent, null);
  assert.equal(running.first, running.last);
});

test('trajectory loads the review module before app.js and exposes the tab', () => {
  const html = fs.readFileSync(path.join(__dirname, '../internal/server/web/index.html'), 'utf8');
  assert.match(html, /session-review\.js\?v=1[^]*app\.js\?v=253/);
  assert.match(html, /data-view="review"[^>]*data-i18n="会话评审"/);
});

test('timeline, event details, and zoom controls respond to selection', () => {
  class Node {
    constructor(tag) { this.tag = tag; this.children = []; this.style = {}; this.attributes = {}; this.className = ''; this.textContent = ''; this.scrollLeft = 0; }
    append(...children) { this.children.push(...children); }
    replaceChildren(...children) { this.children = children; }
    setAttribute(name, value) { this.attributes[name] = value; }
  }
  const previousDocument = global.document;
  global.document = {createElement: tag => new Node(tag)};
  try {
    const host = new Node('div');
    const result = review.mount(host, {id: 'session-1', title: 'Review', runs: [
      {id: 'one', status: 'completed', created: '2026-11-08T00:00:00Z', orchestrationEvents: [
        {kind: 'model_request', stage: 'chat', startedAt: '2026-11-08T00:00:01Z', completedAt: '2026-11-08T00:00:05Z', elapsedMillis: 4000, outcome: 'completed'},
      ]},
      {id: 'two', status: 'failed', created: '2026-11-08T00:01:00Z', orchestrationEvents: [
        {kind: 'step', stage: 'run_shell', startedAt: '2026-11-08T00:01:02Z', completedAt: '2026-11-08T00:01:03Z', elapsedMillis: 1000, outcome: 'failed'},
      ]},
    ]}, {t: (text, ...args) => text.replace(/\{(\d+)\}/g, (_, index) => String(args[Number(index)]))});
    const find = (node, className) => {
      if (node.className.split(' ').includes(className)) return node;
      for (const child of node.children) { const hit = find(child, className); if (hit) return hit; }
      return null;
    };
    assert.equal(result.turns.length, 2);
    assert.ok(find(host, 'sr-status-failed'));
    const track = find(host, 'sr-track');
    assert.equal(track.style.width, '100%');
    const controls = find(host, 'sr-controls');
    controls.children[2].onclick();
    assert.equal(track.style.width, '150%');
    controls.children[0].onclick();
    assert.equal(track.style.width, '100%');
    const turns = find(host, 'sr-turns');
    turns.children[0].onclick();
    assert.equal(find(host, 'sr-detail-head').children[0].textContent, '第 1 轮 · 已完成');
    const marker = find(host, 'sr-event-segment-model_request');
    marker.onclick({stopPropagation() {}});
    assert.equal(find(host, 'sr-event-info').children.length, 7);
  } finally {
    global.document = previousDocument;
  }
});
