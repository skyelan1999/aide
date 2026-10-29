"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), review = require("./drawio-traversal-review.cjs");

module.exports = async (frame, out, theme) => {
    const checks = [];
    for (const copy of [ null, "mxgraph/mxClient.js", "js/app.min.js", "js/integrate.min.js", "js/viewer.min.js", "js/viewer-static.min.js" ]) {
        const results = await frame.evaluate((data => {
            const saved = [], results = [];
            for (const row of data) {
                const old = Function("return " + row.name)();
                saved.push((() => Function("old", row.name + "=old")(old)));
                Function(row.code)();
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
            function graph(n) {
                const g = new Graph(document.createElement("div"));
                g.view.rendering = false;
                const parent = g.getDefaultParent(), nodes = [];
                g.model.beginUpdate();
                try {
                    for (let i = 0; i < n; i++) {
                        nodes.push(g.insertVertex(parent, null, "n" + i, i * 5, 0, 20, 20));
                        if (i) g.insertEdge(parent, null, "", nodes[i - 1], nodes[i]);
                    }
                } finally {
                    g.model.endUpdate();
                }
                return {
                    g: g,
                    parent: parent,
                    nodes: nodes
                };
            }
            try {
                test(400, (() => {
                    const {g: g, nodes: nodes} = graph(3e3);
                    try {
                        let visited = 0;
                        g.traverse(nodes[0], true, (() => {
                            visited++;
                        }));
                        if (visited !== 3e3) throw Error("actual model traversal incomplete");
                        let reverse = 0;
                        g.traverse(nodes.at(-1), true, (() => {
                            reverse++;
                        }), null, null, true);
                        if (reverse !== 3e3) throw Error("inverse traversal incomplete");
                    } finally {
                        g.destroy();
                    }
                }));
                test(399, (() => {
                    for (const size of [ 5, 320 ]) {
                        const {g: g, parent: parent, nodes: nodes} = graph(size);
                        try {
                            let error;
                            try {
                                new mxHierarchicalLayout(g).execute(parent);
                            } catch (e) {
                                error = e;
                            }
                            if (size === 320) {
                                if (!error || !/hierarchical.*256/i.test(error.message)) throw Error("deep path needs explicit layout budget error:" + error?.message);
                            } else if (error) throw error;
                            if (g.model.updateLevel !== 0) throw Error("layout update not closed");
                            if (nodes.some((c => ![ c.geometry.x, c.geometry.y ].every(Number.isFinite)))) throw Error("geometry became nonfinite");
                        } finally {
                            g.destroy();
                        }
                    }
                }));
            } finally {
                saved.reverse().forEach((f => f()));
            }
            return results;
        }), review.snippets(copy));
        fs.writeFileSync(path.join(out, theme + "-traversal-" + (copy || "source").replaceAll("/", "-") + ".json"), JSON.stringify(results, null, 2));
        assert.deepEqual(results.filter((x => x.result !== "PASS")), []);
        checks.push(...results.map((x => ({
            ...x,
            case: "actual graph " + (copy || "source"),
            theme: theme
        }))));
    }
    const adaptor = fs.readFileSync(review.base + "plugins/webcola/mxWebColaAdaptor.js", "utf8");
    const webcola = await frame.evaluate((adaptor => {
        Function(adaptor + "\nwindow.OwnedResourceCola=mxWebColaAdaptor;")();
        const C = OwnedResourceCola, results = [];
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
        test(405, (() => {
            const g = new Graph(document.createElement("div"));
            g.view.rendering = false;
            const p = g.getDefaultParent();
            g.model.beginUpdate();
            try {
                for (let i = 0; i < 1001; i++) g.insertVertex(p, null, "", i, 0, 10, 10);
            } finally {
                g.model.endUpdate();
            }
            try {
                let error;
                try {
                    new C(g);
                } catch (e) {
                    error = e;
                }
                if (!error || !/WebCola.*1000/i.test(error.message)) throw Error("actual visible node budget missing:" + error?.message);
            } finally {
                g.destroy();
            }
        }));
        test(407, (() => {
            const g = new Graph(document.createElement("div"));
            g.view.rendering = false;
            const p = g.getDefaultParent();
            let outer, inner;
            g.model.beginUpdate();
            try {
                outer = g.insertVertex(p, "owned-outer", "outer", 0, 0, 100, 100);
                inner = g.insertVertex(outer, "owned-inner", "inner", 0, 0, 80, 80);
                const a = g.insertVertex(inner, "owned-a", "a", 0, 0, 20, 20), b = g.insertVertex(outer, "owned-b", "b", 50, 50, 20, 20);
                g.insertEdge(p, null, "", a, b);
            } finally {
                g.model.endUpdate();
            }
            try {
                const a = new C(g), result = a.graphToLayout(g);
                if (result.nodes.length !== 2 || result.links.length !== 1 || result.groups.length !== 2 || !result.groups.some((g => g.groups?.length === 1))) throw Error("nested group mapping lost");
            } finally {
                g.destroy();
            }
        }));
        return results;
    }), adaptor);
    fs.writeFileSync(path.join(out, theme + "-webcola-budget-groups.json"), JSON.stringify(webcola, null, 2));
    assert.deepEqual(webcola.filter((x => x.result !== "PASS")), []);
    checks.push(...webcola.map((x => ({
        ...x,
        case: "actual WebCola graph conversion",
        theme: theme
    }))));
    // Reuse the parent component page for the real file preview renderer.
        const page = frame.page(), app = fs.readFileSync("internal/server/web/app.js", "utf8"), source = app.slice(app.indexOf("const DXF_SVG_NS ="), app.indexOf("/* ── docx-preview 渲染器"));
    await page.addScriptTag({
        content: fs.readFileSync("internal/server/web/preview-safety.js", "utf8")
    });
    await page.addScriptTag({
        content: fs.readFileSync("internal/server/web/vendor/dxf-parser/dxf-parser.js", "utf8")
    });
    const fixtures = [ ...[ "LAYER", "VPORT" ].map((kind => [ 0, "SECTION", 2, "TABLES", 0, "TABLE", 2, kind, 70, 1, 0, kind, 2, "owned", 0, "BOGUS", 0, "ENDTAB", 0, "ENDSEC", 0, "EOF" ].join("\n") + "\n")), [ 0, "SECTION", 2, "ENTITIES", 0, "POLYLINE", 8, "owned", 70, 0, 0, "BOGUS", 0, "SEQEND", 0, "ENDSEC", 0, "EOF" ].join("\n") + "\n", [ 0, "SECTION", 2, "ENTITIES", 0, "LINE", 8, "owned", 10, 0, 20, 0, 11, 10, 21, 20, 0, "ENDSEC", 0, "EOF" ].join("\n") + "\n" ];
    let payload = "";
    const route = route => route.fulfill({
        contentType: "text/plain",
        body: payload
    });
    await page.route("**/owned-resource.dxf", route);
    try {
        for (let i = 0; i < fixtures.length; i++) {
            payload = fixtures[i];
            const result = await page.evaluate((async ({source: source, invalid: invalid}) => {
                const host = document.createElement("div");
                host.style.height = "200px";
                document.body.append(host);
                const callbacks = [];
                const el = (tag, cls, text) => {
                    const e = document.createElement(tag);
                    if (cls) e.className = cls;
                    if (text) e.textContent = text;
                    return e;
                }, escape = x => {
                    const e = document.createElement("span");
                    e.textContent = x;
                    return e.innerHTML;
                };
                try {
                    const setup = Function("el", "t", "fileRawUrl", "ensureDxfParser", "bindViewerTeardown", "escapeHtml", source + "\nreturn setupDxfPreview;")(el, (x => x), (() => "/owned-resource.dxf"), (async () => {}), ((_host, f) => callbacks.push(f)), escape);
                    await setup(host, "owned.dxf", "workspace", "");
                    return {
                        error: !!host.querySelector(".dxf-error"),
                        svg: !!host.querySelector("svg"),
                        text: host.textContent.slice(0, 300)
                    };
                } finally {
                    callbacks.forEach((f => f()));
                    host.remove();
                }
            }), {
                source: source,
                invalid: i < 3
            });
            assert.equal(result.error, i < 3);
            assert.equal(result.svg, i === 3);
            checks.push({
                issue: 398,
                case: "actual DXF preview " + i,
                theme: theme,
                result: "PASS"
            });
        }
    } finally {
        await page.unroute("**/owned-resource.dxf", route);
    }
    return checks;
};
