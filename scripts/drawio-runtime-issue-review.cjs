"use strict";

const fs = require("node:fs"), {assignment: assignment} = require("./test_drawio_layout_fixes.cjs");

const methods = [ [ "grapheditor/Editor.js", "Editor.extractGraphModelFromPng" ], [ "diagramly/sidebar/Sidebar.js", "Sidebar.prototype.addCustomEntries" ], [ "diagramly/util/mxJsCanvas.js", "mxJsCanvas.prototype.setMiterLimit" ], [ "diagramly/util/mxJsCanvas.js", "mxJsCanvas.prototype.setGradient" ], [ "grapheditor/Dialogs.js", "ExportDialog.saveLocalFile" ], [ "../mxgraph/src/layout/hierarchical/stage/mxCoordinateAssignment.js", "mxCoordinateAssignment.prototype.setEdgePosition" ] ];

const base = "internal/server/web/vendor/drawio/js/";

function part(s, marker) {
    const at = s.search(new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, "\\$&").replace("=", "\\s*=")));
    const p = at;
    if (p < 0) throw Error(marker);
    let start = s.lastIndexOf(";", p) + 1;
    const name = /([\w$]+\.prototype\.[\w$]+)\s*=/.exec(s.slice(start, p + marker.length + 50));
    if (!name) throw Error(marker);
    return assignment(s, name[1]);
}

function edgeFunction(s) {
    const acorn = require("../internal/server/code_analysis/acorn.cjs");
    const ast = acorn.parse(s, {
        ecmaVersion: "latest"
    });
    let found;
    function walk(n) {
        if (!n || typeof n !== "object") return;
        if ((n.type === "FunctionDeclaration" || n.type === "FunctionExpression") && n.params.length === 5) {
            const t = s.slice(n.start, n.end);
            if (t.includes(".absolutePoints") && t.includes(".call(this,") && t.length < 4500 && (t.match(/\.call\(this,/g) || []).length === 2) {
                if (found) throw Error("ambiguous edge function");
                found = t.replace(/^function\s*\(/, "function OwnedEdge(");
            }
        }
        for (const [k, v] of Object.entries(n)) {
            if (k === "start" || k === "end") continue;
            if (Array.isArray(v)) v.forEach(walk); else if (v && typeof v === "object") walk(v);
        }
    }
    walk(ast);
    if (!found) throw Error("edge function missing");
    return found;
}

function codec(s) {
    const re = /([\w$]+)\.encodeCell\s*=\s*function\s*\(/g;
    for (const m of s.matchAll(re)) {
        const t = assignment(s.slice(m.index), m[1] + ".encodeCell");
        if (t.includes("mxStyleRegistry.getName")) return t;
    }
    throw Error("view codec missing");
}

function snippets(variant) {
    let cache = new Map;
    const read = p => {
        if (!cache.has(p)) cache.set(p, fs.readFileSync(p, "utf8"));
        return cache.get(p);
    };
    const bundle = variant === "source" ? null : read(base + variant);
    return {
        sourceFallbacks: methods.filter((([f, n]) => bundle && !bundle.includes(n + "="))).map((([f, n]) => n)),
        methods: methods.map((([f, n]) => assignment(bundle && bundle.includes(n + "=") ? bundle : read(base + f), n))),
        rect: [ "moveNEInner", "lineNEInner" ].map((n => part(bundle || read(base + "grapheditor/Shapes.js"), ".prototype." + n + "=").replace(/^[\w$]+\.prototype/, "OwnedRect.prototype"))),
        edge: edgeFunction(bundle || read(base + "grapheditor/Shapes.js")),
        codec: codec(bundle || read(base + "../mxgraph/src/io/mxGraphViewCodec.js"))
    };
}

module.exports = {
    methods: methods,
    base: base,
    edgeFunction: edgeFunction,
    codec: codec,
    snippets: snippets,
    part: part
};
