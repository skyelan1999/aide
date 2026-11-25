#!/usr/bin/env node
'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.resolve(__dirname, '..');
const source = fs.readFileSync(path.join(root, 'internal/server/web/usage-analytics.js'), 'utf8');
const context = vm.createContext({ window: {}, console });
vm.runInContext(source, context, { filename: 'usage-analytics.js' });
const analytics = context.window.AideUsageAnalytics;
assert.ok(analytics, 'analytics module must be exposed');
assert.equal(analytics.formatBytes(0), '0 B');
assert.equal(analytics.formatBytes(999), '999 B');
assert.equal(analytics.formatBytes(1500), '1.50 KB');
assert.equal(analytics.formatBytes(12_000_000), '12.0 MB');

const today = new Date().toISOString().slice(0, 10);
const fixture = {
  [today]: { uploadBytes: 100, downloadBytes: 400, interfaces: { eth0: { uploadBytes: 100, downloadBytes: 400 } } },
  '2026-01-01': { uploadBytes: 1, downloadBytes: 2 },
};
const seven = analytics.selectNetworkDays(fixture, '7');
assert.equal(seven.length, 7);
assert.equal(seven.at(-1).day, today);
assert.equal(seven.at(-1).downloadBytes, 400);
assert.equal(analytics.selectNetworkDays(fixture, '30').length, 30);
assert.deepEqual(Array.from(analytics.selectNetworkDays(fixture, 'all').map(item => item.day)), ['2026-01-01', today]);
const disks = analytics.selectStorageDisks({ data: { logicalBytes: 10, files: 1 }, workspace: { logicalBytes: 30, files: 2 }, capacity: { dataAvailableBytes: 90, workspaceAvailableBytes: 70 } });
assert.equal(disks.length, 2);
assert.equal(disks[1].id, 'current-workspace');
assert.equal(disks[1].availableBytes, 70);
assert.equal(analytics.selectStorageDisks({ workspaceAudit: { included: false, reason: 'ssh-source' }, disks: [{ id: 'aide-data', logicalBytes: 10 }] }).length, 1);
assert.equal(analytics.selectStorageDisks({ disks: [{ id: 'data' }, { id: 'workspace' }, { id: 'secondary' }] }).length, 3);

const schema = JSON.parse(fs.readFileSync(path.join(root, 'internal/server/web/settings-schema.json'), 'utf8'));
const sections = schema.sections;
assert.ok(sections.some(section => section.id === 'network-usage' && section.controls[0].type === 'network-usage'));
assert.ok(sections.some(section => section.id === 'storage-usage' && section.controls[0].type === 'storage-usage'));
const app = fs.readFileSync(path.join(root, 'internal/server/web/app.js'), 'utf8');
assert.match(app, /'network-usage': \(\) => window\.AideUsageAnalytics\.renderNetwork/);
assert.match(app, /'storage-usage': \(\) => window\.AideUsageAnalytics\.renderStorage/);
assert.match(source, /const data = await api\('\/network-usage'\)/);
assert.match(source, /const data = await api\('\/storage-usage'\)/);
assert.doesNotMatch(source, /await api\('\/api\//);
assert.match(source, /storageShareStrip\(disks, 'storage-disk-strip'/);
assert.match(source, /data\.workspaceAudit\?\.reason === 'ssh-source'/);
assert.match(source, /--share-color/);
assert.doesNotMatch(source, /response\.json\(\)/);
console.log('usage analytics: PASS (byte formatting, UTC ranges, multi-disk colors, SSH exclusion, API paths)');
