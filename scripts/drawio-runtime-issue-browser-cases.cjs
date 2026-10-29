"use strict";

const fs = require("node:fs"), path = require("node:path"), assert = require("node:assert/strict"), review = require("./drawio-runtime-issue-review.cjs");

module.exports = async function(frame, out, theme) {
    const checks = [];
    for (const variant of [ "source", "app.min.js", "integrate.min.js" ]) {
        const code = review.snippets(variant);
        const result = await frame.evaluate((({code: code}) => {
            const failures = [], notes = [ ...(code.sourceFallbacks || []).map((n => n + " not bundled here;separately shipped source tested")) ];
            const check = (ok, msg) => {
                if (!ok) failures.push(msg);
            };
            // Evaluate reviewed methods in an isolated namespace while using the actual browser Canvas and mxGraph runtime.
                        const LocalSidebar = function() {}, LocalCanvas = function() {};
            LocalCanvas.prototype.hexToRgb = mxJsCanvas.prototype.hexToRgb;
            const LocalExport = {};
            const LocalCoordinate = function() {};
            for (const s of code.methods) Function("Sidebar", "mxJsCanvas", "ExportDialog", "mxCoordinateAssignment", s)(LocalSidebar, LocalCanvas, LocalExport, LocalCoordinate);
            const canvas = document.createElement("canvas");
            canvas.width = 240;
            canvas.height = 160;
            const c = new LocalCanvas;
            c.ctx = canvas.getContext("2d");
            c.state = {};
            c.ctx.lineJoin = "round";
            c.ctx.miterLimit = 2;
            c.setMiterLimit(10);
            check(c.ctx.miterLimit === 10 && c.ctx.lineJoin === "round", "241 miter limit");
            for (const [d, ptA, ptB] of [ [ "east", [ 30, 60 ], [ 150, 60 ] ], [ "west", [ 150, 60 ], [ 30, 60 ] ], [ "south", [ 90, 25 ], [ 90, 95 ] ], [ "north", [ 90, 95 ], [ 90, 25 ] ] ]) {
                c.ctx.setTransform(1, 0, 0, 1, 0, 0);
                c.ctx.clearRect(0, 0, 240, 160);
                c.ctx.translate(5, 7);
                c.ctx.scale(1.2, 1.2);
                c.setGradient("#ff0000", "#0000ff", 20, 15, 140, 90, d);
                c.ctx.fillRect(20, 15, 140, 90);
                const pixel = p => c.ctx.getImageData(Math.floor(5 + p[0] * 1.2), Math.floor(7 + p[1] * 1.2), 1, 1).data;
                const a = pixel(ptA), b = pixel(ptB);
                check(a[0] > b[0] && a[2] < b[2], "242 " + d + " pixels");
            }
            // Deferred and preloaded callbacks complete in reverse order, plus an inline-data library.
                        for (const preload of [ false, true ]) {
                const callbacks = {}, palettes = {}, records = [];
                const sb = new LocalSidebar;
                sb.customEntries = [ {
                    entries: [ {
                        id: "first",
                        libs: [ {
                            title: "A",
                            url: "A",
                            preload: preload
                        }, {
                            title: "B",
                            url: "B",
                            preload: preload
                        } ]
                    }, {
                        id: "last",
                        libs: [ {
                            title: "C",
                            url: "C",
                            preload: preload
                        }, {
                            title: "Inline",
                            data: [ "Inline" ]
                        } ]
                    } ]
                } ];
                sb.maxPreloadCount = 0;
                sb.setCurrentSearchEntryLibrary = (a, b) => sb.current = [ a, b ];
                sb.addEntries = data => records.push([ data[0], ...sb.current || [] ]);
                sb.addPalette = (id, title, expanded, fn) => palettes[id] = fn;
                sb.editorUi = {
                    getResource: x => x,
                    addLibraryEntries() {},
                    editor: {
                        isCorsEnabledForUrl: () => true,
                        loadUrl: (url, fn) => callbacks[url] = fn
                    }
                };
                sb.addCustomEntries();
                for (const [id, fn] of Object.entries(palettes)) fn(document.createElement("div"), document.createElement("div"));
                for (const key of [ "C", "B", "A" ]) callbacks[key]("<mxlibrary>" + JSON.stringify([ key ]) + "</mxlibrary>");
                check(JSON.stringify(records.sort()) === JSON.stringify([ [ "A", "first", "first.0" ], [ "B", "first", "first.1" ], [ "C", "last", "last.0" ], [ "Inline", "last", "last.1" ] ].sort()), "240 " + (preload ? "preload" : "deferred") + " " + JSON.stringify(records));
            }
            const prevPopup = mxUtils.popup, prevAlert = mxUtils.alert, prevMax = MAX_REQUEST_SIZE;
            let popup, alert = 0;
            try {
                mxUtils.popup = data => popup = data;
                mxUtils.alert = () => alert++;
                MAX_REQUEST_SIZE = 3;
                try {
                    LocalExport.saveLocalFile({}, "owned-xml", "x.xml", "xml");
                } catch (e) {
                    failures.push("254 " + e.message);
                }
                check(popup === "owned-xml" && alert === 1, "254 oversized fallback");
            } finally {
                mxUtils.popup = prevPopup;
                mxUtils.alert = prevAlert;
                MAX_REQUEST_SIZE = prevMax;
            }
            function OwnedRect() {}
            for (const s of code.rect) Function("OwnedRect", s)(OwnedRect);
            for (const n of [ "moveNEInner", "lineNEInner" ]) for (const [style, y] of [ [ "square", 4 ], [ "rounded", 22 ], [ "snip", 22 ], [ "invRound", 24 ], [ "fold", 24 ] ]) {
                let p;
                (new OwnedRect)[n]({
                    moveTo: (...v) => p = v,
                    lineTo: (...v) => p = v
                }, 0, 0, 100, 70, style, "default", 20, 4, true);
                check(p && p[0] === 96 && p[1] === y, "255 " + n + " " + style);
            }
            // Use the real generic handle closure with a recording handle constructor to verify finite callbacks.
                        const edgeName = /function\s+([\w$]+)/.exec(code.edge)[1], handleName = /return\s+([\w$]+)\(/.exec(code.edge)[1];
            const factory = Function(handleName, code.edge + ";return " + edgeName)(((state, keys, getPosition, setPosition) => ({
                getPosition: getPosition,
                setPosition: setPosition,
                state: state
            })));
            for (const start of [ true, false ]) for (const points of [ [ {
                x: 10,
                y: 20
            }, {
                x: 10,
                y: 20
            } ], [ {
                x: 10,
                y: 20
            }, {
                x: 70,
                y: 20
            } ], [], [ {
                x: 1,
                y: 1
            } ] ]) {
                let values;
                const state = {
                    absolutePoints: points,
                    view: {
                        translate: {
                            x: 3,
                            y: 4
                        },
                        scale: 2
                    }
                };
                const h = factory(state, [ "width" ], start, ((dist, nx, ny, p0) => {
                    values = [ dist, nx, ny ];
                    return new mxPoint(p0.x + ny * 4, p0.y - nx * 4);
                }), ((dist, nx, ny) => values = [ dist, nx, ny ]));
                try {
                    const p = h.getPosition();
                    if (points.length > 1) check(p && Number.isFinite(p.x) && Number.isFinite(p.y) && values.every(Number.isFinite), "256 finite position");
                    h.setPosition(null, new mxPoint(2, 3));
                    if (points.length > 1) check(values.every(Number.isFinite), "256 finite drag");
                } catch (e) {
                    failures.push("256 " + e.message);
                }
            }
            const graph = new Graph(document.createElement("div"));
            document.body.append(graph.container);
            graph.container.style.cssText = "position:absolute;left:0;top:0;width:200px;height:150px";
            const model = graph.model;
            model.beginUpdate();
            let a, b, edge;
            try {
                a = graph.insertVertex(null, null, "a", 0, 0, 40, 30);
                b = graph.insertVertex(null, null, "b", 80, 70, 40, 30);
                edge = graph.insertEdge(null, null, "label", a, b);
            } finally {
                model.endUpdate();
            }
            graph.view.validate();
            const original = mxCoordinateAssignment.prototype.setEdgePosition, sentinel = {
                owned: true
            }, oldGeometry = window.geometry;
            window.geometry = sentinel;
            try {
                mxCoordinateAssignment.prototype.setEdgePosition = LocalCoordinate.prototype.setEdgePosition;
                new mxHierarchicalLayout(graph).execute(graph.getDefaultParent());
                check(window.geometry === sentinel, "257 global geometry");
                check(edge.geometry.relative && edge.geometry.x === 0 && edge.geometry.y === 0, "257 reset labels");
            } finally {
                mxCoordinateAssignment.prototype.setEdgePosition = original;
                if (oldGeometry === undefined) delete window.geometry; else window.geometry = oldGeometry;
            }
            for (const shape of [ "link", "pipe", "flexArrow" ]) {
                graph.setCellStyle("shape=" + shape + ";startArrow=block;endArrow=block;", [ edge ]);
                graph.view.validate();
                const state = graph.view.getState(edge);
                state.absolutePoints = [ new mxPoint(30, 20), new mxPoint(30, 20) ];
                const handles = Graph.handleFactory[shape](state) || [];
                check(handles.length > 0, "256 actual " + shape + " handles");
                for (const handle of handles) {
                    try {
                        const p = handle.getPosition(state.getPaintBounds());
                        check(p && Number.isFinite(p.x) && Number.isFinite(p.y), "256 actual " + shape + " position");
                        handle.setPosition(state.getPaintBounds(), new mxPoint(35, 25), {
                            getEvent: () => ({})
                        });
                        check(Object.values(state.style).every((v => typeof v !== "number" || Number.isFinite(v))), "256 actual " + shape + " style");
                        handle.redraw();
                        check(!/NaN|Infinity/.test(handle.shape?.node?.outerHTML || ""), "256 actual " + shape + " DOM");
                    } finally {
                        handle.destroy();
                    }
                }
            }
            const codec = {};
            Function("owned", code.codec.replace(/^[\w$]+\.encodeCell/, "owned.encodeCell"))(codec);
            const old = mxCodecRegistry.getCodec(mxGraphView).encodeCell;
            const fn = function() {};
            mxStyleRegistry.putValue("ownedPerimeter", fn);
            graph.getStylesheet().putCellStyle("ownedViewStyle", {
                ownedFunction: fn,
                unknownFunction: function() {},
                primitive: "retained"
            });
            graph.setCellStyle("ownedViewStyle", [ a ]);
            graph.view.validate();
            try {
                mxCodecRegistry.getCodec(mxGraphView).encodeCell = codec.encodeCell;
                const xml = mxUtils.getViewXml(graph);
                const nodes = xml.getElementsByTagName("vertex");
                const node = Array.from(nodes).find((n => n.getAttribute("primitive") === "retained"));
                check(node && node.getAttribute("ownedFunction") === "ownedPerimeter" && !node.hasAttribute("unknownFunction"), "258 registered function view export");
            } finally {
                mxCodecRegistry.getCodec(mxGraphView).encodeCell = old;
                delete mxStyleRegistry.values.ownedPerimeter;
                graph.destroy();
                graph.container?.remove();
            }
            return {
                failures: failures,
                notes: notes
            };
        }), {
            code: code
        });
        fs.writeFileSync(path.join(out, theme + "-runtime-" + variant + ".json"), JSON.stringify(result, null, 2));
        if (!process.env.AIDE_RUNTIME_CATALOG) assert.deepEqual(result.failures, [], variant);
        checks.push({
            case: "runtime " + variant,
            theme: theme,
            result: result.failures.length ? "EXPECTED_FAIL" : "PASS",
            failures: result.failures
        });
    }
    return checks;
};
