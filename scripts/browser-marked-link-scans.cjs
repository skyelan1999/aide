"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), {chromium: chromium} = require("./browser-runtime/node_modules/playwright");

(async () => {
    const out = process.env.AIDE_COMPONENT_EVIDENCE_DIR;
    assert(out);
    fs.mkdirSync(out, {
        recursive: true
    });
    const browser = await chromium.launch({
        executablePath: "/usr/bin/chromium",
        args: [ "--no-sandbox", "--disable-dev-shm-usage" ]
    });
    let context;
    try {
        context = await browser.newContext();
        const page = await context.newPage();
        await page.setContent('<!doctype html><meta charset="utf-8"><main></main>');
        await page.addScriptTag({
            path: "internal/server/web/vendor/marked.min.js"
        });
        const result = await page.evaluate((rows => {
            for (const row of rows) if (marked.parse(row.markdown, row.options) !== row.html) throw Error("ordinary browser Markdown output changed");
            const rules = marked.Lexer.rules.inline.normal;
            if (!rules.reflinkSearch.source.includes("999")) throw Error("bounded reference scanner missing");
            const timings = [];
            for (const [issue, rule, input] of [ [ 420, rules.reflinkSearch, "[" + "\t\\[".repeat(6e4) + "[" ], [ 430, rules.link, "[](" + " ".repeat(1e5) ] ]) {
                const start = performance.now(), matched = new RegExp(rule.source, rule.flags).test(input);
                if (matched) throw Error("invalid link must remain text");
                timings.push({
                    issue: issue,
                    ms: performance.now() - start
                });
            }
            document.querySelector("main").innerHTML = marked.parse("# Owned Markdown\n\n[empty]() and [reference][r]\n\n[r]: /owned");
            if (document.querySelectorAll("main a").length !== 2) throw Error("actual browser links missing");
            return {
                controls: rows.length,
                timings: timings,
                links: 2
            };
        }), JSON.parse(fs.readFileSync("scripts/fixtures/marked-link-controls.json", "utf8")));
        fs.writeFileSync(path.join(out, "result.json"), JSON.stringify({
            result: result,
            browser: browser.version()
        }, null, 2));
        console.log(JSON.stringify(result));
    } finally {
        if (context) await context.close();
        await browser.close();
    }
})().catch((e => {
    console.error(e.stack);
    process.exitCode = 1;
}));
