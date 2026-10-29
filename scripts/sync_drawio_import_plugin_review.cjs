"use strict";

const fs = require("fs"), assert = require("assert/strict"), r = require("./drawio-import-plugin-review.cjs"), {atom: atom} = require("./drawio-editing-review.cjs"), tool = process.env.AIDE_TERSER_MODULE || "terser";

assert.equal(require(tool + "/package.json").version, "5.39.0");

(async () => {
    for (const file of [ "js/app.min.js", "js/integrate.min.js", "js/shapes-14-6-5.min.js", "mxgraph/mxClient.js", "js/viewer.min.js", "js/viewer-static.min.js", "js/orgchart.min.js", "js/extensions.min.js" ]) {
        const p = r.base + file;
        let text = fs.readFileSync(p, "utf8"), old = text, count = 0;
        for (const row of r.methods) {
            const target = r.target(text, row);
            if (!target) continue;
            const source = r.source(row);
            let code = (await require(tool).minify(source + ";", {
                compress: false,
                mangle: true,
                format: {
                    comments: false
                }
            })).code.replace(/;$/, "");
            if (target.property) code = target.property + ":" + atom(code, code.indexOf("function"));
            text = text.replace(target.code, (() => code));
            count++;
        }
        if (process.argv.includes("--check")) assert(text === old, file + " import/plugin methods differ"); else fs.writeFileSync(p, text);
        console.log("PASS import/plugin sync", file, count);
    }
})().catch((e => {
    console.error(e.message);
    process.exitCode = 1;
}));
