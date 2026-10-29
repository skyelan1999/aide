"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), {chromium: chromium} = require("./browser-runtime/node_modules/playwright");

const root = path.resolve("internal/server/web/vendor/drawio/math4/es5");

(async () => {
    const out = process.env.AIDE_COMPONENT_EVIDENCE_DIR;
    assert(out);
    fs.mkdirSync(out, {
        recursive: true
    });
    const browser = await chromium.launch({
        executablePath: "/usr/bin/chromium",
        headless: true,
        args: [ "--no-sandbox", "--disable-dev-shm-usage" ]
    });
    const errors = [], requests = [];
    let context;
    try {
        context = await browser.newContext();
        const page = await context.newPage();
        page.on("pageerror", (e => errors.push(e.message)));
        await context.route("**/*", (async route => {
            const u = new URL(route.request().url());
            requests.push(u.href);
            if (u.origin !== "https://aide-math-owned.test") return route.abort();
            if (u.pathname === "/") return route.fulfill({
                contentType: "text/html",
                body: '<!doctype html><html><head><meta charset="utf-8"></head><body><main id="formulas"></main></body></html>'
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
        await page.evaluate((() => window.MathJax = {
            loader: {
                load: [ "input/tex", "input/asciimath", "output/svg", "ui/safe", "[tex]/html" ],
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
        }));
        await page.addScriptTag({
            url: "https://aide-math-owned.test/startup.js"
        });
        await page.evaluate((() => MathJax.startup.promise));
        const result = await page.evaluate((async () => {
            const results = [];
            async function test(issue, fn) {
                try {
                    const detail = await fn();
                    results.push({
                        issue: issue,
                        result: "PASS",
                        detail: detail
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
            const main = document.querySelector("#formulas");
            function check(ok, msg) {
                if (!ok) throw Error(msg);
            }
            async function tex(s) {
                const node = await MathJax.tex2svgPromise(s);
                main.replaceChildren(node);
                return node;
            }
            for (const [issue, macro] of [ [ 392, "centernot" ], [ 394, "overbracket" ] ]) await test(issue, (async () => {
                const proto = MathJax._.input.tex.TexParser.default.prototype, old = proto.mml;
                let calls = 0;
                proto.mml = function() {
                    if (++calls > 2e4) throw Error("owned parse budget exceeded");
                    return old.apply(this, arguments);
                };
                try {
                    await tex("\\require{" + (issue === 392 ? "centernot" : "mathtools") + "}" + ("\\" + macro + "{").repeat(12) + "x" + "}".repeat(12));
                    check(calls < 2048, "nested parser work exceeds bounded budget:" + calls);
                    const normal = await tex("\\" + macro + "{x}");
                    check(!normal.querySelector('[data-mml-node="merror"]'), "ordinary formula still renders");
                    const boundary = await tex(("\\" + macro + "{").repeat(8) + "x" + "}".repeat(8));
                    check(!boundary.querySelector('[data-mml-node="merror"]'), "eight-level supported boundary");
                    return {
                        calls: calls
                    };
                } finally {
                    proto.mml = old;
                }
            }));
            await test(393, (async () => {
                let phase = "oversized";
                try {
                    const text = "\\require{bussproofs}\\begin{prooftree}\\AxiomC{x}" + "\\UnaryInfC{x}".repeat(300) + "\\end{prooftree}", node = await tex(text);
                    check(!!node.querySelector('[data-mml-node="merror"]'), "oversized proof should fail before quadratic balance pass");
                    phase = "ordinary";
                    check(!(await tex("\\begin{prooftree}\\AxiomC{x}\\UnaryInfC{x}\\end{prooftree}")).querySelector('[data-mml-node="merror"]'), "ordinary proof retained");
                    phase = "boundary";
                    const boundary = await tex("\\begin{prooftree}\\AxiomC{x}" + "\\UnaryInfC{x}".repeat(64) + "\\end{prooftree}");
                    check(!boundary.querySelector('[data-mml-node="merror"]'), "64-inference supported boundary");
                } catch (e) {
                    throw Error(phase + ": " + e.message);
                }
            }));
            await test(395, (async () => {
                const node = await tex("\\require{mhchem}" + "\\ce{".repeat(80) + "H2O" + "}".repeat(80));
                check(!!node.querySelector('[data-mml-node="merror"]'), "deep chemistry should be bounded before recursive scans");
                check(!(await tex("\\ce{2H2 + O2 -> 2H2O}")).querySelector('[data-mml-node="merror"]'), "ordinary chemical equation");
            }));
            await test(396, (async () => {
                const node = await MathJax.asciimath2svgPromise("id(owned-collision)(x)+class(owned-style)(y)");
                main.replaceChildren(node);
                check(!node.querySelector("#owned-collision,.owned-style"), "untrusted AsciiMath attributes escaped safe filter");
                const allowed = await MathJax.asciimath2svgPromise("id(mjx-owned)(x)");
                check(!!allowed.querySelector("#mjx-owned"), "Safe allowlisted ID remains");
                const document = MathJax.startup.document, old = document.safe.filterMethods.filterClassList;
                document.safe.filterMethods.filterClassList = () => {
                    throw Error("owned filter failure");
                };
                try {
                    const rejected = await MathJax.asciimath2svgPromise("class(owned-style)(x)");
                    check(!!rejected.querySelector('[data-mml-node="merror"]'), "filter failure returns compile-error root");
                    check(!rejected.querySelector(".owned-style"), "filter failure cannot return unfiltered root");
                } finally {
                    document.safe.filterMethods.filterClassList = old;
                }
            }));
            await test(397, (async () => {
                await tex("\\require{physics}\\mqty{\\imat{3}}");
                const Original = window.Array;
                let oversized = 0;
                window.Array = new Proxy(Original, {
                    apply(target, self, args) {
                        if (args.length === 1 && typeof args[0] === "number" && args[0] > 64) {
                            oversized++;
                            throw Error("owned matrix allocation budget exceeded");
                        }
                        return Reflect.apply(target, self, args);
                    }
                });
                try {
                    const node = await tex("\\require{physics}\\mqty{\\imat{70}}");
                    check(!!node.querySelector('[data-mml-node="merror"]'), "oversized matrix rejected");
                    const normal = await tex("\\mqty{\\imat{3}}");
                    check(!normal.querySelector('[data-mml-node="merror"]'), "normal identity matrix retained");
                    const x = await tex("\\mqty{\\xmat{a}{70}{70}}");
                    check(!!x.querySelector('[data-mml-node="merror"]'), "placeholder dimensions rejected");
                    check(oversized === 0, "oversized arrays were constructed");
                } finally {
                    window.Array = Original;
                }
            }));
            return results;
        }));
        fs.writeFileSync(path.join(out, "result.json"), JSON.stringify({
            results: result,
            errors: errors,
            requests: requests,
            browser: browser.version(),
            scope: "Actual localMathJax4.1.3 startup,TeX/AsciiMath,Safe,SVG;isolated page,not fullworkbench"
        }, null, 2));
        console.log(JSON.stringify(result));
        await page.screenshot({
            path: path.join(out, "formulas.png")
        });
        if (!process.env.AIDE_EXPECT_REVIEW_FAILURES) assert.deepEqual(result.filter((x => x.result !== "PASS")), []);
        assert.deepEqual(errors, []);
    } finally {
        await (context?.close());
        await browser.close();
    }
})().catch((e => {
    console.error(e.stack);
    process.exitCode = 1;
}));
