#!/usr/bin/env python3
"""Execute the actual offline image bootstrap in PowerShell with isolated Docker fixtures."""
import argparse
import hashlib
from pathlib import Path
import shutil
import subprocess
import tempfile

parser = argparse.ArgumentParser()
parser.add_argument('--pwsh', default=shutil.which('pwsh'))
parser.add_argument('--source', default=str(Path(__file__).resolve().parents[1] / 'scripts/fixtures/legacy-windows/start.ps1'))
args = parser.parse_args()
if not args.pwsh:
    parser.error('PowerShell is required; pass --pwsh PATH')
source = Path(args.source).read_text(encoding='utf-8-sig')
helper = source[source.index('function Resolve-AideImportedImage'):source.index('# --- 定位 docker ---')] if 'function Resolve-AideImportedImage' in source else ''
start = source.index('if ($offlineBundle) {\n    $enginePlatform')
block = source[start:source.index('  New-Item -ItemType Directory', start)] + '\n}\n'
expected = 'sha256:' + 'a' * 64
local = 'sha256:' + '7' * 64
mock = r'''
$ErrorActionPreference = 'Stop'
Import-Module Microsoft.PowerShell.Management
Import-Module Microsoft.PowerShell.Utility
Import-Module Microsoft.PowerShell.Security
$offlineBundle = $true
$dockerExe = 'fixture-docker'
$expectedPlatform = 'linux/amd64'
$expectedImageId = 'EXPECTED'
$releaseTag = 'v0.1.17.5-RC2'
$env:AIDE_IMAGE = 'aide:slot-a'
$script:loaded = $false
$script:tagged = $false
$script:loads = 0
$script:tags = 0
function Invoke-Native {
    param([string]$File, [string[]]$Arguments, [switch]$Capture)
    $text = ''; $code = 0
    $ref = $Arguments[2]
    switch ($Arguments[0] + ':' + $Arguments[1]) {
        'info:--format' { $text = 'linux/x86_64' }
        'image:load' {
            $script:loads++
            if ($case -eq 'load-failure') { $code = 1; break }
            $script:loaded = $true
            $text = 'Loaded image: aide:0.1.17.5-RC2-linux-amd64'
            if ($case -eq 'ambiguous') { $text += "`nLoaded image: other:tag" }
            if ($case -eq 'empty-output') { $text = '' }
            if ($case -eq 'id-output') { $text = 'Loaded image ID: LOCAL' }
        }
        'image:inspect' {
            $id = $null
            if ($ref -eq 'aide:slot-a' -and ($script:tagged -or $case -eq 'repeat')) { $id = if ($case -eq 'legacy') { 'EXPECTED' } else { 'LOCAL' } }
            if ($script:loaded -and $ref -eq 'EXPECTED' -and $case -eq 'legacy') { $id = 'EXPECTED' }
            if ($script:loaded -and $ref -in @('aide:0.1.17.5-RC2-linux-amd64', 'LOCAL') -and $case -ne 'missing') { $id = 'LOCAL' }
            if (-not $id) { $code = 1; break }
            $text = $id
            if ($Arguments[-1] -like '*Architecture*') {
                $platform = if ($case -eq 'wrong-platform') { 'linux/arm64' } else { 'linux/amd64' }
                $text = "$id|$platform"
            }
            if ($case -eq 'readback-failure' -and $script:tagged) { $text = 'EXPECTED' }
        }
        'image:tag' {
            $script:tags++
            $wanted = if ($case -eq 'legacy') { 'EXPECTED' } else { 'LOCAL' }
            if ($ref -ne $wanted -or $case -eq 'tag-failure') { $code = 1 } else { $script:tagged = $true }
        }
        default { throw "Unexpected Docker invocation: $Arguments" }
    }
    [pscustomobject]@{ Code = $code; Text = $text }
}
if ($case -eq 'repeat') { $expectedImageId = 'LOCAL' }
$original = [IO.File]::ReadAllText((Join-Path $PSScriptRoot '.aide-image'))
$failure = $null
try {
BOOTSTRAP
} catch { $failure = $_.Exception.Message }
$after = [IO.File]::ReadAllText((Join-Path $PSScriptRoot '.aide-image'))
$reject = $case -in @('bad-hash', 'load-failure', 'ambiguous', 'empty-output', 'wrong-platform', 'missing', 'tag-failure', 'readback-failure')
if ($reject) {
    if (-not $failure) { throw "Expected rejection for $case" }
    if ($after -ne $original) { throw "Rejected import changed marker for $case" }
    if ($case -eq 'bad-hash' -and $script:loads -ne 0) { throw 'Loaded archive before hash verification' }
    if ($case -in @('ambiguous','empty-output','wrong-platform','missing','load-failure') -and $script:tags -ne 0) { throw 'Tagged an unverified import' }
} else {
    if ($failure) { throw "Unexpected failure for ${case}: $failure" }
    $wanted = if ($case -eq 'legacy') { 'EXPECTED' } else { 'LOCAL' }
    if ($after -ne "aide:slot-a $wanted linux/amd64 v0.1.17.5-RC2") { throw "Wrong persisted marker: $after" }
    if ($case -eq 'repeat' -and ($script:loads -ne 0 -or $script:tags -ne 0)) { throw 'Repeat launch imported or retagged unnecessarily' }
}
Write-Host "PASS $case"
'''.replace('BOOTSTRAP', helper + block).replace('EXPECTED', expected).replace('LOCAL', local)
cases = ['changed-id','legacy','id-output','repeat','bad-hash','load-failure','ambiguous','empty-output','wrong-platform','missing','tag-failure','readback-failure']
for case in cases:
    with tempfile.TemporaryDirectory(prefix='aide-image-slot-') as folder:
        root = Path(folder)
        images = root / 'docker-images'
        images.mkdir()
        payload = b'isolated archive fixture'
        (images / 'aide-local.tar.gz').write_bytes(payload)
        digest = '0' * 64 if case == 'bad-hash' else hashlib.sha256(payload).hexdigest()
        (images / 'SHA256SUMS').write_text(digest + '  aide-local.tar.gz\n')
        (root / '.aide-image').write_text('original marker')
        script = root / 'test.ps1'
        script.write_bytes(b'\xef\xbb\xbf' + ("$case = '" + case + "'\n" + mock).encode())
        subprocess.run([args.pwsh, '-NoLogo', '-NoProfile', '-File', str(script)], check=True)
print(f'PASS: {len(cases)} actual PowerShell offline bootstrap cases (mock Docker; not native Windows)')
