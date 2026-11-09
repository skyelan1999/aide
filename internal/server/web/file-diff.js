(function () {
  'use strict';
  // 行级 diff：先裁掉公共前后缀，再对中间段做 LCS。中间段过大时退化为
  // “整段删除 + 整段新增”，保证渲染不会被超大文件拖死。
  const DP_LIMIT = 400000;   // 中间段行数乘积上限（约 1.6MB 的 DP 表）
  const ROW_LIMIT = 2000;    // 单文件最多渲染的差异行数

  function splitLines(text) {
    const normalized = String(text == null ? '' : text).replace(/\r\n?/g, '\n');
    if (!normalized) return [];
    const lines = normalized.split('\n');
    if (lines[lines.length - 1] === '') lines.pop();
    return lines;
  }

  function lcsOps(a, b) {
    const n = a.length, m = b.length;
    const width = m + 1;
    const table = new Uint32Array((n + 1) * width);
    for (let i = n - 1; i >= 0; i--) {
      for (let j = m - 1; j >= 0; j--) {
        table[i * width + j] = a[i] === b[j]
          ? table[(i + 1) * width + j + 1] + 1
          : Math.max(table[(i + 1) * width + j], table[i * width + j + 1]);
      }
    }
    const ops = [];
    let i = 0, j = 0;
    while (i < n && j < m) {
      if (a[i] === b[j]) { ops.push({kind: 'ctx', text: a[i]}); i++; j++; }
      else if (table[(i + 1) * width + j] >= table[i * width + j + 1]) { ops.push({kind: 'del', text: a[i]}); i++; }
      else { ops.push({kind: 'add', text: b[j]}); j++; }
    }
    while (i < n) ops.push({kind: 'del', text: a[i++]});
    while (j < m) ops.push({kind: 'add', text: b[j++]});
    return ops;
  }

  function compute(before, after) {
    const a = splitLines(before), b = splitLines(after);
    let start = 0;
    while (start < a.length && start < b.length && a[start] === b[start]) start++;
    let endA = a.length, endB = b.length;
    while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) { endA--; endB--; }
    const midA = a.slice(start, endA), midB = b.slice(start, endB);
    const ops = [];
    for (let i = 0; i < start; i++) ops.push({kind: 'ctx', text: a[i]});
    if (midA.length * midB.length > DP_LIMIT) {
      for (const line of midA) ops.push({kind: 'del', text: line});
      for (const line of midB) ops.push({kind: 'add', text: line});
    } else {
      ops.push(...lcsOps(midA, midB));
    }
    for (let i = endA; i < a.length; i++) ops.push({kind: 'ctx', text: a[i]});

    let adds = 0, dels = 0, oldNo = 1, newNo = 1;
    const rows = [];
    for (const op of ops) {
      if (op.kind === 'add') adds++;
      else if (op.kind === 'del') dels++;
      if (rows.length >= ROW_LIMIT) continue;
      rows.push({
        kind: op.kind,
        text: op.text,
        oldNo: op.kind === 'add' ? null : oldNo++,
        newNo: op.kind === 'del' ? null : newNo++,
      });
    }
    return {rows, adds, dels, truncated: ops.length - rows.length};
  }

  const api = Object.freeze({compute, splitLines});
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  if (typeof window !== 'undefined') window.AideFileDiff = api;
})();
