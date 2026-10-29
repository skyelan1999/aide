"use strict";

const fs = require("fs"), assert = require("assert/strict"), crypto = require("crypto"), {atom: atom, assignment: assignment} = require("./drawio-editing-review.cjs");

const base = "internal/server/web/vendor/drawio/", vendor = base + "js/re2js/re2js.umd.js", tool = process.env.AIDE_TERSER_MODULE || "terser";

const begin = "/* AIDE_LINEAR_REGEX_BEGIN re2js-2.9.1 */\n", end = "/* AIDE_LINEAR_REGEX_END */\n";

function strip(text) {
    if (!text.startsWith("/* AIDE_LINEAR_REGEX_BEGIN ")) return text;
    const at = text.indexOf(end);
    assert(at >= 0, "incomplete owned regex prefix");
    return text.slice(at + end.length);
}

async function main() {
    assert.equal(require(tool + "/package.json").version, "5.39.0");
    const upstream = fs.readFileSync(vendor, "utf8");
    assert.equal(crypto.createHash("sha256").update(upstream).digest("hex"), "c826d974feef561e90d526ec78d9ac57f418040a49b7b2b5d862f69ebddd3b2f", "review upstream before upgrading");
    const adapter = fs.readFileSync("internal/server/web/drawio-linear-regex.js", "utf8"), factory = atom(adapter, adapter.indexOf("function createDrawioLinearRegex"));
    const license = fs.readFileSync(base + "js/re2js/LICENSE", "utf8");
    const buildId = crypto.createHash("sha256").update(JSON.stringify([upstream, factory, license])).digest("hex");
    const source = "/*! RE2JS 2.9.1\n" + license + '\n*/\n;(function(root){if(root.AideDrawioLinearRegex&&root.AideDrawioLinearRegex.buildId===' + JSON.stringify(buildId) + ')return;var module={exports:{}},exports=module.exports;\n' + upstream + "\nroot.AideDrawioLinearRegex=(" + factory + ")(exports.RE2JS," + JSON.stringify(buildId) + ");})(globalThis);";
    const compiled = (await require(tool).minify(source, {
        compress: false,
        mangle: true,
        format: {
            comments: /^!/
        }
    })).code + "\n";
    const prefix = begin + compiled + end;
    const standalone = base + "js/re2js-safe.min.js";
    if (process.argv.includes("--check")) assert(fs.readFileSync(standalone, "utf8") === prefix, "standalone regex differs"); else fs.writeFileSync(standalone, prefix);
    const method = "Editor.prototype.isCorsEnabledForUrl", canonical = assignment(fs.readFileSync(base + "js/diagramly/Editor.js", "utf8"), method), code = (await require(tool).minify(canonical + ";", {
        compress: false,
        mangle: true,
        format: {
            comments: false
        }
    })).code.replace(/;$/, "");
    for (const file of [ "app.min.js", "integrate.min.js", "viewer.min.js", "viewer-static.min.js" ]) {
        const p = base + "js/" + file, old = fs.readFileSync(p, "utf8");
        let body = strip(old);
        body = body.replace(assignment(body, method), (() => code));
        const output = prefix + body;
        if (process.argv.includes("--check")) assert(output === old, file + " linear CORS differs"); else fs.writeFileSync(p, output);
        console.log("PASS linear CORS sync", file);
    }
    console.log("regex bootstrap bytes", Buffer.byteLength(prefix));
}

module.exports = {
    strip: strip,
    begin: begin,
    end: end
};

if (require.main === module) main().catch((e => {
    console.error(e.message);
    process.exitCode = 1;
}));
