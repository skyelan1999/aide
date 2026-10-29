"use strict";

const fs = require("fs"), vm = require("vm"), assert = require("assert/strict"), r = require("./drawio-ui-robustness-review.cjs");

const row = r.methods.find((r => r[0] === 457)), files = [ row[1], "js/app.min.js", "js/integrate.min.js", "js/viewer.min.js", "js/viewer-static.min.js" ];

for (const file of files) {
    const source = process.env.AIDE_DRAWIO_EDITING_BASELINE ? require("child_process").execFileSync("git", [ "show", process.env.AIDE_DRAWIO_EDITING_BASELINE + ":" + r.base + file ], {
        encoding: "utf8",
        maxBuffer: 64e6
    }) : fs.readFileSync(r.base + file, "utf8");
    const c = {
        EditorUi: function() {},
        mxUtils: {
            getXml: n => n.xml
        }
    };
    vm.createContext(c);
    vm.runInContext(r.method(source, row), c);
    let data;
    const expected = "<mxGraphModel><root/></mxGraphModel>";
    c.EditorUi.prototype.updateDiagramData.call({
        replaceDiagramData: v => data = v
    }, null, {
        xml: expected
    });
    assert.equal(data, expected);
    console.log("PASS base fallback serializer", file);
}

// Never confuse Graph with the mxGraph base class when synchronizing methods.
assert.equal(r.method("mxGraph.prototype.destroy=function(){}", r.methods.find((r => r[0] === "462-destroy")), true), null);
