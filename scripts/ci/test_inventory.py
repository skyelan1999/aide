#!/usr/bin/env python3
"""Generate a reviewable source test catalog; inventory is not execution proof."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[2]


def inventory(root=ROOT):
    names = subprocess.check_output(['git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z'], cwd=root).decode().split('\0')
    rows = []
    for name in sorted(set(filter(None, names))):
        path = root / name
        if not path.is_file() or name.startswith(('vendor/', 'docs/', '.')):
            continue
        kind = ('go' if name.endswith('_test.go') else
                'python' if path.suffix == '.py' and path.name.startswith('test') else
                'node' if path.suffix in ('.js', '.cjs') and ('.test.' in path.name or path.name.startswith('test')) else
                'browser' if path.suffix == '.cjs' and ('browser' in path.name or path.name.startswith('ui_')) else '')
        if not kind:
            continue
        text = path.read_text()
        if kind == 'go':
            cases = re.findall(r'^func ((?:Test|Fuzz|Example)\w*)\(', text, re.M)
        elif kind == 'python':
            cases = re.findall(r'^\s*def (test\w*)\(', text, re.M)
        else:
            cases = re.findall(r'\b(?:test|it)\(\s*[\'"]([^\'"\n]+)', text)
        opt_in = sorted(set(re.findall(r'(?:Getenv\(|environ.get\(|process\.env\.)[\'\"]?([A-Z][A-Z0-9_]+)', text)))
        rows.append({'path': name, 'kind': kind, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest(),
                     'cases': cases, 'caseCount': len(cases), 'environmentReferences': opt_in,
                     'execution': 'not_run_by_inventory'})
    return {'schemaVersion': 1, 'notice': 'Source inventory only. Case counts do not prove behavior, coverage or execution. Runtime Go inventory includes registered fuzz seeds/examples; Node assertion scripts also have file-level cases.', 'files': rows}


def markdown(data):
    text = ['# 测试用例源码目录', '', '此目录由 `python3 scripts/ci/test_inventory.py --write` 生成。它描述现有用例入口，执行结果必须看本次 Release 的证据，不能把统计数量当作通过。', '',
            '| 类型 | 文件数 | 可静态识别的命名用例 |', '| --- | ---: | ---: |']
    for kind in ('go', 'python', 'node', 'browser'):
        files = [r for r in data['files'] if r['kind'] == kind]
        text.append(f'| {kind} | {len(files)} | {sum(r["caseCount"] for r in files)} |')
    text += ['', '详见[Release 测试矩阵](release-validation.md)，包含基本/全量、平台、插件、人工与外部依赖的通过标准。', '']
    for row in data['files']:
        text += [f'<details><summary>{row["path"]} · {row["kind"]} · {row["caseCount"]} 个命名用例</summary>', '',
                 f'[源码入口](../../{row["path"]})', '']
        text += ['- `' + name.replace('`', '') + '`' for name in row['cases']] or ['- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。']
        if row['environmentReferences']:
            text += ['', '环境引用：' + ', '.join('`' + n + '`' for n in row['environmentReferences']) + '。缺少配置时可能跳过；必须核对运行日志。']
        text += ['', '</details>', '']
    return '\n'.join(text).rstrip() + '\n'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--write', action='store_true')
    parser.add_argument('--out', type=Path, default=ROOT / 'docs/testing/test-catalog.json')
    args = parser.parse_args()
    data = inventory()
    if args.write:
        args.out.parent.mkdir(parents=True, exist_ok=True)
        args.out.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
        args.out.with_name('test-cases.md').write_text(markdown(data))
    else:
        print(json.dumps({'files': len(data['files']), 'namedCases': sum(r['caseCount'] for r in data['files'])}))


if __name__ == '__main__':
    main()
