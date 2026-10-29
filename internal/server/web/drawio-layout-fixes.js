/* Local draw.io fixes installed against bundled globals before App.main. */
(function (root, factory) {
  if (typeof module === 'object' && module.exports) module.exports = factory;
  else root.AideDrawioLayouts = factory(root);
})(typeof window !== 'undefined' ? window : globalThis, function (root) {
  'use strict';
  const workLimit = 1000000, pointLimit = 128;
  let installed = false;
  function maps(graph) {
    for (const key of ['asyncLayoutsApplying', 'asyncLayoutsRunning']) {
      if (graph[key] == null || Object.getPrototypeOf(graph[key]) !== null) {
        graph[key] = Object.assign(Object.create(null), graph[key]);
      }
    }
  }
  function install() {
    if (installed) return;
    const Graph = root.Graph, View = root.mxGraphView, Table = root.TableLayout;
    if (!Graph || !Graph.prototype.scheduleAsyncLayout || !Graph.prototype.runAsyncLayout ||
        !View || !View.prototype.updateLineJumps || !View.prototype.resetValidationState ||
        !Table || !Table.prototype.getRowLayout) throw new Error('draw.io layout runtime unavailable');
    for (const name of ['scheduleAsyncLayout', 'runAsyncLayout']) {
      const original = Graph.prototype[name];
      Graph.prototype[name] = function () {maps(this);return original.apply(this, arguments);};
    }
    const rowLayout = Table.prototype.getRowLayout;
    Table.prototype.getRowLayout = function (row) {
      const positions = rowLayout.apply(this, arguments), cells = this.graph.model.getChildCells(row, true);
      const result = [];let index = 0, position = this.graph.getActualStartSize(row, true).x;
      for (const cell of cells) {
        if (this.graph.getCellGeometry(cell) != null) position = positions[index++];
        // Missing geometry occupies a zero-width column, preserving every following index.
        result.push(position);
      }
      return result;
    };
    const reset = View.prototype.resetValidationState, jumps = View.prototype.updateLineJumps;
    View.prototype.resetValidationState = function () {
      const result = reset.apply(this, arguments);
      this.aideLineJumpWork = workLimit;this.aideLineJumpsLimited = false;
      return result;
    };
    View.prototype.updateLineJumps = function (state) {
      const points = state.absolutePoints, edges = this.validEdges;
      if (!Graph.lineJumpsEnabled || !points || !edges || !state.style ||
          root.mxUtils.getValue(state.style, 'jumpStyle', 'none') === 'none') return jumps.apply(this, arguments);
      if (this.aideLineJumpWork == null) this.aideLineJumpWork = workLimit;
      const degrade = () => {
        this.aideLineJumpWork = 0;this.aideLineJumpsLimited = true;
        const changed = state.routedPoints != null;state.routedPoints = null;return changed;
      };
      if (points.length > pointLimit || edges.length > this.aideLineJumpWork || this.aideLineJumpWork <= 0) return degrade();
      // Charge the scan itself; never add a new unbounded quadratic preflight.
      let segments = 0;
      for (const edge of edges) {
        this.aideLineJumpWork--;
        if (edge.absolutePoints) segments += Math.max(0, edge.absolutePoints.length - 1);
        if (segments > workLimit) return degrade();
      }
      // Conservative bound includes waypoint scans, segment pairs and insertion-sorting all intersections.
      const cost = Math.max(1, points.length - 1) * (1 + points.length + edges.length + segments + segments * segments);
      if (cost > this.aideLineJumpWork) return degrade();
      this.aideLineJumpWork -= cost;
      return jumps.apply(this, arguments);
    };
    installed = true;
  }
  return {install, workLimit, pointLimit, get installed() {return installed;}};
});
