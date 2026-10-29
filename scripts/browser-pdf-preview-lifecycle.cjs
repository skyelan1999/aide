"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), {chromium: chromium} = require("./browser-runtime/node_modules/playwright");

const web = path.resolve("internal/server/web"), app = fs.readFileSync(web + "/app.js", "utf8"), setup = app.slice(app.indexOf("async function setupPdfPreview("), app.indexOf("/* ── DXF 矢量渲染器"));

function pdf() {
    const bodies = [ "<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Count 3 /Kids [3 0 R 4 0 R 5 0 R] >>", ...Array(3).fill("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> >>") ];
    let s = "%PDF-1.4\n", offset = [ 0 ];
    for (let i = 0; i < bodies.length; i++) {
        offset.push(s.length);
        s += i + 1 + " 0 obj\n" + bodies[i] + "\nendobj\n";
    }
    const x = s.length;
    s += "xref\n0 " + (bodies.length + 1) + "\n0000000000 65535 f \n";
    for (const n of offset.slice(1)) s += String(n).padStart(10, "0") + " 00000 n \n";
    s += "trailer\n<< /Size " + (bodies.length + 1) + " /Root 1 0 R >>\nstartxref\n" + x + "\n%%EOF";
    return Buffer.from(s);
}

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
    const results = [], errors = [];
    try {
        context = await browser.newContext();
        for (const issue of [ 415, 416, "late-success", "password-after-close", "normal" ]) {
            const page = await context.newPage();
            page.on("pageerror", (e => errors.push(e.message)));
            await page.route("**/*", (route => {
                const u = new URL(route.request().url());
                if (u.origin !== "https://aide-pdf-owned.test") return route.abort();
                if (u.pathname === "/") return route.fulfill({
                    contentType: "text/html; charset=utf-8",
                    body: '<!doctype html><meta charset="utf-8"><style>main{width:900px;height:650px}.pdf-viewer{height:600px}.pdf-scroll{height:500px;overflow:auto}</style><main id="preview"></main>'
                });
                if (u.pathname === "/owned.pdf") return route.fulfill({
                    contentType: "application/pdf",
                    body: pdf()
                });
                const p = path.resolve(web, "." + u.pathname);
                if (!p.startsWith(web + path.sep) || !fs.existsSync(p)) return route.fulfill({
                    status: 404,
                    body: ""
                });
                return route.fulfill({
                    contentType: "application/javascript; charset=utf-8",
                    body: fs.readFileSync(p)
                });
            }));
            try {
                await page.goto("https://aide-pdf-owned.test/");
                await page.evaluate((async issue => {
                    const native = await import("/vendor/pdfjs/pdf.min.mjs");
                    native.GlobalWorkerOptions.workerSrc = "/vendor/pdfjs/pdf.worker.min.mjs";
                    window.nativePdf = native;
                    window.destroyCalls = 0;
                    window.docDestroyCalls = 0;
                    window.renderCalls = 0;
                    window.createdHolders = 0;
                    window.mode = issue;
                    window.el = (tag, cls, text) => {
                        if (cls === "pdf-page" && ++createdHolders > 10) throw Error("owned placeholder allocation trap");
                        const n = document.createElement(tag);
                        n.className = cls || "";
                        if (text != null) n.textContent = text;
                        return n;
                    };
                    window.t = s => s;
                    window.escapeHtml = s => s;
                    window.fileRawUrl = () => "/owned.pdf";
                    window.bindViewerTeardown = (_host, fn) => window.ownedTeardown = fn;
                    window.passwordPrompt = () => new Promise((resolve => window.resolvePassword = resolve));
                    window.passwordUpdates = 0;
                    window.ensurePdfJs = async () => ({
                        getDocument(options) {
                            const base = native.getDocument(options);
                            window.baseTask = base;
                            base.promise.catch((() => {}));
                            let release;
                            const gate = new Promise((r => release = r));
                            window.releaseGate = release;
                            const view = doc => new Proxy(doc, {
                                get(target, key) {
                                    if (key === "destroy") return function() {
                                        window.docDestroyCalls++;
                                        return target.destroy();
                                    };
                                    if (key === "numPages" && issue === 415) return 1e9;
                                    if (key === "getPage") return async n => {
                                        const p = await target.getPage(n);
                                        return new Proxy(p, {
                                            get(p, k) {
                                                const value = p[k];
                                                if (k === "render") return function(...args) {
                                                    window.renderCalls++;
                                                    return value.apply(p, args);
                                                };
                                                return typeof value === "function" ? value.bind(p) : value;
                                            }
                                        });
                                    };
                                    const value = target[key];
                                    return typeof value === "function" ? value.bind(target) : value;
                                }
                            });
                            return {
                                promise: issue === "late-success" ? base.promise.then((doc => {
                                    window.nativeReady = true;
                                    return gate.then((() => view(doc)));
                                })) : issue === 416 || issue === "password-after-close" ? gate.then((() => base.promise)).then(view) : base.promise.then(view),
                                destroy() {
                                    window.destroyCalls++;
                                    return base.destroy();
                                },
                                set onPassword(fn) {
                                    window.ownedPasswordHandler = fn;
                                    base.onPassword = fn;
                                }
                            };
                        }
                    });
                }), issue);
                await page.addScriptTag({
                    content: setup
                });
                if (issue === 416 || issue === "late-success" || issue === "password-after-close") {
                    await page.evaluate((() => {
                        window.previewPromise = setupPdfPreview(document.querySelector("main"), "owned.pdf");
                        previewPromise.catch((() => {}));
                    }));
                    await page.waitForFunction((() => !!window.baseTask));
                    if (issue === "late-success") await page.waitForFunction((() => window.nativeReady === true));
                    if (issue === "password-after-close") await page.evaluate((() => ownedPasswordHandler((() => passwordUpdates++), 0)));
                    await page.evaluate((() => {
                        window.previewHost = document.querySelector("main");
                        ownedTeardown();
                        previewHost.remove();
                    }));
                    const info = await page.evaluate((() => ({
                        destroyCalls: destroyCalls,
                        taskDestroyed: baseTask.destroyed
                    })));
                    assert.equal(info.destroyCalls, 1, "closing during load destroys loading task immediately");
                    assert(info.taskDestroyed);
                    await page.evaluate((async () => {
                        window.resolvePassword?.("owned synthetic password");
                        releaseGate();
                        await previewPromise;
                        await Promise.resolve();
                        if (passwordUpdates !== 0) throw Error("closed view cannot update password");
                        if (previewHost.querySelector(".pdf-error")) throw Error("closed view cannot receive a late error");
                    }));
                    results.push({
                        issue: issue,
                        result: "PASS",
                        ...info
                    });
                } else {
                    let failure;
                    try {
                        await page.evaluate((() => setupPdfPreview(document.querySelector("main"), "owned.pdf")));
                    } catch (e) {
                        failure = e;
                    }
                    if (issue === 415) {
                        const info = await page.evaluate((() => ({
                            createdHolders: createdHolders,
                            destroyCalls: destroyCalls,
                            error: document.querySelector(".pdf-error")?.textContent
                        })));
                        assert(!failure, "controlled cap, not placeholder trap: " + failure?.message);
                        assert.equal(info.createdHolders, 0);
                        assert(info.destroyCalls === 1 && /页数|pages/i.test(info.error));
                        results.push({
                            issue: issue,
                            result: "PASS",
                            ...info
                        });
                    } else {
                        assert(!failure, failure?.message);
                        await page.waitForFunction((() => renderCalls > 0));
                        const info = await page.evaluate((() => ({
                            holders: document.querySelectorAll(".pdf-page").length,
                            renderCalls: renderCalls,
                            width: document.querySelector("canvas").width
                        })));
                        assert.equal(info.holders, 3);
                        assert(info.width > 0);
                        await page.evaluate((() => ownedTeardown()));
                        assert.equal(await page.evaluate((() => destroyCalls + docDestroyCalls)), 1);
                        results.push({
                            issue: issue,
                            result: "PASS",
                            ...info
                        });
                    }
                }
            } catch (e) {
                results.push({
                    issue: issue,
                    result: "FAIL",
                    error: e.message
                });
            } finally {
                await page.evaluate((async () => {
                    try {
                        window.releaseGate?.();
                        await (window.baseTask?.destroy());
                    } catch {}
                })).catch((() => {}));
                await page.close();
            }
        }
        fs.writeFileSync(path.join(out, "result.json"), JSON.stringify({
            results: results,
            errors: errors,
            browser: browser.version(),
            actualPdfjsWorker: true
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
