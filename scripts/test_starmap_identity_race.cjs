'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync('internal/server/web/starmap.js', 'utf8');
function functions() {
  return source.slice(source.indexOf('function applyLiveUpdate('), source.indexOf('function syncMotion()'));
}
function fixture(cached = false) {
  const elements = new Map();
  const element = id => {
    if (!elements.has(id)) elements.set(id, {textContent: 'private A', value: 'private query A', hidden: false,
      children: ['private A'], replaceChildren() {this.children = [];this.textContent = '';}});
    return elements.get(id);
  };
  let identityReads = 0, updateReads = 0, finish;
  const c = {
    $: element, uiTextStarmap: value => value, AbortController, clearTimeout, params: new URLSearchParams(),
    locked: false, loading: false, graphGeneration: 0, codeView: false, recoveryInput: 0,
    recoveryAttempt: null, recoveryScopeLoaded: 'A', pendingRecoveredView: {version: 1, query: 'old A'},
    cacheIdentity: null, cacheWriteTimer: 0, liveCursor: 'A1', liveFailures: 0,
    graph: {workspace: 'A', nodes: [{id: 'private-A'}], edges: []}, selected: {id: 'private-A'},
    nodeByID: new Map([['private-A', {}]]), documentBatch: null, documentGeneration: 0,
    documentAbort: {abort() {c.documentAborted = true;}}, aiAbort: {signal: {aborted: false}},
    aiExplorer: {cancel() {c.aiCanceled = true;}}, filePreview: {close() {c.previewClosed = true;}},
    clearEvidencePath() {}, stopLive() {}, stellarLogbook: {connecting() {}, interrupted() {}},
    syncCodeView() {}, say() {}, redraw() {}, scheduleLive() {}, progressiveDelay: () => 1000,
    beginGraphTransition() {}, settlePanels() {}, progressiveSummary: () => '', syncGraphChrome() {},
    liveStableScenes: new Map(), liveWireNodes: new Map([['private-A', JSON.stringify({id: 'private-A'})]]),
    pathAbort: null, pathResult: null, cosmicScene: null,
    revealPanel(node, shown) {node.hidden = !shown;},
    layout() {c.nodeByID = new Map(c.graph.nodes.map(node => [node.id, node]));},
    search() {element('results').replaceChildren();}, resetDocumentBatch() {c.documentBatch = null;},
    knowledgeTimeline: {sync() {}}, scheduleGraphCache() {c.cacheWrites = (c.cacheWrites || 0) + 1;},
    restoreSkyView() {}, defaultSkyView: () => ({}),
    window: {AideStarmapCache: {read: async (_mode, identity) => cached && identity.workspace === 'A'
      ? {workspace: 'A', revision: 'A-cached', graph: {nodes: [{id: 'private-A'}], edges: []}} : null},
      AideContinuity: {readTab: async () => null}},
    api: async url => {
      if (url.startsWith('/identity')) return {workspace: ++identityReads === 1 ? 'A' : 'B', epoch: 'owned', policy: 'owned'};
      if (++updateReads === 1) return {workspace: 'B', revision: 'B1', reset: true, nodes: [{id: 'private-B-unverified'}], edges: []};
      return new Promise(resolve => {finish = resolve;});
    }
  };
  vm.createContext(c);vm.runInContext(functions(), c);
  return {c, element, get identityReads() {return identityReads;}, finish: () => finish({workspace: 'B', revision: 'B2', reset: true, nodes: [], edges: []})};
}
async function main() {
  for (const cached of [false, true]) {
    const f = fixture(cached), c = f.c;
    await vm.runInContext('load()', c);await new Promise(resolve => setImmediate(resolve));
    assert.equal(c.selected, null, 'mismatched response must invalidate selected prior-workspace detail');
    assert.equal(c.graph.workspace, 'B');assert.equal(c.nodeByID.size, 0, 'render lookup must be reset');
    assert.equal(c.previewClosed, true);assert.equal(f.element('detail').hidden, true);
    for (const id of ['answer', 'usage', 'ai-found']) assert.equal(f.element(id).textContent, '', id + ' must clear');
    for (const id of ['search', 'question']) assert.equal(f.element(id).value, '', id + ' must clear');
    assert.equal(c.pendingRecoveredView, null, 'prior recovery projection must be discarded');
    assert.equal(c.documentAborted, true);assert.equal(c.aiCanceled, true);assert.equal(c.liveCursor, '');
    assert.equal(f.identityReads, 2, 'mismatch must immediately refetch identity before applying data');
    assert.equal(c.cacheWrites || 0, 0, 'unverified mismatched response cannot enter cache');
    f.finish();await new Promise(resolve => setImmediate(resolve));
    assert.equal(c.liveCursor, 'B2');assert.equal(c.loading, false);
  }
  console.log('PASS mid-load identity mismatch clears prior graph/detail/search/AI/recovery and reloads identity, with/without cached graph');
}
module.exports = {functions};
if (require.main === module) main().catch(error => {console.error(error);process.exitCode = 1;});
