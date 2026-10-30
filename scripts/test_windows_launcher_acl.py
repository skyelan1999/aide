#!/usr/bin/env python3
"""Exercise permission-gate ordering with real PS; native ACL cases on Windows.

Linux checks mock only Protect-AideLauncherDataDirectory, never the restore
logic. They cannot establish NTFS or disconnected-domain acceptance.
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
    parser.add_argument('--source', type=Path, default=ROOT / 'scripts/fixtures/legacy-windows/scripts/launcher-data.ps1')
    parser.add_argument('--require-native', action='store_true')
    args = parser.parse_args()
    if not args.pwsh:
        parser.error('PowerShell required; pass --pwsh')
    env = dict(os.environ, POWERSHELL_TELEMETRY_OPTOUT='1', POWERSHELL_UPDATECHECK='Off')
    with tempfile.TemporaryDirectory(prefix='aide-launcher-acl-') as folder:
        root = Path(folder)
        shutil.copy2(args.source, root / 'launcher-data.ps1')

        def run(code):
            driver = root / 'driver.ps1'
            driver.write_text("$ErrorActionPreference='Stop'\n. (Join-Path $PSScriptRoot 'launcher-data.ps1')\n" + code,
                              encoding='utf-8-sig')
            result = subprocess.run([args.pwsh, '-NoLogo', '-NoProfile', '-File', str(driver)],
                                    env=env, capture_output=True, text=True, encoding='utf-8-sig', timeout=25)
            assert result.returncode == 0, (result.stdout, result.stderr)
            return json.loads(result.stdout)

        for case in ('existing', 'denied', 'new', 'posix'):
            case_root = root / case
            if case != 'new':
                (case_root / '.aide-data').mkdir(parents=True)
                (case_root / '.aide-data/launcher.env').write_text('AIDE_PORT=19000\n')
            else:
                case_root.mkdir()
            env['FIXTURE_ROOT'] = str(case_root)
            env['FIXTURE_CASE'] = case
            data = run(r'''
$env:OS=if($env:FIXTURE_CASE -eq 'posix'){'fixture-posix'}else{'Windows_NT'}
$script:protected=0
function Protect-AideLauncherDataDirectory([string]$Path) {
 if(-not(Test-Path -LiteralPath $Path -PathType Container)){throw 'Directory missing before protection'}
 if(Test-Path -LiteralPath (Join-Path $env:FIXTURE_ROOT '.env')){throw 'Read config before protection'}
 $script:protected++
 if($env:FIXTURE_CASE -eq 'denied'){throw 'fixture access denied'}
}
$failed=$false
try{Restore-AideLauncherEnvironment $env:FIXTURE_ROOT}catch{$failed=$true}
@{failed=$failed;protected=$script:protected;restored=(Test-Path -LiteralPath (Join-Path $env:FIXTURE_ROOT '.env'))}|ConvertTo-Json -Compress
''')
            assert data == {'failed': case == 'denied', 'protected': 0 if case == 'posix' else 1,
                            'restored': case in ('existing', 'posix')}, (case, data)
            print('PASS actual restore permission gate:', case)

        # The shipped permission implementation itself, with no ACL mocks.
        env['FIXTURE_ROOT'] = str(root / 'native')
        (root / 'native').mkdir()
        data = run(r'''
if($env:OS -ne 'Windows_NT'){@{native=$false}|ConvertTo-Json -Compress;return}
$target=Join-Path $env:FIXTURE_ROOT '.aide-data'
Restore-AideLauncherEnvironment $env:FIXTURE_ROOT
$owner=[Security.Principal.WindowsIdentity]::GetCurrent().User
function Read-FixtureAcl {
 $directory=Get-Item -LiteralPath $target
 if($PSVersionTable.PSEdition -eq 'Core'){return [IO.FileSystemAclExtensions]::GetAccessControl($directory,[Security.AccessControl.AccessControlSections]::Access)}
 return $directory.GetAccessControl([Security.AccessControl.AccessControlSections]::Access)
}
function Assert-FixturePrivate {
 $current=Read-FixtureAcl
 $rules=@($current.GetAccessRules($true,$true,[Security.Principal.SecurityIdentifier]))
 if(-not $current.AreAccessRulesProtected -or $rules.Count -ne 1){throw 'DACL is not private'}
 $rule=$rules[0]
 if($rule.IdentityReference.Value -cne $owner.Value -or $rule.IsInherited -or $rule.AccessControlType -ne 'Allow' -or $rule.FileSystemRights -ne 'FullControl' -or $rule.InheritanceFlags -ne 'ContainerInherit,ObjectInherit' -or $rule.PropagationFlags -ne 'None'){throw 'Wrong private ACE'}
}
Assert-FixturePrivate
# A protected ACL can still grant broad access. Only disposable fixture data;
# restore must remove this extra rule before copying any saved configuration.
$acl=Read-FixtureAcl
$world=New-Object Security.Principal.SecurityIdentifier('S-1-1-0')
$rule=New-Object Security.AccessControl.FileSystemAccessRule($world,'ReadAndExecute','ContainerInherit,ObjectInherit','None','Allow')
$acl.AddAccessRule($rule)
$directory=Get-Item -LiteralPath $target
if($PSVersionTable.PSEdition -eq 'Core'){[IO.FileSystemAclExtensions]::SetAccessControl($directory,$acl)}else{$directory.SetAccessControl($acl)}
[IO.File]::WriteAllText((Join-Path $target 'launcher.env'),"AIDE_PORT=19000`n")
Restore-AideLauncherEnvironment $env:FIXTURE_ROOT
Assert-FixturePrivate
if(-not(Test-Path -LiteralPath (Join-Path $env:FIXTURE_ROOT '.env'))){throw 'Config not restored after protection'}
# Repeated startup must keep the same single SID rule.
Restore-AideLauncherEnvironment $env:FIXTURE_ROOT
Assert-FixturePrivate
@{native=$true}|ConvertTo-Json -Compress
''')
        if data['native']:
            print('PASS native SID DACL: create, existing protected broad ACL repair, repeat')
        else:
            print('NOT_RUN native Windows ACL and disconnected-domain environment')
            assert not args.require_native, 'Windows required for native ACL acceptance'

        # Parse, rather than regex-match comments, to prevent Set-Acl regressions.
        data = run(r'''
$errors=$null;$tokens=$null
$ast=[Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'launcher-data.ps1'),[ref]$tokens,[ref]$errors)
if($errors.Count){throw 'Parse failed'}
$setAcl=@($ast.FindAll({param($n)$n -is [Management.Automation.Language.CommandAst] -and $n.GetCommandName() -eq 'Set-Acl'},$true))
$protect=$ast.Find({param($n)$n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -eq 'Protect-AideLauncherDataDirectory'},$true)
@{setAcl=$setAcl.Count;protect=($null -ne $protect)}|ConvertTo-Json -Compress
''')
        assert data == {'setAcl': 0, 'protect': True}, data
        print('PASS permission implementation AST (static only)')


if __name__ == '__main__':
    main()
