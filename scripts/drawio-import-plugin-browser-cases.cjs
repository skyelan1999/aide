"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), review = require("./drawio-import-plugin-review.cjs");

module.exports = async function(frame, out, theme) {
    if (!await frame.evaluate((() => typeof RPOrgChart !== "undefined"))) await frame.addScriptTag({
        url: "https://aide-drawio-owned.test/vendor/drawio/js/orgchart.min.js"
    });
    if (!await frame.evaluate((() => typeof JSZip !== "undefined"))) await frame.addScriptTag({
        url: "https://aide-drawio-owned.test/vendor/drawio/js/jszip/jszip.min.js"
    });
    const Zip = require("../internal/server/web/vendor/drawio/js/jszip/jszip.min.js");
    const good = new Zip;
    good.file("diagram/diagram.xml", "<mxfile><diagram>Owned ZIP</diagram></mxfile>");
    const bomb = new Zip;
    bomb.file("diagram/diagram.xml", "<mxfile>" + "x".repeat(9 * 1024 * 1024) + "</mxfile>");
    const gliffy = new Zip;
    gliffy.file("versions/1", '{"owned":1}');
    gliffy.file("versions/2", '{"owned":2}');
    const fixtures = {
        good: await good.generateAsync({
            type: "base64",
            compression: "DEFLATE"
        }),
        bomb: await bomb.generateAsync({
            type: "base64",
            compression: "DEFLATE"
        }),
        gliffy: await gliffy.generateAsync({
            type: "base64",
            compression: "DEFLATE"
        })
    };
    const plugins = Object.fromEntries([ "update", "sql", "replay" ].map((n => [ n, fs.readFileSync(review.base + "plugins/" + n + ".js", "utf8") ])));
    const checks = [];
    for (const copy of [ "source", "app.min.js", "integrate.min.js" ]) {
        const data = {
            methods: review.snippets(copy),
            plugins: plugins,
            fixtures: fixtures,
            baseline: !!process.env.AIDE_IMPORT_PLUGIN_REVIEW_CATALOG
        };
        const result = await frame.evaluate((async data => {
            const ui = window.ownedUi, editor = window.ownedEditor, failures = [], details = [], saved = [];
            const check = (ok, msg) => {
                if (!ok) failures.push(msg);
            };
            const keep = (o, k) => {
                const old = o[k];
                saved.push((() => o[k] = old));
            };
            async function test(n, fn) {
                try {
                    await fn();
                } catch (e) {
                    failures.push(n + " exception " + e.message);
                }
            }
            for (const {name: name, code: code} of data.methods) {
                const dot = name.lastIndexOf("."), o = Function("return " + name.slice(0, dot))(), k = name.slice(dot + 1);
                keep(o, k);
                Function(code)();
            }
            const div = document.createElement("div");
            div.style.cssText = "position:absolute;left:20px;top:70px;width:600px;height:450px";
            document.body.append(div);
            const graph = new Graph(div), p = graph.getDefaultParent();
            const a = graph.insertVertex(p, null, "Owned source", 20, 20, 80, 40), b = graph.insertVertex(p, null, "Owned target", 250, 130, 80, 40);
            graph.insertEdge(p, null, "", a, b);
            const canvas = limit => {
                let calls = 0;
                return new Proxy({
                    state: {
                        strokeColor: "#000000",
                        fillColor: "#ffffff",
                        alpha: 1
                    }
                }, {
                    get: (o, k) => k === "calls" ? calls : k in o ? o[k] : (...args) => {
                        if (++calls > limit) throw Error("owned canvas limit");
                        for (const v of args) if (typeof v === "number" && !Number.isFinite(v)) throw Error("nonfinite drawing coordinate");
                    }
                });
            };
            try {
                await test(363, (() => {
                    for (const [encoded, valid] of [ [ "%3Cmxfile%3E%ZZ%3C%2Fmxfile%3E", false ], [ encodeURIComponent("<mxfile><diagram>Owned PDF</diagram></mxfile>"), true ] ]) {
                        const pdf = "data:application/pdf;base64," + btoa("%PDF-1.3\n1 0 obj\n<< /Subject (" + encoded + ") >>\nendobj\n%%EOF");
                        const xml = Editor.extractGraphModelFromPdf(pdf);
                        check(valid ? xml === "<mxfile><diagram>Owned PDF</diagram></mxfile>" : xml === null, "363 malformed and valid PDF metadata");
                    }
                }));
                await test(366, (() => {
                    let submitted;
                    const dialog = {};
                    PrintDialog.prototype.create.call(dialog, ui, "Owned print", ((preview, args) => submitted = args), "export");
                    const host = dialog.container;
                    document.body.append(host);
                    try {
                        const from = host.querySelector("#gePrintDlgPages").parentElement.querySelectorAll("input[type=number]")[0], to = host.querySelector("#gePrintDlgPages").parentElement.querySelectorAll("input[type=number]")[1];
                        for (const values of [ [ "", "" ], [ "2", "" ], [ "", "2" ], [ "999", "-5" ] ]) {
                            from.value = values[0];
                            to.value = values[1];
                            from.dispatchEvent(new Event("change"));
                            check(Number.isInteger(Number(from.value)) && Number(from.value) >= 1 && Number(to.value) <= ui.pages.length && Number(from.value) <= Number(to.value), "366 page range normalized " + values);
                        }
                        from.value = "";
                        to.value = "";
                        host.querySelector(".gePrimaryBtn").click();
                        check(Number.isFinite(submitted?.pagesFrom) && Number.isFinite(submitted?.pagesTo), "366 submit validates without change event");
                    } finally {
                        host.remove();
                    }
                }));
                await test(367, (async () => {
                    function blob(value) {
                        return new File([ Uint8Array.from(atob(value), (c => c.charCodeAt(0))) ], "owned.zip", {
                            type: "application/zip"
                        });
                    }
                    const fake = Object.create(ui);
                    fake.isGliffyData = () => true;
                    fake.importGliffy = (data, success) => success(data);
                    async function run(file) {
                        return await new Promise((resolve => {
                            let called = 0;
                            fake.importZipFile(file, (value => resolve({
                                value: value,
                                calls: ++called
                            })), (error => resolve({
                                error: error?.code || "error",
                                calls: ++called
                            })));
                        }));
                    }
                    check((await run(blob(data.fixtures.good))).value?.includes("Owned ZIP"), "367 valid drawio ZIP");
                    check((await run(blob(data.fixtures.gliffy))).value === '{"owned":2}', "367 latest Gliffy version");
                    check((await run(blob(data.fixtures.bomb))).error === "AIDE_DRAWIO_LIMIT", "367 expansion limit");
                    check((await run({
                        size: 33 * 1024 * 1024
                    })).error === "AIDE_DRAWIO_LIMIT", "367 compressed limit");
                    if (!data.baseline) {
                        keep(JSZip, "loadAsync");
                        const load = JSZip.loadAsync;
                        let started = 0;
                        JSZip.loadAsync = async file => {
                            const zip = await load(file);
                            zip.forEach(((path, entry) => {
                                if (!entry.dir) {
                                    entry._data.uncompressedSize = 1;
                                    const stream = entry.internalStream;
                                    entry.internalStream = function() {
                                        started++;
                                        return stream.apply(this, arguments);
                                    };
                                }
                            }));
                            return zip;
                        };
                        const forged = await run(blob(data.fixtures.bomb));
                        check(started === 1 && forged.error === "AIDE_DRAWIO_LIMIT", "367 dishonest metadata stopped while streaming");
                        JSZip.loadAsync = load;
                    }
                }));
                await test(368, (() => {
                    for (const C of [ mxConnector, mxArrowConnector ]) for (const points of [ [ [ 10, 20 ], [ 10, 20 ], [ 130, 20 ] ], [ [ 10, 20 ], [ 130, 20 ], [ 130, 20 ] ], [ [ 10, 20 ], [ 10, 20 ], [ 80, 90 ], [ 130, 20 ], [ 130, 20 ] ], [ [ 10, 20 ], [ 10, 20 ] ] ]) {
                        const pts = points.map((p => new mxPoint(...p))), svg = document.createElementNS(mxConstants.NS_SVG, "svg"), g = document.createElementNS(mxConstants.NS_SVG, "g");
                        svg.append(g);
                        document.body.append(svg);
                        const c = new mxSvgCanvas2D(g), shape = C === mxArrowConnector ? new C(pts, "#000000", "#000000", 1) : new C(pts, "#000000", 1);
                        shape.style = {
                            startArrow: "classic",
                            endArrow: "classic"
                        };
                        shape.configureCanvas(c, 0, 0, 130, 100);
                        try {
                            shape.paintEdgeShape(c, pts);
                            check(pts.every((p => Number.isFinite(p.x) && Number.isFinite(p.y))), "368 finite terminal points " + C.name);
                            check(!/NaN|Infinity/.test(svg.outerHTML), "368 finite SVG " + C.name);
                            if (points.some((p => p[0] !== points[0][0] || p[1] !== points[0][1]))) check(g.childNodes.length > 0, "368 nonzero edge remains visible " + C.name);
                        } finally {
                            svg.remove();
                        }
                    }
                }));
                await test(371, (() => {
                    for (const style of [ {
                        symbol0: "mxgraph.sysml.composite"
                    }, {
                        symbol1: "mxgraph.sysml.composite",
                        symbol2: "rectangle",
                        symbol2Width: 20,
                        symbol2Height: 20
                    }, {
                        symbol0: "rectangle"
                    } ]) {
                        const shape = new mxShapeSysMLComposite;
                        shape.style = style;
                        const c = canvas(1500);
                        shape.paintForeground(c, 0, 0, 100, 80);
                        check(c.calls < 1500, "371 bounded composite " + Object.keys(style)[0]);
                    }
                }));
                await test(372, (() => {
                    for (const key of [ "__proto__", "constructor", "toString" ]) {
                        for (const field of [ "background", "outline", "symbol" ]) {
                            const shape = new mxBpmnShape;
                            shape.style = {
                                [field]: key
                            };
                            for (const layer of [ "background", "outline", "symbol" ]) shape.redrawPath(canvas(1e3), 0, 0, 100, 80, layer);
                        }
                        const event = new mxShapeBpmnEvent;
                        event.style = {
                            outline: key,
                            symbol: key
                        };
                        event.strictDrawShape(canvas(1e3), 0, 0, 60, 60);
                        check(true, "372 inherited painter rejected " + key);
                    }
                    const shape = new mxBpmnShape;
                    shape.style = {
                        background: "none",
                        outline: "standard",
                        symbol: "message"
                    };
                    shape.paintVertexShape(canvas(3e3), 0, 0, 100, 80);
                }));
                await test(374, (() => {
                    const shape = new mxShapeEmoji;
                    for (const key of [ "__proto__", "constructor", "toString" ]) {
                        const text = shape.getUnicodeText(key);
                        check(text == null || typeof text === "string", "374 unknown emoji is never object/function");
                    }
                    const known = Object.keys(shape.emojis)[0];
                    check(typeof shape.getUnicodeText(known) === "string" && shape.getUnicodeText(known).length > 0, "374 known emoji retained");
                    const cell = graph.insertVertex(p, null, "", 50, 240, 50, 50, "shape=mxgraph.emoji;emoji=__proto__;");
                    check(!!graph.view.getState(cell)?.shape?.node?.isConnected, "374 actual invalid emoji renders safely");
                    graph.removeCells([ cell ], true);
                }));
                await test(375, (() => {
                    for (const count of [ 3, 34 ]) {
                        const host = document.createElement("div");
                        document.body.append(host);
                        const g = new Graph(host);
                        let root;
                        g.model.beginUpdate();
                        try {
                            root = g.insertVertex(null, null, "Root", 0, 0, 80, 40);
                            for (let i = 0; i < count; i++) {
                                const child = g.insertVertex(null, null, "Child " + i, 100, 60 + i * 50, 80, 40);
                                g.insertEdge(null, null, "", root, child);
                            }
                        } finally {
                            g.model.endUpdate();
                        }
                        try {
                            new mxOrgChartLayout(g, 5, 20, 20).execute(g.getDefaultParent());
                            let max = 0, distinct = true;
                            const segments = [];
                            RPOrgChart.diagram.getVisualTree().IterateParentFirst((node => {
                                const list = node.State.Connector?.Segments || [];
                                max = Math.max(max, list.length);
                                if (list.length) {
                                    distinct = distinct && new Set(list.map((e => typeof e.mark === "number" ? e.mark : Array.from(e.mark).join(",")))).size === list.length;
                                    segments.push(...list);
                                }
                                return true;
                            }));
                            check(distinct, "375 segment identities do not wrap " + count);
                            if (count === 34) check(max > 32, "375 actual >32-segment connector");
                            check(g.getChildVertices(g.getDefaultParent()).every((c => Number.isFinite(c.geometry.x) && Number.isFinite(c.geometry.y))), "375 real layout finite");
                            const waypoints = g.getChildEdges(g.getDefaultParent()).map((e => (e.geometry.points || []).map((p => ({
                                x: p.x,
                                y: p.y
                            })))));
                            check(waypoints.every((ps => ps.every((p => Number.isFinite(p.x) && Number.isFinite(p.y))))), "375 finite routed waypoints");
                            check(waypoints.every((ps => ps.every(((p, i) => i === 0 || Math.abs(p.x - ps[i - 1].x) < 1e-6 || Math.abs(p.y - ps[i - 1].y) < 1e-6)))), "375 no jumps between disconnected segments");
                            details.push({
                                orgChildren: count,
                                maxSegments: max,
                                waypointCounts: waypoints.map((x => x.length))
                            });
                        } finally {
                            g.destroy();
                            host.remove();
                        }
                    }
                }));
                await test(376, (() => {
                    const host = document.createElement("div");
                    document.body.append(host);
                    const g = new Graph(host);
                    try {
                        const group = g.insertVertex(null, null, "Group", 0, 0, 160, 100), nested = g.insertVertex(group, null, "Nested", 20, 20, 60, 30), outside = g.insertVertex(null, null, "Outside", 220, 0, 80, 40);
                        g.insertEdge(null, null, "", nested, outside);
                        new mxOrgChartLayout(g, 5, 20, 20).execute(g.getDefaultParent());
                        check(Number.isFinite(outside.geometry.x) && Number.isFinite(group.geometry.x), "376 nested outside parent becomes root");
                    } finally {
                        g.destroy();
                        host.remove();
                    }
                }));
                await test(377, (async () => {
                    keep(window, "pageSize");
                    keep(window, "minSize");
                    window.pageSize = "owned-page-size";
                    window.minSize = "owned-min-size";
                    const before = new Set(document.body.children);
                    try {
                        Graph.exploreFromCell(graph, a, {
                            pageSize: 2,
                            minSize: 100
                        });
                        await new Promise((r => setTimeout(r, 50)));
                        check(window.pageSize === "owned-page-size" && window.minSize === "owned-min-size", "377 explorer values local");
                    } finally {
                        for (const child of [ ...document.body.children ]) if (!before.has(child)) {
                            child.querySelector("img")?.click();
                            if (child.isConnected) child.remove();
                        }
                    }
                }));
                await test(378, (() => {
                    keep(mxUtils, "post");
                    keep(urlParams, "update-url");
                    delete urlParams["update-url"];
                    const originalRoot = graph.model.root.value;
                    try {
                        for (const [url, allowed] of [ [ "https://untrusted.invalid/collect", false ], [ "//untrusted.invalid/collect", false ], [ location.origin + "/owned-update", true ] ]) {
                            const meta = mxUtils.createXmlDocument().createElement("owned");
                            meta.setAttribute("updateUrl", url);
                            graph.model.root.value = meta;
                            const sends = [], errors = [];
                            mxUtils.post = (url, body) => sends.push({
                                url: url,
                                body: body
                            });
                            const ed = Object.create(editor);
                            ed.graph = graph;
                            ed.eventListeners = [];
                            ed.isChromelessView = () => true;
                            const fake = Object.create(ui);
                            fake.editor = ed;
                            fake.getCurrentFile = () => null;
                            fake.handleError = e => errors.push(e.message);
                            Function("Draw", data.plugins.update)({
                                loadPlugin: fn => fn(fake)
                            });
                            check(sends.length === (allowed ? 1 : 0), "378 trusted destination " + url);
                            if (allowed) check(decodeURIComponent(sends[0].body).includes("Owned source"), "378 normal XML still sent");
                        }
                    } finally {
                        graph.model.root.value = originalRoot;
                    }
                }));
                await test(379, (() => {
                    keep(window, "sb");
                    if (data.baseline) window.sb = ui.sidebar; else delete window.sb;
                    const actions = {}, menu = {
                        funct() {}
                    }, windows = [], errors = [];
                    const fake = {
                        editor: {
                            graph: graph
                        },
                        actions: {
                            addAction: (name, fn) => actions[name] = fn
                        },
                        menus: {
                            get: () => menu,
                            addMenuItems() {}
                        },
                        handleError: e => errors.push(e.message)
                    };
                    const Window = mxWindow;
                    function OwnWindow(...args) {
                        const w = new Window(...args);
                        windows.push(w);
                        return w;
                    }
                    Function("Draw", "mxWindow", data.plugins.sql)({
                        loadPlugin: fn => fn(fake)
                    }, OwnWindow);
                    const wnd = windows[0], input = wnd.content.querySelector("textarea"), button = [ ...wnd.content.querySelectorAll("button") ].find((b => b.textContent === "Insert MySQL"));
                    function run(sql) {
                        graph.removeCells(graph.getChildCells(p, true, true), true);
                        input.value = sql;
                        button.click();
                        return graph.model.getDescendants(graph.model.root).map((c => String(c.value || "")));
                    }
                    try {
                        const first = run("CREATE TABLE users\n(\nid INT,\nPRIMARY KEY (id)\n);");
                        check(first.some((v => v === "PK")), "379 first import has primary key");
                        const second = run("CREATE TABLE users\n(\nid INT\n);");
                        check(!second.some((v => v === "PK")), "379 second schema does not inherit primary key");
                        for (const keywords of [ "FOREIGN KEY", "foreign key", "FoReIgN KeY" ]) {
                            const ref = keywords === "FOREIGN KEY" ? "REFERENCES" : keywords === "foreign key" ? "references" : "ReFeReNcEs";
                            const values = run("CREATE TABLE parent\n(\nid INT,\nPRIMARY KEY (id)\n);\nCREATE TABLE child\n(\nparent_id INT,\n" + keywords + " (parent_id) " + ref + " parent(id)\n);");
                            check(values.filter((v => /\bFK\b/.test(v))).length >= 2, "380 case-insensitive foreign keys " + keywords);
                        }
                        check(errors.length === 0, "379/380 plugin renders without ambient Sidebar");
                    } finally {
                        for (const w of windows) w.destroy();
                    }
                }));
                await test(381, (() => {
                    keep(urlParams, "replay-data");
                    keep(urlParams, "replay-delay");
                    keep(urlParams, "delay-delay");
                    keep(window, "setTimeout");
                    urlParams["replay-data"] = Graph.compress('<recording><state><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/></root></mxGraphModel></state><delta/></recording>');
                    const delays = [];
                    window.setTimeout = (fn, delay) => {
                        delays.push(delay);
                        return 0;
                    };
                    for (const [value, expected] of [ [ "5000", 5e3 ], [ "0", 0 ], [ "garbage", 1e3 ], [ "-1", 1e3 ] ]) {
                        urlParams["replay-delay"] = value;
                        urlParams["delay-delay"] = "777";
                        const fake = {
                            editor: {
                                graph: graph,
                                isChromelessView: () => true
                            },
                            fileLoaded() {},
                            handleError: e => {
                                throw e;
                            }
                        };
                        Function("Draw", data.plugins.replay)({
                            loadPlugin: fn => fn(fake)
                        });
                        check(delays.at(-1) === expected, "381 replay delay " + value);
                    }
                }));
            } finally {
                graph.destroy();
                div.remove();
                for (const restore of saved.reverse()) restore();
                window.ownedUi = ui;
                window.ownedEditor = editor;
            }
            return {
                failures: failures,
                details: details
            };
        }), data);
        fs.writeFileSync(path.join(out, theme + "-import-plugin-" + copy + ".json"), JSON.stringify(result, null, 2));
        if (!process.env.AIDE_IMPORT_PLUGIN_REVIEW_CATALOG) assert.deepEqual(result.failures, [], copy);
        checks.push({
            case: "import/plugin " + copy,
            theme: theme,
            result: result.failures.length ? "EXPECTED_FAIL" : "PASS"
        });
    }
    return checks;
};
