"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict");

module.exports = async (frame, out, theme) => {
    const checks = [];
    await frame.evaluate((() => {
        window.ownedReviewUi = ownedUi;
        window.ownedReviewEditor = ownedEditor;
    }));
    for (const issue of [ 452, 453, 454, 455, 456, 457, 458, 459, 460, 461, 462, 465, 466 ]) {
        const result = await frame.evaluate((async issue => {
            const ownedUi = window.ownedReviewUi, ownedEditor = window.ownedReviewEditor;
            const ensure = (ok, message) => {
                if (!ok) throw Error(message);
            };
            const container = document.createElement("div");
            container.style.cssText = "position:fixed;left:0;top:0;width:80vw;height:700px;background:white;z-index:10000";
            document.body.append(container);
            let graph, ui;
            try {
                if (issue === 452) {
                    let saved = null;
                    const support = Graph.fileSupport;
                    Graph.fileSupport = true;
                    const dlg = new FilenameDialog(Object.assign(Object.create(ownedUi), {
                        hideDialog() {}
                    }), "old", "Apply", (v => saved = v));
                    container.append(dlg.container);
                    try {
                        dlg.init();
                        const input = dlg.container.querySelector("input");
                        const event = new Event("drop", {
                            bubbles: true,
                            cancelable: true
                        });
                        Object.defineProperty(event, "dataTransfer", {
                            value: {
                                types: [ "text/uri-list" ],
                                getData: () => "https://example.test/%FF"
                            }
                        });
                        input.dispatchEvent(event);
                        ensure(event.defaultPrevented, "drop must be consumed");
                        ensure(saved === "https://example.test/%FF", "malformed URI retained without uncaught decode");
                    } finally {
                        Graph.fileSupport = support;
                    }
                }
                if (issue === 453) {
                    graph = new Graph(container);
                    const cell = graph.insertVertex(null, null, "node", 0, 0, 100, 40);
                    const state = graph.view.getState(cell);
                    state.shape.stencil = {
                        desc: mxUtils.parseXml('<shape><fillcolor color="constructor"/><strokecolor color="toString"/><fontcolor color="__proto__"/></shape>').documentElement,
                        getDefaultColorValue() {
                            return "#000000";
                        }
                    };
                    state.shape.customProperties = [];
                    state.shape.commonCustomPropAdded = false;
                    const target = {
                        editor: {
                            graph: graph
                        }
                    };
                    EditorUi.prototype.findCommonProperties.call(target, cell, Object.create(null), true, {});
                    ensure([ "constructor", "toString", "__proto__" ].every((name => state.shape.customProperties.some((p => p.name === name)))), "all 3 custom stencil colors retained");
                    state.shape.stencil = null;
                }
                if (issue === 454) {
                    const value = {
                        label: "Example",
                        hasOwnProperty: "metadata"
                    };
                    const result = EditorUi.prototype.getCellsForShapePicker.call({
                        editor: {
                            graph: ownedEditor.graph
                        },
                        defaultShapePickerEntries: [ {
                            style: "shape=rect",
                            value: value
                        } ]
                    });
                    const cells = Array.isArray(result) ? result : result.vertices;
                    ensure(cells?.length === 1, "one picker vertex");
                    ensure(cells[0].value.getAttribute("hasOwnProperty") === "metadata", "metadata preserved");
                }
                if (issue === 455 || issue === 456) {
                    const saved = {
                        ...urlParams
                    };
                    Object.assign(urlParams, {
                        embed: "0",
                        chrome: "0",
                        viewer: "1",
                        layers: "1",
                        toolbar: "1",
                        "toolbar-config": issue === 455 ? "not-json" : "{}"
                    });
                    try {
                        const editor = new Editor(true);
                        ui = new EditorUi(editor, container);
                        if (issue === 455) ensure(ui.chromelessToolbar, "malformed toolbar config must retain usable toolbar"); else {
                            const button = [ ...ui.chromelessToolbar.querySelectorAll("img") ].find((n => n.getAttribute("title") === mxResources.get("layers") || n.parentNode.getAttribute("title") === mxResources.get("layers")));
                            ensure(button, "layers button found");
                            button.click();
                            ensure(ui.layersDialog, "layers popup created");
                            ensure(ui.layersDialog.style.top !== "" && Number.isFinite(parseFloat(ui.layersDialog.style.top)), "top toolbar popup has finite top anchor");
                        }
                    } finally {
                        for (const k of Object.keys(urlParams)) delete urlParams[k];
                        Object.assign(urlParams, saved);
                    }
                }
                if (issue === 457) {
                    const snapshot = {
                        node: ownedEditor.getGraphXml(),
                        page: ownedUi.currentPage
                    };
                    const node = ownedEditor.getGraphXml();
                    ownedUi.updateDiagramData(snapshot, node);
                    ensure(ownedEditor.graph.model.root, "actual app page-aware update accepts standalone model");
                }
                if (issue === 458) {
                    const content = document.createElement("div");
                    content.textContent = "owned";
                    const wnd = new mxWindow("Owned", content, 10, 10, 200, 120, true, true);
                    const dlg = {
                        window: wnd
                    };
                    EditorUi.prototype.installResizeHandler.call(ownedUi, dlg, true);
                    try {
                        wnd.setSize(innerWidth * 2, innerHeight * 2);
                        wnd.setLocation(-50, innerHeight * 2);
                        const r = wnd.div.getBoundingClientRect();
                        ensure(r.left >= 0 && r.top >= 0 && r.right <= innerWidth + 1 && r.bottom <= innerHeight + 1, "window clamps to viewport");
                    } finally {
                        dlg.destroy();
                    }
                }
                if (issue === 459) {
                    const selection = [ {
                        id: "selected"
                    } ], parent = {
                        id: "parent"
                    };
                    let passed;
                    const fake = {
                        editor: {
                            graph: {
                                getDefaultParent: () => parent,
                                getSelectionCells: () => selection
                            }
                        },
                        executeLayout(fn, animate, done) {
                            fn();
                            if (done) done();
                        },
                        handleError(e) {
                            throw e;
                        }
                    };
                    await new Promise((resolve => EditorUi.prototype.executeLayouts.call(fake, [ {
                        prepare(p, done) {
                            setTimeout((() => done(null, (() => {}))), 0);
                        }
                    }, {
                        execute(p, c) {
                            passed = c;
                        }
                    } ], resolve)));
                    ensure(passed === selection, "mixed async layout forwards selected cells");
                }
                if (issue === 460) {
                    graph = new Graph(container);
                    const vertex = graph.insertVertex(null, null, "source", 0, 0, 80, 40);
                    let rejected = false;
                    try {
                        Graph.exploreFromCell(graph, vertex, {
                            pageSize: 0
                        });
                    } catch (e) {
                        rejected = /page.*size|positive/i.test(e.message);
                    }
                    ensure(rejected, "zero page size explicitly rejected before opening Explore");
                }
                if (issue === 461) {
                    const before = window.attr;
                    window.attr = "owned-sentinel";
                    try {
                        const div = document.createElement("div");
                        div.innerHTML = '<b title="kept">text</b>';
                        Graph.prototype.replaceElement.call({}, div.firstChild, "span");
                        ensure(window.attr === "owned-sentinel", "host global attr retained");
                        ensure(div.querySelector("span")?.title === "kept", "attributes retained");
                    } finally {
                        if (before === undefined) delete window.attr; else window.attr = before;
                    }
                }
                if (issue === 462) {
                    const add = mxEvent.addListener, remove = mxEvent.removeListener;
                    const callbacks = new Set;
                    mxEvent.addListener = function(el, name, fn, ...rest) {
                        if (el === document && name === "mouseleave") callbacks.add(fn);
                        return add.call(this, el, name, fn, ...rest);
                    };
                    mxEvent.removeListener = function(el, name, fn, ...rest) {
                        if (el === document && name === "mouseleave") callbacks.delete(fn);
                        return remove.call(this, el, name, fn, ...rest);
                    };
                    try {
                        graph = new Graph(container);
                        graph.addClickHandler();
                        ensure(callbacks.size >= 1, "document listener registered");
                        graph.destroy();
                        graph = null;
                        ensure(callbacks.size === 0, "destroy removes document listener");
                    } finally {
                        mxEvent.addListener = add;
                        mxEvent.removeListener = remove;
                        for (const fn of callbacks) remove.call(mxEvent, document, "mouseleave", fn);
                    }
                }
                if (issue === 465) {
                    let args;
                    const printErrors = [];
                    const fake = Object.create(ownedUi);
                    fake.hideDialog = () => {};
                    fake.handleError = e => printErrors.push(e.message);
                    const dlg = new PrintDialog(fake, "Owned", ((preview, data) => args = data));
                    container.append(dlg.container);
                    const nums = [ ...dlg.container.querySelectorAll("input[type=number]") ];
                    const fit = [ ...dlg.container.querySelectorAll("input[name=printSize]") ].at(-1);
                    const across = fit.parentNode.querySelector("input[type=number]");
                    ensure(across, "fit sheet input found");
                    fit.checked = true;
                    const button = [ ...dlg.container.querySelectorAll("button") ].at(-1);
                    button.click();
                    await new Promise((r => setTimeout(r, 30)));
                    ensure(args, "ordinary print/export callback runs; buttons=" + [ ...dlg.container.querySelectorAll("button") ].map((b => b.textContent)));
                    args = null;
                    across.value = "0";
                    button.click();
                    await new Promise((r => setTimeout(r, 30)));
                    ensure(!args && printErrors.some((e => /positive integers/.test(e))), "invalid count reports an error and cannot reach print/export callback");
                }
                if (issue === 466) {
                    const snapshot = ownedUi.getFileData();
                    try {
                        ownedUi.setFileData('<mxfile><diagram id="__proto__" name="One"><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/></root></mxGraphModel></diagram></mxfile>');
                        ensure(ownedUi.pages.length === 1 && ownedUi.pages[0].getId() === "__proto__", "special page ID loaded");
                    } finally {
                        ownedUi.setFileData(snapshot);
                    }
                }
                return {
                    issue: issue,
                    result: "PASS"
                };
            } catch (e) {
                return {
                    issue: issue,
                    result: "FAIL",
                    error: e.message,
                    stack: e.stack
                };
            } finally {
                if (ui) {
                    if (ui.layersDialog?.parentNode) ui.layersDialog.remove();
                    ui.destroy();
                }
                if (graph) graph.destroy();
                container.remove();
                window.ownedUi = ownedUi;
                window.ownedEditor = ownedEditor;
            }
        }), issue);
        checks.push({
            ...result,
            theme: theme
        });
    }
    fs.writeFileSync(path.join(out, theme + "-ui-robustness.json"), JSON.stringify(checks, null, 2));
    if (!process.env.AIDE_EXPECT_REVIEW_FAILURES) assert.deepEqual(checks.filter((r => r.result !== "PASS")), []);
    return checks;
};
