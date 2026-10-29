"use strict";

const fs = require("fs"), vm = require("vm"), assert = require("assert/strict"), {Worker: Worker, isMainThread: isMainThread, parentPort: parentPort, workerData: workerData} = require("worker_threads"), {assignment: assignment} = require("./drawio-editing-review.cjs");

const base = "internal/server/web/vendor/drawio/";

function context(code, bootstrap, cors) {
    function Editor() {}
    const ctx = {
        Graph: {
            isSameOrigin(url) {
                try {
                    return new URL(url, "https://aide-owned.test").origin === "https://aide-owned.test";
                } catch (_) {
                    return false;
                }
            }
        },
        Editor: Editor,
        EditorUi: {
            isElectronApp: false
        },
        mxClient: {
            IS_CHROMEAPP: false
        },
        urlParams: {
            cors: cors
        },
        window: {
            location: {
                origin: "https://aide-owned.test"
            }
        }
    };
    vm.createContext(ctx, {
        codeGeneration: {
            strings: false,
            wasm: false
        }
    });
    if (bootstrap) vm.runInContext(bootstrap, ctx);
    if (ctx.AideDrawioLinearRegex) ctx.window.AideDrawioLinearRegex = ctx.AideDrawioLinearRegex;
    vm.runInContext(code, ctx);
    return {
        ctx: ctx,
        editor: new ctx.Editor
    };
}

if (!isMainThread) {
    const {editor: editor} = context(workerData.code, workerData.bootstrap, encodeURIComponent("^https://example.invalid/(a+)+$"));
    const t = performance.now();
    const result = editor.isCorsEnabledForUrl("https://example.invalid/" + "a".repeat(40) + "!");
    parentPort.postMessage({
        result: result,
        ms: performance.now() - t
    });
}

async function witness(code, bootstrap) {
    return await new Promise(((resolve, reject) => {
        const worker = new Worker(__filename, {
            workerData: {
                code: code,
                bootstrap: bootstrap
            }
        });
        let settled = false;
        const timer = setTimeout((async () => {
            settled = true;
            await worker.terminate();
            resolve({
                timeout: true
            });
        }), 1500);
        worker.once("message", (m => {
            if (!settled) {
                settled = true;
                clearTimeout(timer);
                resolve(m);
            }
        }));
        worker.once("error", (e => {
            if (!settled) {
                settled = true;
                clearTimeout(timer);
                reject(e);
            }
        }));
    }));
}

