"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), {chromium: chromium} = require("./browser-runtime/node_modules/playwright"), review = require("./highlight-grammar-review-cases.cjs");

async function main() {
    const out = process.env.AIDE_COMPONENT_EVIDENCE_DIR;
    assert(out);
    fs.mkdirSync(out, {
        recursive: true
    });
    const web = path.resolve("internal/server/web"), html = fs.readFileSync(web + "/index.html", "utf8");
    const scripts = [ ...html.matchAll(/<script[^>]*src="([^"?]+)(?:\?[^" ]*)?"/g) ].map((m => m[1])).filter((p => p.startsWith("/vendor/highlight.js/")));
    const styles = [ ...html.matchAll(/<link[^>]*href="([^"?]+)(?:\?[^" ]*)?"/g) ].map((m => m[1])).filter((p => p.includes("highlight") || p.endsWith("/tokens.css") || p === "/style.css"));
    const browser = await chromium.launch({
        executablePath: "/usr/bin/chromium",
        args: [ "--no-sandbox", "--disable-dev-shm-usage" ]
    });
    const context = await browser.newContext(), page = await context.newPage(), errors = [];
    page.on("pageerror", (e => errors.push(e.message)));
    page.on("console", (m => {
        if (m.type() === "error" && m.text().includes("Language definition")) errors.push(m.text());
    }));
    try {
        await context.route("**/*", (route => {
            const url = new URL(route.request().url());
            if (url.origin !== "https://aide-highlight-owned.test") return route.abort();
            if (url.pathname === "/") return route.fulfill({
                contentType: "text/html",
                body: "<!doctype html>" + styles.map((s => '<link rel="stylesheet" href="' + s + '">')).join("") + '<style>body{display:block;overflow:auto}main{padding:14px}article{margin-bottom:16px}pre{white-space:pre-wrap;overflow-wrap:anywhere}textarea{height:100px;width:100%}.code-wrapper{height:130px}</style><main id="markdown"></main><div id="editor"></div>'
            });
            if (url.pathname === "/owned-base-hljs.js") return route.fulfill({
                contentType: "application/javascript",
                body: fs.readFileSync(web + "/vendor/highlight.js/highlight.min.js", "utf8").split("/* AIDE grammar repairs:")[0]
            });
            const p = path.resolve(web, "." + url.pathname);
            if (!p.startsWith(web + "/") || !fs.existsSync(p)) return route.fulfill({
                status: 404,
                body: ""
            });
            return route.fulfill({
                contentType: p.endsWith(".js") ? "application/javascript" : "text/css",
                body: fs.readFileSync(p)
            });
        }));
        const results = [];
        for (const variant of [ "source", "bundle" ]) {
            await page.goto("https://aide-highlight-owned.test/");
            for (const script of scripts) {
                const core = script.endsWith("/highlight.min.js");
                await page.addScriptTag({
                    url: variant === "source" && core ? "/owned-base-hljs.js" : script
                });
                if (core && variant === "source") await page.addScriptTag({
                    url: "/highlight-grammar-fixes.js"
                });
            }
            await page.addScriptTag({
                url: "/markdown-code.js"
            });
            await page.addScriptTag({
                url: "/text-editor.js"
            });
            for (const [theme, width] of [ [ "light", 1440 ], [ "dark", 390 ] ]) {
                await page.setViewportSize({
                    width: width,
                    height: 1e3
                });
                const result = await page.evaluate((async ({cases: cases, theme: theme}) => {
                    document.documentElement.dataset.theme = theme;
                    const failures = [], main = document.querySelector("#markdown");
                    main.replaceChildren();
                    const covered = (element, code, marker, scope) => {
                        const start = code.indexOf(marker), end = start + marker.length;
                        let at = 0, found = 0, valid = true;
                        const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
                        for (let node; node = walker.nextNode(); ) {
                            const next = at + node.textContent.length;
                            if (next > start && at < end) {
                                found++;
                                if (!node.parentElement.closest(".hljs-" + scope)) valid = false;
                            }
                            at = next;
                        }
                        return found > 0 && valid;
                    };
                    for (const c of cases) {
                        const article = document.createElement("article"), title = document.createElement("strong"), pre = document.createElement("pre"), code = document.createElement("code");
                        title.textContent = "#" + c.issue + " " + c.language;
                        code.className = "language-" + c.language;
                        code.textContent = c.code;
                        pre.append(code);
                        article.append(title, pre);
                        main.append(article);
                    }
                    AideMarkdownCode.decorate(main);
                    const codes = [ ...main.querySelectorAll("pre > code") ];
                    for (let i = 0; i < cases.length; i++) {
                        const c = cases[i], element = codes[i];
                        if (element.textContent !== c.code || covered(element, c.code, c.marker, c.scope) === !!c.exclude) failures.push("markdown " + c.issue + " " + c.language + " " + c.marker);
                    }
                    const host = document.querySelector("#editor");
                    for (const c of cases) {
                        host.replaceChildren();
                        const textarea = document.createElement("textarea");
                        host.append(textarea);
                        textarea.value = c.code;
                        AideTextEditor.mount(textarea, c.language);
                        await new Promise((r => requestAnimationFrame((() => requestAnimationFrame(r)))));
                        const code = host.querySelector(".code-highlight-overlay code");
                        if (!code || code.textContent !== c.code + "\n" || covered(code, c.code, c.marker, c.scope) === !!c.exclude) failures.push("editor " + c.issue + " " + c.language + " " + c.marker);
                        AideTextEditor.dispose(textarea);
                    }
                    host.replaceChildren();
                    for (const article of [ ...main.children ]) if (Number(article.querySelector("strong").textContent.match(/\d+/)[0]) > 276) article.remove();
                    return {
                        failures: failures,
                        cases: cases.length,
                        markdownCases: codes.length
                    };
                }), {
                    cases: review.cases,
                    theme: theme
                });
                await page.screenshot({
                    path: path.join(out, theme + "-" + variant + ".png"),
                    fullPage: true
                });
                fs.writeFileSync(path.join(out, theme + "-" + variant + ".json"), JSON.stringify(result, null, 2));
                assert.deepEqual(result.failures, [], variant + " " + theme);
                results.push({
                    variant: variant,
                    theme: theme,
                    width: width,
                    cases: result.cases,
                    markdown: result.markdownCases,
                    result: "PASS"
                });
            }
        }
        fs.writeFileSync(path.join(out, "result.json"), JSON.stringify({
            results: results,
            errors: errors
        }, null, 2));
        assert.deepEqual(errors, []);
        console.log(JSON.stringify({
            results: results,
            errors: errors
        }));
    } finally {
        await context.close();
        await browser.close();
    }
}

main().catch((e => {
    console.error(e.stack);
    process.exitCode = 1;
}));
