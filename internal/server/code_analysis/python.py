# AST only: no execution, imports, plugins, or workspace filesystem access.
import ast
import json
import sys
import symtable

out = []
for f in json.load(sys.stdin):
    u = dict(file=f['id'], language='Python', package='', symbols=[], calls=[], imports=[], diagnostics=[])
    try:
        tree = ast.parse(f['text'])
        symtable.symtable(f['text'], '<indexed-source>', 'exec')
        lines, serial = f['text'].splitlines(), [0]
        environments, symbol_keys, pending, nonlocal_writes = {}, {}, [], []
        functions = (ast.FunctionDef, ast.AsyncFunctionDef, ast.Lambda)
        comprehensions = (ast.ListComp, ast.SetComp, ast.DictComp, ast.GeneratorExp)

        def environment(parent, kind):
            return dict(parent=parent, kind=kind, bindings={}, globals=set(), nonlocals=set())

        def bind(env, name, value):
            if name in env['globals']:
                while env['parent']:
                    env = env['parent']
            elif name in env['nonlocals']:
                nonlocal_writes.append((env, name, value))
                return
            old = env['bindings'].get(name)
            env['bindings'][name] = value if old is None or old == value else dict(kind='value')

        def directives(n, env):
            if isinstance(n, functions + (ast.ClassDef, ast.Lambda)):
                return
            if isinstance(n, ast.Global):
                env['globals'].update(n.names)
            elif isinstance(n, ast.Nonlocal):
                env['nonlocals'].update(n.names)
            for child in ast.iter_child_nodes(n):
                directives(child, env)

        def lookup(env, name):
            skip_class = False
            current = env
            while current:
                if name in current['globals']:
                    while current['parent']:
                        current = current['parent']
                    return current['bindings'].get(name)
                if name in current['nonlocals']:
                    current = current['parent']
                    while current and current['kind'] not in ('function', 'module'):
                        current = current['parent']
                    if not current or current['kind'] == 'module':
                        return None
                    continue
                if not (skip_class and current['kind'] == 'class') and name in current['bindings']:
                    return current['bindings'][name]
                if current['kind'] in ('function', 'comprehension'):
                    skip_class = True
                current = current['parent']
            return None

        def import_bindings(n):
            if isinstance(n, ast.Import):
                return [(a.asname or a.name.split('.')[0], dict(kind='import', path=a.name, name='*')) for a in n.names]
            return [(a.asname or a.name, dict(kind='import', path='.' * n.level + (n.module or ''), name=a.name)) for a in n.names]

        def build(n, env):
            here = env
            if isinstance(n, ast.Module):
                here = environment(None, 'module')
            elif isinstance(n, functions + (ast.ClassDef,)):
                if not isinstance(n, ast.Lambda):
                    bind(env, n.name, dict(kind='callable', node=n))
                here = environment(env, 'class' if isinstance(n, ast.ClassDef) else 'function')
                if isinstance(n, functions):
                    if not isinstance(n, ast.Lambda):
                        for statement in n.body:
                            directives(statement, here)
                    args = n.args.posonlyargs + n.args.args + n.args.kwonlyargs
                    args += [x for x in (n.args.vararg, n.args.kwarg) if x]
                    for index, arg in enumerate(args):
                        receiver = index == 0 and env['kind'] == 'class' and arg.arg in ('self', 'cls')
                        bind(here, arg.arg, dict(kind='value', receiver=receiver))
            elif isinstance(n, comprehensions):
                here = environment(env, 'comprehension')
            environments[n] = here
            if isinstance(n, ast.Global):
                here['globals'].update(n.names)
            elif isinstance(n, ast.Nonlocal):
                here['nonlocals'].update(n.names)
            elif isinstance(n, (ast.Import, ast.ImportFrom)):
                for name, binding in import_bindings(n):
                    bind(here, name, binding)
            elif isinstance(n, ast.Name) and isinstance(n.ctx, (ast.Store, ast.Del)):
                bind(here, n.id, dict(kind='value'))
            elif isinstance(n, ast.ExceptHandler) and n.name:
                bind(here, n.name, dict(kind='value'))
            elif isinstance(n, (ast.MatchAs, ast.MatchStar)) and n.name:
                bind(here, n.name, dict(kind='value'))
            elif isinstance(n, ast.MatchMapping) and n.rest:
                bind(here, n.rest, dict(kind='value'))
            if isinstance(n, ast.Assign) and isinstance(n.value, ast.Lambda):
                for target in n.targets:
                    if isinstance(target, ast.Name):
                        bind(here, target.id, dict(kind='callable', node=n.value))
                build(n.value, here)
                for target in n.targets:
                    if not isinstance(target, ast.Name):
                        build(target, here)
                return
            if isinstance(n, ast.NamedExpr):
                destination = here
                while destination['kind'] == 'comprehension':
                    destination = destination['parent']
                build(n.target, destination)
                build(n.value, here)
                return
            if isinstance(n, functions + (ast.ClassDef,)):
                body = [n.body] if isinstance(n, ast.Lambda) else n.body
                for child in ast.iter_child_nodes(n):
                    build(child, here if child in body else env)
                return
            if isinstance(n, comprehensions):
                for index, generator in enumerate(n.generators):
                    build(generator.target, here)
                    build(generator.iter, env if index == 0 else here)
                    for condition in generator.ifs:
                        build(condition, here)
                for child in ([n.key, n.value] if isinstance(n, ast.DictComp) else [n.elt]):
                    build(child, here)
                return
            for child in ast.iter_child_nodes(n):
                build(child, here)

        build(tree, None)
        for env, name, value in nonlocal_writes:
            outer = env['parent']
            while outer:
                if outer['kind'] == 'function' and name in outer['bindings']:
                    bind(outer, name, value)
                    break
                outer = outer['parent']

        def walk(n, scope=''):
            nextscope = scope
            if isinstance(n, functions + (ast.ClassDef,)):
                serial[0] += 1
                name = getattr(n, 'name', f'anonymous@{n.lineno}:{serial[0]}')
                key = (scope + '.' if scope else '') + name
                kind = 'class' if isinstance(n, ast.ClassDef) else 'async function' if isinstance(n, ast.AsyncFunctionDef) else 'function'
                u['symbols'].append(dict(key=key, parent=scope, name=name, kind=kind, line=n.lineno, endLine=getattr(n, 'end_lineno', n.lineno), signature=lines[n.lineno - 1][:240], anonymous=isinstance(n, ast.Lambda)))
                symbol_keys[n] = key
                nextscope = key
            if isinstance(n, (ast.Import, ast.ImportFrom)):
                for alias, binding in import_bindings(n):
                    u['imports'].append(dict(alias=alias, path=binding['path'], name=binding['name']))
            if isinstance(n, ast.Call):
                c = n.func
                call = dict(caller=scope, name='<dynamic>', qualifier='', line=n.lineno, column=n.col_offset + 1, dynamic=True)
                binding = None
                if isinstance(c, ast.Name):
                    call.update(name=c.id, bindingKnown=True)
                    binding = lookup(environments[n], c.id)
                    call['dynamic'] = binding is not None and binding['kind'] == 'value'
                elif isinstance(c, ast.Attribute):
                    call.update(name=c.attr, qualifier=c.value.id if isinstance(c.value, ast.Name) else '<expression>')
                    if isinstance(c.value, ast.Name):
                        binding = lookup(environments[n], c.value.id)
                        call['bindingKnown'] = not (binding and binding.get('receiver'))
                        if binding and binding['kind'] == 'import' and binding['name'] == '*':
                            call['dynamic'] = False
                        else:
                            binding = None
                if binding and binding['kind'] == 'import':
                    call.update(importPath=binding['path'], importName=binding['name'])
                pending.append((call, binding))
                u['calls'].append(call)
            if isinstance(n, functions + (ast.ClassDef,)):
                body = [n.body] if isinstance(n, ast.Lambda) else n.body
                for child in ast.iter_child_nodes(n):
                    walk(child, nextscope if child in body else scope)
            else:
                for child in ast.iter_child_nodes(n):
                    walk(child, nextscope)

        walk(tree)
        for call, binding in pending:
            if binding and binding['kind'] == 'callable':
                call.update(binding=symbol_keys.get(binding['node'], ''), bindingLine=binding['node'].lineno)
    except Exception as e:
        u['diagnostics'].append('语法解析失败：' + str(e)[:180])
    out.append(u)
json.dump(out, sys.stdout, ensure_ascii=False)
