"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), r = require("./drawio-device-shape-review.cjs"), {assignment: assignment, atom: atom} = require("./drawio-editing-review.cjs");

module.exports = async (frame, out, theme) => {
    const checks = [], rack = fs.readFileSync(r.base + "plugins/rackF5.js", "utf8"), webcola = fs.readFileSync(r.base + "plugins/webcola/mxWebColaAdaptor.js", "utf8"), trees = fs.readFileSync(r.base + "plugins/trees/trees.js", "utf8");
    const treeHelper = atom(trees, trees.indexOf("function isTreeCell"));
    await frame.evaluate((({rack: rack, webcola: webcola}) => {
        const old = Draw.loadPlugin;
        Draw.loadPlugin = () => {};
        try {
            Function(rack + "\nwindow.mxRackF5BigIp1600=mxRackF5BigIp1600;")();
            Function(webcola + "\nwindow.OwnedCola=mxWebColaAdaptor;")();
        } finally {
            Draw.loadPlugin = old;
        }
    }), {
        rack: rack,
        webcola: webcola
    });
    for (const copy of [ "source", "shapes-14-6-5.min.js", "integrate.min.js", "viewer-static.min.js" ]) {
        const data = r.snippets(copy);
        data.push({
            issue: 382,
            name: "mxRackF5BigIp1600.prototype.paintVertexShape",
            code: assignment(rack, "mxRackF5BigIp1600.prototype.paintVertexShape")
        });
        const results = await frame.evaluate((({data: data, tree: tree, treeHelper: treeHelper}) => {
            const results = [], saved = [];
            for (const row of data) {
                const old = Function("return " + row.name)();
                saved.push((() => Function("old", row.name + "=old")(old)));
                Function(row.code)();
            }
            function canvas(limit = 2e4) {
                const calls = [];
                return new Proxy({
                    calls: calls
                }, {
                    get(t, k) {
                        if (k === "calls") return calls;
                        return (...args) => {
                            if (calls.length >= limit) throw Error("owned draw budget exceeded");
                            if ([ "moveTo", "lineTo", "roundrect", "rect", "ellipse" ].includes(k) && args.some((n => typeof n === "number" && !Number.isFinite(n)))) throw Error("nonfinite " + k);
                            calls.push([ k, ...args ]);
                        };
                    }
                });
            }
            function test(issue, fn) {
                try {
                    fn();
                    results.push({
                        issue: issue,
                        result: "PASS"
                    });
                } catch (e) {
                    results.push({
                        issue: issue,
                        result: "FAIL",
                        error: e.message
                    });
                }
            }
            function check(v, msg) {
                if (!v) throw Error(msg);
            }
            try {
                test(382, (() => {
                    const s = new mxRackF5BigIp1600;
                    s.style = {
                        hasEars: "0",
                        isFront: "0",
                        isDC: "0",
                        psNum: "2"
                    };
                    const c = canvas();
                    s.paintVertexShape(c, 0, 0, 200, 50);
                    const images = c.calls.filter((x => x[0] === "image")).map((x => x[5]));
                    check(images.some((x => x.endsWith("_back.png"))), "rear template must draw back");
                    check(!images.some((x => x.includes("_ear") || x.includes("_dc"))), "zero flags disabled");
                    s.style = {
                        hasEars: "1",
                        isFront: "1"
                    };
                    const c2 = canvas();
                    s.paintVertexShape(c2, 0, 0, 200, 50);
                    check(c2.calls.some((x => x[0] === "image" && x[5].endsWith("_front.png"))), "front retained");
                }));
                test(385, (() => {
                    for (const dx2 of [ "0", "0.00000001", "75" ]) {
                        const s = new mxShapeIOS7SelectBar;
                        s.style = {
                            dx2: dx2
                        };
                        s.paintVertexShape(canvas(1e4), 0, 0, 300, 40);
                    }
                }));
                test(386, (() => {
                    const style = {
                        barPos: "50"
                    }, state = {
                        style: style,
                        shape: new mxShapeMockupScrollBar,
                        view: ownedEditor.graph.view,
                        cell: ownedEditor.graph.model.getCell("2")
                    };
                    const handles = Graph.handleFactory[mxShapeMockupScrollBar.prototype.cst.SHAPE_SCROLL_BAR](state);
                    for (const width of [ 100, 80, 200 ]) {
                        handles[0].setPosition(new mxRectangle(0, 0, width, 20), new mxPoint(50, 15));
                        check(Number.isFinite(style.barPos), "drag stores finite barPos");
                        const point = handles[0].getPosition(new mxRectangle(0, 0, width, 20));
                        check(Number.isFinite(point.x) && Number.isFinite(point.y), "finite handle");
                    }
                    handles.forEach((h => h.destroy()));
                }));
                test(387, (() => {
                    function painted(type) {
                        const s = new mxArchiMateApplication;
                        s.style = {
                            appType: type
                        };
                        const c = canvas();
                        s.paintVertexShape(c, 0, 0, 80, 80);
                        return JSON.stringify(c.calls);
                    }
                    check(painted("path") === painted("commPath"), "Path renders existing communication icon");
                    check(painted("path") !== painted("comp"), "path decoration exists");
                }));
                test(388, (() => {
                    for (const cls of [ mxShapeEipMessageChannel, mxShapeEipDatatypeChannel, mxShapeEipDeadLetterChannel, mxShapeEipInvalidMessageChannel ]) {
                        const s = new cls;
                        s.style = {};
                        const old = window.mxConnectionConstraint;
                        let count = 0;
                        window.mxConnectionConstraint = function(...args) {
                            if (++count > 11e3) throw Error("owned constraint budget exceeded");
                            return new old(...args);
                        };
                        try {
                            const p = s.getConstraints({}, 1e12, 40);
                            check(p.length <= 1e4, "bounded constraints");
                            check(p.length > 2, "normal anchors retained");
                        } finally {
                            window.mxConnectionConstraint = old;
                        }
                        check(s.getConstraints({}, 40, 40).length === 8, "ordinary spacing retained");
                    }
                }));
                test(389, (() => {
                    for (const stepSize of [ "0", "1000", "0.00001", "25" ]) {
                        const s = new mxShapeArrowsWedgeArrowDashed2;
                        s.style = {
                            stepSize: stepSize
                        };
                        const c = canvas();
                        s.paintEdgeShape(c, [ new mxPoint(0, 0), new mxPoint(100, 0) ]);
                        check(c.calls.some((x => x[0] === "lineTo")), "nonzero arrow remains visible");
                    }
                    const s = new mxShapeArrowsWedgeArrowDashed2;
                    s.style = {};
                    s.paintEdgeShape(canvas(), [ new mxPoint(0, 0), new mxPoint(0, 0) ]);
                }));
                test(390, (() => {
                    const s = new mxShapeArrows2CalloutDouble90Arrow;
                    s.style = {
                        dy1: "5",
                        dx1: "10",
                        dx2: "10",
                        dy2: "120",
                        arrowHead: "5"
                    };
                    const c = canvas();
                    s.paintVertexShape(c, 0, 0, 40, 200);
                    check(c.calls.some((x => x[0] === "lineTo" && x[2] === 120)), "portrait height remains120");
                    const p = s.getConstraints({}, 40, 200);
                    check(p.some((x => x.dy === 120)), "constraints follow callout height");
                }));
            } finally {
                saved.reverse().forEach((f => f()));
            }
            return results;
        }), {
            data: data,
            tree: assignment(trees, "graph.foldCells"),
            treeHelper: treeHelper
        });
        fs.writeFileSync(path.join(out, theme + "-device-shapes-" + copy + ".json"), JSON.stringify(results, null, 2));
        checks.push(...results.map((x => ({
            ...x,
            case: "device-shapes " + copy,
            theme: theme
        }))));
    }
    // Plugins are standalone assets, tested once per theme with actual mxGraph cells.
        const pluginResults = await frame.evaluate((({tree: tree, treeHelper: treeHelper}) => {
        const results = [];
        function test(issue, fn) {
            try {
                fn();
                results.push({
                    issue: issue,
                    result: "PASS"
                });
            } catch (e) {
                results.push({
                    issue: issue,
                    result: "FAIL",
                    error: e.message
                });
            }
        }
        const graph = new Graph(document.createElement("div")), parent = graph.getDefaultParent();
        try {
            test(383, (() => {
                const value = mxUtils.createXmlDocument().createElement("node");
                value.setAttribute("treeRoot", "1");
                const a = graph.insertVertex(parent, null, value, 0, 0, 60, 30, "collapsible=1;"), b = graph.insertVertex(parent, null, "child", 100, 0, 60, 30);
                graph.insertEdge(parent, null, "", a, b);
                Function("graph", "var graphFoldCells=graph.foldCells;" + treeHelper + ";" + tree)(graph);
                const cells = [ a ];
                graph.foldCells(true, false, cells);
                if (cells.length !== 1 || graph.model.isVisible(b) || !graph.model.isCollapsed(a)) throw Error("fold changed input or left child visible");
                graph.setSelectionCell(a);
                graph.foldCells(false, false, null);
                if (!graph.model.isVisible(b)) throw Error("null selection expansion failed");
            }));
            test(384, (() => {
                graph.model.clear();
                const parent = graph.getDefaultParent(), a = graph.insertVertex(parent, "__proto__", "proto", 0, 0, 60, 30), b = graph.insertVertex(parent, "constructor", "ctor", 100, 0, 60, 30);
                graph.insertEdge(parent, null, "", a, b);
                graph.view.validate();
                const adaptor = Object.create(OwnedCola.prototype);
                adaptor.graph = graph;
                adaptor.vertexDegrees = new mxDictionary;
                adaptor.groupDegrees = new mxDictionary;
                adaptor.computeVertexDegrees();
                const result = adaptor.graphToLayout(graph);
                if (result.nodes.length !== 2 || result.links.length !== 1 || !Number.isInteger(result.cellToNode.__proto__)) throw Error("special IDs must map to actual nodes and edge");
            }));
        } finally {
            graph.destroy();
        }
        return results;
    }), {
        tree: assignment(trees, "graph.foldCells"),
        treeHelper: treeHelper
    });
    fs.writeFileSync(path.join(out, theme + "-device-plugins.json"), JSON.stringify(pluginResults, null, 2));
    checks.push(...pluginResults.map((x => ({
        ...x,
        case: "device-plugins",
        theme: theme
    }))));
    if (!process.env.AIDE_EXPECT_REVIEW_FAILURES) assert.deepEqual(checks.filter((x => x.result !== "PASS")), []);
    return checks;
};
