"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), cases = require("./fixtures/mermaid-native-issue-cases.json");

module.exports = async (frame, out, theme) => {
    const checks = [];
    for (const row of cases) {
        console.log("mermaid-native",theme,row.issue);
        const result = await frame.evaluate((async row => {
            const xml = await new Promise(((resolve, reject) => ownedUi.parseMermaidDiagram(row.source, {}, ((xml, warnings) => resolve({
                xml: xml,
                warnings: warnings
            })), reject)));
            if (!EditorUi.isMermaidSupported()) throw Error("native converter not loaded");
            const doc = mxUtils.parseXml(xml.xml);
            if (doc.getElementsByTagName("parsererror").length) throw Error("invalid generated XML");
            const container = document.createElement("div");
            container.style.cssText = "position:fixed;left:0;top:160px;width:100vw;max-width:1100px;height:600px;z-index:9999;";
            container.dataset.ownedMermaid = String(row.issue);
            document.body.append(container);
            const graph = new Graph(container);
            graph.setEnabled(false);
            try {
                const background = doc.documentElement.getAttribute("background") || "#fff";
                container.style.backgroundColor = background;
                new mxCodec(doc).decode(doc.documentElement, graph.model);
                graph.view.validate();
                graph.fit(15);
                const all = Object.values(graph.model.cells), vertices = all.filter((c => c.vertex)), edges = all.filter((c => c.edge));
                for (const cell of all) {
                    const g = cell.geometry;
                    if (g && ![ "x", "y", "width", "height" ].every((k => Number.isFinite(g[k])))) throw Error("nonfinite imported geometry");
                    if (cell.edge) for (const terminal of [ cell.source, cell.target ]) if (terminal && !graph.model.contains(terminal)) throw Error("imported dangling terminal");
                }
                const svg = container.querySelector("svg");
                if (!svg) throw Error("SVG viewport missing");
                for (const p of svg.querySelectorAll("path")) if (/NaN|Infinity/.test(p.getAttribute("d") || "")) throw Error("nonfinite rendered path");
                return {
                    vertices: vertices.length,
                    edges: edges.length,
                    warnings: xml.warnings?.length || 0,
                    native: true,
                    svg: !!svg
                };
            } finally {
                graph.destroy();
                container.remove();
            }
        }), row);
        assert(result.native && result.svg);
        
        checks.push({
            issue: row.issue,
            case: "actual Mermaid native conversion and import",
            theme: theme,
            result: "PASS",
            ...result
        });
    }
    fs.writeFileSync(path.join(out, theme + "-mermaid-native-imports.json"), JSON.stringify(checks, null, 2));
    return checks;
};
