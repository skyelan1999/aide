'use strict';

// Backups retain their real path; only their presentation type ignores one
// trailing .bak suffix (for example report.xlsx.bak -> report.xlsx).
(function exposeFileTypes(root) {
  function effectiveTypePath(filePath) {
    let value = String(filePath || '');
    if (!/\.bak$/i.test(value)) return value;
    value = value.slice(0, -4);
    // aide backup names may carry a version marker after the original suffix:
    // report.md.v0.10.bak and report.md.pre-d7.bak still represent Markdown.
    return value.replace(/(?:\.v\d+(?:\.\d+)*|\.pre-[^.]+)+$/i, '');
  }

  function extension(filePath) {
    const value = effectiveTypePath(filePath).split(/[\\/]/).pop() || '';
    const dot = value.lastIndexOf('.');
    return dot > 0 ? value.slice(dot + 1).toLowerCase() : '';
  }

  const api = { effectiveTypePath, extension };
  if (typeof module === 'object' && module.exports) module.exports = api;
  else root.AideFileTypes = api;
})(globalThis);
