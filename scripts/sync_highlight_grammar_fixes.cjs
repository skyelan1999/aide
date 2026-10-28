"use strict";

const fs = require("fs"), assert = require("assert/strict");

const file = "internal/server/web/vendor/highlight.js/highlight.min.js", marker = "/* AIDE grammar repairs: pinned registration-layer fixes. */";

const before = fs.readFileSync(file, "utf8"), base = before.split(marker)[0].trimEnd(), patch = fs.readFileSync("internal/server/web/highlight-grammar-fixes.js", "utf8");

assert.equal(require('crypto').createHash('sha256').update(base).digest('hex'), '837a6fa5b0c736b52bbde2b2b6190f305da3fc9ed41681db5321507057b5c846', 'Review grammar repairs before upgrading the pinned core');
const after = base + "\n" + marker + "\n" + patch;

if (process.argv.includes("--check")) assert.ok(before === after, "Grammar repair suffix differs from source"); else fs.writeFileSync(file, after);

console.log("PASS highlight grammar repair source/bundle sync");
