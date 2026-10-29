"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict");

module.exports = async (frame, out, theme) => {
    if (!await frame.evaluate((() => typeof RPOrgChart !== "undefined"))) await frame.addScriptTag({
        url: "https://aide-drawio-owned.test/vendor/drawio/js/orgchart.min.js"
    });
    const checks = [];
    for (const issue of [ 419, 421, 422 ]) {
        const result = await frame.evaluate((issue => {
            const check = (b, m) => {
                if (!b) throw Error(m);
            };
            let graph, container;
            try {
                if (issue === 419) {
                    let rejected = false;
                    try {
                        OrgChart.Layout.LayoutAlgorithm.AssertBoxSize({
                            Id: 1,
                            Size: {
                                Width: 80,
                                Height: 1000000001
                            }
                        });
                    } catch (e) {
                        rejected = true;
                    }
                    check(rejected, "reject oversized height");
                    for (const size of [ {
                        Width: 0,
                        Height: 0
                    }, {
                        Width: 80,
                        Height: 40
                    }, {
                        Width: 1e9,
                        Height: 1e9
                    } ]) OrgChart.Layout.LayoutAlgorithm.AssertBoxSize({
                        Id: 1,
                        Size: size
                    });
                    return {
                        issue: issue,
                        result: "PASS"
                    };
                }
                container = document.createElement("div");
                container.style.cssText = "position:fixed;top:100px;left:0;width:80vw;height:700px;background:white";
                document.body.append(container);
                graph = new Graph(container);
                const parent = graph.getDefaultParent();
                function create(count, chain) {
                    graph.model.beginUpdate();
                    try {
                        const root = graph.insertVertex(parent, null, "Root", 0, 0, 80, 40);
                        let prev = root;
                        for (let i = 0; i < count; i++) {
                            const child = graph.insertVertex(parent, null, "Child " + i, 100, 60 + i * 50, 80, 40);
                            graph.insertEdge(parent, null, "", chain ? prev : root, child);
                            prev = child;
                        }
                    } finally {
                        graph.model.endUpdate();
                    }
                }
                if (issue === 421) {
                    create(300, true);
                    const proto = OrgChart.Layout.BoxTree.Node.prototype, old = proto.IterateParentFirst;
                    let depth = 0, peak = 0;
                    proto.IterateParentFirst = function() {
                        if (++depth > 256) {
                            depth--;
                            throw Error("owned recursive traversal trap");
                        }
                        peak = Math.max(peak, depth);
                        try {
                            return old.apply(this, arguments);
                        } finally {
                            depth--;
                        }
                    };
                    let error;
                    try {
                        new mxOrgChartLayout(graph, 0, 20, 20).execute(parent);
                    } catch (e) {
                        error = e;
                    } finally {
                        proto.IterateParentFirst = old;
                    }
                    check(error && /Org Chart.*128/i.test(error.message), "explicit preflight before recursive traversal: " + error?.message);
                    check(peak <= 128, "tree rejected before unsafe recursion");
                    check(graph.model.updateLevel === 0, "failed layout transaction closed");
                    return {
                        issue: issue,
                        result: "PASS",
                        recursivePeak: peak
                    };
                }
                const layouts = [];
                for (const [mode, count] of [ [ 1, 3 ], [ 1, 7 ], [ 2, 5 ], [ 2, 9 ], [ 0, 3 ], [ 5, 3 ], [ 6, 3 ] ]) {
                    graph.removeCells(graph.getChildCells(parent), true);
                    create(count, false);
                    new mxOrgChartLayout(graph, mode, 20, 20).execute(parent);
                    const boxes = graph.getChildVertices(parent).map((c => c.geometry));
                    check(boxes.every((g => [ g.x, g.y, g.width, g.height ].every(Number.isFinite))), "finite positions");
                    for (let i = 0; i < boxes.length; i++) for (let j = i + 1; j < boxes.length; j++) {
                        const a = boxes[i], b = boxes[j];
                        check(a.x + a.width <= b.x + .001 || b.x + b.width <= a.x + .001 || a.y + a.height <= b.y + .001 || b.y + b.height <= a.y + .001, "layout boxes do not overlap");
                    }
                    check(graph.getChildEdges(parent).length === count, "all connectors preserved");
                    let selected = [];
                    RPOrgChart.diagram.getVisualTree().IterateParentFirst((n => {
                        if (n.getElement().getIsDataBound()) selected.push({
                            strategy: n.getState().LayoutStrategyId,
                            name: n.getState().RequireLayoutStrategy().$$name,
                            children: n.getChildCount(),
                            number: n.getState().NumberOfSiblings
                        });
                        return true;
                    }));
                    layouts.push({
                        mode: mode,
                        count: count,
                        selected: selected
                    });
                }
                return {
                    issue: issue,
                    result: "PASS",
                    layouts: layouts
                };
            } catch (e) {
                return {
                    issue: issue,
                    result: "FAIL",
                    error: e.message,
                    stack: e.stack
                };
            } finally {
                if (graph) graph.destroy();
                container?.remove();
            }
        }), issue);
        checks.push({
            ...result,
            theme: theme
        });
    }
    if (!process.env.AIDE_EXPECT_REVIEW_FAILURES) {
        await frame.addScriptTag({
            url: "https://aide-drawio-owned.test/vendor/drawio/js/libavoid-js/libavoid.min.js"
        });
        assert.equal(await frame.evaluate((async () => {
            const ready = await __libavoidReady;
            if (!ready || typeof ready.Router !== "function" || Avoid !== ready) return false;
            const router = new Avoid.Router(Avoid.OrthogonalRouting);
            router.delete();
            return true;
        })), true);
        checks.push({
            issue: 418,
            case: "actual asynchronous libavoid bootstrap and Router",
            theme: theme,
            result: "PASS"
        });
    }
    fs.writeFileSync(path.join(out, theme + "-orgchart-review.json"), JSON.stringify(checks, null, 2));
    if (!process.env.AIDE_EXPECT_REVIEW_FAILURES) assert.deepEqual(checks.filter((r => r.result !== "PASS")), []);
    return checks;
};
