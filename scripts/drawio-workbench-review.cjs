"use strict";

const fs = require("fs"), assert = require("assert/strict"), {base: base, assignment: assignment, atom: atom} = require("./drawio-editing-review.cjs");

const methods = [ [ 551, "js/diagramly/sidebar/Sidebar-AWS3D.js", "Sidebar.prototype.addAWS3DPalette" ], [ 551, "js/diagramly/sidebar/Sidebar-C4.js", "Sidebar.prototype.addC4Palette" ], [ 551, "js/diagramly/sidebar/Sidebar-Floorplan.js", "Sidebar.prototype.addFloorplanPalette" ], [ 551, "js/diagramly/sidebar/Sidebar-GCP2.js", "Sidebar.prototype.addGCP2ServiceCardsPalette" ], [ 551, "js/diagramly/sidebar/Sidebar-Infographic.js", "Sidebar.prototype.addInfographicPalette" ], [ 551, "js/diagramly/sidebar/Sidebar-PID.js", "Sidebar.prototype.addPidValvesPalette" ], [ 551, "js/diagramly/sidebar/Sidebar-Salesforce.js", "Sidebar.prototype.addSalesforceComponentsPalette" ], [ 336, "js/diagramly/EditorUi.js", "EditorUi.prototype.loadOrgChartLayouts" ], [ 337, "js/diagramly/EditorUi.js", "EditorUi.prototype.scopeLayoutsToCells" ], [ 338, "js/diagramly/EditorUi.js", "EditorUi.prototype.getJsonForPage" ], [ 339, "js/diagramly/EditorUi.js", "EditorUi.prototype.getCommentAnchorCells" ], [ 339, "js/diagramly/EditorUi.js", "EditorUi.prototype.updateCommentOverlays" ], [ 341, "js/diagramly/EditorUi.js", "CommentsWindow" ], [ 342, "shapes/mockup/mxMockupiOS.js", "mxShapeMockupiIconGrid.prototype.paintVertexShape" ], [ 342, "shapes/mockup/mxMockupiOS.js", "mxShapeMockupiIconGrid.prototype.background" ], [ 342, "shapes/mockup/mxMockupiOS.js", "mxShapeMockupiIconGrid.prototype.foreground" ], [ 343, "shapes/mockup/mxMockupiOS.js", "mxShapeMockupiPhone.prototype.foreground" ], [ 343, "shapes/mockup/mxMockupiOS.js", "mxShapeMockupiBgStriped.prototype.foreground" ], [ 343, "shapes/mockup/mxMockupiOS.js", "mxShapeMockupiPad.prototype.foreground" ], [ 344, "js/grapheditor/Editor.js", "FilenameDialog" ], [ 345, "js/grapheditor/EditorUi.js", "EditorUi.parsePng" ], [ 346, "js/grapheditor/EditorUi.js", "EditorUi.prototype.installShapePicker" ], [ 347, "js/grapheditor/EditorUi.js", "EditorUi.prototype.initCanvas" ], [ 349, "js/grapheditor/EditorUi.js", "EditorUi.prototype.createKeyHandler" ], [ 350, "js/grapheditor/Format.js", "ArrangePanel.prototype.addGeometryHandler" ], [ 351, "js/grapheditor/Format.js", "TextFormatPanel.prototype.addFont" ], [ 352, "js/grapheditor/Shapes.js", "TableShape.prototype.paintForeground" ], [ 353, "js/diagramly/sidebar/Sidebar-Gmdl.js", "Sidebar.prototype.addGMDLSelectionControlsPalette" ], [ 354, "js/diagramly/sidebar/Sidebar-Gmdl.js", "Sidebar.prototype.addGMDLTextFieldsPalette" ], [ 355, "js/diagramly/sidebar/Sidebar-Gmdl.js", "Sidebar.prototype.addGMDLSteppersPalette" ], [ 357, "js/diagramly/sidebar/Sidebar-Ios.js", "Sidebar.prototype.addIosPalette" ], [ 358, "js/diagramly/sidebar/Sidebar-Mockup.js", "Sidebar.prototype.addMockupFormsPalette" ], [ 359, "js/diagramly/sidebar/Sidebar-SAP.js", "Sidebar.prototype.addSAPAreaShapesPalette" ], [ 359, "js/diagramly/sidebar/Sidebar-SAP.js", "Sidebar.prototype.addSAPDefaultShapesPalette" ], [ 360, "js/diagramly/sidebar/Sidebar-Sysml.js", "Sidebar.prototype.addSysMLPortsAndFlowsPalette" ], [ 361, "js/diagramly/sidebar/Sidebar-ThreatModeling.js", "Sidebar.prototype.addThreatModelingPalette" ], [ 362, "js/diagramly/sidebar/Sidebar-VVD.js", "Sidebar.prototype.addVVDPalette" ] ];

function target(text, row) {
    const name = row[2];
    if (name.startsWith("TableShape.")) {
        const reg = /mxCellRenderer\.registerShape\(['"]table['"],\s*([\w$]+)\)/.exec(text);
        if (!reg) return null;
        const alias = reg[1], prefix = text.slice(0, reg.index), re = new RegExp(alias.replace(/\$/g, "\\$") + "\\.prototype\\.paintForeground\\s*=\\s*function\\s*\\(", "g"), all = [ ...prefix.matchAll(re) ], m = all.at(-1);
        assert(m, "table foreground alias");
        const at = text.indexOf("function", m.index), code = text.slice(m.index, at) + atom(text, at);
        return {
            code: code,
            name: alias + ".prototype.paintForeground",
            canonical: code.replace(alias + ".prototype.paintForeground", name)
        };
    }
    if (!text.includes(name + "=")) return null;
    const code = assignment(text, name);
    return {
        code: code,
        name: name,
        canonical: code
    };
}

function snippets(copy = "source") {
    const cache = new Map;
    const read = p => {
        if (!cache.has(p)) cache.set(p, fs.readFileSync(base + p, "utf8"));
        return cache.get(p);
    };
    return methods.map((row => {
        const text = read(copy === "source" ? row[1] : "js/" + copy);
        const code = copy === "source" ? assignment(text, row[2]) : target(text, row)?.canonical;
        return {
            issue: row[0],
            name: row[2],
            code: code
        };
    })).filter((r => r.code));
}

module.exports = {
    base: base,
    methods: methods,
    snippets: snippets,
    target: target
};
