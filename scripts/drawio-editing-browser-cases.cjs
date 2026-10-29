"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), review = require("./drawio-editing-review.cjs"), {png: png} = require("./drawio-import-security-browser-cases.cjs");

module.exports = async function(frame, out, theme) {
    const checks = [];
    for (const copy of [ "source", "app.min.js", "integrate.min.js" ]) {
        const data = review.snippets(copy);
        data.badPng = png("%ZZ", false, true);
        data.doubleBadPng = png("%25ZZ", false, true);
        data.goodPng = png(encodeURIComponent("<mxGraphModel><root/></mxGraphModel>"));
        const result = await frame.evaluate((async data => {
            const failures = [], details = [], saved = [];
            let graph, host;
            function check(ok, text) {
                if (!ok) failures.push(text);
            }
            function remember(owner, key) {
                const value = owner[key];
                saved.push((() => owner[key] = value));
            }
            function test(number, fn) {
                try {
                    return fn();
                } catch (e) {
                    failures.push(number + " exception " + e.message);
                }
            }
            const methods = data.methods;
            for (const [number, code] of Object.entries(methods)) {
                if ([ "318", "319" ].includes(number)) continue;
                const name = /^([\w.]+)/.exec(code)[1], dot = name.lastIndexOf("."), owner = Function("return " + name.slice(0, dot))(), key = name.slice(dot + 1);
                remember(owner, key);
                Function(code)();
            }
            remember(mxEdgeStyle, "getRoutePattern");
            mxEdgeStyle.getRoutePattern = Function("return (" + data.route + ")")();
            if (methods[319]) {
                const Local = function() {};
                Function("mxConnectionHandler", data.baseMarker)(Local);
                const alias = /[\w$]+\s*=\s*([\w$]+)\.apply\(this,\s*arguments\)/.exec(methods[319])[1];
                remember(mxConnectionHandler.prototype, "createMarker");
                Function(alias, methods[319])(Local.prototype.createMarker);
            }
            function makeGraph() {
                const div = document.createElement("div");
                div.style.cssText = "position:absolute;left:20px;top:60px;width:700px;height:500px;background:white";
                document.body.append(div);
                return new Graph(div);
            }
            try {
                graph = makeGraph();
                const p = graph.getDefaultParent();
                let a, b, c, d, edge;
                graph.model.beginUpdate();
                try {
                    a = graph.insertVertex(p, null, "A", 20, 20, 80, 40);
                    b = graph.insertVertex(p, null, "B", 220, 120, 80, 40);
                    c = graph.insertVertex(p, null, "C", 500, 300, 80, 40);
                    d = graph.insertVertex(p, null, "D", 500, 300, 80, 40);
                    edge = graph.insertEdge(p, null, "edge", a, b, "edgeStyle=none;");
                    graph.insertEdge(p, null, "", c, d, "edgeStyle=none;");
                } finally {
                    graph.model.endUpdate();
                }
                graph.view.validate();
                test(307, (() => {
                    const layout = new mxHierarchicalLayout(graph);
                    layout.execute(p, [ a ]);
                    layout.execute(p);
                    check(c.geometry.x !== d.geometry.x || c.geometry.y !== d.geometry.y, "307 reused layout includes second component");
                }));
                test(308, (() => {
                    const layout = new mxSwimlaneLayout(graph);
                    check(layout.isPort(new mxCell) === false, "308 null geometry port");
                }));
                test(312, (() => {
                    for (const quad of [ 0, 1, 2, 3 ]) for (const dx of [ 0, 10 ]) for (const dy of [ 0, 10 ]) {
                        const pattern = mxEdgeStyle.getRoutePattern([ mxConstants.DIRECTION_MASK_WEST, mxConstants.DIRECTION_MASK_EAST ], quad, dx, dy);
                        check(Array.isArray(pattern) && pattern.length > 0, "312 route pattern " + [ quad, dx, dy ]);
                    }
                }));
                test(313, (() => {
                    const model = graph.model, original = model.updateEdgeParent;
                    const listener = () => {
                        throw Error("owned listener failure");
                    };
                    model.addListener(mxEvent.EXECUTE, listener);
                    try {
                        try {
                            graph.cellsAdded([ new mxCell("fault", new mxGeometry(0, 0, 20, 20)) ], p, 0);
                        } catch (e) {
                            check(e.message === "owned listener failure", "313 preserves original error");
                        }
                        check(model.updateEdgeParent === original, "313 updateEdgeParent restored");
                    } finally {
                        model.removeListener(listener);
                        model.updateEdgeParent = original;
                    }
                }));
                test(314, (() => {
                    graph.setAllowNegativeCoordinates(false);
                    const listener = () => {
                        throw Error("owned moved failure");
                    };
                    graph.addListener(mxEvent.CELLS_MOVED, listener);
                    try {
                        try {
                            graph.moveCells([ a ], 2, 3, false, p);
                        } catch (e) {
                            check(e.message === "owned moved failure", "314 preserves original error");
                        }
                        check(graph.isAllowNegativeCoordinates() === false, "314 negative coordinate policy restored");
                    } finally {
                        graph.removeListener(listener);
                        graph.setAllowNegativeCoordinates(true);
                    }
                }));
                test(315, (() => {
                    for (const scale of [ 1, 1.5 ]) {
                        graph.view.setScale(scale);
                        for (const horizontal of [ 0, 1 ]) for (const direction of [ "east", "south", "west", "north" ]) for (const flipH of [ 0, 1 ]) for (const flipV of [ 0, 1 ]) {
                            const style = "swimlane;horizontal=" + horizontal + ";direction=" + direction + ";flipH=" + flipH + ";flipV=" + flipV + ";startSize=30;";
                            const cell = graph.insertVertex(p, null, "Lane", 100, 100, 200, 120, style);
                            graph.view.validate();
                            const state = graph.view.getState(cell);
                            let side = horizontal ? {
                                east: "top",
                                south: "right",
                                west: "bottom",
                                north: "left"
                            }[direction] : {
                                east: "left",
                                south: "top",
                                west: "right",
                                north: "bottom"
                            }[direction];
                            if (flipH) side = {
                                left: "right",
                                right: "left"
                            }[side] || side;
                            if (flipV) side = {
                                top: "bottom",
                                bottom: "top"
                            }[side] || side;
                            const tests = {
                                left: [ 10, 60 ],
                                right: [ 190, 60 ],
                                top: [ 100, 10 ],
                                bottom: [ 100, 110 ]
                            };
                            for (const [candidate, pt] of Object.entries(tests)) {
                                const actual = graph.hitsSwimlaneContent(cell, state.x + pt[0] * scale, state.y + pt[1] * scale);
                                check(actual === (candidate !== side), "315 header " + [ horizontal, direction, flipH, flipV, candidate ]);
                            }
                            graph.removeCells([ cell ], true);
                        }
                    }
                    graph.view.setScale(1);
                }));
                test(318, (() => {
                    Function(methods[318]).call(graph);
                    const count = graph.getChildEdges(p).length;
                    graph.setCellStyles("newEdgeStyle", "%7Bbad", [ a ]);
                    let error;
                    try {
                        graph.connectionHandler.insertEdge(p, null, "", a, b);
                    } catch (e) {
                        error = e;
                    }
                    check(!!error, "318 malformed style rejected");
                    check(graph.getChildEdges(p).length === count, "318 no partially inserted edge");
                    graph.setCellStyles("newEdgeStyle", encodeURIComponent(JSON.stringify({
                        strokeColor: "#123456"
                    })), [ a ]);
                    const valid = graph.connectionHandler.insertEdge(p, null, "", a, b);
                    check(graph.getCellStyle(valid).strokeColor === "#123456", "318 valid style still applied");
                    graph.removeCells([ valid ], true);
                    graph.setCellStyles("newEdgeStyle", null, [ a ]);
                }));
                test(319, (() => {
                    const h = graph.connectionHandler, oldLoops = graph.allowLoops, oldDangling = graph.allowDanglingEdges;
                    function attempt(cell, loops, dangling, allowed, expectedTarget) {
                        graph.allowLoops = loops;
                        graph.allowDanglingEdges = dangling;
                        const state = graph.view.getState(a);
                        h.previous = state;
                        h.first = new mxPoint(state.x, state.y);
                        h.currentPoint = null;
                        h.shape = h.createShape();
                        h.marker = h.createMarker();
                        // A hidden child can still be supplied by a connection API caller.
                        const targetState = cell ? (graph.view.getState(cell) || new mxCellState(graph.view, cell, graph.getCellStyle(cell))) : null;
                        const event = new MouseEvent("mouseup", {
                            clientX: state.getCenterX(),
                            clientY: state.getCenterY(),
                            bubbles: true
                        });
                        const me = new mxMouseEvent(event, targetState);
                        const before = new Set(graph.getChildEdges(p));
                        const target = h.marker.getCell(me);
                        h.currentState = target ? graph.view.getState(target) : null;
                        check(target === expectedTarget, "319 target " + [ cell?.id, loops, dangling ]);
                        check(h.error === null === allowed, "319 validation " + [ cell?.id, loops, dangling ]);
                        h.mouseUp(graph, me);
                        const added = graph.getChildEdges(p).filter((e => !before.has(e)));
                        check(added.length === (allowed ? 1 : 0), "319 insertion " + [ cell?.id, loops, dangling ]);
                        if (added.length) graph.removeCells(added, true);
                        h.reset();
                    }
                    try {
                        attempt(a, false, true, false, null);
                        attempt(a, true, true, true, a);
                        attempt(b, false, true, true, b);
                        attempt(null, false, true, true, null);
                        attempt(null, false, false, false, null);
                        attempt(edge, false, true, true, null);
                        attempt(edge, false, false, false, null);
                        const nested = new mxCell('Nested edge', new mxGeometry(), 'connectable=0;');
                        nested.setEdge(true);
                        nested.geometry.relative = true;
                        nested.geometry.setTerminalPoint(new mxPoint(5, 5), true);
                        nested.geometry.setTerminalPoint(new mxPoint(40, 30), false);
                        graph.model.add(a, nested);
                        try { attempt(nested, false, true, false, null); }
                        finally { graph.removeCells([nested], true); }

                    } finally {
                        h.reset();
                        graph.allowLoops = oldLoops;
                        graph.allowDanglingEdges = oldDangling;
                    }
                }));
                test(320, (() => {
                    const geometry = edge.geometry.clone();
                    geometry.points = [ new mxPoint(170, 80) ];
                    graph.model.setGeometry(edge, geometry);
                    graph.view.validate();
                    const h = new mxEdgeHandler(graph.view.getState(edge));
                    const oldW = window.w, oldH = window.h;
                    try {
                        const inner = h.bends[1];
                        h.manageLabelHandle = false;
                        h.handleImage = null;
                        h.labelShape.visible = true;
                        const pt = h.abspoints[1];
                        h.labelShape.bounds = new mxRectangle(pt.x - 10, pt.y - 10, 20, 20);
                        window.w = "owned-w";
                        window.h = "owned-h";
                        h.redrawInnerBends();
                        check(window.w === "owned-w" && window.h === "owned-h", "320 bend sizing locals");
                    } finally {
                        if (oldW === undefined) delete window.w; else window.w = oldW;
                        if (oldH === undefined) delete window.h; else window.h = oldH;
                        h.destroy();
                    }
                }));
                test(321, (() => {
                    check(Editor.extractGraphModelFromPng(data.badPng) === null, "321 invalid percent returns no embedded diagram");
                    check(Editor.extractGraphModelFromPng(data.doubleBadPng) === null, "321 double encoded invalid percent returns no embedded diagram");
                    check(Editor.extractGraphModelFromPng(data.goodPng) === "<mxGraphModel><root/></mxGraphModel>", "321 valid PNG remains supported");
                }));
                test(322, (() => {
                    const old = window.detected;
                    window.detected = "owned-detected";
                    try {
                        const dialog = new PageSetupDialog(ownedUi);
                        check(window.detected === "owned-detected", "322 preset detection local");
                        dialog.container.remove();
                    } finally {
                        if (old === undefined) delete window.detected; else window.detected = old;
                    }
                }));
                test(323, (() => {
                    const editor = new Editor(false);
                    try {
                        for (const dims of [ [ "0", "600" ], [ "600", "-1" ], [ "Infinity", "600" ], [ "600", "Infinity" ] ]) {
                            editor.graph.pageFormat = new mxRectangle(0, 0, 827, 1169);
                            editor.readGraphState(mxUtils.parseXml('<mxGraphModel page="1" pageWidth="' + dims[0] + '" pageHeight="' + dims[1] + '"/>').documentElement);
                            check(Number.isFinite(editor.graph.pageFormat.width) && Number.isFinite(editor.graph.pageFormat.height) && editor.graph.pageFormat.width > 0 && editor.graph.pageFormat.height > 0, "323 imported dimensions " + dims);
                        }
                        editor.readGraphState(mxUtils.parseXml('<mxGraphModel pageWidth="900" pageHeight="700"/>').documentElement);
                        check(editor.graph.pageFormat.width === 900 && editor.graph.pageFormat.height === 700, "323 valid imported dimensions");
                    } finally {
                        editor.destroy();
                    }
                }));
                test(310, (() => {
                    const preview = PrintDialog.createPrintPreview(graph, 1, null, 0, 0, 0, true);
                    const label = document.createElement("div"), hint = document.createElement("div");
                    label.textContent = "Owned HTML label";
                    hint.className = "geHint extra";
                    check(preview.isTextLabel(label), "310 ordinary HTML label retained");
                    check(!preview.isTextLabel(hint), "310 hint class excluded");
                    graph.setCellStyles('html', '1', [a]);
                    graph.model.setValue(a, '<b>Owned HTML print label</b>');
                    const iframe = document.createElement('iframe');
                    document.body.append(iframe);
                    try {
                        preview.open(null, iframe.contentWindow, false, true);
                        check(iframe.contentDocument.body.textContent.includes('Owned HTML print label'), '310 HTML label rendered in actual print page');
                    } finally { iframe.remove(); }

                }));
                test(309, (() => {
                    const renderer = graph.cellRenderer, original = renderer.initControl, initialize = renderer.initializeOverlay;
                    const iframe = document.createElement("iframe");
                    document.body.append(iframe);
                    const preview = new mxPrintPreview(graph);
                    preview.printControls = true;
                    preview.printOverlays = true;
                    try {
                        preview.open(null, iframe.contentWindow, false, true);
                        check(renderer.initControl === original && renderer.initializeOverlay === initialize, "309 print overrides restored");
                        check(iframe.contentDocument.body.childNodes.length > 0, "309 actual preview page rendered");
                        details.push({
                            printPages: iframe.contentDocument.body.childNodes.length
                        });
                        const failedPreview = new mxPrintPreview(graph);
                        failedPreview.printControls = true;
                        failedPreview.printOverlays = true;
                        failedPreview.addGraphFragment = function () { throw Error('owned print failure'); };
                        const log = console.error;
                        let observed = false;
                        console.error = function (e) { if (e.message === 'owned print failure') observed = true; else log.apply(this, arguments); };
                        try {
                            failedPreview.open(null, iframe.contentWindow, false, true);
                            check(observed && renderer.initControl === original && renderer.initializeOverlay === initialize, '309 restores renderer after render failure');
                        } finally { console.error = log; }

                    } finally {
                        renderer.initControl = original;
                        renderer.initializeOverlay = initialize;
                        iframe.remove();
                    }
                }));
                test(317, (() => {
                    const originalAdd = mxEvent.addListener, originalRemove = mxEvent.removeListener, tracked = new Set;
                    mxEvent.addListener = function(target, event, fn) {
                        if (target === window && event === "resize") tracked.add(fn);
                        return originalAdd.apply(this, arguments);
                    };
                    mxEvent.removeListener = function(target, event, fn) {
                        if (target === window && event === "resize") tracked.delete(fn);
                        return originalRemove.apply(this, arguments);
                    };
                    let outline;
                    try {
                        outline = EditorUi.prototype.createOutline.call({
                            editor: {
                                graph: graph
                            }
                        }, null);
                        check(tracked.size === 1, "317 owns one resize handler");
                        outline.destroy();
                        check(tracked.size === 0, "317 resize handler released");
                    } finally {
                        if (outline?.source) outline.destroy();
                        for (const fn of tracked) originalRemove(window, "resize", fn);
                        mxEvent.addListener = originalAdd;
                        mxEvent.removeListener = originalRemove;
                    }
                }));
                // Format controls operate on the actual owned editor and its selection state.
                                host = document.createElement("div");
                host.style.cssText = "position:absolute;left:20px;top:60px;width:300px;background:white;z-index:999";
                document.body.append(host);
                const g = ownedEditor.graph, selection = g.getSelectionCells();
                let v, e;
                g.model.beginUpdate();
                try {
                    v = g.insertVertex(null, null, "Owned colors", 10, 10, 80, 40, "ownedColors=#ff0000,#0000ff;");
                    e = g.insertEdge(null, null, "", v, g.model.getCell("2"), "edgeStyle=none;");
                } finally {
                    g.model.endUpdate();
                }
                test(324, (() => {
                    g.setSelectionCell(v);
                    const panel = new BaseFormatPanel(ownedUi.format, ownedUi, host);
                    const div = panel.createArrayCellColorOption("Owned", "ownedColors", "#ffffff");
                    host.append(div);
                    const buttons = [ ...div.querySelectorAll(".geColorBtn") ];
                    check(buttons.length === 2, "324 creates two color controls");
                    g.setCellStyles("ownedColors", "#00ff00,#ffff00", [ v ]);
                    check(buttons.every((b => b.style.display !== "none")), "324 controls remain visible after model change");
                    panel.destroy();
                }));
                test(325, (() => {
                    host.replaceChildren();
                    g.setSelectionCell(e);
                    const panel = new StyleFormatPanel(ownedUi.format, ownedUi, host);
                    const inputs = [ ...host.querySelectorAll('input[title="' + mxResources.get("lineend") + '"]') ];
                    const input = inputs[1];
                    if (!input) throw Error("missing line-end spacing field");
                    input.focus();
                    input.value = "97";
                    g.model.setValue(v, "Unrelated change");
                    check(document.activeElement === input && input.value === "97", "325 pending focused end-spacing retained");
                    panel.destroy();
                }));
                g.removeCells([ v, e ], true);
                g.setSelectionCells(selection);
                const timerGraph = makeGraph(), timerDiv = timerGraph.container;
                let fired = 0;
                timerGraph.tapAndHoldValid = true;
                timerGraph.tapAndHoldInProgress = true;
                timerGraph.tapAndHoldThread = setTimeout((() => {
                    if (timerGraph.tapAndHoldValid) fired++;
                }), 25);
                mxGraph.prototype.destroy.call(timerGraph);
                await new Promise((r => setTimeout(r, 60)));
                check(fired === 0 && timerGraph.tapAndHoldThread == null && !timerGraph.tapAndHoldValid && !timerGraph.tapAndHoldInProgress, "316 tap-and-hold canceled on destroy");
                timerGraph.destroy();
                timerDiv.remove();
            } finally {
                host?.remove();
                if (graph) {
                    const div = graph.container;
                    graph.destroy();
                    div?.remove();
                }
                for (const restore of saved.reverse()) restore();
            }
            return {
                failures: failures,
                details: details
            };
        }), data);
        fs.writeFileSync(path.join(out, theme + "-editing-" + copy + ".json"), JSON.stringify(result, null, 2));
        if (!process.env.AIDE_EDIT_REVIEW_CATALOG) assert.deepEqual(result.failures, [], copy);
        checks.push({
            case: "editing " + copy,
            theme: theme,
            result: result.failures.length ? "EXPECTED_FAIL" : "PASS"
        });
    }
    return checks;
};
