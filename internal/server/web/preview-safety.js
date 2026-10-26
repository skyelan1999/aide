'use strict';
(function (root) {
  const MiB = 1024 * 1024;
  const limits = Object.freeze({dxfBytes: 4 * MiB, dxfLines: 100000, dxfEntities: 50000, dxfDepth: 32, dxfNodes: 50000, dxfPoints: 200000, dxfTextChars: MiB, dxfTextEntityChars: 16384,
    docxBytes: 16 * MiB, docxPartBytes: 8 * MiB, docxExpandedBytes: 32 * MiB, docxEntries: 512, docxMs: 10000});
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
      if (lines > limits.dxfLines) throw limit('DXF 内容超过预览限制，请下载后查看。');
    }
  }
  function createDxfBudget(doc) {
    let sourceEntities = doc?.entities?.length || 0;
    for (const block of Object.values(doc?.blocks || {})) {
      sourceEntities += block?.entities?.length || 0;
      if (sourceEntities > limits.dxfEntities) break;
    }
    if (sourceEntities > limits.dxfEntities) throw limit('DXF 内容超过预览限制，请下载后查看。');
    const active = new Set();let entities = 0, nodes = 0, points = 0, textChars = 0;
    const exceeded = () => limit('DXF 内容超过预览限制，请下载后查看。');
    return {
      visit() {if (++entities > limits.dxfEntities) throw exceeded();},
      node() {if (++nodes > limits.dxfNodes) throw exceeded();},
      point() {if (++points > limits.dxfPoints) throw exceeded();},
      text(value) {textChars += value.length;if (value.length > limits.dxfTextEntityChars || textChars > limits.dxfTextChars) throw exceeded();},
      block(name, draw) {
        if (active.has(name) || active.size >= limits.dxfDepth) throw limit('DXF 块引用存在循环或层级过深。');
        active.add(name);try {return draw();} finally {active.delete(name);}
      }
    };
  }
  const badZip = () => limit('DOCX 包无效或使用不支持的 ZIP 格式。');
  const largeZip = () => limit('DOCX 解压内容超过安全预览限制，请下载后查看。');
  function checkZipDirectory(bytes) {
    if (bytes.byteLength > limits.docxBytes) throw oversized();
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);let end = -1;
    for (let i = bytes.byteLength - 22; i >= Math.max(0, bytes.byteLength - 65557); i--) {
      if (view.getUint32(i, true) === 0x06054b50 && i + 22 + view.getUint16(i + 20, true) === bytes.byteLength) {end = i;break;}
    }
    if (end < 0 || view.getUint16(end + 4, true) !== 0 || view.getUint16(end + 6, true) !== 0) throw badZip();
    const count = view.getUint16(end + 10, true), size = view.getUint32(end + 12, true), offset = view.getUint32(end + 16, true);
    if (count > limits.docxEntries) throw largeZip();
    if (view.getUint16(end + 8, true) !== count || offset + size !== end) throw badZip();
    let cursor = offset, expanded = 0;
    for (let i = 0; i < count; i++) {
      if (cursor + 46 > end || view.getUint32(cursor, true) !== 0x02014b50) throw badZip();
      const flags = view.getUint16(cursor + 8, true), method = view.getUint16(cursor + 10, true);
      const compressed = view.getUint32(cursor + 20, true), unpacked = view.getUint32(cursor + 24, true);
      const local = view.getUint32(cursor + 42, true);
      if ((flags & 1) || ![0, 8].includes(method) || compressed > bytes.byteLength || local + 30 > bytes.byteLength) throw badZip();
      expanded += unpacked;
      if (unpacked > limits.docxPartBytes || expanded > limits.docxExpandedBytes) throw largeZip();
      cursor += 46 + view.getUint16(cursor + 28, true) + view.getUint16(cursor + 30, true) + view.getUint16(cursor + 32, true);
      if (cursor > end) throw badZip();
    }
    if (cursor !== end) throw badZip();
  }
  async function checkDocx(input, JSZip) {
    const bytes = input instanceof Uint8Array ? input : new Uint8Array(input);
    checkZipDirectory(bytes); // Bound metadata allocation before JSZip parses it.
    if (JSZip?.version !== '3.10.1' || typeof JSZip.loadAsync !== 'function') throw limit('DOCX 安全解压检查不可用，请下载后查看。');
    const deadline = Date.now() + limits.docxMs;let expanded = 0;
    try {
      const zip = await JSZip.loadAsync(bytes, {createFolders: false});
      const entries = Object.values(zip.files);if (entries.length > limits.docxEntries) throw largeZip();
      for (const entry of entries) {
        if (Date.now() >= deadline) throw largeZip();
        const stream = entry.internalStream('uint8array');
        if (!stream?.on || !stream.pause || !stream.resume) throw badZip();
        await new Promise((resolve, reject) => {
          let size = 0, stopped = false;
          const finish = error => {
            if (stopped) return;stopped = true;clearTimeout(timer);
            if (error) {stream.pause();reject(error);} else resolve();
          };
          const timer = setTimeout(() => finish(largeZip()), Math.max(1, deadline - Date.now()));
          // No accumulate()/async(): discard each decoded chunk after counting.
          // Public pause stops subsequent compressed chunks. The current bounded
          // JSZip3.10.1 input chunk may finish decoding; later output is ignored.
          stream.on('data', data => {
            if (stopped) return;
            size += data.byteLength;expanded += data.byteLength;
            if (size > limits.docxPartBytes || expanded > limits.docxExpandedBytes) finish(largeZip());
          }).on('error', finish).on('end', () => finish()).resume();
        });
      }
    } catch (error) {
      if (error?.code === 'AIDE_PREVIEW_LIMIT') throw error;
      throw badZip();
    }
  }
  const api = {limits, readBytes, checkDxfText, createDxfBudget, checkDocx, limit, isLimitError: error => error?.code === 'AIDE_PREVIEW_LIMIT'};
  root.AidePreviewSafety = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof window !== 'undefined' ? window : globalThis);
