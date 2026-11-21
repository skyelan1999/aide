"""Bounded Python syntax/name diagnostics. Never import or execute input code.

CLI: python3 -I -S python_static_check.py --file candidate.py
     python3 -I -S python_static_check.py --stdin < candidate.py
Warnings are advisory: dynamic globals, types, control flow, APIs and geometry
are not verified. A static pass never authorizes execution or proves correctness.
"""
import sys

# Refuse before importing even the standard-library parser: a non-isolated
# invocation could otherwise load an input-directory module with that name.
if __name__ == '__main__' and (not sys.flags.isolated or not sys.flags.no_site):
    sys.stdout.write('{"status":"not_run","executedInput":false,"reason":"Use python3 -I -S before invoking the checker."}\n')
    raise SystemExit(4)

import argparse
import ast
import builtins
import hashlib
import json
import platform
import symtable

MAX_BYTES = 65536
MAX_DIAGNOSTICS = 50
MODULE_GLOBALS = {'__name__', '__file__', '__doc__', '__package__', '__loader__',
                  '__spec__', '__builtins__', '__cached__'}


class _ComprehensionScopes(ast.NodeTransformer):
    """Restore lexical subscopes for symtable analysis under PEP 709.

    This AST is only converted to text for symbol analysis, never executed.
    Generator expressions retain the same name-binding subscopes across the
    supported Python versions, including outermost iterable and walrus binding.
    """
    def visit_ListComp(self, node):
        self.generic_visit(node)
        return ast.copy_location(ast.GeneratorExp(elt=node.elt, generators=node.generators), node)

    visit_SetComp = visit_ListComp

    def visit_DictComp(self, node):
        self.generic_visit(node)
        return ast.copy_location(ast.GeneratorExp(
            elt=ast.Tuple(elts=[node.key, node.value], ctx=ast.Load()),
            generators=node.generators), node)


def inspect_code(raw, label='<stdin>'):
    if isinstance(raw, str):
        raw = raw.encode('utf-8')
    result = {'status': 'not_run', 'executedInput': False,
              'scope': 'Syntax and possible unresolved globals only; no type, control-flow, API, CAD or execution validation.',
              'pythonVersion': platform.python_version(), 'label': str(label)[:240]}
    if len(raw) > MAX_BYTES:
        result.update(reason='input exceeds 65536 bytes; no full-source hash or check', inputBytesAtLeast=MAX_BYTES + 1)
        return result
    result.update(codeSHA256=hashlib.sha256(raw).hexdigest(), inputBytes=len(raw))
    try:
        text = raw.decode('utf-8-sig')
    except UnicodeDecodeError:
        result['reason'] = 'input is not UTF-8'
        return result
    try:
        tree = ast.parse(text, filename=str(label), mode='exec')
        symbols = symtable.symtable(text, str(label), 'exec')
        normalized = any(isinstance(node, (ast.ListComp, ast.SetComp, ast.DictComp))
                         for node in ast.walk(tree))
        if normalized:
            analysis_tree = _ComprehensionScopes().visit(ast.parse(text))
            symbols = symtable.symtable(ast.unparse(analysis_tree), str(label), 'exec')
    except SyntaxError as error:
        result.update(status='syntax_error', diagnostics=[{
            'kind': 'syntax_error', 'message': error.msg,
            'line': error.lineno, 'column': error.offset}])
        return result
    except (RecursionError, MemoryError):
        result['reason'] = 'parser resource limit'
        return result
    module_defined = {symbol.get_name() for symbol in symbols.get_symbols()
                      if symbol.is_assigned() or symbol.is_imported() or symbol.is_namespace()}
    tables = [symbols]
    while tables:
        table = tables.pop()
        tables.extend(table.get_children())
        module_defined.update(symbol.get_name() for symbol in table.get_symbols()
                              if symbol.is_global() and (symbol.is_assigned() or symbol.is_imported()))
    known = module_defined | set(dir(builtins)) | MODULE_GLOBALS
    occurrences = {}
    wildcard_imports = False
    for node in ast.walk(tree):
        if isinstance(node, ast.Name) and isinstance(node.ctx, ast.Load):
            occurrences.setdefault(node.id, set()).add(node.lineno)
        if isinstance(node, ast.ImportFrom) and any(alias.name == '*' for alias in node.names):
            wildcard_imports = True
    pending = [symbols]
    diagnostics = []
    count = 0
    while pending:
        table = pending.pop()
        pending.extend(reversed(table.get_children()))
        for symbol in table.get_symbols():
            name = symbol.get_name()
            if symbol.is_referenced() and symbol.is_global() and name not in known:
                count += 1
                if len(diagnostics) < MAX_DIAGNOSTICS:
                    diagnostics.append({'kind': 'possibly_undefined_global', 'name': name[:128],
                                        'scope': (table.get_type() + ':' + table.get_name())[:128],
                                        'scopeLine': None if normalized else table.get_lineno(),
                                        'occurrenceLines': sorted(occurrences.get(name, []))[:10]})
    result.update(status='warnings' if diagnostics or wildcard_imports else 'pass_static_only',
                  diagnostics=diagnostics, possibleUndefinedGlobalCount=count,
                  diagnosticsTruncated=count > MAX_DIAGNOSTICS,
                  wildcardImportsPresent=wildcard_imports,
                  comprehensionScopesNormalized=normalized,
                  assumptions='Normal module globals are available. Dynamic definitions/imports and execution order are not resolved.')
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    source = parser.add_mutually_exclusive_group()
    source.add_argument('--file', help='Read this UTF-8 file; never execute it.')
    source.add_argument('--stdin', action='store_true', help='Read UTF-8 program data from stdin (default).')
    parser.add_argument('--label', help='Diagnostic label only; not opened as a path.')
    args = parser.parse_args()
    if not sys.flags.isolated or not sys.flags.no_site:
        result = {'status': 'not_run', 'executedInput': False,
                  'reason': 'Use python3 -I -S so project/PYTHONPATH modules cannot shadow checker imports.'}
    else:
        try:
            if args.file:
                with open(args.file, 'rb') as stream:
                    raw = stream.read(MAX_BYTES + 1)
            else:
                raw = sys.stdin.buffer.read(MAX_BYTES + 1)
            result = inspect_code(raw, args.label or args.file or '<stdin>')
        except OSError as error:
            result = {'status': 'not_run', 'executedInput': False, 'reason': str(error)}
    print(json.dumps(result, ensure_ascii=False))
    return {'pass_static_only': 0, 'syntax_error': 1, 'warnings': 2, 'not_run': 4}[result['status']]


if __name__ == '__main__':
    raise SystemExit(main())
