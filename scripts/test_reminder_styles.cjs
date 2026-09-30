const fs = require('node:fs');
const assert = require('node:assert/strict');

const app = fs.readFileSync('internal/server/web/app.js', 'utf8');
const css = fs.readFileSync('internal/server/web/style.css', 'utf8');
const server = fs.readFileSync('internal/server/server.go', 'utf8');
const reminderStart = app.indexOf('// ── 全局提醒中心');
const reminderEnd = app.indexOf('\n})();', reminderStart);
assert.ok(reminderStart >= 0 && reminderEnd > reminderStart, 'reminder center initializer exists');
const reminderInit = app.slice(reminderStart, reminderEnd);

assert.doesNotMatch(reminderInit, /createElement\(['"]style['"]\)/, 'reminder UI must not inject CSP-blocked inline styles');
assert.match(css, /\.reminder-center-panel\s*\{[^}]*display\s*:\s*none/s, 'reminder panel is hidden by default');
assert.match(css, /body\.reminders-mode\s+#reminder-center\s*\{[^}]*display\s*:\s*flex/s, 'reminder panel is shown in reminder mode');
assert.match(css, /body\.reminders-mode\s+\.file-panel,\s*body\.reminders-mode\s+\.plugin-panel\s*\{\s*display\s*:\s*none\s*\}/s, 'other side panels are hidden while reminders are open');
for (const selector of ['.reminder-create', '.reminder-item', '.reminder-filter', '.reminder-alert-dialog']) {
  assert.ok(css.includes(selector), `missing external reminder CSS selector ${selector}`);
}
assert.match(css, /@media\s*\(max-width\s*:\s*950px\)[\s\S]*?\.reminder-center-panel\s*\{[^}]*position\s*:\s*fixed/s, 'reminder panel has a narrow viewport layout');
assert.match(server, /style-src 'self';/, 'production CSP stays strict and permits external stylesheets');
console.log('PASS reminder stylesheet and production CSP regression');
