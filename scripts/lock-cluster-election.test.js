#!/usr/bin/env node
/* lock-cluster.js 选举决策 node 模拟（不进 web embed，仅本地验证）。
 *
 * 用 env BACKEND_MODE 控制后端权威状态：
 *   unlocked → 后端明确未锁 → 刷新后当选 must NOT lock（核心回归）
 *   locked   → 后端明确锁定 → 刷新后当选 must stay locked
 *   down     → 后端不可达 → 回退 DEFAULT_LOCK_WHEN_UNKNOWN(false)，must NOT lock
 *
 * 运行：node scripts/lock-cluster-election.test.js <mode>
 */
'use strict';
const fs = require('fs');
const path = require('path');

const mode = process.argv[2] || 'unlocked';

// ── 浏览器环境打桩 ──
global.window = { addEventListener() {}, LockCluster: undefined };
global.location = { hash: '' };
// node 22 全局 crypto 只读且自带 randomUUID，无需打桩
global.localStorage = {
  getItem: () => 'fake-token',
  setItem: () => {},
};
// 单 tab：BroadcastChannel 仅占位，postMessage 无对端 → 选举按“无更强者反超”收敛
global.BroadcastChannel = class {
  constructor() { this.onmessage = null; }
  postMessage() {}
  close() {}
};
// 后端打桩
global.fetch = () => {
  if (mode === 'down') return Promise.reject(new Error('ECONNREFUSED'));
  const locked = mode === 'locked';
  return Promise.resolve({
    ok: true,
    status: 200,
    json: () => Promise.resolve({ locked, gen: locked ? 1 : 0, updatedAt: 1 }),
  });
};

// 加载被测脚本（IIFE，挂载 window.LockCluster）
const src = fs.readFileSync(path.join(__dirname, '..', 'internal/server/web/lock-cluster.js'), 'utf8');
vmRun(src);

function vmRun(code) {
  // 间接 eval 在全局作用域执行脚本；'use strict' IIFE 自包含
  (0, eval)(code);
}

// 选举收敛 ≈ JOIN_WINDOW(600) + ELECTION(400) + 余量
setTimeout(() => {
  const lc = global.window.LockCluster;
  if (!lc) { console.error('FAIL: LockCluster 未挂载'); process.exit(1); }
  const locked = lc.effectiveLocked();
  const phase = lc.isMaster() ? 'master' : (lc.isSlave() ? 'slave' : 'joining');
  const want = (mode === 'locked');
  if (locked !== want) {
    console.error(`FAIL mode=${mode} phase=${phase} locked=${locked} want=${want}`);
    process.exit(1);
  }
  console.log(`PASS mode=${mode} phase=${phase} locked=${locked} (后端权威优先，刷新未误锁/保持锁定符合预期)`);
  process.exit(0);
}, 1600);
