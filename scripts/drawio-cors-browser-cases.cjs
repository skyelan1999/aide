"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), {assignment: assignment} = require("./drawio-editing-review.cjs");

module.exports = async function(frame, out, theme) {
    const checks = [];
    for (const copy of [ "source", "app.min.js", "integrate.min.js", "viewer.min.js", "viewer-static.min.js" ]) {
        const source = fs.readFileSync("internal/server/web/vendor/drawio/js/" + (copy === "source" ? "diagramly/Editor.js" : copy), "utf8");
        const code = assignment(source, "Editor.prototype.isCorsEnabledForUrl");
        const result = await frame.evaluate((code => {
            const failures = [], timings = [], editor = ownedEditor, oldMethod = Editor.prototype.isCorsEnabledForUrl, oldRule = editor.corsRegExp, oldParam = urlParams.cors, oldCross = editor.crossOriginImages;
            const check = (ok, msg) => {
                if (!ok) failures.push(msg);
            };
            try {
                Function(code)();
                check(AideDrawioLinearRegex.version === "re2js-2.9.1", "engine initialized");
                check(typeof window.RE2JS === "undefined", "upstream namespace private");
                editor.crossOriginImages = true;
                for (const [rule, url, expected] of [ [ "^https?://[^/]*\\.example\\.com/.*$", "https://a.example.com/image.png", true ], [ "^https?://[^/]*\\.example\\.com/.*$", "https://untrusted.invalid/image.png", false ], [ "^https://example.invalid/(a+)+$", "https://example.invalid/" + "a".repeat(4e3) + "!", false ], [ "(?=https:)", "https://a.example.com/image.png", false ], [ "(a)\\1", "https://a.example.com/image.png", false ], [ "x".repeat(513), "https://a.example.com/image.png", false ], [ "(?=https:)", location.origin + ".evil.invalid/image.png", false ] ]) {
                    urlParams.cors = encodeURIComponent(rule);
                    editor.corsRegExp = null;
                    const start = performance.now(), actual = editor.isCorsEnabledForUrl(url);
                    timings.push({
                        pattern: rule.slice(0, 60),
                        ms: performance.now() - start
                    });
                    check(actual === expected, "CORS result " + rule.slice(0, 40));
                    check(editor.isCorsEnabledForUrl("https://fonts.googleapis.com/css?family=Owned"), "known provider preserved");
                    const converted = editor.createImageUrlConverter().convert(url);
                    check(expected ? converted === url : converted.includes("url=" + encodeURIComponent(url)), "actual image converter routing");
                }
                urlParams.cors = "%ZZ";
                editor.corsRegExp = null;
                check(!editor.isCorsEnabledForUrl("https://untrusted.invalid/image.png"), "invalid URI escapes fail closed");
            } finally {
                Editor.prototype.isCorsEnabledForUrl = oldMethod;
                editor.corsRegExp = oldRule;
                editor.crossOriginImages = oldCross;
                if (oldParam === undefined) delete urlParams.cors; else urlParams.cors = oldParam;
            }
            return {
                failures: failures,
                timings: timings
            };
        }), code);
        fs.writeFileSync(path.join(out, theme + "-cors-" + copy + ".json"), JSON.stringify(result, null, 2));
        assert.deepEqual(result.failures, [], copy + " actual converter");
        checks.push({
            case: "linear CORS " + copy,
            theme: theme,
            result: "PASS",
            maxObservedMs: Math.max(...result.timings.map((x => x.ms)))
        });
    }
    await frame.evaluate((() => {
        ownedEditor.graph.setSelectionCell(ownedEditor.graph.model.getCell("2"));
        ownedEditor.graph.startEditingAtCell();
    }));
    const input = frame.locator(".mxCellEditor");
    await input.fill("Responsive after CORS rules");
    const before = await frame.page().evaluate((() => ownedSaves.length));
    await frame.getByText("Save", {
        exact: true
    }).last().click();
    await frame.page().waitForFunction((n => ownedSaves.length === n + 1), before);
    const saved = await frame.page().evaluate((() => ownedSaves.pop()));
    assert(saved.includes("Responsive after CORS rules"));
    assert.equal(await frame.evaluate((() => ownedEditor.graph.model.getCell("2").value)), "Responsive after CORS rules");
    return checks;
};
