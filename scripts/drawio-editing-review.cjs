"use strict";

const fs = require("fs"), assert = require("assert/strict"), acorn = require("../internal/server/code_analysis/acorn.cjs");

const base = "internal/server/web/vendor/drawio/";

const revision = process.env.AIDE_DRAWIO_EDITING_BASELINE;

if (revision) assert(/^[a-f0-9]{7,40}$/.test(revision), "baseline must be a commit SHA");

function readSource(p) {
    return revision ? require("child_process").execFileSync("git", [ "show", revision + ":" + base + p ], {
        encoding: "utf8",
        maxBuffer: 64 * 1024 * 1024
    }) : fs.readFileSync(base + p, "utf8");
}

// The vendored Acorn parser reads one function atom, avoiding regex/comment
// delimiter mistakes and avoiding a comma-separated following assignment.
function atom(source, at) {
    const parser = new acorn.Parser({
        ecmaVersion: "latest"
    }, source, at);
    parser.nextToken();
    const node = parser.parseExprAtom();
    assert.equal(node.type, "FunctionExpression");
    return source.slice(at, node.end);
}

function assignment(source, name, which = "first") {
    const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"), re = new RegExp(escaped + "\\s*=\\s*function\\s*\\(", "g"), found = [];
    for (const m of source.matchAll(re)) {
        const eq = source.indexOf("=", m.index), at = source.indexOf("function", eq);
        found.push({
            text: source.slice(m.index, at) + atom(source, at),
            index: m.index
        });
    }
    assert(found.length, name);
    if (which === "wrapper") {
        const selected = found.filter((x => !x.text.includes("new mxCellMarker") && x.text.includes("getCell")));
        if (!selected.length) return null;
        assert.equal(selected.length, 1, name + " wrapper");
        return selected[0].text;
    }
    return found[0].text;
}

function route(source) {
    const match = /getRoutePattern\s*:\s*function\s*\(/.exec(source);
    assert(match, "route property");
    const at = source.indexOf("function", match.index);
    return atom(source, at);
}

const methods = [ [ 307, "mxgraph/src/layout/hierarchical/mxHierarchicalLayout.js", "mxHierarchicalLayout.prototype.execute" ], [ 308, "mxgraph/src/layout/hierarchical/mxSwimlaneLayout.js", "mxSwimlaneLayout.prototype.isPort" ], [ 309, "mxgraph/src/view/mxPrintPreview.js", "mxPrintPreview.prototype.open" ], [ 310, "js/grapheditor/Editor.js", "PrintDialog.createPrintPreview" ], [ "310-base", "mxgraph/src/view/mxPrintPreview.js", "mxPrintPreview.prototype.isTextLabel" ], [ 313, "mxgraph/src/view/mxGraph.js", "mxGraph.prototype.cellsAdded" ], [ 314, "mxgraph/src/view/mxGraph.js", "mxGraph.prototype.moveCells" ], [ 315, "mxgraph/src/view/mxGraph.js", "mxGraph.prototype.hitsSwimlaneContent" ], [ 316, "mxgraph/src/view/mxGraph.js", "mxGraph.prototype.destroy" ], [ 317, "js/grapheditor/EditorUi.js", "EditorUi.prototype.createOutline" ], [ 318, "js/grapheditor/Graph.js", "this.connectionHandler.insertEdge" ], [ 319, "js/grapheditor/Graph.js", "mxConnectionHandler.prototype.createMarker", "wrapper" ], [ 320, "mxgraph/src/handler/mxEdgeHandler.js", "mxEdgeHandler.prototype.redrawInnerBends" ], [ 321, "js/grapheditor/Editor.js", "Editor.extractGraphModelFromPng" ], [ 322, "js/grapheditor/Editor.js", "PageSetupDialog.addPageFormatPanel" ], [ 323, "js/grapheditor/Editor.js", "Editor.prototype.readGraphState" ], [ 324, "js/grapheditor/Format.js", "BaseFormatPanel.prototype.createArrayCellColorOption" ], [ 325, "js/grapheditor/Format.js", "StyleFormatPanel.prototype.addStroke" ] ];

function snippets(copy = "source") {
    const cache = new Map, read = p => {
        if (!cache.has(p)) cache.set(p, readSource(p));
        return cache.get(p);
    };
    const bundle = copy === "source" ? null : read(copy === "core" ? "mxgraph/mxClient.js" : "js/" + copy);
    const result = {
        methods: {},
        baseMarker: assignment(read("mxgraph/src/handler/mxConnectionHandler.js"), "mxConnectionHandler.prototype.createMarker"),
        route: route(bundle || read("mxgraph/src/view/mxEdgeStyle.js"))
    };
    for (const [n, file, name, which] of methods) {
        if (bundle && !bundle.includes(name + "=")) continue;
        const code = assignment(bundle || read(file), name, which);
        if (code) result.methods[n] = code;
    }
    return result;
}

module.exports = {
    base: base,
    methods: methods,
    assignment: assignment,
    atom: atom,
    route: route,
    snippets: snippets
};

if (require.main === module) for (const c of [ "source", "app.min.js", "integrate.min.js", "core" ]) {
    const d = snippets(c);
    console.log(c, Object.keys(d.methods).length, d.route.length);
}
