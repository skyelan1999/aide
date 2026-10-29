"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), {chromium: chromium} = require(path.resolve("scripts/browser-runtime/node_modules/playwright"));

const web = path.resolve("internal/server/web");

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
        const page = await context.newPage(), errors = [];
        page.on("pageerror", (e => errors.push(e.message)));
        await context.route("**/*", (route => {
            const u = new URL(route.request().url());
            if (u.origin !== "https://aide-markdown-owned.test") return route.abort();
            if (u.pathname === "/") return route.fulfill({
                contentType: "text/html",
                headers: {
                    "Content-Security-Policy": "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; font-src 'self' data:;"
                },
                body: '<!doctype html><html><head><meta charset="utf-8"><link rel="stylesheet" href="/host.css"></head><body><div class="workbench-aurora"></div><main id="host"></main></body></html>'
            });
            if (u.pathname === "/host.css") return route.fulfill({
                contentType: "text/css",
                body: "@keyframes workbench-flow-primary{from{opacity:.5}to{opacity:1}}.workbench-aurora{animation:workbench-flow-primary 1s linear paused;animation-delay:-.5s;width:20px;height:20px;}"
            });
            const p = path.resolve(web, "." + u.pathname);
            if (!p.startsWith(web + path.sep) || !fs.existsSync(p)) return route.fulfill({
                status: 404,
                body: ""
            });
            return route.fulfill({
                contentType: p.endsWith(".js") ? "application/javascript; charset=utf-8" : "application/octet-stream",
                body: fs.readFileSync(p)
            });
        }));
        await page.goto("https://aide-markdown-owned.test/");
        for (const url of [ "/vendor/mermaid.min.js", "/markdown-diagrams.js", "/markdown-export.js", "/vendor/markdown-export/html2canvas.min.js" ]) await page.addScriptTag({
            url: "https://aide-markdown-owned.test" + url
        });
        const results = await page.evaluate((async () => {
            const results = [], check = (x, m) => {
                if (!x) throw Error(m);
            };
            async function test(issue, fn) {
                try {
                    results.push({
                        issue: issue,
                        result: "PASS",
                        detail: await fn()
                    });
                } catch (e) {
                    results.push({
                        issue: issue,
                        result: "FAIL",
                        error: e.message,
                        stack: e.stack
                    });
                }
            }
            await test(401, (async () => {
                const host = document.querySelector("#host"), aurora = document.querySelector(".workbench-aurora"), before = getComputedStyle(aurora).opacity, descriptor = Object.getOwnPropertyDescriptor(Document.prototype, "adoptedStyleSheets");
                let globalRules = 0;
                const keyframes = rules => {
                    let count = 0;
                    for (const rule of rules) {
                        if (rule.type === CSSRule.KEYFRAMES_RULE) count++; else if (rule.cssRules) count += keyframes(rule.cssRules);
                    }
                    return count;
                };
                Object.defineProperty(document, "adoptedStyleSheets", {
                    configurable: true,
                    get() {
                        return descriptor.get.call(this);
                    },
                    set(value) {
                        for (const sheet of value) globalRules += keyframes(sheet.cssRules);
                        return descriptor.set.call(this, value);
                    }
                });
                try {
                    for (const css of [ "@keyframes workbench-flow-primary{from{opacity:0}to{opacity:0}}", "@media all{@keyframes owned-global{from{opacity:0}to{opacity:1}}}" ]) {
                        const node = document.createElement("div");
                        node.className = "mermaid";
                        node.textContent = "%%{init: " + JSON.stringify({
                            themeCSS: css
                        }) + "}%%\nflowchart LR\nA[Visible]--\x3eB\nclassDef ordinary fill:#123456\nclass A ordinary";
                        host.replaceChildren(node);
                        await AideMarkdownDiagrams.render(host, {
                            load: async () => {},
                            strict: true
                        });
                        check(node.querySelector("svg"), "actual Mermaid rendered");
                        check(!node.innerHTML.includes("@keyframes"), "serialized diagram cannot carry global animation definitions");
                        check(getComputedStyle(node.querySelector(".node rect")).fill === "rgb(18, 52, 86)", "scoped classDef styles retained");
                    }
                    check(globalRules === 0, "temporary and final adoption must exclude global keyframes: " + globalRules);
                    check(getComputedStyle(aurora).opacity === before, "host animation unchanged");
                    host.replaceChildren();
                    await new Promise((r => setTimeout(r, 30)));
                    check(document.adoptedStyleSheets.length === 0, "detached diagram sheets released");
                    return {
                        hostOpacity: before,
                        adoptedGlobalRules: globalRules
                    };
                } finally {
                    delete document.adoptedStyleSheets;
                    host.replaceChildren();
                }
            }));
            await test(402, (async () => {
                const real = window.html2canvas;
                let rasterCalls = 0;
                window.html2canvas = async () => {
                    rasterCalls++;
                    throw Error("owned rasterizer reached");
                };
                const options = html => ({
                    markdown: "owned",
                    html: html,
                    title: "owned.md",
                    valid: () => true,
                    load: async () => {}
                });
                try {
                    for (const row of [ {
                        html: "<div>".repeat(2e3) + "x" + "</div>".repeat(2e3)
                    }, {
                        html: "<span>x</span>".repeat(50001)
                    }, {
                        html: "<p>ordinary</p>",
                        renderDiagrams: async body => {
                            let parent = body;
                            for (let i = 0; i < 200; i++) {
                                const child = body.ownerDocument.createElement("div");
                                parent.append(child);
                                parent = child;
                            }
                        }
                    } ]) {
                        let error;
                        try {
                            await AideMarkdownExport.run("png", {
                                ...options(row.html),
                                renderDiagrams: row.renderDiagrams
                            });
                        } catch (e) {
                            error = e;
                        }
                        check(error && /结构|节点/.test(error.message), "controlled DOM budget rejection: " + error?.message);
                        check(rasterCalls === 0, "reject before recursive rasterizer");
                        check(document.querySelectorAll(".markdown-print-frame").length === 0, "failed export frames released");
                    }
                } finally {
                    window.html2canvas = real;
                }
                const click = HTMLAnchorElement.prototype.click, create = URL.createObjectURL;
                let name, blob;
                HTMLAnchorElement.prototype.click = function() {
                    name = this.download;
                };
                URL.createObjectURL = function(value) {
                    blob = value;
                    return create.call(this, value);
                };
                try {
                    await AideMarkdownExport.run("png", options("<h1>Owned export</h1><div><span>ordinary nested text</span></div>"));
                    check(name === "owned.png" && blob?.type === "image/png" && blob.size > 100, "ordinary export uses actual html2canvas and returns PNG");
                    check(document.querySelectorAll(".markdown-print-frame").length === 0, "successful export frames released");
                    return {
                        rejectedRasterCalls: rasterCalls,
                        pngBytes: blob.size
                    };
                } finally {
                    HTMLAnchorElement.prototype.click = click;
                    URL.createObjectURL = create;
                }
            }));
            return results;
        }));
        fs.writeFileSync(path.join(out, "result.json"), JSON.stringify({
            results: results,
            errors: errors,
            browser: browser.version()
        }, null, 2));
        console.log(JSON.stringify(results));
        if (!process.env.AIDE_EXPECT_REVIEW_FAILURES) assert.deepEqual(results.filter((r => r.result !== "PASS")), []);
        assert.deepEqual(errors, []);
    } finally {
        if (context) await context.close();
        await browser.close();
    }
})().catch((e => {
    console.error(e.stack);
    process.exitCode = 1;
}));
