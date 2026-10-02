'use strict';
// Regression: the accessibility port control must show the host listener that
// served this tab, not a persisted/default port that may be stale after fallback.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const app = fs.readFileSync(path.join(__dirname, '..', 'internal', 'server', 'web', 'app.js'), 'utf8');
const helper = app.match(/function currentHostPort\(\)\s*\{[^}]+\}/);
assert.ok(helper, 'currentHostPort helper must exist');
assert.match(app, /port\.value\s*=\s*String\(currentHostPort\(\)\)/, 'port input must use active browser origin');
assert.doesNotMatch(app, /port\.value\s*=\s*String\(state\.config\?\.accessibilityHostPort/, 'port input must not use stale persisted default');

function currentHostPort(location) {
  return vm.runInNewContext(`${helper[0]}; currentHostPort()`, { location });
}
assert.equal(currentHostPort({ port: '9999', protocol: 'https:' }), 9999);
assert.equal(currentHostPort({ port: '', protocol: 'https:' }), 443);
assert.equal(currentHostPort({ port: '', protocol: 'http:' }), 80);
console.log('OK: settings port control follows the active browser host port');
