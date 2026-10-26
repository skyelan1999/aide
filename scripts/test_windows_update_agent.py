#!/usr/bin/env python3
"""Run the real Windows slot agent with disposable Docker/HTTP/process adapters.

This checks protocol, activation, failure receipts and process ownership. It does
not claim native Windows Docker Desktop or process-tree end-to-end acceptance.
"""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
OLD = 'sha256:' + 'a' * 64
NEW = 'sha256:' + 'b' * 64
LOCAL = 'sha256:' + 'e' * 64
OP = 'c' * 32
PACKAGE = 'd' * 32
TAG = 'v0.1.17.5-RC11'

DRIVER = r'''
$ErrorActionPreference='Stop'
$global:fixture_launches=@();$global:fixture_progress=@();$global:fixture_result=$null;$global:fixture_sync=@();$global:fixture_loads=0;$global:fixture_waits=0
$env:COMPOSE_FILE='compose.yaml;.aide-data/local-mounts/compose.json'
$env:TEMP=$PSScriptRoot
$case='CASE'
$global:fixture_held=$null
if($case -eq 'owner-busy'){
 New-Item -ItemType Directory -Path (Join-Path $PSScriptRoot '.aide-update-agent.lock') -Force | Out-Null
 $global:fixture_held=[IO.File]::Open((Join-Path $PSScriptRoot '.aide-update-agent.lock/owner.lock'),[IO.FileMode]::OpenOrCreate,[IO.FileAccess]::ReadWrite,[IO.FileShare]::None)
}
function Get-Command {param($Name,$ErrorAction) [pscustomobject]@{Source='Invoke-FixtureDocker'} }
function Start-Sleep {param($Seconds) if($Seconds -eq 3){throw 'FIXTURE_STOP'} }
function Invoke-FixtureDocker {
 $global:LASTEXITCODE=0
 if($args[0] -eq 'compose') {
  if($env:COMPOSE_FILE -ne 'compose.yaml;.aide-data/local-mounts/compose.json'){throw 'Directory mounts lost'}
  if($args -contains 'port'){'127.0.0.1:18498';return}
  if($args -contains 'exec'){'fixture-token';return}
  if($args -contains 'ps'){'cccccccccccccccc';return}
 }
 if($args[0] -eq 'image') {
  if($args[1] -eq 'tag'){
   if($args[2] -eq 'NEW' -and $case -eq 'tag-failure'){$global:LASTEXITCODE=1};return
  }
  if($args[1] -eq 'inspect'){
   if($case -eq 'containerd-package' -and $args[2] -eq 'NEW'){$global:LASTEXITCODE=1;return}
   $id=if($case -eq 'containerd-package'){'LOCAL'}else{'NEW'}
   if($args[-1] -like '*Architecture*'){"$id|linux/amd64"}else{$id};return
  }
  if($args[1] -eq 'load'){$global:fixture_loads++;'Loaded image: aide:fixture-target';return}
 }
 if($args[0] -eq 'inspect'){if($case -eq 'wrong-runtime'){'OLD'}elseif($case -eq 'containerd-package'){'LOCAL'}else{'NEW'};return}
 throw "Unexpected Docker fixture: $args"
}
function Start-Process {
 param($FilePath,$ArgumentList,[switch]$PassThru,[switch]$Wait,$WindowStyle)
 if($Wait){throw 'Process-tree wait would include permanent monitors'}
 if(-not $PassThru){throw 'Launcher process handle missing'}
 $expected='"'+(Join-Path $PWD 'start.ps1')+'"'
 if($ArgumentList[-1] -cne $expected){throw 'Launcher path with spaces is unquoted'}
 $marker=([IO.File]::ReadAllText((Join-Path $PWD '.aide-image'))).Trim()
 $global:fixture_launches+=@{marker=$marker;compose=$env:COMPOSE_FILE}
 $exit=if($case -eq 'launcher-failure' -and $marker.StartsWith('aide:slot-b ')){1}else{0}
 $p=[pscustomobject]@{ExitCode=$exit}
 $p | Add-Member ScriptMethod WaitForExit {param($Timeout) $global:fixture_waits++;return $true}
 $p | Add-Member ScriptMethod Refresh {}
 $p | Add-Member ScriptMethod Kill {}
 return $p
}
function curl.exe {
 $global:LASTEXITCODE=0
 $url=@($args|Where-Object{$_ -is [string] -and $_ -like 'https://*'})[-1]
 $out=[array]::IndexOf($args,'-o')
 $data=[array]::IndexOf($args,'--data-binary')
 if($url.EndsWith('/agent/sync')){$global:fixture_sync+=@(Get-Content -Raw -LiteralPath $args[$data+1].Substring(1)|ConvertFrom-Json);return}
 if($url.EndsWith('/agent/progress')){
  $body=Get-Content -Raw -LiteralPath $args[$data+1].Substring(1)|ConvertFrom-Json
  $global:fixture_progress+=$body.status;return
 }
 if($url.EndsWith('/agent/result')){
  $global:fixture_result=Get-Content -Raw -LiteralPath $args[$data+1].Substring(1)|ConvertFrom-Json;return
 }
 if($url.EndsWith('/agent')){
  Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'command.tsv') -Destination $args[$out+1];return
 }
 if($url -like '*/agent/packages/*'){
  Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'fixture.zip') -Destination $args[$out+1];return
 }
 if($url.EndsWith('/healthz')){
  if($case -eq 'health-failure'){$global:LASTEXITCODE=22;return}
  '{"service":"aide","status":"ok"}';return
 }
 throw "Unexpected HTTP fixture: $url"
}
try { & (Join-Path $PSScriptRoot 'scripts/update-agent.ps1') }
catch {if($_.Exception.Message -ne 'FIXTURE_STOP'){throw}}
finally {if($global:fixture_held){$global:fixture_held.Dispose()}}
[IO.File]::WriteAllText((Join-Path $PSScriptRoot 'observed.json'),(@{
 errors=@($Error|ForEach-Object{$_.Exception.Message});sync=@($global:fixture_sync);result=$global:fixture_result;launches=@($global:fixture_launches);progress=@($global:fixture_progress);loads=$global:fixture_loads;waits=$global:fixture_waits
 marker=([IO.File]::ReadAllText((Join-Path $PSScriptRoot '.aide-image'))).Trim()
}|ConvertTo-Json -Depth 8 -Compress))
'''.replace('NEW', NEW).replace('OLD', OLD).replace('LOCAL', LOCAL)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--pwsh', default=shutil.which('pwsh'))
    parser.add_argument('--source', type=Path, default=ROOT / 'scripts/update-agent.ps1')
    args = parser.parse_args()
    if not args.pwsh:
        parser.error('PowerShell is required; pass --pwsh PATH')
    cases = ('local-sentinel', 'local-empty', 'stale-lock', 'package', 'bad-hash',
             'tag-failure', 'launcher-failure', 'health-failure', 'wrong-ref', 'owner-busy', 'wrong-runtime', 'containerd-package')
    for case in cases:
        with tempfile.TemporaryDirectory(prefix='aide upgrade space ') as folder:
            root = Path(folder)
            (root / 'scripts').mkdir()
            shutil.copy2(args.source, root / 'scripts/update-agent.ps1')
            (root / 'start.ps1').write_text('# process adapter fixture; never executed\n')
            original = f'aide:slot-a {OLD} linux/amd64 v0.1.17.5-RC10'
            (root / '.aide-image').write_text(original)
            if case == 'stale-lock':
                (root / '.aide-update-agent.lock').mkdir()
            payload = b'isolated image fixture, not an actual runtime'
            digest = hashlib.sha256(payload).hexdigest()
            archive = f'aide-{TAG}-linux-amd64-image.tar.gz'
            with zipfile.ZipFile(root / 'fixture.zip', 'w') as z:
                z.writestr(archive, payload)
                z.writestr('SHA256SUMS', digest + '  ' + archive + '\n')
            package = PACKAGE if case in ('package', 'bad-hash', 'containerd-package') else '-'
            image_hash = ('0' * 64 if case == 'bad-hash' else digest) if package != '-' else '-'
            if case == 'local-empty':
                package = image_hash = ''
            ref = 'aide:unrelated' if case == 'wrong-ref' else 'aide:slot-b'
            fields = [OP, 'B', package, TAG, NEW, 'linux/amd64', image_hash, ref, '0.1.17.5 RC11']
            (root / 'command.tsv').write_text('\t'.join(fields) + '\n')
            driver = root / 'driver.ps1'
            driver.write_text(DRIVER.replace('CASE', case), encoding='utf-8-sig')
            subprocess.run([args.pwsh, '-NoLogo', '-NoProfile', '-File', str(driver)], check=True, timeout=35)
            data = json.loads((root / 'observed.json').read_text(encoding='utf-8-sig'))
            if case in ('wrong-ref', 'owner-busy'):
                assert not data['result'] and not data['launches'] and data['marker'] == original, (case, data)
            elif case in ('bad-hash', 'tag-failure', 'launcher-failure', 'health-failure', 'wrong-runtime'):
                assert data['result'] and data['result']['success'] is False and data['marker'] == original, (case, data)
                assert data['launches'][-1]['marker'] == original, (case, data)
                if case == 'bad-hash':
                    assert data['loads'] == 0, data
            else:
                assert data['result'] and data['result']['success'] is True, (case, data)
                assert data['result']['operationId'] == OP and data['marker'].startswith('aide:slot-b ' + (LOCAL if case == 'containerd-package' else NEW)), data
                assert len(data['launches']) == 1 and data['waits'] == 1, data
                assert 'activating' in data['progress'] and 'health-check' in data['progress'], data
                assert data['loads'] == (1 if case in ('package','containerd-package') else 0), data
            if case == 'containerd-package':
                assert data['sync'][-1]['activeSlot'] == 'B' and data['sync'][-1]['slots']['B']['imageId'] == LOCAL, data
            print('PASS real PowerShell update agent:', case, flush=True)
    print('PASS: 12 activation/rollback/ownership cases with synthetic adapters; native Windows Desktop NOT_RUN')


if __name__ == '__main__':
    main()
