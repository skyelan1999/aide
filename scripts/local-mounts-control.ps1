[CmdletBinding()]
param([ValidateSet('prepare','start','stop')][string]$Action='start',[string]$Image='', [string]$DockerBin='docker', [string[]]$ComposeCommand=@('docker','compose'))
$ErrorActionPreference='Stop'
$mountRoot=Split-Path -Parent $PSScriptRoot
$nativeDir=Join-Path $mountRoot '.aide-data/local-mounts'
$arch=if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') {'arm64'} else {'amd64'}
$name="aide-local-mounts-windows-$arch.exe"
$binary=Join-Path $nativeDir $name
New-Item -ItemType Directory -Path $nativeDir -Force | Out-Null
$env:AIDE_LOCAL_MOUNTS_COMPOSE=if($ComposeCommand.Count -gt 1 -and $ComposeCommand[1] -eq 'compose'){'docker'}else{$ComposeCommand[0]}
function Skip-UnavailableDirectoryHelper {
 foreach($relative in @('.aide-data/storage/data/local-mounts/desired.json','.aide-data/local-mounts/active.json')) {
  $file=Join-Path $mountRoot $relative
  if(Test-Path -LiteralPath $file) {
   $value=Get-Content -LiteralPath $file -Raw | ConvertFrom-Json
   if(-not($value -is [pscustomobject]) -or -not($value.PSObject.Properties.Name -contains 'entries') -or -not($value.entries -is [array]) -or $value.entries.Count -gt 0){throw 'Selected image has no directory helper but local mounts are configured; update the image first.'}
  }
 }
 if(Test-Path -LiteralPath $binary){& $binary -mode stop -root $mountRoot;if($LASTEXITCODE -ne 0){throw 'Directory helper stop failed'}}
 New-Item -ItemType File -Path (Join-Path $nativeDir 'unavailable') -Force | Out-Null
 [Console]::Error.WriteLine('Selected image has no directory helper; no local mounts configured, continuing without the optional helper.')
}
if ($Action -eq 'stop') { if(Test-Path -LiteralPath $binary){ & $binary -mode stop -root $mountRoot; if($LASTEXITCODE -ne 0){throw 'Directory helper stop failed'} };return }
if ($Action -eq 'prepare') {
 if (-not $Image) { throw 'Directory helper requires selected image' }
 $temp=Join-Path $nativeDir ([Guid]::NewGuid().ToString('N'));New-Item -ItemType Directory -Path $temp | Out-Null
 $source=''
 try {
  $source=((& $DockerBin create --network none --entrypoint /bin/true $Image)|Out-String).Trim()
  if($LASTEXITCODE -ne 0 -or -not $source){throw 'Cannot extract directory helper'}
  & $DockerBin cp "${source}:/opt/aide/message-lan/$name" (Join-Path $temp $name) *> $null
  if($LASTEXITCODE -ne 0){Skip-UnavailableDirectoryHelper;return}
  & $DockerBin cp "${source}:/opt/aide/message-lan/SHA256SUMS" (Join-Path $temp 'SHA256SUMS') *> $null
  if($LASTEXITCODE -ne 0){throw 'Missing directory helper checksum'}
  $expected=@(Get-Content -LiteralPath (Join-Path $temp 'SHA256SUMS')|ForEach-Object{if($_ -match ('^([a-f0-9]{64})\s+'+[regex]::Escape($name)+'$')){$Matches[1]}})
  $actual=(Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $temp $name)).Hash.ToLowerInvariant()
  if($expected.Count -ne 1 -or $expected[0] -cne $actual){throw 'Directory helper checksum mismatch'}
  if(Test-Path -LiteralPath $binary){& $binary -mode stop -root $mountRoot;if($LASTEXITCODE -ne 0){throw 'Directory helper stop failed'}}
  Move-Item -LiteralPath (Join-Path $temp $name) -Destination $binary -Force
  Remove-Item -LiteralPath (Join-Path $nativeDir 'unavailable') -ErrorAction SilentlyContinue
  & $binary -mode prepare -root $mountRoot -docker $DockerBin
  if($LASTEXITCODE -ne 0){throw 'Directory preparation failed'}
 }finally{if($source){& $DockerBin rm $source *> $null};Remove-Item -LiteralPath $temp -Recurse -Force}
}else{
 if((Test-Path -LiteralPath (Join-Path $nativeDir 'unavailable')) -or -not(Test-Path -LiteralPath $binary)){Skip-UnavailableDirectoryHelper;return}
 Start-Process -FilePath $binary -ArgumentList @('-root',('"'+$mountRoot+'"'),'-docker',('"'+$DockerBin+'"')) -WindowStyle Hidden -RedirectStandardOutput (Join-Path $nativeDir 'helper.stdout.log') -RedirectStandardError (Join-Path $nativeDir 'helper.log') | Out-Null
}
