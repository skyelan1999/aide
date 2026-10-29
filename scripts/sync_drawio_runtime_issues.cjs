"use strict";

const fs = require("node:fs"), assert = require("node:assert/strict"), r = require("./drawio-runtime-issue-review.cjs"), {assignment: assignment} = require("./test_drawio_layout_fixes.cjs");

const terser = require(process.env.AIDE_TERSER_MODULE || "terser");

assert.equal(require((process.env.AIDE_TERSER_MODULE || "terser") + "/package.json").version, "5.39.0");

async function compact(code) {
    return (await terser.minify(code, {
        compress: false,
        mangle: true,
        format: {
            comments: false
        }
    })).code;
}

async function main() {
    for (const file of [ "app.min.js", "integrate.min.js", "../mxgraph/mxClient.js" ]) {
        const path = r.base + file;
        let s = fs.readFileSync(path, "utf8"), original = s;
        for (const [f, name] of r.methods) {
            if (!s.includes(name + "=")) continue;
            const old = assignment(s, name), code = await compact(assignment(fs.readFileSync(r.base + f, "utf8"), name));
            s = s.replace(old.slice(0, -1), code.slice(0, -1));
        }
        if (file !== "../mxgraph/mxClient.js") {
            const source = fs.readFileSync(r.base + "grapheditor/Shapes.js", "utf8");
            for (const name of [ "moveNEInner", "lineNEInner" ]) {
                const old = r.part(s, ".prototype." + name + "="), prefix = old.slice(0, old.indexOf(".prototype"));
                const code = await compact(r.part(source, ".prototype." + name + "=").replace(/^[\w$]+\.prototype/, prefix + ".prototype"));
                s = s.replace(old.slice(0, -1), code.slice(0, -1));
            }
            const old = r.edgeFunction(s), handleName = /return\s+([\w$]+)\(/.exec(old)[1];
            let actual = old;
            if (!s.includes(actual)) actual = actual.replace("function OwnedEdge(", "function(");
            assert(s.includes(actual));
            const code = await compact(r.edgeFunction(source).replaceAll("createHandle(", handleName + "("));
            s = s.replace(actual, code.replace(/;$/, ""));
        }
        const old = r.codec(s), prefix = old.slice(0, old.indexOf(".encodeCell"));
        const code = await compact(r.codec(fs.readFileSync(r.base + "../mxgraph/src/io/mxGraphViewCodec.js", "utf8")).replace(/^codec\.encodeCell/, prefix + ".encodeCell"));
        s = s.replace(old.slice(0, -1), code.slice(0, -1));
        if (process.argv.includes("--check")) assert.equal(s === original, true, file + " runtime assignments differ"); else fs.writeFileSync(path, s);
        console.log("PASS " + file + " runtime sync");
    }
}

main().catch((e => {
    console.error(e.message);
    process.exitCode = 1;
}));
