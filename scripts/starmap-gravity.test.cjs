'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const sandbox = { window: {} };
vm.createContext(sandbox);
vm.runInContext(fs.readFileSync('internal/server/web/starmap-gravity.js', 'utf8'), sandbox);
const analyze = sandbox.window.AideStarGravity.analyze;

const nodes = Array.from({ length: 40 }, (_, index) => ({ id: `n${index}` }));
const edges = [];
// Dense 16-node region (complete graph), with lower-density links elsewhere.
for (let a = 0; a < 16; a++) for (let b = a + 1; b < 16; b++) edges.push({ from: `n${a}`, to: `n${b}`, kind: 'relation' });
for (let a = 16; a < 40; a++) edges.push({ from: `n${a}`, to: `n${16 + (a - 15) % 24}`, kind: 'relation' });
edges.push({ from: 'n0', to: 'n0', kind: 'relation' });
edges.push({ from: 'n0', to: 'n1', kind: 'contains' });
const groups = [
 { key: 'dense', members: nodes.slice(0, 16).map(node => node.id) },
 { key: 'sparse', members: nodes.slice(16).map(node => node.id) },
];
const result = analyze(nodes, edges, groups);

assert.equal(result.degrees.get('n0'), 15);
assert.equal(result.degrees.get('n39'), 2);
assert(result.highlightedIds.has('n0'));
assert(!result.highlightedIds.has('n39'));
assert.deepEqual(Array.from(result.blackHoles, hole => hole.groupKey), ['dense']);
assert.equal(result.blackHoles[0].linkCount, 120);
assert(result.blackHoles[0].radius > 14 && result.blackHoles[0].radius <= 34);

const structuralOnly = analyze(nodes.slice(0, 16), [{ from: 'n0', to: 'n1', kind: 'defines' }], [groups[0]]);
assert.equal(structuralOnly.highlightedIds.size, 0);
assert.equal(structuralOnly.blackHoles.length, 0);
assert.equal(analyze([], [], []).blackHoles.length, 0);
console.log('PASS starmap gravity: adaptive highlight, unique effective links, dense regions, structural-edge exclusion, empty graph');
