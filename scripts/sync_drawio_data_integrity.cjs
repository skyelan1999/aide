"use strict";

const fs = require("fs"), assert = require("assert/strict"), {assignment: assignment} = require("./drawio-editing-review.cjs"), r = require("./drawio-data-integrity-review.cjs"), tool = process.env.AIDE_TERSER_MODULE || "terser";

assert.equal(require(tool + "/package.json").version, "5.39.0");

(async () => {
    for (const file of [ "js/app.min.js", "js/integrate.min.js", "mxgraph/mxClient.js", "js/viewer.min.js", "js/viewer-static.min.js" ]) {
        const p = r.base + file;
        let text = fs.readFileSync(p, "utf8"), old = text, count = 0;
        for (const [n, source, name] of r.methods) {
            if (!text.includes(name + "=")) continue;
            const code = assignment(fs.readFileSync(r.base + source, "utf8"), name), target = assignment(text, name);
            const min = (await require(tool).minify(code + ";", {
                compress: false,
                mangle: true,
                format: {
                    comments: false
                }
            })).code.replace(/;$/, "");
            text = text.replace(target, (() => min));
            count++;
        }
        if (process.argv.includes("--check")) assert(text === old, file + " mismatched data functions"); else fs.writeFileSync(p, text);
        console.log("PASS data sync", file, count);
    }
})().catch((e => {
    console.error(e.message);
    process.exitCode = 1;
}));
