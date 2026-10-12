'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');

const js = fs.readFileSync('internal/server/web/markdown-media.js', 'utf8');
const css = fs.readFileSync('internal/server/web/macos.css', 'utf8');
const en = fs.readFileSync('internal/server/web/locales/en.js', 'utf8');
const html = fs.readFileSync('internal/server/web/index.html', 'utf8');
const templateSource = js.slice(js.indexOf('const templates=['), js.indexOf('for(const [name,description'));
assert.ok(templateSource.length > 0, 'template catalog must exist');
const templates = [...templateSource.matchAll(/\['([^']+)','([^']+)','([^']+)','/g)];
assert.equal(templates.length, 26, 'all Markdown templates stay available as direct tools');
const required = ['paragraph','h1','h2','h3','h4','h5','h6','bold','italic','strike','inline','code','mermaid','table','link','image','quote','unordered','ordered','task','rule','footnote','alert','details','math','frontmatter'];
assert.deepEqual(templates.map(([, , , icon]) => icon), required);
for (const [, name, description] of templates) {
  assert.ok(en.includes(`"${name}"`), `English label is missing: ${name}`);
  assert.ok(en.includes(`"${description}"`), `English usage hint is missing: ${description}`);
}
assert.ok(js.includes("item.className='md-tool-icon md-template-tool'"), 'templates use the same toolbar level as media and diagram buttons');
assert.ok(js.includes('templateTools.append(item)'), 'templates are direct buttons, not a popup menu');
assert.ok(js.includes('item.title=accessible;item.setAttribute(\'aria-label\',accessible)'), 'each template exposes its name and usage hint');
assert.ok(js.includes("item.addEventListener('focus'"), 'keyboard focus announces template guidance');
assert.ok(js.includes("editor.setSelectionRange(start,start+placeholder.length)"), 'insertion selects the editable example text');
assert.match(css, /\.md-template-tools\{[^}]*overflow-x:auto/);
assert.ok(css.includes('.md-template-tools{max-width:calc(100% - 128px)'), 'narrow layout keeps the template row scrollable beside media controls');
for (const asset of ['/markdown-media.js?v=3', '/locales/en.js?v=108', '/task-outcome.js?v=8', '/macos.css?v=109']) {
  assert.ok(html.includes(asset), `updated UI asset must bypass the old browser cache: ${asset}`);
}
console.log('PASS: 26 direct Markdown template icons, labels, guidance, insertion affordance and narrow-screen scrolling');
