"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), zlib = require("zlib"), {assignment: assignment} = require("./test_drawio_layout_fixes.cjs");

const base = "internal/server/web/vendor/drawio/js/";

function png(text, raw = false, plain = false) {
    const chunk = (type, data) => {
        const header = Buffer.alloc(8);
        header.writeUInt32BE(data.length);
        header.write(type, 4);
        const input = Buffer.concat([ Buffer.from(type), data ]);
        let crc = -1;
        for (const byte of input) {
            crc ^= byte;
            for (let i = 0; i < 8; i++) crc = crc >>> 1 ^ (crc & 1 ? 3988292384 : 0);
        }
        const end = Buffer.alloc(4);
        end.writeUInt32BE((crc ^ -1) >>> 0);
        return Buffer.concat([ header, data, end ]);
    };
    const body = plain ? Buffer.concat([ Buffer.from("mxGraphModel\0"), Buffer.from(text) ]) : Buffer.concat([ Buffer.from("mxGraphModel\0\0"), raw ? zlib.deflateRawSync(text) : zlib.deflateSync(text) ]);
    return "data:image/png;base64," + Buffer.concat([ Buffer.from([ 137, 80, 78, 71, 13, 10, 26, 10 ]), chunk("IHDR", Buffer.from([ 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0 ])), chunk(plain ? "tEXt" : "zTXt", body), chunk("IEND", Buffer.alloc(0)) ]).toString("base64");
}

module.exports = async function(frame, out, theme) {
    const checks = [], xml = '<mxGraphModel><root><mxCell id="0"/></root></mxGraphModel>';
    for (const variant of [ "source", "app.min.js", "integrate.min.js" ]) {
        const bundle = variant === "source" ? null : fs.readFileSync(base + variant, "utf8");
        const purify = bundle ? bundle.slice(bundle.indexOf("/*! @license DOMPurify"), bundle.indexOf("//# sourceMappingURL=purify.min.js.map") + "//# sourceMappingURL=purify.min.js.map".length) : fs.readFileSync(base + "sanitizer/purify.min.js", "utf8");
        const rough = bundle ? bundle.slice(bundle.indexOf("var rough="), bundle.indexOf(fs.readFileSync(base + "rough/rough.min.js", "utf8").trim()) + fs.readFileSync(base + "rough/rough.min.js", "utf8").trim().length) : fs.readFileSync(base + "rough/rough.min.js", "utf8");
        const extract = assignment(bundle || fs.readFileSync(base + "grapheditor/Editor.js", "utf8"), "Editor.extractGraphModelFromPng");
        const data = {
            purify: purify,
            rough: rough,
            extract: extract,
            xml: xml,
            png: png(encodeURIComponent(xml)),
            raw: png(encodeURIComponent(xml), true),
            plain: png(xml, false, true),
            bomb: png("x".repeat(9 * 1024 * 1024)),
            rawBomb: png("x".repeat(9 * 1024 * 1024), true)
        };
        const result = await frame.evaluate((d => {
            const failures = [], checks = [];
            const check = (yes, msg) => {
                if (!yes) failures.push(msg); else checks.push(msg);
            };
            const saved = window.DOMPurify;
            try {
                Function(d.purify)();
                const purifier = window.DOMPurify(window);
                for (const hook of [ "beforeSanitizeElements", "uponSanitizeElement", "afterSanitizeElements", "beforeSanitizeAttributes", "afterSanitizeAttributes" ]) {
                    const root = document.createElement("div");
                    root.innerHTML = '<div id="owned-wrap"><img id="owned-tail" onerror="owned-noop"></div><span>safe</span>';
                    const tail = root.querySelector("#owned-tail");
                    purifier.addHook(hook, (node => {
                        if (node.id === "owned-wrap") node.remove();
                    }));
                    try {
                        const ret = purifier.sanitize(root, {
                            IN_PLACE: true
                        });
                        check(ret === root && !ret.querySelector("#owned-wrap") && !tail.hasAttribute("onerror") && ret.textContent === "safe", "245 " + hook);
                    } finally {
                        purifier.removeHook(hook);
                    }
                }
                const raw = document.createElement("style");
                raw.textContent = '</style><img onerror="owned-noop">';
                document.body.appendChild(raw);
                let rejected = false;
                try {
                    purifier.sanitize(raw, {
                        IN_PLACE: true
                    });
                } catch (e) {
                    rejected = /refusing to sanitize in place/.test(e.message);
                }
                raw.remove();
                check(rejected, "245 removed root fails closed");
                check(purifier.version === "3.4.16", "245 fixed version");
            } finally {
                window.DOMPurify = saved;
            }
            const svg = mxUtils.parseXml('<svg xmlns="http://www.w3.org/2000/svg"><use href="https://example.invalid/x"/><style>rect {fill:url(https://example.invalid/x)}</style><rect width="10" height="10" fill="red"/></svg>').documentElement;
            Graph.sanitizeNode(svg);
            check(!svg.querySelector("use,style") && !!svg.querySelector("rect"), "245 actual Graph hooks preserved");
            const rough = Function(d.rough + ";return rough;")();
            for (const opts of [ {
                dashOffset: 0,
                dashGap: 0
            }, {
                dashOffset: 2,
                dashGap: 3
            }, {
                dashOffset: -1,
                dashGap: -1
            }, {
                dashOffset: Infinity,
                dashGap: 0
            } ]) {
                const x = rough.generator().rectangle(0, 0, 80, 50, {
                    fill: "red",
                    fillStyle: "dashed",
                    seed: 1,
                    ...opts
                });
                check(x.sets.every((s => s.ops.every((op => op.data.every(Number.isFinite))))), "246 finite dashes " + JSON.stringify(opts));
            }
            check(rough.generator().rectangle(0, 0, 80, 50, {
                fill: "red",
                fillStyle: "dashed",
                dashOffset: 0,
                dashGap: 0
            }).sets.find((s => s.type === "fillSketch")).ops.length === 0, "246 zero period no dashes");
            const E = {};
            Function("Editor", d.extract)(E);
            for (const name of [ "png", "raw", "plain" ]) check(E.extractGraphModelFromPng(d[name]) === d.xml, "253 " + name + " metadata");
            for (const name of [ "bomb", "rawBomb" ]) {
                let code;
                try {
                    E.extractGraphModelFromPng(d[name]);
                } catch (e) {
                    code = e.code;
                }
                check(code === "AIDE_DRAWIO_LIMIT", "253 " + name + " bounded");
            }
            let code;
            try {
                E.extractGraphModelFromPng("data:image/png;base64," + "A".repeat(12 * 1024 * 1024 + 1));
            } catch (e) {
                code = e.code;
            }
            check(code === "AIDE_DRAWIO_LIMIT", "253 before base64 allocation");
            check(E.extractGraphModelFromPng("data:image/png;base64,invalid") === null, "253 malformed ordinary PNG");
            return {
                failures: failures,
                checks: checks
            };
        }), data);
        fs.writeFileSync(path.join(out, theme + "-import-security-" + variant + ".json"), JSON.stringify(result, null, 2));
        if (!process.env.AIDE_IMPORT_SECURITY_CATALOG) assert.deepEqual(result.failures, [], variant);
        checks.push({
            case: "import-security " + variant,
            theme: theme,
            result: result.failures.length ? "EXPECTED_FAIL" : "PASS"
        });
    }
    return checks;
};

module.exports.png = png;
