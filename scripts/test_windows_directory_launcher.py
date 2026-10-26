#!/usr/bin/env python3
"""Execute launcher directory precedence and watcher code with disposable fixtures.

Runs on Windows PowerShell 5.1 and PowerShell 7. The Docker adapter is a fixture;
this is not native folder-dialog/Docker Desktop acceptance.
"""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--pwsh', default=shutil.which('pwsh'))
    parser.add_argument('--source', type=Path, default=ROOT / 'start.ps1')
    args = parser.parse_args()
    if not args.pwsh:
        parser.error('PowerShell is required; pass --pwsh PATH')
    source = args.source.read_text(encoding='utf-8-sig')
    begin = source.index('# Process environment wins') if '# Process environment wins' in source else source.index('# Release bundles contain')
    block = source[begin:source.index('# 执行原生命令')]
    variables = ('AIDE_WORKSPACE', 'AIDE_CONTEXT', 'AIDE_LOCAL_ROOT', 'COMPOSE_FILE')
    cases = ('default', 'dotenv', 'process', 'exported', 'comments', 'source')
    for case in cases:
        with tempfile.TemporaryDirectory(prefix='aide-directory-') as folder:
            root = Path(folder)
            if case != 'source':
                (root / '.aide-image').write_text('aide:slot-a sha256:' + 'a' * 64 + ' linux/amd64 v0.1.17.5-RC11')
            if case in ('dotenv', 'exported'):
                prefix = 'export ' if case == 'exported' else ''
                (root / '.env').write_text('\n'.join(prefix + name + '="E:/Outside/' + name + '"' for name in variables[:3]))
            elif case == 'comments':
                (root / '.env').write_text('# AIDE_LOCAL_ROOT=E:/Ignored\nAI_MODEL=unchanged\n')
            script = root / 'test.ps1'
            script.write_text("$ErrorActionPreference='Stop'\n" + block + r'''
[pscustomobject]@{
 workspace=$env:AIDE_WORKSPACE; context=$env:AIDE_CONTEXT; local=$env:AIDE_LOCAL_ROOT
 expectedRoot=[IO.Path]::GetPathRoot($env:USERPROFILE)
 expectedWork=(Join-Path $PSScriptRoot 'workspace')
 expectedContext=(Join-Path $PSScriptRoot 'context')
} | ConvertTo-Json -Compress
''', encoding='utf-8-sig')
            env = {k: v for k, v in os.environ.items() if k.upper() not in variables}
            # Use a real native path; GetPathRoot semantics differ on macOS.
            env['USERPROFILE'] = str(root / 'profile')
            if case == 'process':
                for name in variables[:3]:
                    env[name] = 'D:/Explicit/' + name
                (root / '.env').write_text('AIDE_LOCAL_ROOT=E:/LowerPriority\n')
            data = json.loads(subprocess.check_output([args.pwsh, '-NoLogo', '-NoProfile', '-File', str(script)], env=env, text=True, encoding='utf-8-sig'))
            if case in ('dotenv', 'exported'):
                assert all(data[k] in (None, '') for k in ('workspace', 'context', 'local')), (case, data)
            elif case == 'process':
                assert [data[k] for k in ('workspace', 'context', 'local')] == ['D:/Explicit/' + n for n in variables[:3]], data
            else:
                assert data['local'] == data['expectedRoot'], (case, data)
                if case != 'source':
                    assert data['workspace'] == data['expectedWork'] and data['context'] == data['expectedContext'], data
                else:
                    assert not data['workspace'] and not data['context'], data
            print('PASS directory precedence:', case)

    for preset in (True, False):
        with tempfile.TemporaryDirectory(prefix='aide-watcher-') as folder:
            root = Path(folder)
            (root / 'scripts').mkdir()
            watcher = root / 'scripts/watch-port.ps1'
            shutil.copy2(ROOT / 'scripts/watch-port.ps1', watcher)
            driver = root / 'driver.ps1'
            # Run the unmodified watcher: one real port-change iteration with a
            # Docker fixture, then exit. Capture the Compose environment on up.
            driver.write_text(r'''
$ErrorActionPreference='Stop'
$env:COMPOSE_FILE=if(PRESET){'compose.yaml;.aide-data/local-mounts/compose.json'}else{''}
$script:polls=0
function Get-Command { param($Name,$ErrorAction) [pscustomobject]@{Source='Invoke-FixtureDocker'} }
function Start-Sleep { param($Seconds) }
function Invoke-FixtureDocker {
 $global:LASTEXITCODE=0
 if($args -contains '-f'){throw 'Compose override discarded by explicit file flag'}
 if($args -contains 'ps'){$script:polls++;if($script:polls -eq 1){'aide'};return}
 if($args -contains 'exec'){'18499';return}
 if($args -contains 'port'){'127.0.0.1:18498';return}
 if($args -contains 'up'){
  [IO.File]::WriteAllText((Join-Path $PWD 'observed.json'),(@{compose=$env:COMPOSE_FILE;port=$env:AIDE_PORT}|ConvertTo-Json -Compress))
  return
 }
 throw "Unexpected fixture command: $args"
}
& (Join-Path $PSScriptRoot 'scripts/watch-port.ps1')
'''.replace('PRESET', '$true' if preset else '$false'), encoding='utf-8-sig')
            subprocess.run([args.pwsh, '-NoLogo', '-NoProfile', '-File', str(driver)], check=True)
            observed = json.loads((root / 'observed.json').read_text(encoding='utf-8-sig'))
            assert observed == {'compose': 'compose.yaml;.aide-data/local-mounts/compose.json' if preset else 'compose.yaml', 'port': '18499'}, observed
            assert not (root / '.aide-port-watcher.lock').exists()
            print('PASS watcher Compose boundary:', 'extra mounts' if preset else 'legacy default')

    # Parse each shipped PowerShell script, including those not executed above.
    for name in ('start.ps1', 'scripts/watch-port.ps1', 'scripts/update-agent.ps1', 'scripts/local-mounts-control.ps1'):
        code = "$e=$null;$t=$null;[System.Management.Automation.Language.Parser]::ParseFile($args[0],[ref]$t,[ref]$e)|Out-Null;if($e.Count){$e|Out-String|Write-Error;exit 1}"
        # -Command argument passing is inconsistent across 5.1 and 7; use file.
        with tempfile.TemporaryDirectory(prefix='aide-ps-parse-') as folder:
            script = Path(folder) / 'parse.ps1'
            script.write_text(code, encoding='utf-8-sig')
            subprocess.run([args.pwsh, '-NoLogo', '-NoProfile', '-File', str(script), str(ROOT / name)], check=True)
    print('PASS: actual PowerShell directory/watcher checks; Docker and native dialogs not exercised')


if __name__ == '__main__':
    main()
