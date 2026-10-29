'use strict';
// Observational review of the existing AWS17 SDK palette, not a requirement
// that its historical internal stencil aliases must stay unchanged.
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path');
const {assignment} = require('./test_drawio_layout_fixes.cjs');
module.exports = async function(frame, out, theme) {
  const results = [];
  for (const file of ['diagramly/sidebar/Sidebar-AWS3.js', 'app.min.js', 'integrate.min.js']) {
    const source = fs.readFileSync('internal/server/web/vendor/drawio/js/' + file, 'utf8');
    const observed = await frame.evaluate(({method}) => {
      const entries = [], receiver = Object.create(Sidebar.prototype);
      receiver.createVertexTemplateEntry = function(style, width, height, value, title, showLabel, showTitle, tags) {
        const entry = {style, width, height, value, title, tags};entries.push(entry);return entry;
      };
      let palette;
      receiver.addPaletteFunctions = function(id, title, expanded, functions) {palette = {id, title, count:functions.length};};
      // Evaluate the trusted shipped assignment in an isolated function scope.
      // Only capture the factory arguments; leave the live sidebar untouched.
      const capture = Function('Sidebar', method + 'return Sidebar.prototype.addAWS3SDKPalette;');
      function CapturedSidebar() {}
      capture(CapturedSidebar).call(receiver);
      const graph = ownedEditor.graph, model = graph.model;
      graph.selectAll();graph.removeCells(graph.getSelectionCells());graph.clearSelection();
      const rendered = [];
      model.beginUpdate();
      try {
        for (const [i, entry] of entries.entries()) {
          const x = 40 + (i % 7) * 135, y = 80 + Math.floor(i / 7) * 175;
          // Keep the palette's original empty cell value. Separate labels are
          // review annotations and do not claim the palette inserts text.
          const cell = graph.insertVertex(graph.getDefaultParent(), null, entry.value, x, y, entry.width, entry.height, entry.style);
          graph.insertVertex(graph.getDefaultParent(), null, entry.title, x - 20, y + 90, 120, 50, 'text;whiteSpace=wrap;html=0;align=center;verticalAlign=top;');
          rendered.push(cell);
        }
      } finally {model.endUpdate();}
      const icons = rendered.map(cell => {
        const state = graph.view.getState(cell), desc = state?.shape?.stencil?.desc;
        if (!desc || !state.shape.node.getBoundingClientRect().width) throw Error('SDK stencil did not render');
        return {shape:state.style.shape, fill:state.style.fillColor, stencil:desc.getAttribute('name'),
          textElements:desc.getElementsByTagName('text').length, imageElements:desc.getElementsByTagName('image').length,
          paths:desc.getElementsByTagName('path').length, svgPaths:state.shape.node.querySelectorAll('path').length};
      });
      const android = mxStencilRegistry.getStencil('mxgraph.aws3.android').desc;
      const ios = mxStencilRegistry.getStencil('mxgraph.aws3.ios').desc;
      return {palette, entries, icons, catalogue:{android:mxUtils.getXml(android),ios:mxUtils.getXml(ios)}};
    }, {method:assignment(source, 'Sidebar.prototype.addAWS3SDKPalette')});
    assert.equal(observed.palette.id,'aws3SDKs');assert.equal(observed.entries.length,14);
    for (const [i,entry] of observed.entries.entries()) {
      assert.equal(entry.value,'');assert.ok(entry.tags.length > 0);assert.ok(observed.icons[i].svgPaths > 0);
    }
    assert.equal(observed.icons.filter(icon=>icon.stencil.toLowerCase()==='android').length,10);
    for(const icon of observed.icons){assert.equal(icon.textElements,0);assert.equal(icon.imageElements,0);}
    fs.writeFileSync(path.join(out,theme+'-sdk-'+path.basename(file)+'.json'),JSON.stringify(observed,null,2)+'\n');
    await frame.evaluate(()=>ownedUi.actions.get('fitWindow').funct());
    await frame.page().screenshot({path:path.join(out,theme+'-sdk-'+path.basename(file)+'.png')});
    results.push({case:'AWS17 SDK actual palette factory capture and real stencil rendering; review evidence',file,theme,result:'PASS',entries:14});
  }
  return results;
};
