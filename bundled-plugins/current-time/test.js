'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const plugin = require('./index');

const manifest = JSON.parse(fs.readFileSync(path.join(__dirname, 'manifest.json'), 'utf8'));
const registry = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'registry.json'), 'utf8'));
assert.equal(manifest.id, 'current-time');
assert.equal(manifest.enabled, true);
assert.ok(registry.plugins.some(entry => entry.id === manifest.id && entry.enabled));

const tools = [];
plugin.apply({ tool: definition => tools.push(definition) });
assert.equal(plugin.name, 'current-time');
assert.equal(tools.length, 2);
assert.equal(tools[0].name, 'get_current_datetime');
assert.equal(tools[1].name, 'get_week_number');

const before = Date.now();
const shanghai = tools[0].handler({ timeZone: 'Asia/Shanghai' });
const after = Date.now();
assert.equal(shanghai.timeZone, 'Asia/Shanghai');
assert.ok(/^\d{4}-\d{2}-\d{2}$/.test(shanghai.date), shanghai.date);
assert.ok(/^\d{2}:\d{2}:\d{2}$/.test(shanghai.time), shanghai.time);
assert.ok(Number.isFinite(Date.parse(shanghai.isoUtc)));
assert.equal(shanghai.isoWeek.standard, 'ISO-8601');
assert.ok(shanghai.isoWeek.weekNumber >= 1 && shanghai.isoWeek.weekNumber <= 53);
assert.ok(shanghai.unixMs >= before && shanghai.unixMs <= after, `${before} <= ${shanghai.unixMs} <= ${after}`);
const expectedParts = Object.fromEntries(new Intl.DateTimeFormat('en', {
  timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit',
}).formatToParts(new Date(shanghai.unixMs)).map(part => [part.type, part.value]));
assert.equal(shanghai.date, `${expectedParts.year}-${expectedParts.month}-${expectedParts.day}`);
assert.throws(() => tools[0].handler({ timeZone: 'No/Such_Zone' }), RangeError);

const week = tools[1].handler({ date: '2021-01-01' });
assert.deepEqual({ weekYear: week.weekYear, weekNumber: week.weekNumber, weekStart: week.weekStart, weekEnd: week.weekEnd }, {
  weekYear: 2020, weekNumber: 53, weekStart: '2020-12-28', weekEnd: '2021-01-03',
});
const monday = tools[1].handler({ date: '2021-01-04' });
assert.equal(monday.weekYear, 2021);
assert.equal(monday.weekNumber, 1);
assert.equal(monday.weekStart, '2021-01-04');
const currentWeek = tools[1].handler({ timeZone: 'Asia/Shanghai' });
assert.equal(currentWeek.timeZone, 'Asia/Shanghai');
assert.ok(/^\d{4}-\d{2}-\d{2}$/.test(currentWeek.date));
assert.ok(currentWeek.weekNumber >= 1 && currentWeek.weekNumber <= 53);
assert.throws(() => tools[1].handler({ date: '2021-02-29' }), RangeError);
assert.throws(() => tools[1].handler({ date: 'next Friday' }), RangeError);
assert.throws(() => tools[1].handler({ date: '2026-09-30', timeZone: 'No/Such_Zone' }), RangeError);
console.log('current-time plugin PASS');
