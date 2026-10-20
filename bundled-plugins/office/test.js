'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const plugin = require('./index');

const tools = [];
plugin.apply({tool: definition => tools.push(definition)});
const manifest = JSON.parse(fs.readFileSync(path.join(__dirname, 'manifest.json'), 'utf8'));
const registry = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'registry.json'), 'utf8'));
assert.equal(manifest.id, 'office');
assert.equal(registry.plugins.find(p => p.id === 'office')?.enabled, true);
assert.deepEqual(tools.map(t => t.name), ['office_document_search', 'office_create', 'office_comments', 'office_comment_edit']);
assert.deepEqual(tools[1].parameters.required, ['path', 'format', 'content']);
assert.throws(() => tools[1].handler({}), /原生工作区执行器/);
console.log('office plugin registration PASS');
