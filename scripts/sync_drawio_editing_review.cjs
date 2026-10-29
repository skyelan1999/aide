"use strict";

const fs = require("fs"), assert = require("assert/strict"), r = require("./drawio-editing-review.cjs");

const tool = process.env.AIDE_TERSER_MODULE || "terser";

assert.equal(require(tool + "/package.json").version, "5.39.0");

const terser = require(tool);

async function compact(code) {
    return (await terser.minify(code + ";", {
        compress: false,
        mangle: true,
        format: {
            comments: false
        }
    })).code.replace(/;$/, "");
}

async function functionCode(code) {
    const text = await compact("var ownedFunction=" + code);
    return r.atom(text, text.indexOf("function"));
}

async function main() {
    const sources = new Map;
    for (const [number, file, name, which] of r.methods) sources.set(number, r.assignment(fs.readFileSync(r.base + file, "utf8"), name, which));
    const route = r.route(fs.readFileSync(r.base + "mxgraph/src/view/mxEdgeStyle.js", "utf8"));
    for (const file of [ "js/app.min.js", "js/integrate.min.js", "mxgraph/mxClient.js", "js/viewer.min.js", "js/viewer-static.min.js" ]) {
        const p = r.base + file, original = fs.readFileSync(p, "utf8");
        let output = original, count = 0;
        for (const [number, _source, name, which] of r.methods) {
            if (!output.includes(name + "=")) continue;
            const old = r.assignment(output, name, which);
            if (!old) continue;
            let source = sources.get(number);
            if (which === "wrapper") {
                const match = /[\w$]+\s*=\s*([\w$]+)\.apply\(this,\s*arguments\)/.exec(old);
                assert(match, "marker alias");
                source = source.replaceAll("mxConnectionHandlerCreateMarker", match[1]);
            }
            const code = await compact(source);
            output = output.replace(old, code);
            count++;
        }
        if (output.includes("getRoutePattern:")) {
            output = output.replace(r.route(output), await functionCode(route));
            count++;
        }
        if (process.argv.includes("--check")) assert(output === original, file + " editing functions differ"); else fs.writeFileSync(p, output);
        console.log("PASS editing sync " + file + " " + count + " functions");
    }
}

main().catch((e => {
    console.error(e.message);
    process.exitCode = 1;
}));
