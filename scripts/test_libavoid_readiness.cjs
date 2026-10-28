"use strict";

const fs = require("fs"), vm = require("vm"), assert = require("assert/strict");

const s = fs.readFileSync("internal/server/web/vendor/drawio/js/libavoid-js/libavoid.min.js", "utf8"), start = s.lastIndexOf("\n(function () {");

assert(start >= 0);

const code = s.slice(start);

const result = [];

async function run(name, fn) {
    try {
        await fn();
        result.push({
            name: name,
            result: "PASS"
        });
    } catch (e) {
        result.push({
            name: name,
            result: "FAIL",
            error: e.message
        });
    }
}

function context(factory) {
    const logs = [], c = {
        initAvoidModule: factory,
        console: {
            error: (...a) => logs.push(a),
            debug() {}
        }
    };
    vm.createContext(c);
    vm.runInContext(code, c);
    return {
        c: c,
        logs: logs
    };
}

(async () => {
    await run("pending factory is not ready", (async () => {
        let resolve, mod;
        const p = new Promise((r => resolve = r)), {c: c} = context((m => {
            mod = m;
            return p;
        }));
        assert(!c.Avoid, "partial module must not be published");
        await Promise.resolve();
        mod.Router = function() {};
        resolve(mod);
        const value = await c.__libavoidReady;
        assert.equal(value, mod);
        assert.equal(c.Avoid, mod);
    }));
    await run("asynchronous rejection is observed", (async () => {
        const failure = Promise.reject(Error("owned factory failure"));
        failure.catch((() => {}));
        const {c: c, logs: logs} = context((() => failure));
        assert.equal(await c.__libavoidReady, null);
        assert(!c.Avoid);
        assert.equal(logs.length, 1);
    }));
    await run("synchronous throw is unavailable", (async () => {
        const {c: c} = context((() => {
            throw Error("owned sync failure");
        }));
        assert.equal(await c.__libavoidReady, null);
        assert(!c.Avoid);
    }));
    console.log(JSON.stringify(result));
    if (!process.env.AIDE_EXPECT_REVIEW_FAILURES) assert.deepEqual(result.filter((r => r.result !== "PASS")), []);
})();
