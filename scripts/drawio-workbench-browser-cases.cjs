"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), review = require("./drawio-workbench-review.cjs"), {png: png} = require("./drawio-import-security-browser-cases.cjs");

module.exports = async function(frame, out, theme) {
    const checks = [];
    for (const copy of [ "source", "app.min.js", "integrate.min.js" ]) {
        const data = review.snippets(copy).filter((x => !x.name.startsWith("Sidebar.")));
        const shapeCopy = copy === "source" ? null : copy === "app.min.js" ? "shapes-14-6-5.min.js" : copy;
        for (const row of review.methods.filter((x => [ 342, 343 ].includes(x[0])))) {
            if (!data.some((x => x.name === row[2]))) data.push({
                issue: row[0],
                name: row[2],
                code: require("./drawio-editing-review.cjs").assignment(fs.readFileSync(review.base + (shapeCopy ? "js/" + shapeCopy : row[1]), "utf8"), row[2])
            });
        }
        data.push({
            png: png("Owned chunk", false, true)
        });
        const result = await frame.evaluate((async data => {
            const ownedUi = window.ownedUi, ownedEditor = window.ownedEditor;
            const failures = [], details = [], saved = [];
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
            const table = mxCellRenderer.defaultShapes.table;
            keep(mxKeyHandler.prototype, "getFunction");
            keep(urlParams, "embed");
            urlParams.embed = "0";
            for (const {code: code, name: name} of data.filter((x => x.code))) {
                if (name === "EditorUi.prototype.initCanvas") continue;
                if (!name.includes(".")) {
                    keep(window, name);
                    Function(code)();
                } else if (name.startsWith("TableShape.")) {
                    keep(table.prototype, "paintForeground");
                    Function("TableShape", code)(table);
                } else {
                    const dot = name.lastIndexOf("."), o = Function("return " + name.slice(0, dot))(), k = name.slice(dot + 1);
                    keep(o, k);
                    Function(code)();
                }
            }
            const div = document.createElement("div");
            div.style.cssText = "position:absolute;left:10px;top:70px;width:600px;height:500px";
            document.body.append(div);
            const graph = new Graph(div), p = graph.getDefaultParent();
            const a = graph.insertVertex(p, "__proto__", "Owned", 40, 40, 100, 60);
            const b = graph.insertVertex(p, "constructor", "Group", 250, 40, 150, 120);
            const child = graph.insertVertex(b, "owned-child", "Child", 10, 10, 60, 30);
            const fake = Object.create(ownedUi);
            fake.editor = {
                graph: graph,
                isChromelessView: () => false
            };
            try {
                await test(336, (() => {
                    keep(window, "mxOrgChartLayout");
                    keep(window, "mxscript");
                    const scripts = [], calls = [], errors = [];
                    window.mxOrgChartLayout = undefined;
                    window.mxscript = (url, onload, a, b, c, onerror) => scripts.push({
                        onload: onload,
                        onerror: onerror
                    });
                    const ui = {
                        createTimeout: (ms, fn) => {
                            let alive = true;
                            fn({
                                clear: () => {
                                    const old = alive;
                                    alive = false;
                                    return old;
                                }
                            });
                        },
                        spinner: {
                            spin: () => true,
                            stop: () => {}
                        },
                        isOffline: () => false,
                        handleError: e => errors.push(e)
                    };
                    const fn = EditorUi.prototype.loadOrgChartLayouts;
                    fn.call(ui, (() => calls.push("first")), true);
                    fn.call(ui, (() => calls.push("second")), true);
                    check(calls.length === 0 && scripts.length === 1, "336 concurrent callbacks wait");
                    window.mxOrgChartLayout = function() {};
                    scripts[0].onload();
                    check(calls.join(",") === "first,second", "336 callbacks after load");
                    fn.call(ui, (() => calls.push("ready")), true);
                    check(calls.at(-1) === "ready" && scripts.length === 1, "336 ready path");
                    window.mxOrgChartLayout = undefined;
                    fn.call(ui, (() => calls.push("bad")), true);
                    scripts.at(-1).onerror(Error("owned load failure"));
                    check(!ui.loadingOrgChart && errors.length === 1, "336 failed load released");
                    fn.call(ui, (() => calls.push("retry")), true);
                    window.mxOrgChartLayout = function() {};
                    scripts.at(-1).onload();
                    check(calls.at(-1) === "retry", "336 retry succeeds");
                }));
                await test(337, (() => {
                    const layout = new mxHierarchicalLayout(graph);
                    ownedUi.scopeLayoutsToCells([ layout ], [ a ]);
                    check(!layout.isVertexIgnored(a) && layout.isVertexIgnored(b), "337 scoped prototype ID");
                }));
                await test(338, (() => {
                    const result = EditorUi.prototype.getJsonForPage.call(fake, null, [ child ]);
                    check(result.cells.some((c => c.id === child.id)), "338 selected child exported");
                    const both = EditorUi.prototype.getJsonForPage.call(fake, null, [ b, child ]);
                    check(both.cells.filter((c => c.id === child.id)).length === 1, "338 selected subtree deduplicated");
                }));
                await test(339, (() => {
                    const page = {
                        getId: () => "owned-page",
                        root: graph.model.root
                    };
                    const comment = {
                        getAnchorCellIds: () => [ "__proto__" ],
                        getAnchorPageId: () => "owned-page",
                        isResolved: false
                    };
                    fake.currentPage = {
                        getId: () => "other"
                    };
                    fake.getPageById = () => page;
                    fake.updatePageRoot = () => page;
                    check(EditorUi.prototype.getCommentAnchorCells.call(fake, comment)[0] === a, "339 noncurrent anchor resolved");
                    fake.currentPage = page;
                    fake.getCurrentFile = () => ({
                        anchoredCommentsSupported: () => true,
                        commentCache: [ comment ]
                    });
                    EditorUi.prototype.updateCommentOverlays.call(fake);
                    check(fake.commentOverlays?.some((o => o.cell === a)), "339 actual comment overlay");
                    if (fake.commentOverlays) for (const o of fake.commentOverlays) graph.removeCellOverlay(o.cell, o.overlay);
                }));
                await test(341, (() => {
                    keep(window, "setInterval");
                    keep(window, "clearInterval");
                    const timers = new Set, oldSet = window.setInterval, oldClear = window.clearInterval;
                    window.setInterval = (fn, ms) => {
                        const id = oldSet(fn, ms);
                        timers.add(id);
                        return id;
                    };
                    window.clearInterval = id => {
                        timers.delete(id);
                        oldClear(id);
                    };
                    const ui = Object.create(ownedUi);
                    ui.canComment = () => false;
                    ui.canReplyToReplies = () => false;
                    ui.mentionsSupported = () => false;
                    ui.anchoredCommentsSupported = () => false;
                    ui.commentsSupported = () => true;
                    ui.refreshCommentCache = done => done?.([]);
                    ui.getCommentCacheTarget = () => ({
                        commentCache: []
                    });
                    ui.getCurrentUser = () => ({
                        id: "owned"
                    });
                    let wnd;
                    try {
                        wnd = new CommentsWindow(ui, 10, 10, 300, 300);
                        check(timers.size === 1, "341 one refresh interval");
                        wnd.destroy();
                        wnd = null;
                        check(timers.size === 0, "341 interval released on destroy");
                    } finally {
                        wnd?.destroy();
                        for (const id of timers) oldClear(id);
                    }
                }));
                const boundedCanvas = limit => {
                    let calls = 0;
                    const c = new Proxy({
                        state: {}
                    }, {
                        get: (o, k) => k === "calls" ? calls : k in o ? o[k] : (...args) => {
                            if (++calls > limit) throw Error("owned canvas operation limit");
                            for (const v of args) if (typeof v === "number" && !Number.isFinite(v)) throw Error("nonfinite draw value");
                        }
                    });
                    return c;
                };
                await test(342, (() => {
                    const shape = new mxShapeMockupiIconGrid;
                    shape.style = {};
                    for (const [size, cap] of [ [ [ "3", "3" ], 300 ], [ [ "100000", "100000" ], 18e3 ] ]) {
                        shape.style = {
                            gridSize: size.join(",")
                        };
                        const c = boundedCanvas(cap);
                        try {
                            shape.paintVertexShape(c, 0, 0, 100, 100);
                            check(c.calls < cap, "342 bounded grid " + size);
                        } catch (e) {
                            failures.push("342 " + e.message);
                        }
                    }
                }));
                await test(343, (() => {
                    for (const C of [ mxShapeMockupiPhone, mxShapeMockupiBgStriped, mxShapeMockupiPad ]) {
                        const shape = new C;
                        shape.style = {
                            bgStyle: "bgStriped"
                        };
                        for (const width of [ 250, 1e9 ]) {
                            const c = boundedCanvas(5e3);
                            try {
                                shape.foreground(c, 0, 0, width, 300, 10);
                                check(c.calls < 5e3, "343 bounded " + C.name + " " + width);
                            } catch (e) {
                                failures.push("343 " + C.name + " " + e.message);
                            }
                        }
                    }
                }));
                await test(344, (() => {
                    keep(window, "row");
                    window.row = "owned-row";
                    const dialog = new FilenameDialog(ownedUi, "owned.txt", "Save", (() => {}));
                    check(window.row === "owned-row", "344 filename row local");
                    dialog.container.remove();
                }));
                await test(345, (() => {
                    keep(window, "value");
                    window.value = "owned-value";
                    EditorUi.parsePng(atob(data.find((x => x.png)).png.split(",")[1]), (() => false));
                    check(window.value === "owned-value", "345 chunk data local");
                }));
                await test(346, (async () => {
                    keep(window, "cell");
                    window.cell = "owned-cell";
                    const host = document.createElement("div");
                    host.style.cssText = "position:fixed;left:-5000px;top:0;width:600px;height:400px";
                    document.body.append(host);
                    const ui = new EditorUi(new Editor(false), host);
                    try {
                        const g = ui.editor.graph, r = g.insertVertex(null, null, "Relative", .2, .2, 50, 30);
                        r.geometry.relative = true;
                        g.view.validate();
                        const state = g.view.getState(r);
                        g.connectVertex = (...args) => args[6](0, 0, (() => {}));
                        ui.showShapePicker = () => document.createElement("div");
                        const me = new mxMouseEvent(new MouseEvent("click"), state);
                        ui.hoverIcons.execute(state, mxConstants.DIRECTION_EAST, me);
                        await new Promise((r => setTimeout(r, 50)));
                        check(window.cell === "owned-cell", "346 traversal cell local");
                    } finally {
                        ui.destroy();
                        host.remove();
                    }
                }));
                await test(349, (() => {
                    const original = mxKeyHandler.prototype.getFunction;
                    for (let i = 0; i < 2; i++) {
                        const host = document.createElement("div");
                        host.style.cssText = "position:fixed;left:-5000px;top:0;width:600px;height:400px";
                        document.body.append(host);
                        const ui = new EditorUi(new Editor(false), host);
                        try {
                            check(mxKeyHandler.prototype.getFunction === original, "349 global prototype unchanged");
                            check(Object.hasOwn(ui.keyHandler, "getFunction"), "349 per-instance keyboard dispatch");
                        } finally {
                            ui.destroy();
                            host.remove();
                            mxKeyHandler.prototype.getFunction = original;
                        }
                    }
                }));
                await test(350, (() => {
                    const host = document.createElement("div");
                    document.body.append(host);
                    const panel = Object.create(ArrangePanel.prototype);
                    BaseFormatPanel.call(panel, ownedUi.format, ownedUi, host);
                    const input = document.createElement("input");
                    host.append(input);
                    let writes = 0;
                    panel.addGeometryHandler(input, (() => writes++));
                    try {
                        for (let i = 0; i < 3; i++) {
                            input.value = "100 px";
                            input.focus();
                            input.value = "invalid";
                            input.blur();
                            check(input.value === "100 px", "350 one unit after invalid input");
                        }
                        check(writes === 0, "350 invalid input does not write geometry");
                    } finally {
                        panel.destroy();
                        host.remove();
                    }
                }));
                await test(351, (async () => {
                    const ui = ownedUi, g = ui.editor.graph, host = document.createElement("div");
                    host.style.cssText = "position:absolute;right:0;top:70px;width:300px;background:white";
                    document.body.append(host);
                    const v = g.insertVertex(null, null, "<b>Owned rich text</b>", 20, 20, 100, 40, "html=1;");
                    g.setSelectionCell(v);
                    g.startEditingAtCell(v);
                    await new Promise((r => setTimeout(r, 40)));
                    let panel;
                    try {
                        panel = new TextFormatPanel(ui.format, ui, host);
                        const tip = mxResources.get("superscript");
                        const button = [ ...host.querySelectorAll("[title]") ].find((e => e.title.startsWith(tip + " ")));
                        check(button?.style.marginLeft === "10px", "351 actual superscript button spacing");
                    } finally {
                        panel?.destroy();
                        g.stopEditing(true);
                        g.removeCells([ v ], true);
                        host.remove();
                    }
                }));
                await test(352, (() => {
                    keep(window, "s");
                    window.s = "owned-scale";
                    const cell = graph.insertVertex(p, null, "", 30, 200, 180, 80, "shape=table;");
                    graph.view.validate();
                    const shape = graph.view.getState(cell).shape;
                    shape.paintForeground(boundedCanvas(3e3), 0, 0, 180, 80);
                    check(window.s === "owned-scale", "352 table rendering scale local");
                    graph.removeCells([ cell ], true);
                }));
                // Toolbar clicks run against a controlled location facade so the owned test frame stays loaded.
                                await test(347, (() => {
                    const method = data.find((x => x.issue === 347)).code, original = EditorUi.prototype.initCanvas, params = urlParams["toolbar-config"];
                    const navigation = {
                        href: "",
                        reload() {
                            this.reloaded = true;
                        }
                    };
                    const facade = new Proxy(window, {
                        get: (o, k) => k === "location" ? navigation : typeof o[k] === "function" ? o[k].bind(o) : o[k]
                    });
                    try {
                        Function("window", method)(facade);
                        for (const [url, allowed] of [ [ "https://untrusted.invalid/path", false ], [ location.origin + "/owned-return", true ] ]) {
                            urlParams["toolbar-config"] = encodeURIComponent(JSON.stringify({
                                backBtn: {
                                    url: url
                                },
                                refreshBtn: {
                                    url: url
                                }
                            }));
                            const host = document.createElement("div");
                            host.style.cssText = "position:fixed;left:-5000px;top:0;width:600px;height:400px";
                            document.body.append(host);
                            let ui;
                            try {
                                ui = new EditorUi(new Editor(true), host);
                                navigation.href = "";
                                const back = ui.chromelessToolbar?.querySelector('[title="' + mxResources.get("back") + '"]');
                                back?.click();
                                check(allowed ? navigation.href === url : navigation.href === "", "347 back same-origin policy");
                                navigation.href = "";
                                const refresh = ui.chromelessToolbar?.querySelector('[title="' + mxResources.get("refresh") + '"]');
                                refresh?.click();
                                check(allowed ? navigation.href === url : navigation.href === "", "347 refresh same-origin policy");
                            } finally {
                                ui?.destroy();
                                host.remove();
                            }
                        }
                    } finally {
                        EditorUi.prototype.initCanvas = original;
                        if (params === undefined) delete urlParams["toolbar-config"]; else urlParams["toolbar-config"] = params;
                    }
                }));
            } finally {
                graph.destroy();
                div.remove();
                for (const restore of saved.reverse()) restore();
                window.ownedUi = ownedUi;
                window.ownedEditor = ownedEditor;
            }
            return {
                failures: failures,
                details: details
            };
        }), data);
        fs.writeFileSync(path.join(out, theme + "-workbench-" + copy + ".json"), JSON.stringify(result, null, 2));
        if (!process.env.AIDE_WORKBENCH_REVIEW_CATALOG) assert.deepEqual(result.failures, [], copy);
        checks.push({
            case: "workbench runtime " + copy,
            theme: theme,
            result: result.failures.length ? "EXPECTED_FAIL" : "PASS"
        });
    }
    return checks;
};
