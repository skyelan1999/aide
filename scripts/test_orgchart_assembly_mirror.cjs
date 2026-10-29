"use strict";

const fs = require("fs"), assert = require("assert/strict");

const base = "internal/server/web/vendor/drawio/js/", assembly = fs.readFileSync(base + "orgchart/OrgChart.Layout.min.js", "utf8");

for (const name of [ "extensions.min.js", "integrate.min.js", "orgchart.min.js" ]) {
    const text = fs.readFileSync(base + name, "utf8");
    assert.equal(text.split(assembly).length, 2, name + " must embed exactly the current OrgChart assembly");
}

console.log("PASS exact OrgChart assembly mirror in all 3 runtime bundles");
