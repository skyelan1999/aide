"use strict";

const fs = require("fs"), vm = require("vm"), assert = require("assert/strict"), {Worker: Worker, isMainThread: isMainThread, parentPort: parentPort, workerData: workerData} = require("worker_threads"), r = require("./drawio-traversal-review.cjs");

const results = [];

async function test(issue, name, fn) {
    try {
        const detail = await fn();
        results.push({
            issue: issue,
            name: name,
            result: "PASS",
            detail: detail
        });
    } catch (e) {
        results.push({
            issue: issue,
            name: name,
            result: "FAIL",
            error: e.message
        });
    }
}

function ctx() {
    let next = 0;
    const ids = new WeakMap;
    function Dict() {
        this.map = new Map;
    }
    Dict.prototype.get = function(k) {
        return this.map.get(k);
    };
    Dict.prototype.put = function(k, v) {
        this.map.set(k, v);
    };
    const c = {
        mxGraph: function() {},
        mxHierarchicalLayout: function() {},
        mxGraphHierarchyModel: function() {},
        mxDictionary: Dict,
        mxObjectIdentity: {
            get(o) {
                if (!ids.has(o)) ids.set(o, "owned" + next++);
                return ids.get(o);
            }
        }
    };
    return vm.createContext(c, {
        codeGeneration: {
            strings: false,
            wasm: false
        }
    });
}

function chain(n) {
    const nodes = Array.from({
        length: n
    }, ((_, i) => ({
        id: "n" + i,
        edges: [],
        connectsAsSource: [],
        hashCode: [ 0 ]
    })));
    for (let i = 0; i < n - 1; i++) {
        const e = {
            source: nodes[i],
            target: nodes[i + 1]
        };
        nodes[i].edges.push(e);
        nodes[i + 1].edges.push(e);
        nodes[i].connectsAsSource.push(e);
    }
    return nodes;
}

function graph(c, nodes) {
    const g = new c.mxGraph;
    g.model = {
        getEdgeCount: v => v.edges.length,
        getEdgeAt: (v, i) => v.edges[i],
        getTerminal: (e, s) => s ? e.source : e.target
    };
    return g;
}

function dxf(records) {
    return records.flat().join("\n") + "\n";
}

const badDxf = [ ...[ "LAYER", "VPORT" ].map((type => dxf([ 0, "SECTION", 2, "TABLES", 0, "TABLE", 2, type, 70, 1, 0, type, 2, "owned", 0, "BOGUS", 0, "ENDTAB", 0, "ENDSEC", 0, "EOF" ]))), dxf([ 0, "SECTION", 2, "ENTITIES", 0, "POLYLINE", 8, "owned", 70, 0, 0, "BOGUS", 0, "SEQEND", 0, "ENDSEC", 0, "EOF" ]) ];

if (!isMainThread) {
    try {
        const Parser = require("../internal/server/web/vendor/dxf-parser/dxf-parser.js");
        (new Parser).parseSync(workerData.input);
        parentPort.postMessage({
            returned: true
        });
    } catch (e) {
        parentPort.postMessage({
            error: e.message
        });
    }
}

function parseWorker(input) {
    return new Promise(((resolve, reject) => {
        const w = new Worker(__filename, {
            workerData: {
                input: input
            }
        });
        let done = false;
        const timer = setTimeout((async () => {
            done = true;
            await w.terminate();
            resolve({
                timeout: true
            });
        }), 800);
        w.once("message", (m => {
            if (!done) {
                done = true;
                clearTimeout(timer);
                resolve(m);
            }
        }));
        w.once("error", (e => {
            if (!done) {
                done = true;
                clearTimeout(timer);
                reject(e);
            }
        }));
    }));
}

async function zip64(lib, extra, corrupt = false) {
    const standard = await (new lib).file("owned.txt", "").generateAsync({
        type: "nodebuffer"
    }), at = standard.length - 22, eocd = Buffer.from(standard.subarray(at)), record = Buffer.alloc(56);
    record.writeUInt32LE(101075792, 0);
    record.writeBigUInt64LE(BigInt(44 + extra.length), 4);
    record.writeUInt16LE(45, 12);
    record.writeUInt16LE(45, 14);
    record.writeBigUInt64LE(1n, 24);
    record.writeBigUInt64LE(1n, 32);
    record.writeBigUInt64LE(BigInt(eocd.readUInt32LE(12)), 40);
    record.writeBigUInt64LE(BigInt(eocd.readUInt32LE(16)), 48);
    const locator = Buffer.alloc(20);
    locator.writeUInt32LE(117853008);
    locator.writeBigUInt64LE(BigInt(at), 8);
    locator.writeUInt32LE(1, 16);
    eocd.writeUInt16LE(65535, 8);
    eocd.writeUInt16LE(65535, 10);
    eocd.writeUInt32LE(4294967295, 12);
    eocd.writeUInt32LE(4294967295, 16);
    return Buffer.concat([ standard.subarray(0, at), record, extra, locator, eocd ]);
}

