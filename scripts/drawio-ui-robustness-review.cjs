"use strict";

const {base: base, assignment: assignment, atom: atom} = require("./drawio-editing-review.cjs");

const methods = [ [ 452, "js/grapheditor/Editor.js", "FilenameDialog" ], [ 453, "js/grapheditor/EditorUi.js", "EditorUi.prototype.findCommonProperties" ], [ 454, "js/grapheditor/EditorUi.js", "EditorUi.prototype.getCellsForShapePicker" ], [ 455, "js/grapheditor/EditorUi.js", "EditorUi.prototype.initCanvas" ], [ 457, "js/grapheditor/EditorUi.js", "EditorUi.prototype.updateDiagramData" ], [ 459, "js/grapheditor/EditorUi.js", "EditorUi.prototype.executeLayouts" ], [ 460, "js/grapheditor/Graph.js", "Graph.exploreFromCell" ], [ 461, "js/grapheditor/Graph.js", "Graph.prototype.replaceElement" ], [ 462, "js/grapheditor/Graph.js", "Graph.prototype.addClickHandler" ], [ "462-destroy", "js/grapheditor/Graph.js", "Graph.prototype.destroy" ], [ 465, "js/diagramly/Editor.js", "PrintDialog.prototype.create", "last" ], [ 466, "js/diagramly/EditorUi.js", "EditorUi.prototype.setFileData" ] ];

function method(text, row, optional = false) {
    const escaped = row[2].replace(/[.*+?^${}()|[\]\\]/g, "\\$&"), matches = [ ...text.matchAll(new RegExp("(?<![$\\w.])" + escaped + "\\s*=\\s*function\\s*\\(", "g")) ], selected = row[3] === "last" ? matches.at(-1) : matches[0];
    if (!selected) {
        if (optional) return null;
        throw Error(row[2]);
    }
    const at = text.indexOf("function", selected.index);
    return text.slice(selected.index, at) + atom(text, at);
}

function target(text, row) {
    const code = method(text, row, true);
    return code ? {
        name: row[2],
        code: code
    } : null;
}

module.exports = {
    base: base,
    methods: methods,
    method: method,
    assignment: assignment,
    atom: atom,
    target: target
};
