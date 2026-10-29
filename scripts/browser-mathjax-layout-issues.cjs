"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), {chromium: chromium} = require(path.resolve("scripts/browser-runtime/node_modules/playwright"));

const root = path.resolve(process.env.AIDE_MATH_BASELINE_ROOT || "internal/server/web/vendor/drawio/math4/es5"), output = process.env.AIDE_MATH_OUTPUT || "svg";

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
    const errors = [];
    try {
        context = await browser.newContext();
        const page = await context.newPage();
        page.on("pageerror", (e => errors.push(e.message)));
        page.on("console", (m => console.log(m.text())));
        await context.route("**/*", (route => {
            const u = new URL(route.request().url());
            if (u.origin !== "https://aide-math-owned.test") return route.abort();
            if (u.pathname === "/") return route.fulfill({
                contentType: "text/html",
                body: "<!doctype html><html><body><main></main></body></html>"
            });
            const p = path.resolve(root, "." + decodeURIComponent(u.pathname));
            if (!p.startsWith(root + path.sep) || !fs.existsSync(p)) return route.fulfill({
                status: 404,
                body: ""
            });
            return route.fulfill({
                contentType: p.endsWith(".js") ? "application/javascript" : "application/octet-stream",
                body: fs.readFileSync(p)
            });
        }));
        await page.goto("https://aide-math-owned.test/");
        await page.evaluate((output => {
            window.MathJax = {
                loader: {
                    load: [ "input/tex", "output/" + output, "ui/safe" ],
                    paths: {
                        fonts: "https://aide-math-owned.test/fonts"
                    }
                },
                output: {
                    font: "mathjax-tex"
                },
                startup: {
                    typeset: false
                }
            };
        }), output);
        await page.addScriptTag({
            url: "https://aide-math-owned.test/startup.js"
        });
        await page.evaluate((() => Promise.race([ MathJax.startup.promise, new Promise(((_, reject) => setTimeout((() => reject(Error("owned startup timeout"))), 2e4))) ])));
        const results = await page.evaluate((async output => {
            const results = [], main = document.querySelector("main"), check = (b, m) => {
                if (!b) throw Error(m);
            }, convert = async (source, type = "tex") => {
                const node = await MathJax[type + "2" + output + "Promise"](source);
                main.replaceChildren(node);
                return node;
            }, merror = n => !!n.querySelector('[data-mml-node="merror"],mjx-merror');
            async function test(issue, fn) {
                console.log("math-layout", issue);
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
            const mod = MathJax._.output[output].Wrappers.menclose;
            console.log("menclose exports", Object.keys(mod));
            const klass = Object.values(mod).find((v => typeof v === "function" && typeof v.prototype?.arrowData === "function"));
            await test(408, (async () => {
                const proto = klass.prototype, original = proto.removeRedundantNotations;
                let notations;
                proto.removeRedundantNotations = function() {
                    const value = original.call(this);
                    notations = Object.keys(this.notations);
                    return value;
                };
                try {
                    await convert("\\require{enclose}\\enclose{downarrow verticalstrike}{x}");
                    check(notations.includes("downarrow") && !notations.includes("verticalstrike"), "downarrow removes redundant verticalstrike: " + notations);
                    await convert("\\enclose{downarrow}{x}");
                    return {
                        notations: notations
                    };
                } finally {
                    delete proto.removeRedundantNotations;
                }
            }));
            await test(409, (async () => {
                const value = klass.prototype.arrowData.call({
                    padding: .2,
                    thickness: .067,
                    arrowhead: {
                        x: 4,
                        y: 2,
                        dx: 1
                    },
                    childNodes: [ {
                        getBBox: () => ({
                            w: 0,
                            h: 0,
                            d: 0
                        })
                    } ],
                    getArgMod: (w, h) => [ Math.atan2(h, w), Math.hypot(h, w) ]
                });
                check(Object.values(value).every(Number.isFinite), "zero-box arrow geometry is finite");
                const node = await convert("\\require{enclose}\\enclose{updiagonalarrow}{}");
                check(!/NaN|Infinity/.test(node.outerHTML), "rendered empty arrow geometry finite");
                return value;
            }));
            await test(410, (async () => {
                const Original = window.Array;
                let allocations = 0;
                window.Array = new Proxy(Original, {
                    construct(target, args, newTarget) {
                        if (args.length === 1 && typeof args[0] === "number" && args[0] > 1e4) {
                            allocations++;
                            throw Error("owned column allocation budget");
                        }
                        return Reflect.construct(target, args, newTarget);
                    }
                });
                try {
                    const node = await convert("\\begin{array}{*{100000000}{c}}x\\end{array}");
                    check(merror(node), "oversized repeat reports controlled math error");
                    check(allocations === 0, "rejects before oversized allocation");
                    check(!merror(await convert("\\begin{array}{*{3}{c}}a&b&c\\end{array}")), "ordinary repeated columns");
                    check(!merror(await convert("\\begin{array}{*{2}{*{2}{c}}}a&b&c&d\\end{array}")), "nested ordinary repeats");
                    return {
                        oversizedAllocations: allocations
                    };
                } finally {
                    window.Array = Original;
                }
            }));
            await test(424, (async () => {
                const factory = MathJax.startup.document.mmlFactory, token = (kind, text) => factory.create(kind, {}, [ factory.create("text").setText(text) ]), base = token("mi", "x"), under = token("mi", "u"), over = token("mo", "⃐"), tree = factory.create("munderover", {}, [ base, under, over ]);
                tree.setInheritedAttributes({}, false, 0, false);
                check(over.getProperty("mathaccent") === true, "over child gets fallback mathaccent property");
                check(under.getProperty("mathaccent") !== true, "under child not misclassified");
                const node = await convert("\\require{unicode}\\overunderset{\\unicode{x20D0}}{u}{x}");
                check(!merror(node) && !/NaN|Infinity/.test(node.outerHTML), "accent formula renders finite");
                return {
                    accent: over.getProperty("mathaccent")
                };
            }));
            return results;
        }), output);
        fs.writeFileSync(path.join(out, "result.json"), JSON.stringify({
            output: output,
            results: results,
            errors: errors,
            browser: browser.version()
        }, null, 2));
        console.log(JSON.stringify(results));
        assert.deepEqual(errors, []);
        if (!process.env.AIDE_EXPECT_REVIEW_FAILURES) assert.deepEqual(results.filter((r => r.result !== "PASS")), []);
    } finally {
        if (context) await context.close();
        await browser.close();
    }
})().catch((e => {
    console.error(e.stack);
    process.exitCode = 1;
}));
