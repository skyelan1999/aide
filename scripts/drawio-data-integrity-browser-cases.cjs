"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), review = require("./drawio-data-integrity-review.cjs");

module.exports = async function(frame, out, theme) {
    const checks = [];
    for (const copy of [ "source", "app.min.js", "integrate.min.js" ]) {
        const data = review.snippets(copy);
        const result = await frame.evaluate((async data => {
            const failures = [], passed = [], saved = [];
            const check = (ok, msg) => {
                (ok ? passed : failures).push(msg);
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
            for (const {code: code, name: name} of data) {
                const dot = name.lastIndexOf("."), o = Function("return " + name.slice(0, dot))(), key = name.slice(dot + 1);
                keep(o, key);
                Function(code)();
            }
            const host = document.createElement("div");
            host.style.cssText = "position:absolute;left:20px;top:80px;width:600px;height:400px";
            document.body.append(host);
            const graph = new Graph(host), p = graph.getDefaultParent(), cells = [];
            try {
                for (const id of [ "constructor", "toString", "__proto__", "ordinary" ]) cells.push(graph.insertVertex(p, id, id, 30 + cells.length * 100, 40, 80, 40));
                graph.view.validate();
                await test(311, (async () => {
                    for (const text of [ '<img src="/owned-missing-image" onerror="window.ownedInitialTextExecuted=1">', '<b>plain</b> & "quote"', "中文\nsecond" ]) {
                        window.ownedInitialTextExecuted = 0;
                        graph.startEditingAtCell(cells[3], null, text);
                        await new Promise((r => setTimeout(r, 60)));
                        check(graph.cellEditor.textarea.textContent === text, "311 plain initial text " + text.slice(0, 12));
                        check(!graph.cellEditor.textarea.querySelector("img,b"), "311 markup never parsed");
                        check(window.ownedInitialTextExecuted === 0, "311 handler never executes");
                        graph.stopEditing(true);
                    }
                }));
                await test(326, (() => {
                    graph.setAttributeForCell(cells[0], "tags", "constructor");
                    graph.setAttributeForCell(cells[1], "tags", "requested");
                    graph.setAttributeForCell(cells[2], "tags", "__proto__");
                    graph.setAttributeForCell(cells[3], "tags", "requested extra");
                    check(graph.getCellsForTags([ "requested" ]).map((c => c.id)).sort().join(",") === "ordinary,toString", "326 only explicit tags");
                    check(graph.getCellsForTags([ "__proto__" ])[0] === cells[2], "326 explicit prototype tag supported");
                    check(graph.getCellsForTags([ "requested" ], null, false, false, "or").length === 2, "326 OR mode");
                }));
                await test(365, (() => {
                    check(graph.getCellsForTags([ "requested", "requested" ]).length === 2, "365 duplicate requested tag counted once");
                    graph.setAttributeForCell(cells[3], "tags", "requested requested");
                    check(!graph.getCellsForTags([ "requested", "extra" ]).includes(cells[3]), "365 duplicate cell tag cannot replace missing tag");
                }));
                await test(327, (() => {
                    const action = {
                        cells: [ "constructor", "toString", "__proto__", "constructor" ],
                        excludeCells: [ "ordinary" ]
                    };
                    check(graph.getCellsForAction(action).length === 3, "327 explicit IDs and exclusion map");
                    const player = new Editor.AnimationPlayer(graph, {
                        steps: [ {
                            hide: action
                        } ]
                    });
                    check(player.collectReferencedCells().length === 3, "327 animation references");
                }));
                const ui = ownedUi;
                await test(328, (() => {
                    for (const bg of [ 'red" onload="window.ownedExportInjected=1', "#123456", "rgba(1, 2, 3, 0.5)" ]) {
                        graph.background = bg;
                        const html = ui.getHtml(mxUtils.parseXml("<mxGraphModel/>").documentElement, graph, "Owned");
                        const doc = (new DOMParser).parseFromString(html, "text/html");
                        check(!doc.body.hasAttribute("onload"), "328 body attribute injection rejected");
                        if (!bg.includes("onload")) check(!!doc.body.style.backgroundColor, "328 ordinary CSS color preserved");
                    }
                }));
                await test(329, (() => {
                    const current = ui.currentPage, pages = ui.pages, hidden = ui.editor.graph.hiddenTags;
                    try {
                        const special = new DiagramPage(mxUtils.parseXml('<diagram id="__proto__"/>').documentElement);
                        special.viewState = {
                            hiddenTags: [ "owned" ]
                        };
                        ui.pages = [ special ];
                        ui.currentPage = null;
                        const map = JSON.parse(JSON.stringify(ui.getHiddenTagsMap()));
                        check(Object.hasOwn(map, "__proto__") && map.__proto__[0] === "owned", "329 hidden tags serialized");
                    } finally {
                        ui.pages = pages;
                        ui.currentPage = current;
                        ui.editor.graph.hiddenTags = hidden;
                    }
                }));
                await test(330, (() => {
                    keep(window, "counter");
                    window.counter = "owned-counter";
                    let calls = 0;
                    for (const count of [ 0, 3 ]) {
                        const root = mxUtils.parseXml('<svg xmlns="http://www.w3.org/2000/svg">' + "<switch/>".repeat(count) + "</svg>").documentElement;
                        ui.replaceAlternateContent(root, null, (() => calls++));
                    }
                    check(calls === 2, "330 fallback conversion callback once per call");
                    check(window.counter === "owned-counter", "330 counter local");
                }));
                await test(331, (() => {
                    for (const id of [ "a&x=1#tail", "中文 /+?" ]) {
                        const page = new DiagramPage(mxUtils.parseXml("<diagram/>").documentElement);
                        page.node.setAttribute("id", id);
                        const args = Array(15).fill(null);
                        args[7] = true;
                        args[10] = page;
                        const url = new URL(ui.createLink(...args));
                        check(url.searchParams.get("page-id") === id, "331 page ID round-trip");
                        check(url.hash.startsWith("#R"), "331 embedded copy fragment preserved");
                    }
                }));
                await test(332, (() => {
                    const current = ui.currentPage, pages = ui.pages;
                    try {
                        const special = new DiagramPage(mxUtils.parseXml('<diagram id="__proto__"/>').documentElement);
                        ui.pages = [ special ];
                        const map = ui.addLocalPagesToMapping();
                        check(Object.hasOwn(map, "__proto__") && map.__proto__ === "__proto__", "332 local page map");
                        check(ui.updatePageLink({}, "data:page/id,constructor") === null, "332 inherited mapping never used");
                    } finally {
                        ui.pages = pages;
                        ui.currentPage = current;
                    }
                    const backup = ui.getFileData(true);
                    try {
                        const model = '<mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><UserObject id="owned-link" label="Owned" link="data:page/id,__proto__"><mxCell vertex="1" parent="1"><mxGeometry x="10" y="10" width="80" height="40" as="geometry"/></mxCell></UserObject></root></mxGraphModel>';
                        ui.importXml('<mxfile><diagram id="__proto__" name="Owned Import">' + model + '</diagram><diagram id="other" name="Owned Second">' + model + "</diagram></mxfile>", 0, 0, false, true, true);
                        const page = ui.pages.find((p => p.getName() === "Owned Import"));
                        check(!!page, "332 actual page imported");
                        const cell = ui.editor.graph.model.getDescendants(page.root).find((c => c.value?.getAttribute?.("label") === "Owned"));
                        check(ui.editor.graph.getLinkForCell(cell) === "data:page/id," + page.getId(), "332 imported link remapped to new page");
                    } finally {
                        ui.setFileData(backup);
                    }
                }));
                await test(333, (() => {
                    keep(window, "result");
                    keep(window, "mxXmlRequest");
                    keep(ui, "createTimeout");
                    keep(ui, "handleError");
                    window.result = "owned-result";
                    let output;
                    window.mxXmlRequest = function() {
                        this.send = cb => cb(this);
                        this.getStatus = () => 200;
                        this.getText = () => JSON.stringify({
                            result: "<mxGraphModel><root/></mxGraphModel>"
                        });
                    };
                    ui.createTimeout = (ms, fn) => {
                        let alive = true;
                        fn({
                            isAlive: () => alive,
                            clear: () => {
                                const old = alive;
                                alive = false;
                                return old;
                            }
                        });
                    };
                    ui.generateOpenAiMermaidDiagram("Owned mock", (v => output = v), (e => {
                        throw e;
                    }));
                    check(output?.includes("mxGraphModel"), "333 mocked response delivered");
                    check(window.result === "owned-result", "333 response stays local");
                }));
                await test(334, (() => {
                    keep(window, "FileReader");
                    keep(ui, "spinner");
                    keep(ui, "handleError");
                    const readers = [];
                    window.FileReader = function() {
                        readers.push(this);
                        this.readAsText = () => {};
                        this.readAsDataURL = () => {};
                    };
                    let stopped = 0, done = 0, errors = 0;
                    ui.spinner = {
                        spin: () => true,
                        stop: () => stopped++
                    };
                    ui.handleError = () => errors++;
                    ui.importFiles([ new File([ "bad" ], "bad.txt", {
                        type: "text/plain"
                    }), new File([ "ok" ], "ok.txt", {
                        type: "text/plain"
                    }) ], 0, 0, null, ((...args) => args[8]([])), (() => done++));
                    check(readers.length === 2, "334 two reads started");
                    readers[0].onerror?.({
                        target: readers[0]
                    });
                    readers[0].onabort?.({
                        target: readers[0]
                    });
                    readers[1].onload({
                        target: {
                            result: "ok"
                        }
                    });
                    check(stopped === 1 && done === 1, "334 failure and success finish once");
                    check(errors === 1, "334 failure reported once");
                }));
            } finally {
                graph.destroy();
                host.remove();
                for (const restore of saved.reverse()) restore();
                delete window.ownedInitialTextExecuted;
            }
            return {
                failures: failures,
                passed: passed
            };
        }), data);
        fs.writeFileSync(path.join(out, theme + "-data-" + copy + ".json"), JSON.stringify(result, null, 2));
        if (!process.env.AIDE_DATA_REVIEW_CATALOG) assert.deepEqual(result.failures, [], copy);
        checks.push({
            case: "data integrity " + copy,
            theme: theme,
            result: result.failures.length ? "EXPECTED_FAIL" : "PASS"
        });
    }
    return checks;
};
