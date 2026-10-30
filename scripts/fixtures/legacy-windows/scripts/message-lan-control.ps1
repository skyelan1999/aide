[CmdletBinding()]
param([ValidateSet('start','stop')][string]$Action='start', [string]$DockerBin='docker', [string[]]$ComposeCommand=@('docker','compose'))
$ErrorActionPreference='Stop'
$lanRoot=Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $lanRoot
$lanDir=Join-Path $lanRoot '.aide-data/storage/data/messages/lan-bridge'
$nativeDir=Join-Path $lanRoot '.aide-data/message-lan'
$arch=if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') {'arm64'} else {'amd64'}
$name="aide-message-lan-windows-$arch.exe"
$binary=Join-Path $nativeDir $name
if ($Action -eq 'stop') { if (Test-Path -LiteralPath $binary) { & $binary -stop -dir $lanDir; if ($LASTEXITCODE -ne 0) { throw 'LAN bridge stop failed.' } }; return }
function Get-LanCompose([string[]]$Items) {
    $program=$ComposeCommand[0]
    [string[]]$prefix=@(if ($ComposeCommand.Count -gt 1) { $ComposeCommand[1..($ComposeCommand.Count-1)] })
    $text=((& $program @prefix @Items) | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'LAN bridge Compose query failed.' }
    return $text
}
function Invoke-LanDocker([string[]]$Items) {
    $previous=$ErrorActionPreference
    try {
        $ErrorActionPreference='Continue'
        $text=((& $DockerBin @Items 2> $null)|Out-String).Trim()
        return [pscustomobject]@{Code=$LASTEXITCODE;Text=$text}
    } finally { $ErrorActionPreference=$previous }
}
function New-TrustedLanSource([string]$Container,[string]$Identity) {
    $created=Invoke-LanDocker -Items @('create','--pull','never','--network','none','--entrypoint','/bin/true',$Identity)
    if ($created.Code -ne 0 -or -not $created.Text) {
        # Docker Desktop/containerd can retain the container's config digest
        # without resolving it as an image reference. A recorded tag is only a
        # locator: the new stopped container must still have the exact digest.
        $reference=Invoke-LanDocker -Items @('inspect',$Container,'--format','{{.Config.Image}}')
        if ($reference.Code -ne 0 -or $reference.Text -notmatch '^[A-Za-z0-9][A-Za-z0-9._:/@-]*$') { throw 'Cannot resolve the running image reference for the LAN helper.' }
        $created=Invoke-LanDocker -Items @('create','--pull','never','--network','none','--entrypoint','/bin/true',$reference.Text)
    }
    if ($created.Code -ne 0 -or $created.Text -notmatch '^[A-Za-z0-9][A-Za-z0-9_.-]*$') { throw 'Cannot extract a trusted LAN helper; restore the running image or recreate Aide from the selected image.' }
    $candidate=$created.Text
    try {
        $actual=Invoke-LanDocker -Items @('inspect',$candidate,'--format','{{.Image}}')
        if ($actual.Code -ne 0 -or $actual.Text -cne $Identity) { throw 'LAN helper image identity changed; recreate Aide from the selected image before retrying.' }
        return $candidate
    } catch {
        & $DockerBin rm $candidate *> $null
        throw
    }
}
$cid=Get-LanCompose -Items @('ps','-q','aide')
if (-not $cid -or $cid -match '\s') { throw 'Cannot identify the Aide container.' }
$binding=Get-LanCompose -Items @('port','aide','45840')
if ($binding -notmatch '^127\.0\.0\.1:([0-9]{1,5})$') { throw 'LAN bridge requires a loopback-only message mapping; restart using the updated Compose file.' }
$target="127.0.0.1:$($Matches[1])"
$captureBinding=Get-LanCompose -Items @('port','aide','45841')
if ($captureBinding -notmatch '^127\.0\.0\.1:([0-9]{1,5})$') { throw 'Missing loopback collector mapping; restart Aide.' }
$captureTarget='127.0.0.1:'+$Matches[1]
$metadata=Invoke-LanDocker -Items @('inspect',$cid,'--format','{{.Image}}')
$image=$metadata.Text
if ($metadata.Code -ne 0 -or $image -notmatch '^sha256:[a-f0-9]{64}$') { throw 'Invalid running image identity.' }
New-Item -ItemType Directory -Path $nativeDir,$lanDir -Force | Out-Null
$temp=Join-Path $nativeDir ([Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $temp | Out-Null
$source=''
try {
    $source=New-TrustedLanSource -Container $cid -Identity $image
    & $DockerBin cp "${source}:/opt/aide/message-lan/$name" (Join-Path $temp $name)
    if ($LASTEXITCODE -ne 0) { throw 'This image does not ship the LAN helper; rebuild it first.' }
    & $DockerBin cp "${source}:/opt/aide/message-lan/SHA256SUMS" (Join-Path $temp 'SHA256SUMS')
    if ($LASTEXITCODE -ne 0) { throw 'LAN helper checksums missing.' }
    $expected=@(Get-Content -LiteralPath (Join-Path $temp 'SHA256SUMS') | ForEach-Object { if ($_ -match ('^([a-f0-9]{64})\s+'+[regex]::Escape($name)+'$')) { $Matches[1] } })
    $actual=(Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $temp $name)).Hash.ToLowerInvariant()
    if ($expected.Count -ne 1 -or $expected[0] -cne $actual) { throw 'LAN helper checksum mismatch.' }
    if (Test-Path -LiteralPath $binary) { & $binary -stop -dir $lanDir; if ($LASTEXITCODE -ne 0) { throw 'Previous LAN bridge did not stop.' } }
    Move-Item -LiteralPath (Join-Path $temp $name) -Destination $binary -Force
    # Quoted arguments preserve directories with spaces; no command-string evaluation.
    Start-Process -FilePath $binary -ArgumentList @('-dir',('"'+$lanDir+'"'),'-target',$target,'-capture-target',$captureTarget) -WindowStyle Hidden -RedirectStandardOutput (Join-Path $nativeDir 'bridge.stdout.log') -RedirectStandardError (Join-Path $nativeDir 'bridge.log') | Out-Null
    for ($attempt=0;$attempt -lt 30;$attempt++) { & $binary -status -dir $lanDir *> $null; if ($LASTEXITCODE -eq 0) { Write-Host 'Message LAN bridge ready; proxy and firewall unchanged.'; return }; Start-Sleep -Milliseconds 100 }
    throw "LAN bridge not ready; see $nativeDir/bridge.log"
} finally {
    if ($source) { & $DockerBin rm $source *> $null }
    Remove-Item -LiteralPath $temp -Recurse -Force
}
