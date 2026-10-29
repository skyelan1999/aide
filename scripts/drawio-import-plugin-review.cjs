"use strict";

const fs = require("fs"), assert = require("assert/strict"), {base: base, assignment: assignment, atom: atom} = require("./drawio-editing-review.cjs");

const methods = [ [ 363, "js/diagramly/Editor.js", "Editor.extractGraphModelFromPdf" ], [ 366, "js/diagramly/Editor.js", "PrintDialog.prototype.create" ], [ 367, "js/diagramly/EditorUi.js", "EditorUi.prototype.importZipFile" ], [ 368, "mxgraph/src/shape/mxConnector.js", "mxConnector.prototype.createMarker" ], [ 368, "mxgraph/src/shape/mxArrowConnector.js", "mxArrowConnector.prototype.paintEdgeShape" ], [ 371, "shapes/mxSysML.js", "mxShapeSysMLComposite.prototype.paintForeground" ], [ 372, "shapes/bpmn/mxBpmnShape2.js", "mxBpmnShape.prototype.redrawPath" ], [ 372, "shapes/bpmn/mxBpmnShape2.js", "mxShapeBpmnEvent.prototype.strictDrawShape" ], [ 374, "shapes/emoji/mxEmoji.js", "mxShapeEmoji.prototype.getUnicodeText" ], [ 375, "js/orgchart/mxOrgChartLayout.js", "RPOrgChart.positionBoxes", "property" ], [ 376, "js/orgchart/mxOrgChartLayout.js", "RPOrgChart.generateData", "property" ], [ 377, "js/grapheditor/Graph.js", "Graph.exploreFromCell" ] ];

function property(text, key) {
    const m = new RegExp("\\b" + key + "\\s*:\\s*function\\s*\\(").exec(text);
    assert(m, key);
    const at = text.indexOf("function", m.index);
    return {
        text: text.slice(m.index, at) + atom(text, at),
        fn: atom(text, at)
    };
}

function source(row) {
    const s = fs.readFileSync(base + row[1], "utf8");
    return row[3] === "property" ? row[2] + " = " + property(s, row[2].split(".").at(-1)).fn : assignment(s, row[2]);
}

function target(text, row) {
    if (row[3] === "property") {
        const key = row[2].split(".").at(-1);
        if (!text.includes(key + ":")) return null;
        const p = property(text, key);
        return {
            code: p.text,
            canonical: row[2] + " = " + p.fn,
            property: key
        };
    }
    if (!text.includes(row[2] + "=")) return null;
    const code = assignment(text, row[2]);
    return {
        code: code,
        canonical: code
    };
}

function snippets(copy = "source") {
    const cache = new Map, read = p => {
        if (!cache.has(p)) cache.set(p, fs.readFileSync(base + p, "utf8"));
        return cache.get(p);
    };
    return methods.map((row => {
        let code;
        if (copy === "source") code = source(row); else {
            let asset = copy;
            if (row[3] === "property" && copy === "app.min.js") asset = "orgchart.min.js";
            if (row[1].startsWith("shapes/") && copy === "app.min.js") asset = "shapes-14-6-5.min.js";
            code = target(read("js/" + asset), row)?.canonical;
        }
        return {
            issue: row[0],
            name: row[2],
            code: code
        };
    })).filter((x => x.code));
}

module.exports = {
    base: base,
    methods: methods,
    source: source,
    target: target,
    snippets: snippets,
    property: property
};
