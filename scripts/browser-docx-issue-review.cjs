"use strict";

const assert = require("node:assert/strict"), fs = require("node:fs"), path = require("node:path"), JSZip = require("../internal/server/web/vendor/jszip.min.js"), {chromium: chromium} = require("./browser-runtime/node_modules/playwright");

const web = path.resolve("internal/server/web");

async function fixture() {
    const z = new JSZip, w = "http://schemas.openxmlformats.org/wordprocessingml/2006/main", rel = "http://schemas.openxmlformats.org/officeDocument/2006/relationships";
    z.file("[Content_Types].xml", '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>');
    z.file("_rels/.rels", '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="' + rel + '/officeDocument" Target="word/document.xml"/></Relationships>');
    let body = '<w:p><w:pPr><w:pStyle w:val="Owned"/></w:pPr><w:r><w:t>Owned styled heading</w:t></w:r></w:p>';
    const targets = [ "javascript:window.ownedExecuted=1", "JaVa\tScRiPt:window.ownedExecuted=1", "data:text/html,bad", "vbscript:bad", "https://example.invalid/ok", "http://example.invalid/ok", "mailto:owned@example.invalid", "../relative.docx" ];
    let relationships = '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="styles" Type="' + rel + '/styles" Target="styles.xml"/>';
    for (let i = 0; i < targets.length; i++) {
        relationships += '<Relationship Id="link' + i + '" Type="' + rel + '/hyperlink" Target="' + targets[i].replaceAll("&", "&amp;").replaceAll("\t", "&#9;") + '" TargetMode="External"/>';
        body += '<w:p><w:hyperlink r:id="link' + i + '"><w:r><w:t>Link ' + i + "</w:t></w:r></w:hyperlink></w:p>";
    }
    body += '<w:p><w:bookmarkStart w:id="0" w:name="owned-anchor"/><w:hyperlink w:anchor="owned-anchor"><w:r><w:t>Internal</w:t></w:r></w:hyperlink></w:p>';
    for (const [key, char] of [ [ "valid", "263A" ], [ "supplementary", "1F680" ], [ "invalid", "41;&lt;img id=&quot;owned-injected&quot; src=&quot;/owned-symbol-probe&quot;&gt;" ], [ "surrogate", "D800" ], [ "overflow", "110000" ] ]) body += "<w:p><w:r><w:t>" + key + ':</w:t><w:sym w:font="Arial" w:char="' + char + '"/></w:r></w:p>';
    body += '<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>Numbered item</w:t></w:r></w:p>';
    body += '<w:tbl><w:tr><w:tc><w:p><w:r><w:t>Table cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl><w:sectPr><w:pgSz w:w="12240" w:h="15840"/></w:sectPr>';
    relationships += '<Relationship Id="numbering" Type="' + rel + '/numbering" Target="numbering.xml"/>';
    z.file("word/numbering.xml", '<w:numbering xmlns:w="' + w + '"><w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/></w:lvl></w:abstractNum><w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num></w:numbering>');
    z.file("word/document.xml", '<w:document xmlns:w="' + w + '" xmlns:r="' + rel + '"><w:body>' + body + "</w:body></w:document>");
    z.file("word/_rels/document.xml.rels", relationships + "</Relationships>");
    z.file("word/styles.xml", '<w:styles xmlns:w="' + w + '"><w:style w:type="paragraph" w:styleId="Owned"><w:name w:val="Owned"/><w:rPr><w:color w:val="C00000"/><w:sz w:val="40"/></w:rPr></w:style></w:styles>');
    return z.generateAsync({
        type: "uint8array"
    });
}

