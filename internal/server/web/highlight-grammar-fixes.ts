declare var hljs: any;
declare var module: any;
/* Aide repairs for the pinned Highlight.js 11.9 grammar registration pipeline. */
(function (root: any, install: any) {
  const engine = typeof hljs !== 'undefined' ? hljs : root.hljs;
  if (engine) install(engine);
  else if (typeof module === 'object' && module.exports) module.exports = install;
})(typeof globalThis !== 'undefined' ? globalThis : window, function install(engine: any): any {
  'use strict';
  if (engine.__aideGrammarRepairs) return;
  Object.defineProperty(engine, '__aideGrammarRepairs', {value: true});
  const resets = new Map<any, any>();
  const scope = (mode: any): any => mode.scope || mode.className;
  const source = (regex: any): string => typeof regex === 'string' ? regex : regex?.source || '';
  function records(grammar: any): any[] {
    const seen = new Set<any>(), result: any[] = [];
    function visit(mode: any, parent?: any, inheritedScope?: any, inheritedContains?: any): void {
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
  function balanced(mode: any, children: any, open: any, close: any, keywords: any): void {
    const nest = {begin: open, end: close, relevance: 0, keywords,
      contains: ['self', ...children]};
    mode.contains = [nest, ...children];
  }
  function jsx(grammar: any, name: any): void {
    let previous: any, trie: any;
    resets.set(name, () => { previous = trie = undefined; });
    function index(input: any): void {
      if (input === previous) return;
      previous = input;
      trie = {children: new Map<any, any>(), last: -1};
      // Index every closing tag once. Prefix positions preserve the original
      // tag detector's prefix semantics without repeated suffix searches.
      const tags = /<\/([A-Za-z0-9\\._:-]+)/g;
      for (let match: any; (match = tags.exec(input));) {
        let node = trie;
        for (const char of match[1]) {
          if (!node.children.has(char)) node.children.set(char, {children: new Map<any, any>(), last: -1});
          node = node.children.get(char);
          node.last = match.index;
        }
      }
    }
    for (const {mode} of records(grammar)) {
      const callback = mode['on:begin'];
      if (typeof callback !== 'function' || !String(callback).includes('.input.indexOf')) continue;
      mode['on:begin'] = function (match: any, response: any): void {
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
  function swift(grammar: any): void {
    const stack: any[] = [];
    let input: any;
    const reset = (): void => { stack.length = 0; input = undefined; };
    resets.set('swift', reset);
    function begin(match: any, response: any): void {
      if (match.input !== input) { reset(); input = match.input; }
      response.data.hashes = match[1].length;
      stack.push(match[1].length);
    }
    function end(match: any, response: any): void {
      if (match[1].length !== stack[stack.length - 1]) { response.ignoreMatch(); return; }
      stack.pop();
    }
    const interpolation: any = {className: 'subst', begin: /\\(#{4,})\(/, end: /\)/,
      keywords: grammar.keywords, contains: [],
      'on:begin': (match: any, response: any): void => { if (match[1].length !== stack[stack.length - 1]) response.ignoreMatch(); }};
    const raw = (beginRegex: any, endRegex: any, scopeName: any): any => ({className: scopeName,
      begin: beginRegex, end: endRegex, contains: [interpolation],
      'on:begin': begin, 'on:end': end});
    const triple = raw(/(#{4,})"""/, /"""(#{4,})/, 'string');
    const single = raw(/(#{4,})"/, /"(#{4,})/, 'string');
    const regexp = raw(/(#{4,})\//, /\/(#{4,})/, 'regexp');
    regexp.contains.push({begin: /\[/, end: /\]/, contains: [engine.BACKSLASH_ESCAPE]}, engine.BACKSLASH_ESCAPE);
    const contains = [triple, single, regexp, ...grammar.contains];
    const parens: any = {begin: /\(/, end: /\)/, relevance: 0, contains: []};
    parens.contains = ['self', ...contains];
    interpolation.contains = [parens, ...contains];
    grammar.contains = contains;
  }
  function declarationWork(name: any, list: any): void {
    for (const {mode} of list) {
      const begin = source(mode.begin);
      if (name === 'c' || name === 'cpp') {
        const separator = '[\\*&\\s]+)+', at = begin.indexOf(separator);
        if (begin[0] !== '(' || at < 0) continue;
        const type = begin.slice(1, at), title = begin.slice(at + separator.length);
        // Upstream #4413 disambiguates whitespace from pointer/reference groups.
        // Bound the number of type/pointer groups as well: malformed pointer
        // chains otherwise retry the entire remaining suffix at every word.
        mode.begin = '(' + type + '\\s*[\\*&]+\\s*){0,16}(?:' + type + '\\s*[\\*&]*\\s*)?' + title;
      } else if (name === 'csharp' && scope(mode) === 'function' && begin.includes('\\s+)+')) {
        // A declaration needs a finite modifier/type prefix, not an arbitrary
        // number of identifiers. Keep up to16 prefix words and all title modes.
        mode.begin = begin.replace('\\s+)+', '\\s+){1,16}');
      }
    }
  }
  function rubyHeredocs(grammar: any): void {
    let previous: any, labels: any;
    resets.set('ruby', () => { previous = labels = undefined; });
    function index(input: any): void {
      if (input === previous) return;
      previous = input; labels = new Map<any, any>();
      // Preserve the pinned grammar's first-word/word-boundary semantics.
      // Each potential terminator line is visited once per highlight input.
      const lines = /^[^\S\n]*(\w+)/gm;
      for (let match: any; (match = lines.exec(input));) labels.set(match[1], match.index + match[0].length - match[1].length);
    }
    for (const {mode} of records(grammar)) {
      const begin = source(mode.begin);
      if (!begin.startsWith('<<') || !begin.includes('[^\\n]*\\n')) continue;
      mode.begin = /<<[-~]?'?/;
      mode['on:begin'] = function (match: any, response: any): void {
        const after = match.index + match[0].length;
        const label = /^\w+/.exec(match.input.slice(after))?.[0];
        index(match.input);
        if (!label || !(labels.get(label) > after)) response.ignoreMatch();
      };
    }
  }
  function patch(name: any, grammar: any): any {
    if (!new Set(['c','cpp','ini','javascript','typescript','kotlin','bash','vim','csharp','graphql','go','cmake','dart','makefile','lua','xml','elixir','erlang','fsharp','groovy','perl','r','ruby','swift','scala','scss']).has(name)) return grammar;
    if (grammar.__aideRepaired) return grammar;
    Object.defineProperty(grammar, '__aideRepaired', {value: true});
    const list = records(grammar);
    if (['c','cpp','csharp'].includes(name)) declarationWork(name, list);
    if (name === 'ini') for (const {mode, scope: kind} of list) if (kind === 'attr') {
      // INI/TOML assignments start on a line; failed dotted keys must not
      // restart at every component or consume a following line's whitespace.
      mode.begin = '^[^\\S\\r\\n]*' + source(mode.begin).replaceAll('\\s*', '[^\\S\\r\\n]*');
    }
    if (name === 'ruby') rubyHeredocs(grammar);
    if (name === 'javascript' || name === 'typescript') jsx(grammar, name);
    if (name === 'kotlin') {
      for (const {mode, scope: kind} of list) if (!Object.isFrozen(mode) && kind === 'comment' && source(mode.begin).startsWith('/\\*')) {
        mode.contains = ['self', ...(mode.contains || []).filter((child: any) => source(child?.begin) !== '/\\*')];
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
        if (name === 'lua') mode.contains = (mode.contains || []).filter((child: any) => child !== 'self' && !(source(child?.begin).includes('\\[') && source(child?.begin).includes('=') && source(child?.end).includes('\\]')));
        engine.END_SAME_AS_BEGIN(mode);
      }
    }
    if (name === 'erlang') {
      for (const {mode} of list) {
        if (!source(mode.begin).includes('[a-fA-F0-9]')) continue;
        mode.begin = source(mode.begin).replaceAll('[a-fA-F0-9]', '[0-9A-Za-z]');
        const before = mode['on:begin'];
        mode['on:begin'] = function (match: any, response: any): void {
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
          for (const key of ['contains', 'variants']) if (parent?.[key]) parent[key] = parent[key].map((child: any) => child === mode ? replacement : child);
        } else mode.contains = contains;
      }
    }
    if (name === 'scss') {
      const variable = list.find(({scope: kind}) => kind === 'variable')?.mode;
      const subst: any = {className: 'subst', begin: /#\{/, end: /\}/, contains: [variable, engine.NUMBER_MODE].filter(Boolean)};
      balanced(subst, subst.contains, /\{/, /\}/, grammar.keywords);
      for (const {mode} of list) if (mode.contains && !Object.isFrozen(mode)) {
        mode.contains = mode.contains.map((child: any) => ['"', "'"].includes(source(child?.begin)) && scope(child) === 'string'
          ? engine.inherit(child, {contains: [...(child.contains || []), subst]}) : child);
      }
    }
    if (name === 'swift') swift(grammar);
    return grammar;
  }
  const register = engine.registerLanguage;
  engine.registerLanguage = function (this: any, name: any, definition: any): any {
    return register.call(this, name, (hl: any) => patch(name, definition(hl)));
  };
  for (const name of engine.listLanguages()) {
    const definition = engine.getLanguage(name).rawDefinition;
    if (definition) engine.registerLanguage(name, definition);
  }
  const reset = (): void => { for (const clear of resets.values()) clear(); };
  engine.addPlugin({'before:highlight': reset, 'before:highlightElement': reset, 'after:highlight': reset});
  const auto = engine.highlightAuto;
  engine.highlightAuto = function (this: any): any { reset(); return auto.apply(this, arguments); };
});
