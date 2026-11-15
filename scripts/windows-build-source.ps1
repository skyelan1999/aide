# First-source-build recovery before a native host exists. The bootstrap already
# uses Windows PowerShell; the installed native host does not call this script.
$ErrorActionPreference = 'Continue'
$command = @('buildx', 'build', '--builder', $env:AIDE_BOOT_BUILDER, '--load', '--pull=false',
    '--target', 'native-launcher', '--file', 'Dockerfile', '--build-arg',
    ('AIDE_LAUNCHER_ARCH=' + $env:AIDE_BOOT_ARCH), '--tag', $env:AIDE_BOOT_IMAGE)
$enabled = $env:AIDE_CN_SOURCE_FALLBACK -ne '0'
if (-not $enabled) { $command += @('--build-arg', 'AIDE_CN_SOURCE_FALLBACK=0') }
$defaults = [IO.File]::ReadAllText((Join-Path (Get-Location) 'Dockerfile'))
$mapped = @{}
for ($attempt = 0; $attempt -le 3; $attempt++) {
    $tail = New-Object Text.StringBuilder
    & docker.exe @command '.' 2>&1 | ForEach-Object {
        $text = $_.ToString()
        Write-Host $text
        [void]$tail.AppendLine($text)
        if ($tail.Length -gt 262144) { [void]$tail.Remove(0, $tail.Length - 262144) }
    }
    $code = $LASTEXITCODE
    if ($code -eq 0) { exit 0 }
    if (-not $enabled -or $attempt -eq 3) { exit $code }
    $errors = (($tail.ToString() -split "`n") | Where-Object { $_ -cmatch 'ERROR|error:' }) -join "`n"
    if ($errors -cnotmatch 'failed to (resolve source metadata|authorize|fetch (anonymous |oauth )?token|do request)|pull access denied|unexpected status from (HEAD|GET)') { exit $code }
    $changed = $false
    foreach ($key in @('PYTHON_BASE', 'NODE_BASE', 'GO_BASE')) {
        if ($mapped.ContainsKey($key)) { continue }
        $match = [regex]::Match($defaults, '(?m)^ARG ' + $key + '=([^\r\n ]+)')
        if (-not $match.Success) { continue }
        $ref = $match.Groups[1].Value
        $canonical = $ref -creplace '^(docker\.io/library/|docker\.io/|library/)', ''
        if ($canonical -cnotmatch '^(python|node|golang)(:[A-Za-z0-9_.-]+)?(@sha256:[a-f0-9]{64})?$') { continue }
        if (-not $errors.Contains($ref) -and -not $errors.Contains($canonical)) { continue }
        $command += @('--build-arg', ($key + '=docker.m.daocloud.io/library/' + $canonical))
        $mapped[$key] = $true
        $changed = $true
    }
    if (-not $changed) { exit $code }
    Write-Host '[Aide] Base image pull failed; matching the same tag/digest in the mainland DaoCloud mirror (once per base)...'
}
exit 1
