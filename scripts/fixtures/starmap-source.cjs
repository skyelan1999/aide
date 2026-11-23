'use strict';
// TypeScript emission normalizes whitespace. These helpers keep source-level
// assertions independent of formatting without weakening their code checks.
const assert = require('node:assert/strict');

function compactWithMap(source) {
  let compact = '';
  const map = [];
  for (let i = 0; i < source.length; i++) {
    if (/\s/.test(source[i])) continue;
    map.push(i);
    compact += source[i];
  }
  return { compact, map };
}

function sourceIndex(source, marker, from = 0) {
  const direct = source.indexOf(marker, from);
  if (direct >= 0) return direct;
  const { compact, map } = compactWithMap(source);
  const needle = marker.replace(/\s+/g, '');
  let compactFrom = 0;
  if (from > 0) {
    compactFrom = compact.length;
    for (let i = 0; i < map.length; i++) {
      if (map[i] >= from) { compactFrom = i; break; }
    }
  }
  const index = compact.indexOf(needle, compactFrom);
  if (index < 0) throw new Error('Source marker not found: ' + marker);
  return map[index];
}

function sourceIncludes(source, marker) {
  if (source.includes(marker)) return true;
  return source.replace(/\s+/g, '').includes(marker.replace(/\s+/g, ''));
}

function sourceMatch(source, regex, message) {
  if (regex.test(source)) { assert.ok(true, message); return; }
  const compactSource = source.replace(/\s+/g, '');
  const compactPattern = regex.source.replace(/\s+/g, '');
  assert.match(compactSource, new RegExp(compactPattern, regex.flags), message);
}

function sourceRegex(source, regex) {
  return source.match(regex) || source.replace(/\s+/g, '').match(new RegExp(regex.source.replace(/\s+/g, ''), regex.flags));
}

module.exports = { sourceIndex, sourceIncludes, sourceMatch, sourceRegex };
