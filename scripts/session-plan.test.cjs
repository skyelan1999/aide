'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

class Node {
  constructor(tag) {
    this.tagName = tag.toUpperCase(); this.children = []; this.dataset = {}; this.style = {};
    this.className = ''; this.textContent = ''; this.isConnected = true; this.value = ''; this.open = false;
  }
  get childNodes() { return this.children; }
  append(...items) { for (const item of items) if (item != null) this.children.push(item); }
  replaceChildren(...items) { this.children = []; this.append(...items); }
  setAttribute(name, value) { this[name] = value; }
  addEventListener(name, fn) { (this.listeners ||= {})[name] = fn; }
  querySelector(selector) { return this.querySelectorAll(selector)[0] || null; }
  querySelectorAll(selector) {
    if (!selector.startsWith('.')) return [];
    const name = selector.slice(1).split(' ')[0], found = [];
    const visit = node => {
      for (const child of node.children || []) {
        if (String(child.className || '').split(' ').includes(name)) found.push(child);
        visit(child);
      }
    };
    visit(this); return found;
  }
}
function text(node) {
  return [node.textContent || '', ...(node.children || []).map(text)].join(' ');
}
async function mount(plan) {
  const host = new Node('div');
  const session = {id:'session', number:1, title:'Planning', plan, runs:[{id:'ordinary', status:'completed', prompt:'ordinary run'}]};
  const window = {};
  const context = {
    window, document:{createElement:tag=>new Node(tag), querySelector:()=>null},
    setInterval, clearInterval, console,
  };
  vm.runInNewContext(fs.readFileSync('internal/server/web/session-plan.js', 'utf8'), context);
  const mounted = window.AideSessionPlan.sessionOverview(host, {
    session, t:(value,...args)=>String(value).replace(/\{(\d+)\}/g,(_,i)=>args[Number(i)]),
    api:async()=>session, workspaceId:'workspace', blocked:()=>false,
  });
  await mounted.ready;
  await Promise.resolve();
  return {host, mounted};
}

(async()=>{
  const explicit = [
    {id:'task', title:'明确任务', source:'user', status:'in_progress', created:'2026-11-26T00:00:00Z'},
    {id:'step', title:'所属执行步骤', source:'aide', status:'pending', parentId:'task', runId:'planned-run'},
    {id:'orphan', title:'普通对话自我规划', source:'aide', status:'in_progress', runId:'ordinary'},
  ];
  const {host, mounted} = await mount(explicit);
  assert.equal(host.querySelectorAll('.plan-task').length, 1, 'one explicit task card');
  assert.equal(host.querySelectorAll('.plan-steps').length, 1, 'agent steps stay inside their parent task');
  assert.equal(host.querySelectorAll('.plan-advanced').length, 1, 'advanced management is consolidated');
  assert.equal(host.querySelectorAll('.plan-add-box')[0].open, false, 'composer starts closed when tasks exist');
  assert.ok(text(host).includes('明确任务'));
  assert.ok(text(host).includes('所属执行步骤'));
  assert.ok(!text(host).includes('普通对话自我规划'), 'ordinary model planning is not shown');
  assert.ok(!text(host).includes('自然语言'), 'source badges no longer clutter every row');
  mounted.close();

  const empty = await mount([explicit[2]]);
  assert.equal(empty.host.querySelectorAll('.plan-task').length, 0);
  assert.equal(empty.host.querySelectorAll('.plan-add-box')[0].open, true, 'empty state exposes the composer');
  assert.ok(text(empty.host).includes('还没有明确任务'));
  assert.ok(!text(empty.host).includes('普通对话自我规划'), 'legacy orphan planning stays hidden');
  empty.mounted.close();
  console.log('session plan PASS: explicit-only tasks, nested steps, legacy orphan filtering, clean empty state');
})().catch(error=>{console.error(error);process.exitCode=1});
