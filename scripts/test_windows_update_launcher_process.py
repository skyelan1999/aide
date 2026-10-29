#!/usr/bin/env python3
"""Execute the shipped launcher-process helper with owned real PS children.

On Linux only WindowStyle is omitted by the OS adapter. Process waiting,
encoded bootstrap, redaction and file writes are actual production code.
This does not establish Windows taskkill/5.1/Docker Desktop acceptance.
"""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
DRIVER = r'''
$ErrorActionPreference='Stop'
$errors=$null;$tokens=$null
$ast=[Management.Automation.Language.Parser]::ParseFile($env:FIXTURE_SOURCE,[ref]$tokens,[ref]$errors)
if($errors.Count){throw 'Agent parse failed'}
foreach($name in @('ConvertTo-AideUpdateDiagnostic','Write-AideUpdateDiagnostic','Invoke-AideSlotLauncher')){
 $fn=$ast.Find({param($n)$n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -eq $name},$true)
 if(-not $fn){throw "Missing helper: $name"};Invoke-Expression $fn.Extent.Text
}
$currentPowerShell=(Get-Process -Id $PID).Path
$token='fixture-known-token'
$updateLogDir=Join-Path $PSScriptRoot 'logs'
New-Item -ItemType Directory -Path $updateLogDir -Force | Out-Null
if($env:FIXTURE_CASE -eq 'rotation'){[IO.File]::WriteAllText((Join-Path $updateLogDir 'activation.log'),('x'*1MB))}
Set-Location -LiteralPath $PSScriptRoot
# PS7/Linux rejects WindowStyle; retain the real Start-Process and real handle.
function Start-Process {
 param($FilePath,$ArgumentList,[switch]$PassThru,$WindowStyle)
 $options=@{FilePath=$FilePath;ArgumentList=$ArgumentList;PassThru=$PassThru}
 if($env:OS -eq 'Windows_NT'){$options.WindowStyle=$WindowStyle}
 Microsoft.PowerShell.Management\Start-Process @options
}
$env:AIDE_UPDATE_DIAGNOSTIC_TOKEN='fixture-parent-env'
$clock=[Diagnostics.Stopwatch]::StartNew()
$failure=$null;$descendantRunning=$false
try {
 try {Invoke-AideSlotLauncher -TimeoutMilliseconds $(if($env:FIXTURE_CASE -eq 'timeout'){700}else{5000})}catch{$failure=$_.Exception.Message}
 $elapsed=$clock.ElapsedMilliseconds
 $descFile=Join-Path $PSScriptRoot 'descendant.json'
 if(Test-Path -LiteralPath $descFile){
  $meta=Get-Content -LiteralPath $descFile -Raw|ConvertFrom-Json
  $descendant=[Diagnostics.Process]::GetProcessById($meta.id)
  try {
   # Linux derives StartTime from boot time; separate readers can differ by
   # a scheduler tick. The owned fixture sleeps20s; tolerate up to1s drift.
   if([Math]::Abs($descendant.StartTime.ToUniversalTime().Ticks - $meta.start) -gt 10000000){throw 'Owned descendant identity changed'}
   $descendantRunning=-not $descendant.HasExited
   if($descendantRunning){$descendant.Kill();[void]$descendant.WaitForExit(5000)}
  }finally{$descendant.Dispose()}
 }
 @{failure=$failure;elapsed=$elapsed;descendantRunning=$descendantRunning;restored=$env:AIDE_UPDATE_DIAGNOSTIC_TOKEN}|ConvertTo-Json -Compress
} finally {
 # Child timeout cleanup is performed by the production helper. No arbitrary
 # process lookup/termination is used; the sole descendant above is identified
 # by both its recorded ID and creation time in a private disposable fixture.
}
'''


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--pwsh', default=shutil.which('pwsh'))
    parser.add_argument('--source', type=Path, default=ROOT / 'scripts/update-agent.ps1')
    args = parser.parse_args()
    if not args.pwsh:
        parser.error('PowerShell required; pass --pwsh')
    env = dict(os.environ, POWERSHELL_TELEMETRY_OPTOUT='1', POWERSHELL_UPDATECHECK='Off',
               FIXTURE_SOURCE=str(args.source.resolve()))
    for case in ('success', 'exit7', 'throws', 'daemon', 'timeout', 'last-native-failure', 'rotation', 'dangling-log'):
        if case == 'dangling-log' and os.name == 'nt':
            print('NOT_RUN symlink fixture on Windows without link privilege')
            continue
        with tempfile.TemporaryDirectory(prefix="aide launcher ' space ") as folder:
            root = Path(folder)
            if case == 'dangling-log':
                (root / 'logs').mkdir()
                (root / 'logs/activation.log').symlink_to(root / 'outside-log')
            child = r'''
$ErrorActionPreference='Stop'
[IO.File]::WriteAllText((Join-Path $PSScriptRoot 'child-env.json'),(@{hasToken=[bool]$env:AIDE_UPDATE_DIAGNOSTIC_TOKEN;browser=$env:AIDE_OPEN_BROWSER}|ConvertTo-Json -Compress))
Write-Host 'https://localhost:8097/#token=fixture-url-secret'
Write-Output 'Authorization: Bearer fixture-bearer-secret'
Write-Warning 'password="fixture spaced secret"'
Write-Output 'fixture-known-token'
'''
            if case == 'exit7':
                child += 'exit 7\n'
            elif case == 'throws':
                child += "throw 'owned original failure #token=fixture-throw-secret'\n"
            elif case == 'timeout':
                child += 'Start-Sleep -Seconds 30\n'
            elif case == 'last-native-failure':
                child += "& (Get-Process -Id $PID).Path -NoProfile -Command 'exit 7'\nWrite-Host 'Healthy script completed'\n"
            elif case == 'daemon':
                child += r'''
$encoded=[Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes('Start-Sleep -Seconds 20'))
$descendant=Start-Process -FilePath (Get-Process -Id $PID).Path -ArgumentList @('-NoProfile','-EncodedCommand',$encoded) -PassThru -RedirectStandardOutput (Join-Path $PSScriptRoot 'descendant.stdout') -RedirectStandardError (Join-Path $PSScriptRoot 'descendant.stderr')
[IO.File]::WriteAllText((Join-Path $PSScriptRoot 'descendant.json'),(@{id=$descendant.Id;start=$descendant.StartTime.ToUniversalTime().Ticks}|ConvertTo-Json -Compress))
'''
            (root / 'start.ps1').write_text(child, encoding='utf-8-sig')
            (root / 'driver.ps1').write_text(DRIVER, encoding='utf-8-sig')
            result = subprocess.run([args.pwsh, '-NoLogo', '-NoProfile', '-File', str(root / 'driver.ps1')],
                                    env=dict(env, FIXTURE_CASE=case), capture_output=True, text=True,
                                    encoding='utf-8-sig', timeout=18)
            assert result.returncode == 0, (case, result.stdout, result.stderr)
            data = json.loads(result.stdout)
            assert bool(data['failure']) == (case in ('exit7', 'throws', 'timeout', 'dangling-log')), (case, data)
            if case == 'dangling-log':
                assert not (root / 'outside-log').exists()
                print('PASS real launcher process: dangling log rejected before write')
                continue
            if case == 'exit7':
                assert 'exit 7' in data['failure'], data
            if case == 'daemon':
                assert data['descendantRunning'] and data['elapsed'] < 5000, data
            if case == 'timeout':
                assert data['elapsed'] < 8000 and 'timed out' in data['failure'], data
            assert data['restored'] == 'fixture-parent-env', data
            child_env = json.loads((root / 'child-env.json').read_text(encoding='utf-8-sig'))
            assert child_env == {'hasToken': False, 'browser': '0'}, child_env
            logs = ''.join(p.read_text(encoding='utf-8-sig') for p in (root / 'logs').glob('*.log*'))
            if case == 'rotation':
                assert (root / 'logs/activation.log.1').stat().st_size == 1 << 20
            assert logs.strip(), (case, 'missing diagnostics')
            for secret in ('fixture-url-secret', 'fixture-bearer-secret', 'fixture spaced secret',
                           'fixture-known-token', 'fixture-throw-secret'):
                assert secret not in logs, (case, 'fixture secret in diagnostics')
            if case == 'throws':
                assert 'owned original failure' in logs, logs
            print('PASS real launcher process/bootstrap/redaction:', case, data['elapsed'], 'ms')
    print('PASS owned real PS processes; native Windows Desktop/taskkill/5.1 NOT_RUN')


if __name__ == '__main__':
    main()
