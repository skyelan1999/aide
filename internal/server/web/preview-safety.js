'use strict';
(function (root) {
  const MiB = 1024 * 1024;
  const limits = Object.freeze({dxfBytes: 4 * MiB, dxfLines: 100000, dxfEntities: 50000, dxfDepth: 32, dxfNodes: 50000, dxfPoints: 200000});
  function limit(key) {
    const error = new Error(root.aideI18n?.t(key) || key);error.code = 'AIDE_PREVIEW_LIMIT';return error;
  }
  const oversized = () => limit('文件超过预览大小限制，请下载后查看。');
  async function readBytes(response, maxBytes) {
    const declared = response.headers?.get('Content-Length');
    if (declared && /^\d+$/.test(declared) && Number(declared) > maxBytes) {
      await response.body?.cancel();throw oversized();
    }
    if (!response.body?.getReader) throw limit('浏览器不支持有界读取，请下载后查看。');
    const reader = response.body.getReader(), chunks = [];let total = 0;
    try {
      while (true) {
        const {done, value} = await reader.read();if (done) break;
        if (!value || !Number.isSafeInteger(value.byteLength)) throw oversized();
        total += value.byteLength;if (total > maxBytes) throw oversized();
        chunks.push(value);
      }
      const bytes = new Uint8Array(total);let offset = 0;
      for (const chunk of chunks) {bytes.set(chunk, offset);offset += chunk.byteLength;}
      return bytes;
    } catch (error) {
      try {await reader.cancel();} catch (_) {}throw error;
    } finally {reader.releaseLock();}
  }
  function checkDxfText(text) {
    let lines = 1;
    for (let i = 0; i < text.length; i++) {
      if (text[i] === '\r') {lines++;if (text[i + 1] === '\n') i++;}
      else if (text[i] === '\n') lines++;
      if (lines > limits.dxfLines) throw limit('DXF 实体或几何数量超过预览限制。');
    }
  }
  function createDxfBudget(doc) {
    let sourceEntities = doc?.entities?.length || 0;
    for (const block of Object.values(doc?.blocks || {})) {
      sourceEntities += block?.entities?.length || 0;
      if (sourceEntities > limits.dxfEntities) break;
    }
    if (sourceEntities > limits.dxfEntities) throw limit('DXF 实体或几何数量超过预览限制。');
    const active = new Set();let entities = 0, nodes = 0, points = 0;
    const exceeded = () => limit('DXF 实体或几何数量超过预览限制。');
    return {
      visit() {if (++entities > limits.dxfEntities) throw exceeded();},
      node() {if (++nodes > limits.dxfNodes) throw exceeded();},
      point() {if (++points > limits.dxfPoints) throw exceeded();},
      block(name, draw) {
        if (active.has(name) || active.size >= limits.dxfDepth) throw limit('DXF 块引用存在循环或层级过深。');
        active.add(name);try {return draw();} finally {active.delete(name);}
      }
    };
  }
  const api = {limits, readBytes, checkDxfText, createDxfBudget, limit, isLimitError: error => error?.code === 'AIDE_PREVIEW_LIMIT'};
  root.AidePreviewSafety = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof window !== 'undefined' ? window : globalThis);
