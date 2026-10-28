"use strict";

const assert = require("assert/strict"), fs = require("fs"), vm = require("vm"), {assignment: assignment} = require("./test_drawio_layout_fixes.cjs"), {png: png} = require("./drawio-import-security-browser-cases.cjs");

const pako = require("../internal/server/web/vendor/drawio/js/deflate/pako.min.js"), base = "internal/server/web/vendor/drawio/js/";

for (const variant of [ "source", "app.min.js", "integrate.min.js" ]) {
    const s = fs.readFileSync(base + (variant === "source" ? "grapheditor/Editor.js" : variant), "utf8");
    const ctx = {
        Editor: {},
        EditorUi: {},
        window: {
            atob: atob
        },
        atob: atob,
        mxClient: {
            IS_SF: false
        },
        mxUtils: {
            bind: (self, fn) => fn.bind(self)
        },
        Graph: {
            stringToArrayBuffer: s => Uint8Array.from(s, (c => c.charCodeAt(0)))
        },
        pako: pako,
        Uint8Array: Uint8Array,
        TextDecoder: TextDecoder
    };
    vm.createContext(ctx);
    vm.runInContext(assignment(fs.readFileSync(base + "grapheditor/EditorUi.js", "utf8"), "EditorUi.parsePng"), ctx);
    vm.runInContext(assignment(s, "Editor.extractGraphModelFromPng"), ctx);
    const E = ctx.Editor, xml = "<mxGraphModel><root/></mxGraphModel>";
    for (const [raw, plain] of [ [ false, false ], [ true, false ], [ false, true ] ]) assert.equal(E.extractGraphModelFromPng(png(plain ? xml : encodeURIComponent(xml), raw, plain)), xml);
    const bomb = png("x".repeat(9 * 1024 * 1024));
    if (process.env.AIDE_PNG_CATALOG) {
        console.log(variant, "expanded bytes", E.extractGraphModelFromPng(bomb).length);
        continue;
    }
    assert.throws((() => E.extractGraphModelFromPng(bomb)), (e => e.code === "AIDE_DRAWIO_LIMIT"));
    assert.throws((() => E.extractGraphModelFromPng(png("x".repeat(9 * 1024 * 1024), true))), (e => e.code === "AIDE_DRAWIO_LIMIT"));
    assert.throws((() => E.extractGraphModelFromPng("data:image/png;base64," + "A".repeat(12 * 1024 * 1024 + 1))), (e => e.code === "AIDE_DRAWIO_LIMIT"));
    assert.equal(E.extractGraphModelFromPng("data:image/png;base64,invalid"), null);
    console.log("PASS PNG metadata budget " + variant);
}
