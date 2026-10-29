"use strict";

const fs = require("fs"), {base: base, assignment: assignment, atom: atom} = require("./drawio-editing-review.cjs");

const rows = [ [ 385, "shapes/ios7/mxIOS7Ui.js", "mxShapeIOS7SelectBar.prototype.paintVertexShape" ], [ 386, "shapes/mockup/mxMockupNavigation.js", "Graph.handleFactory[mxShapeMockupScrollBar.prototype.cst.SHAPE_SCROLL_BAR]" ], [ 387, "shapes/mxArchiMate.js", "mxArchiMateApplication.prototype.foreground" ], ...[ "Message", "Datatype", "DeadLetter", "InvalidMessage" ].map((n => [ 388, "shapes/mxEip.js", "mxShapeEip" + n + "Channel.prototype.getConstraints" ])), [ 389, "shapes/mxArrows.js", "mxShapeArrowsWedgeArrowDashed2.prototype.paintEdgeShape" ], [ 390, "shapes/mxArrows.js", "mxShapeArrows2CalloutDouble90Arrow.prototype.paintVertexShape" ], [ 390, "shapes/mxArrows.js", "mxShapeArrows2CalloutDouble90Arrow.prototype.getConstraints" ] ];

function snippets(copy = "source") {
    return rows.map((([issue, file, name]) => ({
        issue: issue,
        name: name,
        code: assignment(fs.readFileSync(base + (copy === "source" ? file : "js/" + copy), "utf8"), name)
    })));
}

module.exports = {
    base: base,
    methods: rows,
    snippets: snippets
};
