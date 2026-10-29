"use strict";

const fs = require("fs"), {assignment: assignment, base: base} = require("./drawio-editing-review.cjs");

const methods = [ [ 311, "mxgraph/src/view/mxCellEditor.js", "mxCellEditor.prototype.startEditing" ], [ 326, "js/diagramly/Editor.js", "Graph.prototype.getCellsForTags" ], [ 327, "js/diagramly/Editor.js", "Graph.prototype.getCellsForAction" ], [ 327, "js/diagramly/Editor.js", "Editor.AnimationPlayer.prototype.collectReferencedCells" ], [ 328, "js/diagramly/EditorUi.js", "EditorUi.prototype.getHtml" ], [ 329, "js/diagramly/EditorUi.js", "EditorUi.prototype.getHiddenTagsMap" ], [ 330, "js/diagramly/EditorUi.js", "EditorUi.prototype.replaceAlternateContent" ], [ 331, "js/diagramly/EditorUi.js", "EditorUi.prototype.createLink" ], [ 332, "js/diagramly/EditorUi.js", "EditorUi.prototype.addLocalPagesToMapping" ], [ 332, "js/diagramly/EditorUi.js", "EditorUi.prototype.importXml" ], [ 332, "js/diagramly/EditorUi.js", "EditorUi.prototype.updatePageLink" ], [ 333, "js/diagramly/EditorUi.js", "EditorUi.prototype.generateOpenAiMermaidDiagram" ], [ 334, "js/diagramly/EditorUi.js", "EditorUi.prototype.importFiles" ] ];

function snippets(copy = "source") {
    const cache = new Map, read = p => {
        if (!cache.has(p)) cache.set(p, fs.readFileSync(base + p, "utf8"));
        return cache.get(p);
    };
    const bundle = copy === "source" ? null : read("js/" + copy);
    return methods.map((([issue, file, name]) => ({
        issue: issue,
        name: name,
        code: assignment(bundle || read(file), name)
    })));
}

module.exports = {
    methods: methods,
    base: base,
    snippets: snippets
};
