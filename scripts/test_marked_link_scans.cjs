"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), {Worker: Worker} = require("worker_threads");

const bundle = path.resolve("internal/server/web/vendor/marked.min.js"), marked = require(bundle), controls = JSON.parse(fs.readFileSync(process.env.AIDE_MARKED_CONTROLS || "scripts/fixtures/marked-link-controls.json", "utf8"));

for (const row of controls) assert.equal(marked.parse(row.markdown, row.options), row.html, "ordinary Markdown output preserved");

function probe(issue) {
    return new Promise(((resolve, reject) => {
        const worker = new Worker(`const {parentPort,workerData}=require('worker_threads');const m=require(workerData.bundle),r=m.Lexer.rules.inline.normal;const input=workerData.issue===420?'['+'\\t\\\\['.repeat(60000)+'[':'[]('+' '.repeat(100000);const rule=workerData.issue===420?r.reflinkSearch:r.link;const start=performance.now();const match=new RegExp(rule.source,rule.flags).test(input);parentPort.postMessage({issue:workerData.issue,match,ms:performance.now()-start});`, {
            eval: true,
            workerData: {
                issue: issue,
                bundle: bundle
            }
        });
        const timer = setTimeout((async () => {
            await worker.terminate();
            reject(Error("controlled regex worker exceeded 5000ms"));
        }), 5e3);
        worker.once("message", (r => {
            clearTimeout(timer);
            resolve(r);
        }));
        worker.once("error", (e => {
            clearTimeout(timer);
            reject(e);
        }));
    }));
}

(async () => {
    const results = await Promise.all([ 420, 430 ].map((async issue => {
        try {
            const r = await probe(issue);
            assert.equal(r.match, false, "incomplete syntax stays text");
            return {
                ...r,
                result: "PASS"
            };
        } catch (e) {
            return {
                issue: issue,
                result: "FAIL",
                error: e.message
            };
        }
    })));
    console.log(JSON.stringify({
        controls: controls.length,
        results: results
    }));
    if (!process.env.AIDE_EXPECT_REVIEW_FAILURES) assert.deepEqual(results.filter((r => r.result !== "PASS")), []);
})();
