"use strict";

const fs = require("fs"), assert = require("assert/strict"), {assignment: assignment} = require("./drawio-editing-review.cjs"), r = require("./drawio-device-shape-review.cjs"), tool = process.env.AIDE_TERSER_MODULE || "terser";

assert.equal(require(tool + "/package.json").version, "5.39.0");

(async () => {
    for (const file of [ "js/shapes-14-6-5.min.js", "js/integrate.min.js", "js/viewer.min.js", "js/viewer-static.min.js" ]) {
        const p = r.base + file;
        let text = fs.readFileSync(p, "utf8"), old = text, count = 0;
        for (const row of r.methods) {
            const target = text.includes(row[2]+"=") ? {name:row[2],code:assignment(text,row[2])} : null;
            if (!target) continue;
            let source = assignment(fs.readFileSync(r.base + row[1], "utf8"), row[2]);
            if (target.name !== row[2]) source = source.replace(row[2], target.name);
            const min = (await require(tool).minify(source + ";", {
                compress: false,
                mangle: true,
                format: {
                    comments: false
                }
            })).code.replace(/;$/, "");
            text = text.replace(target.code, (() => min));
            count++;
        }
        if (process.argv.includes("--check")) assert(text === old, file + " device-shape methods differ"); else fs.writeFileSync(p, text);
        console.log("PASS device-shape sync", file, count);
    }
})().catch((e => {
    console.error(e.message);
    process.exitCode = 1;
}));
