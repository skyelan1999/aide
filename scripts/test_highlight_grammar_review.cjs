"use strict";

const fs = require("fs"), vm = require("vm"), assert = require("assert/strict"), review = require("./highlight-grammar-review-cases.cjs");

const base = "internal/server/web/vendor/highlight.js/";

function runtime(mode, standalone = true) {
    const root = {
        console: console
    };
    vm.createContext(root);
    let source = fs.readFileSync(base + "highlight.min.js", "utf8");
    if (mode === "source") source = source.split("/* AIDE grammar repairs:")[0];
    vm.runInContext(source, root);
    if (mode === "source" && fs.existsSync("internal/server/web/highlight-grammar-fixes.js")) vm.runInContext(fs.readFileSync("internal/server/web/highlight-grammar-fixes.js", "utf8"), root);
    if (standalone) for (const lang of new Set([ "javascript", "typescript", ...review.cases.map((c => c.language)) ])) vm.runInContext(fs.readFileSync(base + "languages/" + lang + ".min.js", "utf8"), root);
    return root;
}

function main() {
    for (const mode of [ "source-core", "bundle-core", "source", "bundle" ]) {
        const root = runtime(mode.split("-")[0], !mode.endsWith("-core")), errors = [];
        for (const c of review.cases) {
            if (!root.hljs.getLanguage(c.language)) continue;
            const result = root.hljs.highlight(c.code, {
                language: c.language,
                ignoreIllegals: true
            });
            assert.equal(review.spans(result).map((s => s.text)).join(""), c.code);
            if (review.covered(result, c.code, c.marker, c.scope) === !!c.exclude) errors.push(c.issue + ": " + c.language + " missing " + c.scope + " for " + c.marker);
        }
        vm.runInContext(`globalThis.ownedSuffixWork=0;const ownedIndexOf=String.prototype.indexOf;String.prototype.indexOf=function(needle,from=0){if(typeof needle==='string'&&needle.startsWith('</'))ownedSuffixWork+=Math.max(0,this.length-from);return ownedIndexOf.call(this,needle,from);}`, root);
        for (const lang of [ "javascript", "typescript" ]) {
            if (!root.hljs.getLanguage(lang)) continue;
            root.ownedSuffixWork = 0;
            const input = "return " + "<A>".repeat(2e3);
            root.hljs.highlight(input, {
                language: lang,
                ignoreIllegals: true
            });
            if (root.ownedSuffixWork > input.length * 10) errors.push("268 " + lang + " suffix work " + root.ownedSuffixWork);
        }
        console.log(JSON.stringify({
            mode: mode,
            errors: errors
        }));
        if (errors.length && !process.env.AIDE_HIGHLIGHT_CATALOG) throw Error("grammar acceptance failed");
    }
}

module.exports = {
    runtime: runtime
};

if (require.main === module) try {
    main();
} catch (e) {
    console.error(e.message);
    process.exitCode = 1;
}
