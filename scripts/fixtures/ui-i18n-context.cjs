'use strict';
const fs = require('node:fs');
const path = require('node:path');
// Source-slice tests load the same translation bridge as the complete page.
module.exports = function createUiContext(vm, bindings) {
  const context = vm.createContext(bindings);
  const source = fs.readFileSync(path.join(__dirname, '../../internal/server/web/starmap.js'), 'utf8');
  const bridges = source.split('\n').filter(line => /^function uiText[A-Z]\w*\(/.test(line)).join('\n');
  if (bridges) vm.runInContext(bridges, context);
  return context;
};
