'use strict';
// Additional held-out quality checks, never copied into a model workspace.
// This is not a duration workload and does not change timed acceptance cases.
const fs = require('fs'), os = require('os'), path = require('path'), assert = require('assert/strict');
const fixture = require('./luna-large-workload.cjs');
let seed = 0x741fab19;
const rnd = n => { seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0; return seed % n; };
const generated = new Map();
const put = (id, input) => { if (!generated.has(id)) generated.set(id, []); generated.get(id).push(input); };
for (let i = 0; i < 128; i++) {
  put('decimal', {value: `${i % 3 === 0 ? '-' : '+'}${rnd(100000)}.${String(rnd(10000000)).padStart(7, '0')}`, scale: rnd(7)});
  put('allocation', {total: i % 11 === 0 ? Number.MAX_SAFE_INTEGER : rnd(10000), rows: Array.from({length: rnd(9)}, (_, j) => ({id: `r${j}`, weight: i % 7 === 0 ? 0 : rnd(100)}))});
  put('tierprice', {quantity: rnd(160), tiers: [{upTo: 10 + rnd(20), price: rnd(100)}, {upTo: 50 + rnd(30), price: rnd(50)}, {upTo: i % 9 === 0 ? 100 : null, price: rnd(20)}]});
  const subtotal = rnd(100000), now = rnd(100);
  put('coupon', {subtotal, eligible: rnd(subtotal + 1), now, coupon: {kind: i % 2 ? 'fixed' : 'percent', value: i % 2 ? rnd(100000) : rnd(10001), minimum: rnd(100000), expires: i % 3 ? rnd(120) : null, cap: i % 4 ? rnd(100000) : null}});
  const events = Array.from({length: 1 + rnd(12)}, (_, j) => ({id: `e${j}`, sku: ['A', 'B', '__proto__', 'constructor'][rnd(4)], delta: rnd(31) - 15}));
  if (i % 3 === 0) events.push({...events[0]});
  if (i % 7 === 0) events.push({...events[0], delta: events[0].delta + 1});
  put('inventory', {initial: JSON.parse('{"A":50,"B":50,"__proto__":50,"constructor":50}'), events});
  const reservations = Array.from({length: rnd(7)}, (_, j) => {const start = rnd(30) - 10; return {id: `r${j}`, start, end: start + 1 + rnd(10), units: 1 + rnd(4)};});
  const start = rnd(30) - 10;
  let request = {id: 'new', start, end: start + 1 + rnd(10), units: 1 + rnd(4)};
  if (reservations.length && i % 3 === 0) request = {...reservations[0], units: reservations[0].units + (i % 2)};
  put('reservations', {capacity: 1 + rnd(12), reservations, request});
  put('orderstate', {events: Array.from({length: rnd(8)}, (_, j) => ({id: `e${i % 7 === 0 ? j % 2 : j}`, type: ['submit', 'pay', 'ship', 'deliver', 'cancel', 'refund', 'unknown'][rnd(7)]}))});
  const ttl = 1 + rnd(20), at = rnd(20) - 10, payload = {a: rnd(4), b: [rnd(4), {c: rnd(4)}]};
  put('idempotency', {ttl, requests: [{key: '__proto__', payload, now: at}, {key: '__proto__', payload: {b: payload.b, a: payload.a}, now: at + rnd(ttl)}, {key: '__proto__', payload: {a: payload.a + 1, b: payload.b}, now: at + ttl}, {key: 'other', payload: {}, now: at + ttl + 1}]});
  const authorized = rnd(1000), captured = rnd(authorized + 1), refunded = rnd(captured + 1);
  const payments = [{id: 'a', type: 'authorize', amount: authorized}, {id: 'c', type: 'capture', amount: captured}, {id: 'r', type: 'refund', amount: refunded}];
  if (i % 5 === 0) payments.push({...payments[1]});
  if (i % 7 === 0) payments.push({id: 'v', type: 'void', amount: 0});
  if (i % 11 === 0) payments[1].amount = authorized + 1;
  put('payment', {events: payments});
  const amount = 1 + rnd(10000), currency = i % 2 ? 'USD' : 'CNY';
  const tx = {id: 'tx', entries: [{account: 'cash', currency, side: 'debit', amount}, {account: 'sales', currency, side: 'credit', amount: amount + (i % 11 === 0 ? 1 : 0)}]};
  put('ledger', {transactions: i % 3 === 0 ? [tx, JSON.parse(JSON.stringify(tx))] : [tx]});
}
for (const m of fixture.modules) if (generated.has(m.id)) m.inputs = generated.get(m.id);
function evaluate(root, scale) {
  const score = fixture.verify(root, scale);
  return {...score, generatedGroups: [...generated.keys()], seed: '741fab19', kind: 'additional differential quality checks; not timed acceptance'};
}
if (process.argv[2] === '--self-test') {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'aide-robustness-'));
  try {
    fixture.setup(root, 'large');
    for (const m of fixture.modules) fs.writeFileSync(path.join(root, 'src', m.id + '.js'), 'module.exports = ' + m.source + ';');
    for (const scale of ['medium', 'large']) {
      const score = evaluate(root, scale);
      assert.equal(score.complete, true);
      console.log(JSON.stringify({scale, passed: score.passed, total: score.total, generatedGroups: score.generatedGroups.length}));
    }
  } finally { fs.rmSync(root, {recursive: true, force: true}); }
} else if (require.main === module) {
  console.log(JSON.stringify(evaluate(process.argv[2], process.argv[3] || 'medium')));
}
module.exports = {evaluate};
