'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const plugin = require('./index');

const manifest = JSON.parse(fs.readFileSync(path.join(__dirname, 'manifest.json'), 'utf8'));
const registry = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'registry.json'), 'utf8'));
assert.equal(manifest.id, 'lunar-calendar');
assert.equal(manifest.enabled, true);
assert.ok(registry.plugins.some(entry => entry.id === manifest.id && entry.enabled));

const tools = [];
plugin.apply({ tool: definition => tools.push(definition) });
assert.equal(plugin.name, 'lunar-calendar');
assert.equal(tools.length, 1);
assert.equal(tools[0].name, 'get_lunar_date');

const newYear = tools[0].handler({ date: '2024-02-10' });
assert.equal(newYear.lunarDate, '甲辰年正月初一');
assert.equal(newYear.zodiac, '龙');
assert.equal(newYear.isLeapMonth, false);
assert.equal(newYear.gregorianDate, '2024-02-10');

const midAutumn = tools[0].handler({ date: '2026-09-30' });
assert.equal(midAutumn.lunarDate, '丙午年八月二十');
assert.equal(midAutumn.ganzhiYear, '丙午');
assert.equal(midAutumn.zodiac, '马');

const leapMonth = tools[0].handler({ date: '2023-03-22' });
assert.equal(leapMonth.lunarDate, '癸卯年闰二月初一');
assert.equal(leapMonth.isLeapMonth, true);

const today = tools[0].handler();
assert.ok(/^\d{4}-\d{2}-\d{2}$/.test(today.gregorianDate));
assert.equal(today.timeZone, 'Asia/Shanghai');
assert.ok(today.lunarDate.endsWith(today.lunarDayName));
assert.throws(() => tools[0].handler({ date: '2024-02-30' }), RangeError);
assert.throws(() => tools[0].handler({ date: '20240210' }), TypeError);
console.log('lunar-calendar plugin PASS');
