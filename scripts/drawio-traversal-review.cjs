"use strict";

const fs = require("fs"), {base: base, assignment: assignment} = require("./drawio-editing-review.cjs");

const methods = [ [ 399, "mxgraph/src/layout/hierarchical/mxHierarchicalLayout.js", "mxHierarchicalLayout.prototype.traverse" ], [ 399, "mxgraph/src/layout/hierarchical/model/mxGraphHierarchyModel.js", "mxGraphHierarchyModel.prototype.dfs" ], [ 399, "mxgraph/src/layout/hierarchical/model/mxGraphHierarchyModel.js", "mxGraphHierarchyModel.prototype.extendedDfs" ], [ 400, "mxgraph/src/view/mxGraph.js", "mxGraph.prototype.traverse" ] ];

function snippets(file) {
    return methods.map((([issue, p, name]) => {
        const text = fs.readFileSync(base + (file || p), "utf8");
        return text.includes(name + "=") || !file ? {
            issue: issue,
            name: name,
            code: assignment(text, name)
        } : null;
    })).filter(Boolean);
}

module.exports = {
    base: base,
    methods: methods,
    snippets: snippets
};
