"use strict";

const fs = require("fs"), vm = require("vm"), assert = require("assert/strict");

const file = "internal/server/web/vendor/drawio/js/plantuml/drawio-plantuml.min.js", code = fs.readFileSync(file, "utf8"), cases = require("./fixtures/plantuml-issue-cases.json");

function parse(source, config) {
    const c = {};
    vm.createContext(c, {
        codeGeneration: {
            strings: false,
            wasm: false
        }
    });
    vm.runInContext(code, c);
    const before = vm.runInContext("JSON.stringify([Object.getOwnPropertyNames(Object.prototype).sort(),Object.getOwnPropertyNames(Object).sort()])", c);
    const xml = c.mxPlantUmlToDrawio.parseText(source, config), cells = Array.from(xml.matchAll(/<mxCell\b([^>]*)>/g), (m => {
        const attrs = {};
        for (const a of m[1].matchAll(/([\w:-]+)="([^"]*)"/g)) attrs[a[1]] = a[2];
        return attrs;
    }));
    const after = vm.runInContext("JSON.stringify([Object.getOwnPropertyNames(Object.prototype).sort(),Object.getOwnPropertyNames(Object).sort()])", c);
    assert.equal(after, before, "converter must not add properties to native Object/prototype");
    return {
        xml: xml,
        cells: cells,
        warnings: Array.from(c.mxPlantUmlToDrawio.lastWarnings || []),
        context: c
    };
}

function integrity(result) {
    const ids = new Set;
    for (const cell of result.cells) {
        assert(!ids.has(cell.id), "unique cell IDs");
        ids.add(cell.id);
    }
    for (const cell of result.cells) for (const key of [ "source", "target" ]) if (cell[key] != null) assert(ids.has(cell[key]), "edge " + key + " must refer to a generated cell:" + cell[key]);
    for (const m of result.xml.matchAll(/<(?:mxGeometry|mxPoint)\b([^>]*)>/g)) for (const a of m[1].matchAll(/\b(x|y|width|height)="([^"]*)"/g)) assert(Number.isFinite(Number(a[2])), "finite " + a[1] + ":" + a[2]);
}

function style(cell, key) {
    const m = new RegExp("(?:^|;)" + key + "=([^;]*)").exec(cell.style || "");
    return m?.[1];
}

const smokeSources = [ "@startmindmap\n* Root\n** Child\n@endmindmap", '@startjson\n{"name":"owned","hasOwnProperty":"value"}\n@endjson', "@startyaml\nname: owned\nitems:\n  - one\n@endyaml", '@startuml\n(*) --\x3e "Work"\n--\x3e (*)\n@enduml', "@startuml\n!startsub hasOwnProperty\nAlice -> Bob\n!endsub\n!includesub hasOwnProperty\n@enduml" ];

for (const source of smokeSources) {
    const result = parse(source);
    integrity(result);
    assert(result.cells.some((c => c.vertex === "1")), "ordinary parser/subpart smoke");
}

const results = [];

for (const row of cases) {
    try {
        const result = parse(row.source);
        integrity(result);
        const vertices = result.cells.filter((c => c.vertex === "1")), edges = result.cells.filter((c => c.edge === "1"));
        if ([ 435, 444, 468, 475, 476, 478, 479, 481, 482, 483, 485, 488, 490, 491, 492, 494, 495, 496 ].includes(row.issue)) {
            const control = parse(row.source.replace(/hasOwnProperty|__proto__|constructor/g, "OwnedOrdinary"));
            integrity(control);
            assert.equal(vertices.length, control.cells.filter((c => c.vertex === "1")).length, "declared vertices preserved");
            assert.equal(edges.length, control.cells.filter((c => c.edge === "1")).length, "relationships preserved");
        }
        if (row.issue === 470) {
            assert.equal(style(edges[0], "fontColor")?.toLowerCase(), "#eaeaea", "Hacker arrow label uses defined foreground");
            const light = parse(row.source.replace('!$BGCOLOR = "#111111"\n', ""));
            assert(light.cells.filter((c => c.edge === "1")).every((c => style(c, "fontColor")?.toLowerCase() === "#151515")), "transparent/light background remains readable");
            const custom = parse(row.source.replace("!theme hacker", '!$FGCOLOR = "#123456"\n!theme hacker'));
            assert(custom.cells.filter((c => c.edge === "1")).every((c => style(c, "fontColor")?.toLowerCase() === "#123456")), "explicit foreground preserved");
        }
        if (row.issue === 471) {
            assert(/background="#[0-9a-f]{6}"/i.test(result.xml), "Mars background is valid CSS hex");
            for (const c of vertices) for (const k of [ "fillColor", "fontColor", "strokeColor" ]) assert(/^#[0-9a-f]{3,8}$/i.test(style(c, k) || ""), "Mars " + k + " is valid hex");
        }
        if (row.issue === 472) {
            assert(vertices.some((c => style(c, "fontFamily") === "Courier New")), "Mono preserves explicitFNT");
            const ordinary = parse(row.source.replace('!$FNT = "Courier New"\n', ""));
            assert(ordinary.cells.some((c => style(c, "fontFamily") === "Hack")), "default Mono font retained");
        }
        if (row.issue === 473) assert.equal(result.warnings.length, 0, "theme internals are not user-input warnings");
        if (row.issue === 477) {
            const alice = vertices.find((c => c.value === "Alice"));
            assert(alice);
            assert.equal(style(alice, "fillColor")?.toUpperCase(), "#F1F1F1", "absent stereotype cannot match inherited constructor");
            const matched = parse(row.source.replace("class Alice", "class Alice <<constructor>>"));
            assert(matched.cells.some((c => style(c, "fillColor")?.toUpperCase() === "#FF0000")), "explicit stereotype still matches");
        }
        if (row.issue === 484) {
            assert.equal(edges.length, 2);
            assert(!edges.some((c => /(?:entry|exit)[XY]=0\.(?:32|68)(?:;|$)/.test(c.style || ""))), "unrelated component pairs must not get parallel-edge offsets");
        }
        results.push({
            issue: row.issue,
            result: "PASS",
            vertices: vertices.length,
            edges: edges.length,
            warnings: result.warnings.length
        });
    } catch (e) {
        results.push({
            issue: row.issue,
            result: "FAIL",
            error: e.message
        });
    }
}

console.log(JSON.stringify(results));

if (!process.env.AIDE_EXPECT_REVIEW_FAILURES) assert.deepEqual(results.filter((x => x.result !== "PASS")), []);
