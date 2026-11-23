declare var module: any;
/* Resource limits for the locally embedded draw.io runtime (including app.min.js). */
(function (root: any, factory: any) {
  if (typeof module === 'object' && module.exports) module.exports = factory;
  else root.AideDrawioSafety = factory(root);
})(typeof window !== 'undefined' ? window : globalThis, function (root: any): any {
  'use strict';
  const limits = Object.freeze({bytes: 8 * 1024 * 1024, expandedTotal: 16 * 1024 * 1024,
    pages: 64, nodes: 50000, xmlDepth: 128, cells: 10000, cellDepth: 128});
  let installed = false;
  function fail(): never {
    const error: any = new Error('draw.io preview resource limit exceeded');
    error.code = 'AIDE_DRAWIO_LIMIT';throw error;
  }
  function byteLength(text: any): number {
    if (typeof text !== 'string' || text.length > limits.bytes) fail();
    let bytes = 0;
    for (let i = 0; i < text.length; i++) {
      const c = text.charCodeAt(i);
      bytes += c < 128 ? 1 : c < 2048 ? 2 : 3;
      if (c >= 0xd800 && c <= 0xdbff && i + 1 < text.length &&
          text.charCodeAt(i + 1) >= 0xdc00 && text.charCodeAt(i + 1) <= 0xdfff) {bytes++;i++;}
      if (bytes > limits.bytes) fail();
    }
    return bytes;
  }
  // Scan before DOMParser allocation. Quoted delimiters, comments and CDATA are opaque.
  function checkXml(text: any): void {
    byteLength(text);
    let i = 0, depth = 0, nodes = 0;
    while (i < text.length) {
      if (++nodes > limits.nodes) fail();
      if (text[i] !== '<') {const end = text.indexOf('<', i);i = end < 0 ? text.length : end;continue;}
      let delimiter: any;
      if (text.startsWith('<!--', i)) delimiter = '-->';
      else if (text.startsWith('<![CDATA[', i)) delimiter = ']]>';
      else if (text.startsWith('<?', i)) delimiter = '?>';
      else if (text.startsWith('<!', i)) fail(); // No DTD/entity expansion in previews.
      if (delimiter) {const end = text.indexOf(delimiter, i + 2);i = end < 0 ? text.length : end + delimiter.length;continue;}
      let quote = '', end = i + 1;
      for (; end < text.length; end++) {
        const c = text[end];
        if (quote) {if (c === quote) quote = '';}
        else if (c === '"' || c === "'") quote = c;
        else if (c === '>') break;
      }
      if (text[i + 1] === '/') depth--;
      else {if (++depth > limits.xmlDepth) fail();if (/\/\s*$/.test(text.slice(i, end))) depth--;}
      i = end + 1;
    }
  }
  function checkParents(entries: any): void {
    const parents = new Map<any, any>(), depths = new Map<any, any>();
    if (entries.length > limits.cells * 2) fail();
    for (const entry of entries) {
      if (!entry.id) continue;
      if (parents.has(entry.id) && parents.get(entry.id) !== entry.parent) fail();
      parents.set(entry.id, entry.parent);
    }
    for (const id of parents.keys()) {
      const path: any[] = [], active = new Set<any>();let current = id;
      while (parents.has(current) && !depths.has(current)) {
        if (active.has(current) || path.length >= limits.cellDepth) fail();
        active.add(current);path.push(current);current = parents.get(current);
      }
      let depth = depths.get(current) || 0;
      while (path.length) {if (++depth > limits.cellDepth) fail();depths.set(path.pop(), depth);}
    }
  }
  function checkDom(doc: any): void {
    if (!doc || !doc.documentElement) return;
    const stack: any[] = [[doc.documentElement, 1, null]], groups = new Map<any, any>();let nodes = 0, cells = 0;
    while (stack.length) {
      const [node, depth, enclosing] = stack.pop()!;
      if (++nodes > limits.nodes || depth > limits.xmlDepth) fail();
      let group = enclosing;
      if (node.nodeType === 1) {
        if (node.nodeName === 'mxGraphModel') group = node;
        if (node.nodeName === 'mxCell') {
          if (++cells > limits.cells) fail();
          if (!groups.has(group)) groups.set(group, []);
          const entries = groups.get(group), parent = node.getAttribute('parent');
          entries.push({id: node.getAttribute('id'), parent});
          const wrapper = node.parentNode;
          if (wrapper && wrapper.nodeType === 1 && wrapper.nodeName !== 'root' && wrapper.nodeName !== 'mxCell') {
            const id = wrapper.getAttribute('id');if (id) entries.push({id, parent});
          }
        }
      }
      for (let child = node.lastChild; child; child = child.previousSibling) {
        if (stack.length >= limits.nodes) fail();stack.push([child, depth + (child.nodeType === 1 ? 1 : 0), group]);
      }
    }
    for (const entries of groups.values()) checkParents(entries);
  }
  function checkRoot(cell: any): void {
    if (cell == null) return;
    const stack: any[] = [[cell, 1]], seen = new Set<any>();
    while (stack.length) {
      const [current, depth] = stack.pop()!;
      if (seen.has(current) || depth > limits.cellDepth || seen.size >= limits.cells) fail();
      seen.add(current);
      const count = current.getChildCount();
      if (!Number.isSafeInteger(count) || count < 0 || count + stack.length + seen.size > limits.cells) fail();
      for (let i = 0; i < count; i++) stack.push([current.getChildAt(i), depth + 1]);
    }
  }
  function decompress(text: any, inflate?: any, checked?: any): any {
    if (text == null || text.length === 0) return text;
    byteLength(text);
    const input = root.Graph.stringToArrayBuffer(root.atob(text));
    const stream = new root.pako.Inflate({raw: !inflate, chunkSize: 16384});
    const chunks: any[] = [];let size = 0;
    stream.onData = (chunk: any): void => {size += chunk.length;if (size > limits.bytes) fail();chunks.push(chunk);};
    // A limit exception escapes push immediately; no output accumulator or later input chunks.
    for (let pos = 0; pos < input.length; pos += 4096) {
      stream.push(input.subarray(pos, pos + 4096), pos + 4096 >= input.length);
      if (stream.err) throw new Error('Invalid compressed draw.io diagram');
    }
    if (!stream.ended) throw new Error('Incomplete compressed draw.io diagram');
    const output = new Uint8Array(size);let offset = 0;
    for (const chunk of chunks) {output.set(chunk, offset);offset += chunk.length;}
    const result = decodeURIComponent(new root.TextDecoder('utf-8', {fatal: true}).decode(output));
    return checked ? result : root.Graph.zapGremlins(result);
  }
  function notify(fn: any): any {
    return function (this: any, ...args: any[]): any {
      try {return fn.apply(this, args);}
      catch (error: any) {
        if (error.code === 'AIDE_DRAWIO_LIMIT' && !error.aideNotified) {
          error.aideNotified = true;
          if (root.parent && root.parent !== root) root.parent.postMessage(JSON.stringify({event: 'aide-preview-error'}), root.location.origin);
        }
        throw error;
      }
    };
  }
  function install(): void {
    if (installed) return;
    if (!root.Graph || !root.Graph.decompress || !root.mxUtils || !root.mxUtils.parseXml ||
        !root.mxCodec || !root.mxCodec.prototype.updateElements || !root.mxGraphModel ||
        !root.mxGraphModel.prototype.setRoot || !root.mxObjectCodec || !root.mxObjectCodec.prototype.decode ||
        !root.pako || !root.pako.Inflate) throw new Error('draw.io safety runtime unavailable');
    const parse = root.mxUtils.parseXml, update = root.mxCodec.prototype.updateElements, setRoot = root.mxGraphModel.prototype.setRoot,
      decode = root.mxObjectCodec.prototype.decode;
    let decodeDepth = 0;
    root.Graph.decompress = notify(decompress);
    root.mxUtils.parseXml = notify(function (this: any, text: any): any {
      checkXml(text);const doc = parse.apply(this, arguments);checkDom(doc);
      // Inspect inactive pages too, before any page is decoded by mxCodec.
      if (doc.documentElement && doc.documentElement.nodeName === 'mxfile') {
        let total = 0, pages = 0, nodes = 0;
        for (const diagram of doc.documentElement.childNodes) {
          if (diagram.nodeType !== 1 || diagram.nodeName !== 'diagram') continue;
          if (++pages > limits.pages) fail();
          const compressed = !diagram.firstElementChild && diagram.textContent.trim();
          if (compressed) {
            const expanded = decompress(compressed);total += byteLength(expanded);if (total > limits.expandedTotal) fail();
            checkXml(expanded);const page = parse.call(this, expanded);checkDom(page);
            if (page.documentElement && page.documentElement.nodeName === 'mxfile') fail();
            nodes += page.getElementsByTagName('*').length;if (nodes > limits.nodes) fail();
          }
        }
      }
      return doc;
    });
    root.mxCodec.prototype.updateElements = notify(function (this: any): any {
      if (this.elements == null) checkDom(this.document);
      return update.apply(this, arguments);
    });
    root.mxGraphModel.prototype.setRoot = notify(function (this: any, cell: any): any {checkRoot(cell);return setRoot.apply(this, arguments);});
    // Also bound lazy cross-model/source/target references, which need not follow one model's parent table.
    root.mxObjectCodec.prototype.decode = notify(function (this: any): any {
      if (decodeDepth >= limits.cellDepth) fail();
      decodeDepth++;
      try {return decode.apply(this, arguments);} finally {decodeDepth--;}
    });
    installed = true;
  }
  return {install, limits, checkParents, checkXml, checkDom, checkRoot, get installed() {return installed;}};
});
