'use strict';
// 回归：前端 EventSource 必须指向 /events 路由并携带 access_token。
// 曾漏写 /events 段导致流式请求 401 失效（全凭轮询兜底，界面只剩“正在思考”）。
const fs = require('fs');
const path = require('path');

const app = fs.readFileSync(path.join(__dirname, '..', 'internal', 'server', 'web', 'app.js'), 'utf8');
const m = app.match(/new EventSource\(([^)]*)\)/);
if (!m) {
  console.error('FAIL: 未找到 EventSource 构造');
  process.exit(1);
}
if (!m[1].includes('/events')) {
  console.error('FAIL: EventSource URL 缺少 /events：' + m[1].trim());
  process.exit(1);
}
if (!m[1].includes('access_token')) {
  console.error('FAIL: EventSource URL 缺少 access_token：' + m[1].trim());
  process.exit(1);
}
console.log('OK: EventSource URL 指向 /events 并携带 access_token');
