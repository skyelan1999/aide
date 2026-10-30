#!/usr/bin/env python3
"""Run real PowerShell recovery branches with disposable native/Docker fixtures.

Requires Go and PowerShell (5.1 or 7). PS7/Linux execution does not establish
Windows/Docker Desktop acceptance. No real Docker, browser or user data is used.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
NATIVE = r'''
package main
import ("fmt"; "os"; "path/filepath")
func main() {
 args:=os.Args[1:]; mode:=""; root:=""
 for i:=0;i+1<len(args);i++ {if args[i]=="-mode" {mode=args[i+1]}; if args[i]=="-root" {root=args[i+1]}}
 exe,_:=os.Executable()
 f,_:=os.OpenFile(os.Getenv("FIXTURE_NATIVE_LOG"),os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600)
 if f!=nil {fmt.Fprintln(f,exe+"|"+mode);f.Close()}
 if mode=="stop" {
  dir:=filepath.Join(root,".aide-data","storage","data","local-mounts")
  os.MkdirAll(dir,0700);os.WriteFile(filepath.Join(dir,"stop"),[]byte("stop"),0600)
  fail:=os.Getenv("FIXTURE_STOP")
  if os.Getenv("FIXTURE_FRESH_OK")=="1" && filepath.Dir(exe)!=os.Getenv("FIXTURE_CACHE") {return}
  if fail=="race" || fail=="replacement" {
   os.Remove(filepath.Join(dir,"serve.pid"))
   if fail=="replacement" {os.WriteFile(filepath.Join(dir,"serve.pid"),[]byte("123"),0600)}
   fmt.Fprintln(os.Stderr,"open serve.pid: file disappeared");os.Exit(1)
  }
  if fail=="generic" {fmt.Fprintln(os.Stderr,"unexpected helper failure");os.Exit(1)}
 }
 if mode=="prepare" {fmt.Println(filepath.Join(root,".aide-data","local-mounts","compose.json"))}
}
'''

DOCKER = r'''
function Invoke-FixtureDocker {
 $global:LASTEXITCODE=0
 [IO.File]::AppendAllText($env:FIXTURE_DOCKER_LOG,(@($args)|ConvertTo-Json -Compress)+"`n")
 if($args[0] -eq 'compose') {
  if($args -contains 'ps'){'running-app';return}
  if($env:FIXTURE_LAN -eq 'public'){'0.0.0.0:49000'}else{'127.0.0.1:49000'};return
 }
 if($args[0] -eq 'inspect') {
  if($args[-1] -eq '{{.Config.Image}}'){
   if($env:FIXTURE_LAN -eq 'bad-reference'){'--untrusted option'}else{'aide:local'};return
  }
  if($args[1] -eq 'fresh-source' -and $env:FIXTURE_LAN -eq 'inspect-failed'){$global:LASTEXITCODE=1;return}
  if($args[1] -eq 'fresh-source' -and $env:FIXTURE_LAN -eq 'mismatch'){'sha256:'+('b'*64)}else{'sha256:'+('a'*64)}
  return
 }
 if($args[0] -eq 'create') {
  if($env:FIXTURE_LAN -eq 'missing'){$global:LASTEXITCODE=1;return}
  if($args[-1] -like 'sha256:*' -and $env:FIXTURE_LAN -ne 'direct'){
   [Console]::Error.WriteLine('No such image (fixture)');$global:LASTEXITCODE=1;return
  }
  'fresh-source';return
 }
 if($args[0] -eq 'cp') {
  if($args[1] -notlike 'fresh-source:*'){throw 'Copied mutable running container'}
  if($args[1] -like '*/SHA256SUMS'){
   $hash=(Get-FileHash -Algorithm SHA256 -LiteralPath $env:FIXTURE_NATIVE).Hash.ToLowerInvariant()
   if($env:FIXTURE_LAN -eq 'checksum'){$hash='0'*64}
   [IO.File]::WriteAllText($args[2],$hash+'  aide-message-lan-windows-amd64.exe'+"`n"+$hash+'  aide-local-mounts-windows-amd64.exe'+"`n")
  }else{Copy-Item -LiteralPath $env:FIXTURE_NATIVE -Destination $args[2]}
  return
 }
 if($args[0] -eq 'rm'){return}
 throw "Unexpected Docker fixture call: $args"
}
function Start-Process {
 param($FilePath,$ArgumentList,$WindowStyle,$RedirectStandardOutput,$RedirectStandardError)
 [IO.File]::WriteAllText($env:FIXTURE_LAUNCH,'started')
}
'''


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--pwsh', default=shutil.which('pwsh'))
    parser.add_argument('--go', default=shutil.which('go'))
    parser.add_argument('--source-root', type=Path, default=ROOT / 'scripts/fixtures/legacy-windows')
    parser.add_argument('--only', choices=('browser', 'directory', 'lan'))
    args = parser.parse_args()
    if not args.pwsh or not args.go:
        parser.error('PowerShell and Go required; pass --pwsh and --go')
    env = dict(os.environ, POWERSHELL_TELEMETRY_OPTOUT='1', POWERSHELL_UPDATECHECK='Off',
               PROCESSOR_ARCHITECTURE='AMD64')
    with tempfile.TemporaryDirectory(prefix='aide-startup-recovery-') as folder:
        work = Path(folder)

        def ps(code, root, extra=None):
            driver = root / 'driver.ps1'
            driver.write_text("$ErrorActionPreference='Stop'\n" + code, encoding='utf-8-sig')
            result = subprocess.run([args.pwsh, '-NoLogo', '-NoProfile', '-File', str(driver)],
                                    env=dict(env, **(extra or {})), text=True, capture_output=True,
                                    encoding='utf-8-sig', timeout=25)
            assert result.returncode == 0, (result.stdout, result.stderr)
            return json.loads(result.stdout.strip())

        # Parse real source, then execute its actual final opener statement and
        # dependency function only; avoid starting the rest of the launcher.
        browser = work / 'browser'
        browser.mkdir()
        for mode in (('throws', 'disabled', 'success') if args.only in (None, 'browser') else ()):
            data = ps(r'''
$errors=$null;$tokens=$null
$ast=[Management.Automation.Language.Parser]::ParseFile($env:FIXTURE_START,[ref]$tokens,[ref]$errors)
if($errors.Count){throw 'Launcher parse failed'}
$fn=$ast.Find({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -eq 'Open-AideLauncherBrowser'},$true)
if($fn){Invoke-Expression $fn.Extent.Text}
$lines=@($ast.EndBlock.Statements|Where-Object {$_.Extent.Text -match '^(if \(\$env:AIDE_OPEN_BROWSER|Open-AideLauncherBrowser -Url)'})
if($lines.Count -ne 1){throw 'Missing unique launcher browser call'}
$script:calls=0;$url='https://localhost:8097/#token=fixture-secret'
function Start-Process {param($FilePath,$ErrorAction) $script:calls++;if($env:FIXTURE_BROWSER -eq 'throws'){throw "association failed $FilePath"}}
$env:AIDE_OPEN_BROWSER=if($env:FIXTURE_BROWSER -eq 'disabled'){'0'}else{'1'}
$failed=$false;$warnings=@()
try {Invoke-Expression $lines[0].Extent.Text -WarningVariable warnings 3>$null}catch{$failed=$true}
@{failed=$failed;calls=$script:calls;warnings=(@($warnings)|Out-String)}|ConvertTo-Json -Compress
''', browser, {'FIXTURE_START': str(args.source_root / 'start.ps1'), 'FIXTURE_BROWSER': mode})
            assert not data['failed'], ('browser became fatal', mode, data)
            assert data['calls'] == (0 if mode == 'disabled' else 1), data
            assert 'fixture-secret' not in data['warnings'], data
            if mode == 'throws':
                assert data['warnings'].strip(), data
            print('PASS optional browser:', mode)

        native_src = work / 'fixture.go'
        native_src.write_text(NATIVE)
        native = work / 'fixture.exe'
        subprocess.run([args.go, 'build', '-o', str(native), str(native_src)], check=True, env=env)
        # A known, owned, reaped PID; never terminate arbitrary processes.
        child = subprocess.Popen([str(native)], env=env, stdout=subprocess.DEVNULL)
        dead_pid = child.pid
        assert child.wait(timeout=10) == 0
        for case in (('race', 'live', 'replacement', 'generic', 'prepare', 'missing') if args.only in (None, 'directory') else ()):
            root = work / ('local ' + case)
            (root / 'scripts').mkdir(parents=True)
            shutil.copy2(args.source_root / 'scripts/local-mounts-control.ps1', root / 'scripts')
            cache = root / '.aide-data/local-mounts'
            cache.mkdir(parents=True)
            shutil.copy2(native, cache / 'aide-local-mounts-windows-amd64.exe')
            state = root / '.aide-data/storage/data/local-mounts'
            state.mkdir(parents=True)
            (state / 'serve.pid').write_text(str(dead_pid))
            if case == 'missing':
                (state / 'serve.pid').unlink()
            settings = {'FIXTURE_NATIVE': str(native), 'FIXTURE_CACHE': str(cache),
                        'FIXTURE_NATIVE_LOG': str(root / 'native.log'),
                        'FIXTURE_DOCKER_LOG': str(root / 'docker.jsonl'),
                        'FIXTURE_STOP': 'race' if case in ('live', 'prepare') else case,
                        'FIXTURE_FRESH_OK': '1' if case == 'prepare' else '0',
                        'FIXTURE_ACTION': 'prepare' if case == 'prepare' else 'stop',
                        'FIXTURE_LIVE': '1' if case == 'live' else '0'}
            data = ps(DOCKER + r'''
$lock=Join-Path $PSScriptRoot '.aide-data/storage/data/local-mounts/serve.pid'
if($env:FIXTURE_LIVE -eq '1'){[IO.File]::WriteAllText($lock,[string]$PID)}
$failed=$false
try {& (Join-Path $PSScriptRoot 'scripts/local-mounts-control.ps1') -Action $env:FIXTURE_ACTION -Image 'aide:local' -DockerBin Invoke-FixtureDocker -ComposeCommand @('Invoke-FixtureDocker','compose') *> $null}catch{$failed=$true}
@{failed=$failed;pidExists=(Test-Path -LiteralPath $lock)}|ConvertTo-Json -Compress
''', root, settings)
            assert data['failed'] == (case in ('live', 'replacement', 'generic')), (case, data)
            if case in ('replacement', 'generic'):
                assert data['pidExists'], (case, data)
            if case == 'prepare':
                stops = [line.split('|')[0] for line in (root / 'native.log').read_text().splitlines() if line.endswith('|stop')]
                assert stops and all(Path(p).parent != cache for p in stops), stops
                assert hashlib.sha256((cache / 'aide-local-mounts-windows-amd64.exe').read_bytes()).digest() == hashlib.sha256(native.read_bytes()).digest()
            print('PASS directory recovery:', case)

        for case in (('fallback', 'direct', 'mismatch', 'checksum', 'public', 'bad-reference', 'missing', 'inspect-failed') if args.only in (None, 'lan') else ()):
            root = work / ('lan ' + case)
            (root / 'scripts').mkdir(parents=True)
            shutil.copy2(args.source_root / 'scripts/message-lan-control.ps1', root / 'scripts')
            settings = {'FIXTURE_NATIVE': str(native), 'FIXTURE_NATIVE_LOG': str(root / 'native.log'),
                        'FIXTURE_DOCKER_LOG': str(root / 'docker.jsonl'),
                        'FIXTURE_LAUNCH': str(root / 'launched'), 'FIXTURE_LAN': case}
            data = ps(DOCKER + r'''
$failed=$false
try {& (Join-Path $PSScriptRoot 'scripts/message-lan-control.ps1') -DockerBin Invoke-FixtureDocker -ComposeCommand @('Invoke-FixtureDocker','compose') *> $null}catch{$failed=$true}
@{failed=$failed;launched=(Test-Path -LiteralPath $env:FIXTURE_LAUNCH)}|ConvertTo-Json -Compress
''', root, settings)
            accepted = case in ('direct', 'fallback')
            assert data == {'failed': not accepted, 'launched': accepted}, (case, data)
            calls = [json.loads(line) for line in (root / 'docker.jsonl').read_text().splitlines()]
            copies = [c for c in calls if c[0] == 'cp']
            assert all(c[1].startswith('fresh-source:') for c in copies), calls
            creates = [c for c in calls if c[0] == 'create']
            assert all('--pull' in c and c[c.index('--pull')+1] == 'never' for c in creates), creates
            if case in ('mismatch', 'inspect-failed', 'bad-reference', 'missing'):
                assert not copies, calls
            if creates and case not in ('missing', 'bad-reference') and (case == 'direct' or len(creates) > 1):
                assert ['rm', 'fresh-source'] in calls, calls
            print('PASS trusted LAN extraction:', case)
    print('PASS actual PowerShell recovery; native Windows/Docker Desktop NOT_RUN')


if __name__ == '__main__':
    main()
