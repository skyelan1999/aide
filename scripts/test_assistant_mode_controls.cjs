'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../internal/server/web/app.js'), 'utf8');
const start = source.indexOf('function syncAssistantModeControls(');
const end = source.indexOf('// AI 工作流阶段', start);
assert.ok(start >= 0 && end > start, 'assistant controls synchronizer exists');

function button() {
  const classes = new Set(['active']);
  return {
    disabled: false,
    attributes: {},
    classList: {
      add(value) { classes.add(value); },
      remove(value) { classes.delete(value); },
      contains(value) { return classes.has(value); },
    },
    setAttribute(key, value) { this.attributes[key] = value; },
  };
}

const trajectory = button();
const chat = button();
const workflow = button();
const queue = button();
const phase = button();
const phaseBar = button();
const map = {'trajectory-toggle': trajectory, 'queue-toggle': queue, 'workflow-phases': phaseBar};
const state = {mode: 'workflow', workflowPhase: 'design', queueMode: true};
const context = {
  state,
  $: id => map[id],
  document: {querySelectorAll(selector) {
    if (selector === '.mode-switch button') return [chat, workflow];
    if (selector === '#workflow-phases .phase-btn') return [phase];
    throw new Error('unexpected selector ' + selector);
  }},
  setMode(mode) { state.mode = mode; },
};
vm.createContext(context);
vm.runInContext(source.slice(start, end), context);

context.syncAssistantModeControls(true);
assert.equal(state.mode, 'chat');
assert.equal(state.workflowPhase, '');
assert.equal(state.queueMode, false);
assert.equal(queue.classList.contains('active'), false);
assert.equal(phase.classList.contains('selected'), false);
assert.equal(phaseBar.classList.contains('hidden'), true);
for (const control of [trajectory, chat, workflow, queue]) {
  assert.equal(control.disabled, true);
  assert.equal(control.attributes['aria-disabled'], 'true');
}

context.syncAssistantModeControls(false);
for (const control of [trajectory, chat, workflow, queue]) {
  assert.equal(control.disabled, false);
  assert.equal(control.attributes['aria-disabled'], 'false');
}
console.log('assistant mode controls PASS');
