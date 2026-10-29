'use strict';
const fs = require('fs'), path = require('path'), assert = require('assert/strict');
const {chromium} = require('./browser-runtime/node_modules/playwright');
const cases = require('./fixtures/mermaid-svg-issue-cases.json');
function control(row) {
  let source = row.source.replaceAll('__proto__', 'ordinary').replaceAll('constructor', 'ordinary');
  if (row.issue === 517) source = 'timeline\n2002 : period\n : next';
  if (row.issue === 522) source = 'sankey-beta\nA,B,1';
  if (row.issue === 526) source = 'xychart-beta\nline [1, 2, 3]';
  return {...row, source, error: undefined, phase: 'control', label: 'ordinary'};
}
async function inspect(page, row) {
  return page.evaluate(async row => {
    const before = JSON.stringify(Object.getOwnPropertyNames(Object.prototype).sort());
    const source = row.source.replace('HUGE', '1'+'0'.repeat(400));
    try {
      const {svg} = await mermaid.render('owned-diagram', source);
      document.querySelector('main').innerHTML = svg;
      const el = document.querySelector('main svg');
      const info = {svg: !!el, text: [...el.querySelectorAll('text')].map(n=>n.textContent).join('\n'),
        title: el.querySelector('.flowchartTitleText')?.textContent,
        invalid: /NaN|Infinity/.test(svg), slices: el.querySelectorAll('.pieCircle').length};
      if (row.issue === 528) {
        const d = await mermaid.mermaidAPI.getDiagramFromText(source);
        info.tasks = d.db.getTasks().map(t=>({task:t.task.trim(),id:t.id,start:t.startTime?.toISOString(),end:t.endTime?.toISOString()}));
      }
      if (before !== JSON.stringify(Object.getOwnPropertyNames(Object.prototype).sort())) throw Error('Object.prototype changed');
      return info;
    } catch(e) { return {error:e.message,name:e.name,stack:e.stack}; }
  }, row);
}
(async () => {
  const out = process.env.AIDE_COMPONENT_EVIDENCE_DIR;
  assert(out); fs.mkdirSync(out, {recursive:true});
  const browser = await chromium.launch({executablePath:'/usr/bin/chromium',headless:true,args:['--no-sandbox','--disable-dev-shm-usage']});
  const results = []; let context;
  try {
    context = await browser.newContext();
    const selected = cases.filter(r=>!process.env.AIDE_REVIEW_ISSUES||process.env.AIDE_REVIEW_ISSUES.split(',').includes(String(r.issue)));
    for (const fixture of selected) {
      const page = await context.newPage();
      try {
        await page.setContent('<!doctype html><html><body><main></main></body></html>');
        await page.addScriptTag({path:process.env.AIDE_MERMAID_SVG_BASELINE||'internal/server/web/vendor/mermaid.min.js'});
        await page.evaluate(()=>mermaid.initialize({startOnLoad:false,securityLevel:'strict',theme:'base',flowchart:{htmlLabels:false}}));
        // Reuse each database across ordinary/special/ordinary/special diagrams to test clear/reset paths.
        const special = {...fixture,phase:'regression',label:fixture.issue===515?'constructor':'__proto__'};
        for (const row of process.env.AIDE_REVIEW_RED_ONLY ? [special] : [control(fixture),special,control(fixture),{...special,phase:'reset'}]) {
          const result = await inspect(page,row);
          try {
            if (row.error) assert(result.error && new RegExp(row.error,'i').test(result.error),JSON.stringify(result));
            else {
              assert(!result.error,JSON.stringify(result));
              assert(result.svg && !result.invalid,JSON.stringify(result));
              if ([469,524,525,527].includes(row.issue)) assert(result.text.includes(row.label),JSON.stringify(result));
              if (row.issue===515) assert(result.text.includes(row.label) && result.slices===1,JSON.stringify(result));
              if (row.issue===521) assert.equal(result.title,'Q&A < draft');
              if (row.issue===523) assert(result.text.includes('Sample'));
              if (row.issue===528) assert(result.tasks.find(t=>t.task==='Child').start.startsWith('2024-01-04'),JSON.stringify(result));
            }
            results.push({issue:row.issue,phase:row.phase,result:'PASS',...result});
          } catch(e) {results.push({issue:row.issue,phase:row.phase,result:'FAIL',error:e.message});}
        }
      } finally {await page.close();}
    }
    fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(results,null,2));
    console.log(JSON.stringify(results));
    if (!process.env.AIDE_EXPECT_REVIEW_FAILURES) assert.deepEqual(results.filter(r=>r.result!=='PASS'),[]);
  } finally {if(context)await context.close();await browser.close();}
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
