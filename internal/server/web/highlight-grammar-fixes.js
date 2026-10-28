/* Aide repairs for the pinned Highlight.js 11.9 grammar registration pipeline. */
(function (root, install) {
  const engine = typeof hljs !== 'undefined' ? hljs : root.hljs;
  if (engine) install(engine);
  else if (typeof module === 'object' && module.exports) module.exports = install;
})(typeof globalThis !== 'undefined' ? globalThis : window, function install(engine) {
  'use strict';
  if (engine.__aideGrammarRepairs) return;
  Object.defineProperty(engine, '__aideGrammarRepairs', {value: true});
  const resets = new Map();
  const scope = mode => mode.scope || mode.className;
  const source = regex => typeof regex === 'string' ? regex : regex?.source || '';
  function records(grammar) {
    const seen = new Set(), result = [];
    function visit(mode, parent, inheritedScope, inheritedContains) {
      if (!mode || typeof mode !== 'object' || seen.has(mode)) return;
      seen.add(mode);
      const current = scope(mode) || inheritedScope;
      result.push({mode, parent, scope: current, children: mode.contains || inheritedContains || []});
      for (const child of mode.contains || []) visit(child, mode);
      for (const child of mode.variants || []) visit(child, mode, current, mode.contains || inheritedContains);
      if (mode.starts) visit(mode.starts, mode);
    }
    visit(grammar);
    return result;
  }
  function balanced(mode, children, open, close, keywords) {
    const nest = {begin: open, end: close, relevance: 0, keywords,
      contains: ['self', ...children]};
    mode.contains = [nest, ...children];
  }
  function jsx(grammar, name) {
    let previous, trie;
    resets.set(name, () => { previous = trie = undefined; });
    function index(input) {
      if (input === previous) return;
      previous = input;
      trie = {children: new Map(), last: -1};
      // Index every closing tag once. Prefix positions preserve the original
      // tag detector's prefix semantics without repeated suffix searches.
      const tags = /<\/([A-Za-z0-9\\._:-]+)/g;
      for (let match; (match = tags.exec(input));) {
        let node = trie;
        for (const char of match[1]) {
          if (!node.children.has(char)) node.children.set(char, {children: new Map(), last: -1});
          node = node.children.get(char);
          node.last = match.index;
        }
      }
    }
    for (const {mode} of records(grammar)) {
      const callback = mode['on:begin'];
      if (typeof callback !== 'function' || !String(callback).includes('.input.indexOf')) continue;
      mode['on:begin'] = function (match, response) {
        const after = match.index + match[0].length, char = match.input[after];
        if (char === '<' || char === ',') { response.ignoreMatch(); return; }
        if (char === '>') {
          index(match.input);
          let node = trie;
          for (const char of match[0].slice(1)) { node = node?.children.get(char); if (!node) break; }
          if (!node || node.last < after) response.ignoreMatch();
        }
        if (/^\s*=/.test(match.input.slice(after)) || /^\s+extends\s+/.test(match.input.slice(after))) response.ignoreMatch();
      };
    }
  }
  function swift(grammar) {
    const stack = [];
    let input;
    const reset = () => { stack.length = 0; input = undefined; };
    resets.set('swift', reset);
    function begin(match, response) {
      if (match.input !== input) { reset(); input = match.input; }
      response.data.hashes = match[1].length;
      stack.push(match[1].length);
    }
    function end(match, response) {
      if (match[1].length !== stack[stack.length - 1]) { response.ignoreMatch(); return; }
      stack.pop();
    }
    const interpolation = {className: 'subst', begin: /\\(#{4,})\(/, end: /\)/,
      keywords: grammar.keywords, contains: [],
      'on:begin': (match, response) => { if (match[1].length !== stack[stack.length - 1]) response.ignoreMatch(); }};
    const raw = (beginRegex, endRegex, scopeName) => ({className: scopeName,
      begin: beginRegex, end: endRegex, contains: [interpolation],
      'on:begin': begin, 'on:end': end});
    const triple = raw(/(#{4,})"""/, /"""(#{4,})/, 'string');
    const single = raw(/(#{4,})"/, /"(#{4,})/, 'string');
    const regexp = raw(/(#{4,})\//, /\/(#{4,})/, 'regexp');
    regexp.contains.push({begin: /\[/, end: /\]/, contains: [engine.BACKSLASH_ESCAPE]}, engine.BACKSLASH_ESCAPE);
    const contains = [triple, single, regexp, ...grammar.contains];
    const parens = {begin: /\(/, end: /\)/, relevance: 0, contains: []};
    parens.contains = ['self', ...contains];
    interpolation.contains = [parens, ...contains];
    grammar.contains = contains;
  }
  function patch(name, grammar) {
    if (!new Set(['javascript','typescript','kotlin','bash','vim','csharp','graphql','go','cmake','dart','makefile','lua','xml','elixir','erlang','fsharp','groovy','perl','r','ruby','swift','scala','scss']).has(name)) return grammar;
    if (grammar.__aideRepaired) return grammar;
    Object.defineProperty(grammar, '__aideRepaired', {value: true});
    const list = records(grammar);
    if (name === 'javascript' || name === 'typescript') jsx(grammar, name);
    if (name === 'kotlin') {
      for (const {mode, scope: kind} of list) if (!Object.isFrozen(mode) && kind === 'comment' && source(mode.begin).startsWith('/\\*')) {
        mode.contains = ['self', ...(mode.contains || []).filter(child => source(child?.begin) !== '/\\*')];
      }
    }
    if (name === 'vim') grammar.contains.unshift({className: 'comment', begin: /^[ \t]*"/, end: /$/, relevance: 0});
    if (name === 'graphql') grammar.contains.unshift({className: 'string', begin: /"""/, end: /"""/, contains: [{match: /\\"""/}]});
    const braces = ['csharp', 'dart', 'elixir', 'fsharp', 'perl', 'ruby', 'scala'];
    if (braces.includes(name)) {
      for (const {mode, scope: kind, children, parent} of list) if (!Object.isFrozen(mode) && kind === 'subst' && ['}', '\\}'].includes(source(mode.end))) {
        balanced(mode, children, /\{/, /\}/, mode.keywords || parent?.keywords || grammar.keywords);
      }
    }
    if (name === 'go') for (const {mode, scope: kind, children} of list) if (kind === 'params' && source(mode.begin) === '\\(') balanced(mode, children, /\(/, /\)/, mode.keywords);
    if (name === 'bash') {
      const substitution = list.find(({mode, scope: kind}) => kind === 'subst' && source(mode.begin) === '\\$\\(')?.mode;
      if (substitution) {
        const singleQuoted = list.find(({mode, scope: kind}) => kind === 'string' && source(mode.begin) === "'")?.mode || engine.APOS_STRING_MODE;
        const children = [...(substitution.contains || []), singleQuoted, 'self'];
        balanced(substitution, children, /\(/, /\)/, grammar.keywords);
        grammar.contains.push(substitution);
      }
    }
    if (name === 'makefile') {
      for (const {mode, scope: kind, children} of list) if (kind === 'variable' && source(mode.begin).startsWith('\\$\\(') && source(mode.end) === '\\)') {
        balanced(mode, ['self', ...children], /\(/, /\)/, mode.keywords);
      }
    }
    if (name === 'lua' || name === 'cmake') {
      for (const {mode} of list) {
        if (Object.isFrozen(mode)) continue;
        const start = source(mode.begin), finish = source(mode.end);
        if (!start.includes('[') || !finish.includes(']') || !finish.includes('=') || !start.includes('=')) continue;
        mode.begin = new RegExp(start.replace('\\[=*\\[', '\\[(=*)\\['));
        mode.end = new RegExp(finish.replace('\\]=*\\]', '\\](=*)\\]'));
        if (name === 'lua') mode.contains = (mode.contains || []).filter(child => child !== 'self' && !(source(child?.begin).includes('\\[') && source(child?.begin).includes('=') && source(child?.end).includes('\\]')));
        engine.END_SAME_AS_BEGIN(mode);
      }
    }
    if (name === 'erlang') {
      for (const {mode} of list) {
        if (!source(mode.begin).includes('[a-fA-F0-9]')) continue;
        mode.begin = source(mode.begin).replaceAll('[a-fA-F0-9]', '[0-9A-Za-z]');
        const before = mode['on:begin'];
        mode['on:begin'] = function (match, response) {
          if (match[0].includes('#')) {
            const [radix, digits] = match[0].replaceAll('_', '').split('#'), base = Number(radix);
            if (base < 2 || base > 36 || [...digits].some(char => parseInt(char, 36) >= base)) { response.ignoreMatch(); return; }
          }
          if (before) before(match, response);
        };
      }
    }
    if (name === 'xml') {
      const start = '(?:[:A-Z_a-z\\u00C0-\\u00D6\\u00D8-\\u00F6\\u00F8-\\u02FF\\u0370-\\u037D\\u037F-\\u1FFF\\u200C-\\u200D\\u2070-\\u218F\\u2C00-\\u2FEF\\u3001-\\uD7FF\\uF900-\\uFDCF\\uFDF0-\\uFFFD]|[\\uD800-\\uDB7F][\\uDC00-\\uDFFF])';
      const entity = new RegExp('&(?:' + start + '(?:' + start + '|[-.0-9\\u00B7\\u0300-\\u036F\\u203F-\\u2040])*|#[0-9]+|#x[0-9a-fA-F]+);');
      for (const {mode, scope: kind} of list) if (!Object.isFrozen(mode) && kind === 'symbol' && source(mode.begin).includes('&')) mode.begin = entity;
    }
    if (name === 'r') for (const {mode} of list) {
      if (Array.isArray(mode.match) && mode.match.length === 4 && source(mode.match[2]) === '<-') mode.match[3] = /(?=\s+)/;
    }
    if (name === 'groovy') {
      const subst = {className: 'subst', begin: /\$\{/, end: /\}/, keywords: grammar.keywords};
      balanced(subst, [...grammar.contains], /\{/, /\}/, grammar.keywords);
      const variable = {className: 'variable', match: /\$[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*/};
      for (const {mode, scope: kind, children} of list) if (kind === 'string' && mode.begin && !source(mode.begin).startsWith("'")) {
        const contains = [engine.BACKSLASH_ESCAPE, subst, variable, ...children];
        if (Object.isFrozen(mode)) {
          const replacement = engine.inherit(mode, {contains});
          const parent = list.find(record => record.mode === mode).parent;
          for (const key of ['contains', 'variants']) if (parent?.[key]) parent[key] = parent[key].map(child => child === mode ? replacement : child);
        } else mode.contains = contains;
      }
    }
    if (name === 'scss') {
      const variable = list.find(({scope: kind}) => kind === 'variable')?.mode;
      const subst = {className: 'subst', begin: /#\{/, end: /\}/, contains: [variable, engine.NUMBER_MODE].filter(Boolean)};
      balanced(subst, subst.contains, /\{/, /\}/, grammar.keywords);
      for (const {mode} of list) if (mode.contains && !Object.isFrozen(mode)) {
        mode.contains = mode.contains.map(child => ['"', "'"].includes(source(child?.begin)) && scope(child) === 'string'
          ? engine.inherit(child, {contains: [...(child.contains || []), subst]}) : child);
      }
    }
    if (name === 'swift') swift(grammar);
    return grammar;
  }
  const register = engine.registerLanguage;
  engine.registerLanguage = function (name, definition) {
    return register.call(this, name, hl => patch(name, definition(hl)));
  };
  for (const name of engine.listLanguages()) {
    const definition = engine.getLanguage(name).rawDefinition;
    if (definition) engine.registerLanguage(name, definition);
  }
  const reset = () => { for (const clear of resets.values()) clear(); };
  engine.addPlugin({'before:highlight': reset, 'before:highlightElement': reset, 'after:highlight': reset});
  const auto = engine.highlightAuto;
  engine.highlightAuto = function () { reset(); return auto.apply(this, arguments); };
});
