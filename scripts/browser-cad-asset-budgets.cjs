"use strict";

const fs = require("fs"), path = require("path"), assert = require("assert/strict"), {chromium: chromium} = require("./browser-runtime/node_modules/playwright");

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
        const page = await context.newPage();
        await page.setContent("<!doctype html><html><body></body></html>");
        for (const p of [ "vendor/three.min.js", "vendor/STLLoader.js", "vendor/ColladaLoader.js", "urdf-preview.js" ]) await page.addScriptTag({
            path: "internal/server/web/" + p
        });
        const result = await page.evaluate((async fixture => {
            const api = AideUrdfPreview, checks = [], check = (x, m) => {
                if (!x) throw Error(m);
            }, parse = text => (new DOMParser).parseFromString(text, "application/xml");
            async function test(issue, fn) {
                try {
                    checks.push({
                        issue: issue,
                        result: "PASS",
                        detail: await fn()
                    });
                } catch (e) {
                    checks.push({
                        issue: issue,
                        result: "FAIL",
                        error: e.message,
                        stack: e.stack
                    });
                }
            }
            await test(539, (() => {
                const bytes = new ArrayBuffer(84);
                new DataView(bytes).setUint32(80, 5e6, true);
                const Original = window.Float32Array;
                let allocations = 0;
                window.Float32Array = new Proxy(Original, {
                    construct(target, args, newTarget) {
                        if (typeof args[0] === "number" && args[0] > 1e6) {
                            allocations++;
                            throw Error("owned STL allocation trap");
                        }
                        return Reflect.construct(target, args, newTarget);
                    }
                });
                let error;
                try {
                    (new THREE.STLLoader).parse(bytes);
                } catch (e) {
                    error = e;
                } finally {
                    window.Float32Array = Original;
                }
                check(error && /STL/.test(error.message) && allocations === 0, "reject header before geometry allocation: " + error?.message);
                const normal = new ArrayBuffer(134);
                new DataView(normal).setUint32(80, 1, true);
                const geom = (new THREE.STLLoader).parse(normal);
                check(geom.getAttribute("position").count === 3, "one real binary facet");
                geom.dispose();
                return {
                    oversizedAllocations: allocations
                };
            }));
            await test(541, (() => {
                const limit = api.LIMITS.colladaFloats || 3e6, doc = parse('<COLLADA><library_geometries><geometry><mesh><source><float_array count="' + (limit + 1) + '">' + "0 ".repeat(limit + 1) + "</float_array></source></mesh></geometry></library_geometries></COLLADA>");
                let error;
                try {
                    api.validateCollada(doc);
                } catch (e) {
                    error = e;
                }
                check(error && /DAE.*(?:float|浮点|数值)/i.test(error.message), "reject total float-array budget before parser tokenization");
                const aggregate = parse('<COLLADA><float_array count="' + Math.ceil(limit / 2) + '">' + "0 ".repeat(Math.ceil(limit / 2)) + '</float_array><float_array count="' + Math.ceil(limit / 2 + 1) + '">' + "0 ".repeat(Math.ceil(limit / 2 + 1)) + "</float_array></COLLADA>");
                let aggregateError;
                try {
                    api.validateCollada(aggregate);
                } catch (e) {
                    aggregateError = e;
                }
                check(aggregateError, "aggregate source budget");
                api.validateCollada(parse('<COLLADA><float_array count="3">0 1 2</float_array></COLLADA>'));
                return {
                    floatLimit: limit
                };
            }));
            await test(543, (() => {
                const bad = fixture.replace('<instance_geometry url="#G"/>', '<instance_geometry url="#G"/><instance_node url="#A"/>');
                let error;
                try {
                    api.validateCollada(parse(bad));
                } catch (e) {
                    error = e;
                }
                check(error && /DAE.*(?:cycle|循环)/i.test(error.message), "detect cycle before vendor construction");
                const chain = n => "<COLLADA><library_nodes>" + Array.from({
                    length: n
                }, ((_, i) => '<node id="n' + i + '">' + (i ? '<instance_node url="#n' + (i - 1) + '"/>' : "") + "</node>")).join("") + "</library_nodes></COLLADA>";
                let depthError;
                try {
                    api.validateCollada(parse(chain(129)));
                } catch (e) {
                    depthError = e;
                }
                check(depthError && /深度|depth/i.test(depthError.message), "reverse-declared reference chain depth bounded");
                api.validateCollada(parse(chain(128)));
                api.validateCollada(parse(fixture));
                const scene = (new THREE.ColladaLoader).parse(fixture, "").scene;
                let meshes = 0;
                scene.traverse((n => {
                    if (n.isMesh) {
                        meshes++;
                        n.geometry.dispose();
                        [].concat(n.material || []).forEach((m => m.dispose()));
                    }
                }));
                check(meshes === 1, "acyclic triangle scene builds");
                return {
                    meshes: meshes
                };
            }));
            await test(544, (() => {
                const withLights = n => fixture.replace("<library_nodes>", '<library_lights><light id="L"><technique_common><point><color>1 1 1</color></point></technique_common></light></library_lights><library_nodes>').replace('<instance_geometry url="#G"/>', '<instance_geometry url="#G"/>' + '<instance_light url="#L"/>'.repeat(n));
                let error;
                try {
                    api.validateCollada(parse(withLights(5e3)));
                } catch (e) {
                    error = e;
                }
                check(error && /DAE.*(?:light|灯光)/i.test(error.message), "reject lights before shader creation");
                api.validateCollada(parse(withLights(16)));
                api.validateCollada(parse(withLights(1)));
                const scene = (new THREE.ColladaLoader).parse(withLights(1), "").scene;
                let lights = 0;
                scene.traverse((n => {
                    if (n.isLight) lights++;
                    if (n.isMesh) {
                        n.geometry.dispose();
                        [].concat(n.material || []).forEach((m => m.dispose()));
                    }
                }));
                check(lights === 1, "ordinary light retained");
                const reused = withLights(1).replace('<instance_node url="#A"/></node></visual_scene>', '<instance_node url="#A"/>'.repeat(17) + "</node></visual_scene>");
                let reuseError;
                try {
                    api.validateCollada(parse(reused));
                } catch (e) {
                    reuseError = e;
                }
                check(reuseError, "light budget includes reused node expansion");
                return {
                    lights: lights
                };
            }));
            return checks;
        }), fs.readFileSync("scripts/fixtures/collada-budget-scene.dae", "utf8"));
        fs.writeFileSync(path.join(out, "result.json"), JSON.stringify(result, null, 2));
        console.log(JSON.stringify(result));
        if (!process.env.AIDE_EXPECT_REVIEW_FAILURES) assert.deepEqual(result.filter((r => r.result !== "PASS")), []);
    } finally {
        if (context) await context.close();
        await browser.close();
    }
})().catch((e => {
    console.error(e.stack);
    process.exitCode = 1;
}));