async function main() {
    const raw = process.env.AIDE_CORS_BASELINE ? require("child_process").execFileSync("git", [ "show", "e4a3ae1d:" + base + "js/diagramly/Editor.js" ], {
        encoding: "utf8",
        maxBuffer: 8 * 1024 * 1024
    }) : fs.readFileSync(base + "js/diagramly/Editor.js", "utf8");
    const canonical = assignment(raw, "Editor.prototype.isCorsEnabledForUrl"), bootstrap = fs.existsSync(base + "js/re2js-safe.min.js") ? fs.readFileSync(base + "js/re2js-safe.min.js", "utf8") : "";
    const observed = await witness(canonical, bootstrap);
    console.log(JSON.stringify({
        case: "actual CORS API nested-quantifier witness",
        ...observed
    }));
    assert(!observed.timeout, "untrusted CORS expression blocked the worker for1500ms");
    if (process.env.AIDE_CORS_WITNESS_ONLY) return;
    const cacheContext = vm.createContext({AideDrawioLinearRegex: {version: "re2js-2.9.1", buildId: "stale-adapter"}}, {codeGeneration: {strings: false, wasm: false}});
    const stale = cacheContext.AideDrawioLinearRegex;
    vm.runInContext(bootstrap, cacheContext);
    assert.notEqual(cacheContext.AideDrawioLinearRegex, stale, "same engine version must replace stale adapter builds");
    const current = cacheContext.AideDrawioLinearRegex;
    assert.match(current.buildId, /^[a-f0-9]{64}$/);
    vm.runInContext(bootstrap, cacheContext);
    assert.equal(cacheContext.AideDrawioLinearRegex, current, "identical bootstrap initializes once");
    const create = require("../internal/server/web/drawio-linear-regex.js"), engine = require("../internal/server/web/vendor/drawio/js/re2js/re2js.umd.js").RE2JS, api = create(engine);
    let comparisons = 0;
    const patterns = [ "", "example", "^https?:", "^https?://[^/]*\\.example\\.com/.*$", "(?:https|http)://(?:a|b)\\.example/[^?]*(?:\\?.*)?$", "[a-z0-9_-]+", "\\bfonts\\b", "^https://[a-z.]{1,30}/[0-9]{2,4}$", "(?:foo|bar)+", "^https://(a+)+$" ];
    const urls = [ "", "https://a.example.com/path", "http://b.example/12?q=yes", "https://fonts.example/234", "https://evil.invalid", "abc_123", "https://a.example/123", "barfoo", "https://" + "a".repeat(10) + "!" ];
    for (const p of patterns) {
        const re = api.compile(p);
        assert(re, p);
        for (const input of urls) {
            assert.equal(re.test(input), new RegExp(p).test(input), p + " " + input);
            comparisons++;
        }
    }
    for (let i = 0; i < 200; i++) {
        const width = 1 + i % 8, domain = "owned" + i + ".example";
        const p = "^https?://(?:api|cdn)[.]owned" + i + "[.]example/[a-z0-9_/-]{0," + width + "}(?:[?][a-z]+=[0-9]+)?$";
        const re = api.compile(p);
        assert(re);
        for (const input of [ "https://api." + domain + "/a", "http://cdn." + domain + "/?n=12", "https://api." + domain + "/" + "a".repeat(width + 1), "https://other." + domain + "/a", "https://api." + domain + "/a?q=x", "https://api." + domain + "/" ]) {
            assert.equal(re.test(input), new RegExp(p).test(input), p + " " + input);
            comparisons++;
        }
    }
    for (const p of [ "(?=https:)", "(?<=a)b", "(a)\\1", "[", "a".repeat(513), "a{1000}b{1000}c{1000}d{1000}e{1000}" ]) assert.equal(api.compile(p), null, "reject unsupported/oversized " + p.slice(0, 20));
    assert.equal(api.compile(".*").test("x".repeat(8193)), false);
    assert.equal(api.compile("^https://(a+)+$").test("https://" + "a".repeat(4e3) + "!"), false);
    for (const file of [ "source", "app.min.js", "integrate.min.js", "viewer.min.js", "viewer-static.min.js" ]) {
        const text = file === "source" ? null : fs.readFileSync(base + "js/" + file, "utf8"), code = text ? assignment(text, "Editor.prototype.isCorsEnabledForUrl") : canonical;
        const result = await witness(code, bootstrap);
        assert(!result.timeout && !result.result, file + " bounded witness");
        for (const rule of [ "%ZZ", encodeURIComponent("(?=https:)"), "x".repeat(513) ]) {
            const {editor: editor} = context(code, bootstrap, rule);
            assert.equal(editor.isCorsEnabledForUrl("https://untrusted.invalid"), false);
            assert.equal(editor.isCorsEnabledForUrl("https://fonts.googleapis.com/css"), true);
            const cached = editor.corsRegExp;
            assert.equal(editor.isCorsEnabledForUrl("https://other.invalid"), false);
            assert.equal(editor.corsRegExp, cached, "rejected rules cached");
        }
        const {editor: editor, ctx: ctx} = context(code, bootstrap, encodeURIComponent("^https?://[^/]*\\.example\\.com/.*$"));
        assert.equal(editor.isCorsEnabledForUrl("https://a.example.com/x"), true);
        assert.equal(editor.isCorsEnabledForUrl("https://evil.invalid/x"), false);
        assert.equal(ctx.RE2JS, undefined, "library stays in owned namespace");
        const origins = context(code, bootstrap, undefined);
        assert.equal(origins.editor.isCorsEnabledForUrl("https://aide-owned.test.evil.invalid/x"), false, "origin prefix is not origin equality");
        assert.equal(origins.editor.isCorsEnabledForUrl("/owned.svg"), true, "same-origin relative path");
        const missing = context(code, "", encodeURIComponent(".*"));
        assert.equal(missing.editor.isCorsEnabledForUrl("https://untrusted.invalid"), false, "engine absence never uses native fallback");
        console.log("PASS CORS API", file);
    }
    console.log("PASS linear regex native-oracle comparisons", comparisons);
}

if (isMainThread) main().catch((e => {
    console.error(e.message);
    process.exitCode = 1;
}));