async function main() {
    for (let i = 0; i < badDxf.length; i++) await test(398, "malformed DXF " + i, (async () => {
        const x = await parseWorker(badDxf[i]);
        assert(!x.timeout, "DXF parser blocked for800ms");
        assert(x.error, "malformed sequence rejected");
        return x;
    }));
    await test(398, "ordinary LINE remains readable", (() => {
        const Parser = require("../internal/server/web/vendor/dxf-parser/dxf-parser.js"), doc = (new Parser).parseSync(dxf([ 0, "SECTION", 2, "ENTITIES", 0, "LINE", 8, "owned", 10, 0, 20, 0, 11, 10, 21, 20, 0, "ENDSEC", 0, "EOF" ]));
        assert.equal(doc.entities.length, 1);
        assert.equal(doc.entities[0].type, "LINE");
    }));
    const oracleContext = ctx(), oracleText = fs.readFileSync(
        require("node:path").join(__dirname, "fixtures/mxgraph-traverse-oracle.js"), "utf8");
    vm.runInContext(require("./drawio-editing-review.cjs").assignment(oracleText, "mxGraph.prototype.traverse"), oracleContext);
    for (const file of [ null, "mxgraph/mxClient.js", "js/app.min.js", "js/integrate.min.js", "js/viewer.min.js", "js/viewer-static.min.js" ]) {
        const c = ctx(), data = r.snippets(file);
        for (const row of data) vm.runInContext(row.code, c);
        if (data.some((x => x.issue === 400))) await test(400, "iterative tree traversal " + (file || "source"), (() => {
            const nodes = chain(12e3), g = graph(c, nodes);
            let count = 0;
            g.traverse(nodes[0], true, (() => {
                count++;
            }));
            assert.equal(count, 12e3);
            let reverse = 0;
            g.traverse(nodes.at(-1), true, (() => {
                reverse++;
            }), null, null, true);
            assert.equal(reverse, 12e3);
            let pruned = 0;
            g.traverse(nodes[0], true, (() => ++pruned < 3));
            assert.equal(pruned, 3);
        }));
        if (data.some((x => x.issue === 400))) await test(400, "recursive-oracle callback order " + (file || "source"), (() => {
            let compared = 0;
            for (let seed = 0; seed < 50; seed++) for (const directed of [ true, false ]) for (const inverse of [ true, false ]) {
                function make(C) {
                    const nodes = chain(8);
                    for (let i = 0; i < 8; i++) {
                        const target = nodes[(i * 3 + seed) % 8], edge = {
                            source: nodes[i],
                            target: target
                        };
                        nodes[i].edges.push(edge);
                        target.edges.push(edge);
                    }
                    return {
                        g: graph(C, nodes),
                        nodes: nodes
                    };
                }
                function run(C) {
                    const {g: g, nodes: nodes} = make(C), log = [];
                    g.traverse(nodes[seed % 8], directed, ((v, e) => {
                        log.push(v.id + ":" + (e ? e.source.id + ">" + e.target.id : "root"));
                        return seed % 2 && v.id === "n3" ? false : undefined;
                    }), null, null, inverse);
                    return log;
                }
                assert.deepEqual(run(c), run(oracleContext));
                compared++;
            }
            return {
                compared: compared
            };
        }));
        for (const name of [ "mxHierarchicalLayout.prototype.traverse", "mxGraphHierarchyModel.prototype.dfs", "mxGraphHierarchyModel.prototype.extendedDfs" ]) if (data.some((x => x.name === name))) await test(399, "bounded hierarchy " + (file || "source") + " " + name, (() => {
            function run(n) {
                const nodes = chain(n);
                if (name.startsWith("mxHierarchicalLayout.")) {
                    const o = new c.mxHierarchicalLayout;
                    o.getEdges = v => v.edges;
                    o.getVisibleTerminal = (e, s) => s ? e.source : e.target;
                    return o.traverse(nodes[0], true, null, Object.create(null), Object.create(null), [], null);
                }
                const o = new c.mxGraphHierarchyModel, seen = Object.create(null), visitor = () => {};
                return name.endsWith(".extendedDfs") ? o.extendedDfs(null, nodes[0], null, visitor, seen, [], 0, 0) : o.dfs(null, nodes[0], null, visitor, seen, 0);
            }
            assert.throws((() => run(12e3)), /hierarchical.*256/i, "reject with explicit budget error instead of native stack overflow");
            run(128);
        }));
    }
    const webcolaContext = ctx();
    vm.runInContext(fs.readFileSync(r.base + "plugins/webcola/mxWebColaAdaptor.js", "utf8"), webcolaContext);
    await test(405, "WebCola budget precedes conversion and dense matrices", (() => {
        const C = webcolaContext.mxWebColaAdaptor, old = C.prototype.computeVertexDegrees;
        C.prototype.computeVertexDegrees = () => {
            throw Error("owned conversion sink");
        };
        try {
            function make(n, visible = true) {
                const children = Array.from({
                    length: n
                }, (() => ({
                    children: [],
                    isVertex: () => true,
                    visible: visible
                })));
                return {
                    getModel: () => ({
                        root: {
                            children: children,
                            isVertex: () => false
                        }
                    }),
                    getView: () => ({
                        getState: c => c.visible ? {} : null
                    })
                };
            }
            assert.throws((() => new C(make(1001))), /WebCola.*1000/i);
            assert.throws((() => new C(make(1e3))), /owned conversion sink/);
            assert.throws((() => new C(make(1200, false))), /owned conversion sink/);
        } finally {
            C.prototype.computeVertexDegrees = old;
        }
    }));
    await test(407, "WebCola group hierarchy construction is linear", (() => {
        const C = webcolaContext.mxWebColaAdaptor, cells = Object.create(null), root = {
            id: "root",
            children: [],
            isVertex: () => false,
            isEdge: () => false
        };
        let parents = 0;
        for (let i = 0; i < 200; i++) {
            const g = {
                id: "g" + i,
                children: [],
                isVertex: () => true,
                isEdge: () => false,
                getParent() {
                    parents++;
                    return root;
                }
            }, v = {
                id: "v" + i,
                children: [],
                isVertex: () => true,
                isEdge: () => false,
                getParent() {
                    parents++;
                    return g;
                }
            };
            g.children.push(v);
            root.children.push(g);
            cells[g.id] = g;
            cells[v.id] = v;
        }
        const graph = {
            getModel: () => ({
                root: root,
                cells: cells,
                getGeometry: () => ({
                    width: 40,
                    height: 20
                })
            }),
            getView: () => ({
                getState: () => ({}),
                getBoundingBox: () => ({})
            })
        }, a = Object.create(C.prototype);
        a.graph = graph;
        a.isLeafOrCollapsed = c => !c.children?.length;
        const result = a.graphToLayout(graph);
        assert.equal(result.groups.length, 200);
        assert.equal(result.nodes.length, 200);
        assert(parents < 8e3, "group parent lookups:" + parents);
        assert(result.groups.every((g => g.leaves.length === 1)));
        return {
            parentLookups: parents
        };
    }));
    const cola = require("../internal/server/web/vendor/drawio/plugins/webcola/cola.min.js");
    await test(404, "iterative WebCola connected components", (() => {
        const nodes = Array.from({
            length: 15e3
        }, ((_, index) => ({
            index: index
        }))), edges = nodes.slice(1).map(((v, i) => ({
            source: nodes[i],
            target: v
        })));
        const parts = cola.separateGraphs(nodes, edges);
        assert.equal(parts.length, 1);
        assert.equal(parts[0].array.length, nodes.length);
        assert.deepEqual(parts[0].array.map((x => x.index)), nodes.map((x => x.index)));
    }));
    await test(406, "iterative WebCola pairing heap merge", (() => {
        const root = new cola.PairingHeap(0), less = (a, b) => a < b;
        root.subheaps = Array.from({
            length: 2e4
        }, ((_, i) => new cola.PairingHeap(i + 1)));
        let merged = root.removeMin(less);
        for (let i = 1; i <= 2e4; i++) {
            assert.equal(merged.min(), i);
            merged = merged.removeMin(less);
        }
        assert(merged.empty());
    }));
    for (const path of [ "internal/server/web/vendor/jszip.min.js", "internal/server/web/vendor/drawio/js/jszip/jszip.min.js" ]) await test(403, "ZIP64 record boundary " + path, (async () => {
        const lib = require("../" + path), extra = Buffer.alloc(9);
        extra.writeUInt16LE(51966, 0);
        extra.writeUInt32LE(3, 2);
        extra.write("abc", 6);
        for (const payload of [ Buffer.alloc(0), extra, Buffer.concat([ extra, Buffer.alloc(6) ]) ]) {
            const result = await lib.loadAsync(await zip64(lib, payload));
            assert.equal(await result.file("owned.txt").async("string"), "");
        }
        const bad = Buffer.from(extra);
        bad.writeUInt32LE(20, 2);
        await assert.rejects(lib.loadAsync(await zip64(lib, bad)), /ZIP64 extensible/i);
        await assert.rejects(lib.loadAsync(await zip64(lib, Buffer.alloc(1))), /ZIP64 extensible/i);
        const short = await zip64(lib, Buffer.alloc(0));
        short.writeBigUInt64LE(43n, short.indexOf(Buffer.from([ 80, 75, 6, 6 ])) + 4);
        await assert.rejects(lib.loadAsync(short), /ZIP64 end record/i);
    }));
    console.log(JSON.stringify(results));
    if (!process.env.AIDE_EXPECT_REVIEW_FAILURES) assert.deepEqual(results.filter((x => x.result !== "PASS")), []);
}

if (isMainThread) main().catch((e => {
    console.error(e.stack);
    process.exitCode = 1;
}));
