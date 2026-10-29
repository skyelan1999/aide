"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), review = require("./drawio-workbench-review.cjs");

module.exports = async function(frame, out, theme) {
    const checks = [];
    for (const copy of [ "source", "app.min.js", "integrate.min.js" ]) {
        const data = review.snippets(copy).filter((x => x.name.startsWith("Sidebar.")));
        const result = await frame.evaluate((data => {
            function Captured() {}
            Captured.prototype = Object.create(Sidebar.prototype);
            const sb = new Captured, entries = [], failures = [], details = [];
            sb.graph = ownedEditor.graph;
            sb.editorUi = ownedUi;
            sb.taglist = Object.create(null);
            sb.searchIndexData = null;
            sb.setCurrentSearchEntryLibrary = () => {};
            sb.addPaletteFunctions = () => {};
            sb.addPalette = () => {};
            sb.addStencilPalette = () => {};
            const check = (ok, msg) => {
                if (!ok) failures.push(msg);
            };
            Function("Sidebar", data.map((x => x.code)).join("\n"))(Captured);
            let family, current;
            sb.createItem = function(cells, title, a, b, width, height) {
                if (current) Object.assign(current, {
                    cells: cells,
                    title: title,
                    width: width,
                    height: height
                });
                return document.createElement("div");
            };
            sb.addEntry = function(tags, fn) {
                const e = {
                    tags: tags,
                    fn: fn,
                    family: family
                };
                entries.push(e);
                return Sidebar.prototype.addEntry.call(this, tags, fn);
            };
            const oldSb = window.sb;
            window.sb = "owned-sb";
            try {
                for (const x of data) {
                    family = x.issue;
                    const name = x.name.split(".").at(-1);
                    if (x.issue === 551) sb[name](80, 80); else sb[name]("", "", sb);
                }
                check(window.sb === "owned-sb", "357 sidebar stays local");
            } finally {
                if (oldSb === undefined) delete window.sb; else window.sb = oldSb;
            }
            const priorSb = window.sb;
            window.sb = {
                createVertexTemplateFromCells() {
                    throw Error("owned wrong Sidebar");
                },
                createEdgeTemplateFromCells() {
                    throw Error("owned wrong Sidebar");
                }
            };
            for (const e of entries) {
                current = e;
                try {
                    e.fn();
                } catch (error) {
                    failures.push(e.family + " factory " + error.message);
                }
                current = null;
            }
            window.sb = priorSb;
            check(entries.some((e => e.family === 551)), "551 dependent factories captured");
            check(entries.filter((e => e.family === 551)).every((e => e.cells)), "551 factories bind to their own sidebar");
            const get = n => entries.filter((e => e.family === n)), tokens = e => (e.tags || "").toLowerCase().split(/\W+/), query = (term, e) => {
                let hit = false;
                sb.searchEntries(term, 1e4, 0, (fns => hit = fns.includes(e.fn)), null, true);
                return hit;
            };
            const selection = get(353).filter((e => /^(Checkbox|Radio button) /.test(e.title)));
            check(selection.length === 28, "353 selection target count");
            check(selection.every((e => !e.title.endsWith("))"))), "353 balanced selection titles");
            const fields = get(354).filter((e => e.title?.startsWith("Single-line text field") && e.cells?.length === 3 && e.cells[0].value === "Label text" && (e.cells[0].style || "").includes("fontSize=12;")));
            check(fields.length === 5, "354 floating-label target count");
            let focused = 0, normal = 0;
            for (const e of fields) {
                const blue = e.cells?.some((c => (c.style || "").includes("#1F9BFD")));
                if (blue) {
                    focused++;
                    check(e.title.includes("focused"), "354 blue focused title");
                    check(tokens(e).includes("focus"), "354 focused searchable");
                } else {
                    normal++;
                    check(!tokens(e).includes("focus"), "354 gray normal not focus");
                }
            }
            check(focused === 2 && normal >= 3, "354 actual field state colors");
            check(get(355).length === 11, "355 stepper count");
            check(get(355).every((e => !tokens(e).includes("tab"))), "355 no tab search pollution");
            const email = get(358).find((e => e.title === "Email"));
            check(!!email && tokens(email).includes("email") && !tokens(email).includes("calendar") && !tokens(email).includes("date"), "358 email tags");
            const calendar = get(358).find((e => e.title === "Calendar"));
            check(calendar && query("calendar", calendar) && !query("calendar", email), "358 actual search isolation");
            const sap = get(359);
            check(sap.length > 10, "359 SAP collections captured");
            check(sap.every((e => query(e.tags.includes("area") ? "sap area shapes" : "sap default shapes", e))), "359 SAP category searches");
            const full = get(360).find((e => e.title === "Full Port")), proxy = get(360).find((e => e.title === "Proxy Port"));
            check(!!full && !!proxy, "360 port targets");
            check(full?.cells[0]?.children?.some((c => (c.style || "").includes("part=1;"))), "360 full-port composite label");
            const and = get(361).find((e => e.title === "AND Gate")), or = get(361).find((e => e.title === "OR Gate"));
            check(and?.cells[0].style.includes("shape=mxgraph.electrical.logic_gates.and;"), "361 actual AND resource");
            check(or?.cells[0].style.includes("shape=or;"), "361 OR renderer");
            const roles = get(362).filter((e => [ "Administrator", "Infrastructure Role", "Tenant Role" ].includes(e.title)));
            check(roles.length === 3, "362 role targets");
            for (const e of roles) check(query("administrator", e) === (e.title === "Administrator"), "362 administrator search " + e.title);
            const graph = ownedEditor.graph;
            const inserted = [];
            for (const e of [ selection[0], fields.find((e => e.title?.includes("focused"))), get(355)[0], email, sap[0], full, and, or, ...roles ].filter(Boolean)) {
                try {
                    const clones = graph.importCells(e.cells, 30 + inserted.length % 4 * 210, 40 + Math.floor(inserted.length / 4) * 200);
                    inserted.push(...clones);
                    for (const c of clones) {
                        const state = graph.view.getState(c);
                        check(!!state?.shape?.node?.isConnected, e.family + " actual rendered template " + e.title);
                    }
                    if (e === full) {
                        const child = clones[0].children?.find((c => (c.style || "").includes("part=1;")));
                        check(child && graph.getCompositeParent(child) === clones[0], "360 actual composite parent");
                    }
                    if (e === and) {
                        const stencil = graph.view.getState(clones[0])?.shape?.stencil;
                        check(stencil?.desc?.getAttribute("name") === "AND", "361 independent AND stencil identity");
                    }
                    details.push({
                        issue: e.family,
                        title: e.title,
                        tags: e.tags
                    });
                } catch (error) {
                    failures.push(e.family + " render " + error.message);
                }
            }
            graph.removeCells(inserted, true);
            return {
                failures: failures,
                details: details,
                captured: entries.length
            };
        }), data);
        fs.writeFileSync(path.join(out, theme + "-workbench-palette-" + copy + ".json"), JSON.stringify(result, null, 2));
        if (!process.env.AIDE_WORKBENCH_REVIEW_CATALOG) assert.deepEqual(result.failures, [], copy);
        checks.push({
            case: "workbench palette " + copy,
            theme: theme,
            result: result.failures.length ? "EXPECTED_FAIL" : "PASS"
        });
    }
    return checks;
};