async function main() {
    const out = process.env.AIDE_COMPONENT_EVIDENCE_DIR;
    assert(out);
    fs.mkdirSync(out, {
        recursive: true
    });
    const browser = await chromium.launch({
        executablePath: "/usr/bin/chromium",
        headless: true,
        args: [ "--no-sandbox" ]
    });
    const context = await browser.newContext();
    const page = await context.newPage(), requests = [], errors = [], violations = [];
    page.on("pageerror", (e => errors.push(e.message)));
    const bytes = await fixture();
    const server = fs.readFileSync("internal/server/server.go", "utf8");
    const policy = [ ...server.matchAll(/Set\("Content-Security-Policy", "([^"]+)"\)/g) ].map((m => m[1])).find((s => s.includes("wasm-unsafe-eval")));
    assert(policy);
    try {
        await context.route("**/*", (async route => {
            const u = new URL(route.request().url());
            if (u.origin !== "https://aide-docx-owned.test") return route.abort();
            requests.push(u.pathname);
            if (u.pathname === "/") return route.fulfill({
                headers: {
                    "Content-Security-Policy": policy
                },
                contentType: "text/html",
                body: '<!doctype html><html><body><div id="outside" class="docx-rendered">Outside</div><main id="preview"></main></body></html>'
            });
            if (u.pathname === "/owned.docx") return route.fulfill({
                body: Buffer.from(bytes)
            });
            const p = path.resolve(web, "." + u.pathname);
            if (!p.startsWith(web + path.sep) || !fs.existsSync(p)) return route.fulfill({
                status: 404,
                body: ""
            });
            return route.fulfill({
                contentType: p.endsWith(".js") ? "application/javascript" : "text/css",
                body: fs.readFileSync(p)
            });
        }));
        await page.goto("https://aide-docx-owned.test/");
        await page.evaluate((() => {
            window.ownedViolations = [];
            document.addEventListener("securitypolicyviolation", (e => ownedViolations.push({
                directive: e.effectiveDirective,
                blocked: e.blockedURI
            })));
        }));
        for (const f of [ "/vendor/jszip.min.js", "/vendor/docx-preview/docx-preview.min.js", "/preview-safety.js" ]) await page.addScriptTag({
            url: f
        });
        if (fs.existsSync(web + "/docx-style-bridge.js")) await page.addScriptTag({
            url: "/docx-style-bridge.js"
        });
        const app = fs.readFileSync(web + "/app.js", "utf8"), start = app.indexOf("async function setupDocxPreview("), end = app.indexOf("window.AideMarkdownCode?.install", start);
        await page.evaluate((code => {
            window.isDocPath = () => false;
            window.isBackupPath = () => false;
            window.fileRawUrl = () => "/owned.docx";
            window.t = x => x;
            window.el = (tag, cls, text) => {
                const e = document.createElement(tag);
                e.className = cls || "";
                if (text) e.textContent = text;
                return e;
            };
            window.teardownViewer = c => {
                c._aideViewerTeardown?.();
            };
            window.bindViewerTeardown = (c, fn) => c._aideViewerTeardown = fn;
            window.eval(code);
        }), app.slice(start, end));
        await page.evaluate((() => setupDocxPreview(document.querySelector("#preview"), "owned.docx", "", "", {
            readOnly: true,
            origin: "owned"
        })));
        const observed = await page.evaluate((() => {
            const c = document.querySelector("#preview");
            return {
                links: [ ...c.querySelectorAll("a") ].map((a => ({
                    text: a.textContent,
                    href: a.getAttribute("href")
                }))),
                injected: !!document.querySelector("#owned-injected"),
                text: c.textContent,
                heading: [ ...c.querySelectorAll("p") ].find((p => p.textContent === "Owned styled heading"))?.outerHTML,
                style: (() => {
                    const p = [ ...c.querySelectorAll("p") ].find((p => p.textContent === "Owned styled heading"))?.querySelector("span");
                    return p ? {
                        color: getComputedStyle(p).color,
                        fontSize: getComputedStyle(p).fontSize,
                        display: getComputedStyle(c.querySelector("section")).display
                    } : null;
                })(),
                numbering: (() => {
                    const p = [ ...c.querySelectorAll("p") ].find((p => p.textContent === "Numbered item"));
                    return p ? getComputedStyle(p, "::before").content : null;
                })(),
                range: (() => {
                    const p = [ ...c.querySelectorAll("p") ].find((p => p.textContent === "Owned styled heading"));
                    const range = document.createRange();
                    range.selectNodeContents(p);
                    return range.toString();
                })(),
                outside: getComputedStyle(document.querySelector("#outside")).backgroundColor,
                violations: ownedViolations,
                adopted: document.adoptedStyleSheets.length
            };
        }));
        await page.screenshot({
            path: path.join(out, "docx.png"),
            fullPage: true
        });
        fs.writeFileSync(path.join(out, "observed.json"), JSON.stringify({
            observed: observed,
            requests: requests,
            errors: errors,
            policy: policy
        }, null, 2));
        if (!process.env.AIDE_DOCX_CATALOG) {
            for (let i = 0; i < 4; i++) assert.equal(observed.links[i].href, null, "unsafe URL " + i);
            for (let i = 4; i < 8; i++) assert(observed.links[i].href, "safe URL " + i);
            assert(observed.links[8].href?.startsWith("#"));
            assert.equal(observed.injected, false);
            assert(observed.text.includes("☺") && observed.text.includes("🚀"));
            assert.equal(observed.style.color, "rgb(192, 0, 0)");
            assert.equal(observed.style.fontSize, "26.6667px");
            assert(!requests.includes("/owned-symbol-probe"));
            assert.equal(observed.outside, "rgba(0, 0, 0, 0)");
            assert.deepEqual(errors, []);
            assert(observed.numbering && observed.numbering !== "none", "numbering pseudo-element");
            assert.equal(observed.range, "Owned styled heading");
            const multiple = await page.evaluate((async () => {
                const extra = document.createElement("main");
                extra.id = "second";
                document.body.appendChild(extra);
                await setupDocxPreview(extra, "owned.docx", "", "", {
                    readOnly: true,
                    origin: "owned"
                });
                const count = document.adoptedStyleSheets.length;
                teardownViewer(document.querySelector("#preview"));
                const p = [ ...extra.querySelectorAll("p") ].find((p => p.textContent === "Owned styled heading")).querySelector("span");
                const color = getComputedStyle(p).color;
                teardownViewer(extra);
                extra.remove();
                return {
                    count: count,
                    color: color,
                    remaining: document.adoptedStyleSheets.length
                };
            }));
            assert.deepEqual(multiple, {
                count: 2,
                color: "rgb(192, 0, 0)",
                remaining: 0
            });
            const cleanup = await page.evaluate((() => {
                teardownViewer(document.querySelector("#preview"));
                return document.adoptedStyleSheets.length;
            }));
            assert.equal(cleanup, 0);
        }
        console.log(JSON.stringify({
            result: "PASS",
            catalog: !!process.env.AIDE_DOCX_CATALOG,
            requests: requests.length,
            errors: errors
        }));
    } finally {
        await context.close();
        await browser.close();
    }
}

main().catch((e => {
    console.error(e.stack);
    process.exitCode = 1;
}));
