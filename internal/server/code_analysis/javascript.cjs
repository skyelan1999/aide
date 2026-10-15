// Source-only AST extraction. Never require, import or evaluate workspace code.
const files = JSON.parse(__AIDE_CODE_INPUT__), out = [];
for (const file of files) {
  const u = {file: file.id, language: 'JavaScript', package: '', symbols: [], calls: [], imports: [], diagnostics: [], events: [], requests: [], unresolvedWeb: [], webReferencesTruncated: false};
  try {
    let tree;
    try { tree = acorn.parse(file.text, {ecmaVersion: 'latest', sourceType: 'module', locations: true, allowHashBang: true}); }
    catch (e) { tree = acorn.parse(file.text, {ecmaVersion: 'latest', sourceType: 'script', locations: true, allowHashBang: true}); }
    const environments = new WeakMap(), symbolKeys = new WeakMap(), writes = [], pending = [], pendingEvents = [], globalMutations = new Set();
    const isFunction = n => /^(FunctionDeclaration|FunctionExpression|ArrowFunctionExpression)$/.test(n.type);
    function children(n, visit) {
      for (const [k, v] of Object.entries(n)) {
        if (['loc', 'start', 'end', 'type'].includes(k)) continue;
        if (Array.isArray(v)) { for (const c of v) if (c && typeof c.type === 'string') visit(c); }
        else if (v && typeof v.type === 'string') visit(v);
      }
    }
    function names(pattern) {
      if (!pattern) return [];
      if (pattern.type === 'Identifier') return [pattern.name];
      if (pattern.type === 'RestElement') return names(pattern.argument);
      if (pattern.type === 'AssignmentPattern') return names(pattern.left);
      if (pattern.type === 'ArrayPattern') return pattern.elements.flatMap(names);
      if (pattern.type === 'ObjectPattern') return pattern.properties.flatMap(p => names(p.type === 'RestElement' ? p.argument : p.value));
      return [];
    }
    function bind(env, name, value) {
      const old = env.bindings.get(name);
      if (old?.kind === 'callable' && value.kind === 'var') return;
      if (old?.kind === 'var' && value.kind === 'callable') { env.bindings.set(name, value); return; }
      if (old && (old.kind !== value.kind || old.node !== value.node || old.path !== value.path || old.name !== value.name)) value = {kind: 'value'};
      env.bindings.set(name, value);
    }
    function lookup(env, name) {
      for (let e = env; e; e = e.parent) if (e.bindings.has(name)) return e.bindings.get(name);
      return null;
    }
    function build(n, env, fn, parent = null) {
      let here = env, functionEnv = fn;
      if (n.type === 'FunctionDeclaration' && n.id) bind(env.functionBody ? fn : env, n.id.name, {kind: 'callable', node: n});
      if (n.type === 'ClassDeclaration' && n.id) bind(env, n.id.name, {kind: 'callable', node: n});
      if (n.type === 'Program' || isFunction(n) || ['BlockStatement', 'StaticBlock', 'SwitchStatement', 'CatchClause', 'ForStatement', 'ForInStatement', 'ForOfStatement', 'ClassExpression'].includes(n.type)) {
        here = {parent: env, bindings: new Map(), functionBody: n.type === 'BlockStatement' && parent && isFunction(parent) && parent.body === n};
        if (n.type === 'Program' || n.type === 'StaticBlock' || isFunction(n) || here.functionBody) functionEnv = here;
      }
      environments.set(n, here);
      if (isFunction(n)) {
        if (n.type === 'FunctionExpression' && n.id) bind(here, n.id.name, {kind: 'callable', node: n});
        for (const param of n.params) for (const name of names(param)) bind(here, name, {kind: 'value'});
      }
      if (n.type === 'ClassExpression' && n.id) bind(here, n.id.name, {kind: 'callable', node: n});
      if (n.type === 'CatchClause') for (const name of names(n.param)) bind(here, name, {kind: 'value'});
      if (n.type === 'VariableDeclaration') {
        for (const d of n.declarations) for (const name of names(d.id)) {
          const value = d.id.type === 'Identifier' && d.init && (isFunction(d.init) || d.init.type === 'ClassExpression') ? {kind: 'callable', node: d.init} : {kind: n.kind === 'var' && !d.init ? 'var' : 'value'};
          bind(n.kind === 'var' ? functionEnv : here, name, value);
        }
      }
      if (n.type === 'ImportDeclaration') for (const s of n.specifiers) bind(here, s.local.name, {kind: 'import', path: n.source.value, name: s.type === 'ImportSpecifier' ? s.imported.name || s.imported.value : s.type === 'ImportDefaultSpecifier' ? 'default' : '*'});
      if (n.type === 'AssignmentExpression' || n.type === 'UpdateExpression') for (const name of names(n.left || n.argument)) writes.push([here, name]);
      if (n.type === 'AssignmentExpression' || n.type === 'UpdateExpression') {
        const target = n.left || n.argument;
        if (target?.type === 'MemberExpression' && ['window', 'globalThis'].includes(target.object?.name)) {
          const name = target.computed ? target.property?.value : target.property?.name;
          globalMutations.add(typeof name === 'string' ? name : '*');
        }
        if (target?.type === 'MemberExpression' && target.object?.name === 'document') globalMutations.add('document');
      }
      if (['ForInStatement', 'ForOfStatement'].includes(n.type) && n.left.type !== 'VariableDeclaration') for (const name of names(n.left)) writes.push([here, name]);
      children(n, c => build(c, here, functionEnv, n));
    }
    build(tree, null, null);
    for (const [env, name] of writes) {
      for (let e = env; e; e = e.parent) if (e.bindings.has(name)) { e.bindings.set(name, {kind: 'value'}); break; }
    }
    let ordinal = 0;
    function webRecord(list, record) {
      if (list.length < 256) list.push(record);
      else u.webReferencesTruncated = true;
    }
    function unresolved(n, kind, reason) {
      webRecord(u.unresolvedWeb, {kind, reason, line: n.loc.start.line, column: n.loc.start.column + 1});
    }
    function globalAvailable(name, n) {
      return !globalMutations.has('*') && !globalMutations.has(name) && !lookup(environments.get(n), name) &&
        !writes.some(([env, written]) => written === name && !lookup(env, name));
    }
    function literal(n) {
      if (n?.type === 'Literal' && typeof n.value === 'string') return n.value;
      if (n?.type === 'TemplateLiteral' && !n.expressions.length) return n.quasis[0].value.cooked;
      return null;
    }
    function domTarget(n) {
      if (n?.type !== 'CallExpression' || n.arguments.length !== 1) return null;
      const c = n.callee;
      if (c.type !== 'MemberExpression' || c.computed || c.object?.name !== 'document' || !globalAvailable('document', n)) return null;
      const value = literal(n.arguments[0]);
      if (!value || value.length > 256) return null;
      if (c.property.name === 'getElementById') return value;
      if (c.property.name === 'querySelector' && /^#[A-Za-z_][\w-]*$/.test(value)) return value.slice(1);
      return null;
    }
    function event(n, target, name, callback, caller) {
      if (!target) { unresolved(n, 'event', 'unresolved_dom_target'); return; }
      if (!name || name.length > 64 || !callback) { unresolved(n, 'event', 'dynamic_event_or_callback'); return; }
      const binding = callback.type === 'Identifier' ? lookup(environments.get(n), callback.name) : isFunction(callback) ? {kind:'callable', node:callback} : null;
      if (binding?.kind !== 'callable' || !isFunction(binding.node)) { unresolved(n, 'event', 'unresolved_callback_binding'); return; }
      webRecord(pendingEvents, {node:binding.node, record:{target, event:name, caller, line:n.loc.start.line, column:n.loc.start.column+1}});
    }
    function webEvidence(n, scope) {
      if (n.type === 'AssignmentExpression' && n.operator === '=' && n.left.type === 'MemberExpression' && !n.left.computed) {
        const name = n.left.property.name;
        if (/^on[a-z]+$/.test(name)) event(n, domTarget(n.left.object), name.slice(2), n.right, scope);
      }
      if (n.type !== 'CallExpression') return;
      const c = n.callee;
      if (c.type === 'MemberExpression' && !c.computed && c.property.name === 'addEventListener') {
        event(n, domTarget(c.object), literal(n.arguments[0]), n.arguments[1], scope);
      }
      if (c.type !== 'Identifier' || c.name !== 'fetch') return;
      if (!globalAvailable('fetch', n)) { unresolved(n, 'http', 'shadowed_or_reassigned_fetch'); return; }
      const url = literal(n.arguments[0]);
      if (!url || url.length > 2048) { unresolved(n, 'http', 'dynamic_url'); return; }
      let method = 'GET';
      const options = n.arguments[1];
      if (options && !(options.type === 'Literal' && options.value === null)) {
        if (options.type !== 'ObjectExpression' || options.properties.some(p => p.type !== 'Property' || p.computed || p.kind !== 'init')) {
          unresolved(n, 'http', 'dynamic_options'); return;
        }
        for (const p of options.properties) if ((p.key.name || p.key.value) === 'method') {
          const value = literal(p.value);
          if (!value || !/^[A-Za-z]+$/.test(value)) { unresolved(n, 'http', 'dynamic_method'); return; }
          method = value.toUpperCase();
        }
      }
      webRecord(u.requests, {caller:scope, url, method, line:n.loc.start.line, column:n.loc.start.column+1});
    }
    function walk(n, scope = '', parent = null) {
      let next = scope;
      const func = isFunction(n), cls = /^(ClassDeclaration|ClassExpression)$/.test(n.type);
      if (func || cls) {
        const named = n.id?.name || (parent?.type === 'VariableDeclarator' ? parent.id?.name : null) || (parent?.type === 'MethodDefinition' || parent?.type === 'Property' ? (!parent.computed ? parent.key?.name || parent.key?.value : null) : null);
        const name = String(named || `anonymous@${n.loc.start.line}:${++ordinal}`), key = (scope ? scope + '.' : '') + name;
        const head = file.text.slice(n.start, func ? n.body.start : n.start + Math.min(n.end - n.start, 120));
        u.symbols.push({key, parent: scope, name, kind: cls ? 'class' : parent?.type === 'MethodDefinition' ? 'method' : func && n.async ? 'async function' : 'function', line: n.loc.start.line, endLine: n.loc.end.line, signature: head.slice(0, 240), anonymous: !named});
        symbolKeys.set(n, key); next = key;
      }
      if (n.type === 'ImportDeclaration') for (const s of n.specifiers) u.imports.push({alias: s.local.name, path: n.source.value, name: s.type === 'ImportSpecifier' ? s.imported.name || s.imported.value : s.type === 'ImportDefaultSpecifier' ? 'default' : '*'});
      webEvidence(n, scope);
      if (n.type === 'CallExpression' || n.type === 'NewExpression') {
        const c = n.callee;
        const call = {caller: scope, name: '<dynamic>', qualifier: '', line: n.loc.start.line, column: n.loc.start.column + 1, dynamic: true};
        let binding = null;
        if (c.type === 'Identifier') {
          call.name = c.name; call.bindingKnown = true; binding = lookup(environments.get(n), c.name); call.dynamic = binding?.kind === 'value' || binding?.kind === 'var';
        } else if (c.type === 'MemberExpression' && !c.computed && c.property.type === 'Identifier') {
          call.name = c.property.name;
          call.qualifier = c.object.type === 'Identifier' ? c.object.name : c.object.type === 'ThisExpression' ? 'this' : '<expression>';
          if (c.object.type === 'Identifier') {
            call.bindingKnown = true; binding = lookup(environments.get(n), c.object.name);
            if (binding?.kind === 'import' && binding.name === '*') call.dynamic = false;
            else binding = null;
          }
        }
        if (binding?.kind === 'import') { call.importPath = binding.path; call.importName = binding.name; }
        pending.push([call, binding]); u.calls.push(call);
      }
      children(n, c => walk(c, next, n));
    }
    walk(tree);
    for (const [call, binding] of pending) if (binding?.kind === 'callable') {
      call.binding = symbolKeys.get(binding.node) || ''; call.bindingLine = binding.node.loc.start.line;
    }
    for (const {node, record} of pendingEvents) {
      const callback = symbolKeys.get(node);
      if (callback) webRecord(u.events, {...record, callback});
    }
  } catch (e) { u.diagnostics.push('语法解析失败：' + String(e.message).slice(0, 180)); }
  out.push(u);
}
process.stdout.write(JSON.stringify(out));
