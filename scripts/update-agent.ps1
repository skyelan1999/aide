# Windows host-side A/B installer. Runs outside the aide container.
$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)
$currentPowerShell = (Get-Process -Id $PID).Path
Remove-Item Env:AIDE_PORT -ErrorAction SilentlyContinue
if (-not $env:COMPOSE_FILE) { $env:COMPOSE_FILE = 'compose.yaml' }
if (-not (Test-Path -LiteralPath '.aide-image')) { exit 0 }
# A directory alone survives a crashed process. An exclusive OS file handle
# recovers automatically after exit, while still rejecting a concurrent agent.
$lock = Join-Path $PWD '.aide-update-agent.lock'
$ownerFile = Join-Path $lock 'owner.lock'
if ((Test-Path -LiteralPath $lock) -and -not (Test-Path -LiteralPath $ownerFile) -and $env:OS -eq 'Windows_NT') {
  $thisScript = $PSCommandPath
  $legacy = @(Get-CimInstance Win32_Process -Filter "Name='powershell.exe' OR Name='pwsh.exe'" | Where-Object {
    $_.ProcessId -ne $PID -and $_.CommandLine -and $_.CommandLine.IndexOf($thisScript, [StringComparison]::OrdinalIgnoreCase) -ge 0
  })
  if ($legacy.Count) { Write-Warning 'An older update agent is still running. Restart Windows before using the repaired launcher.'; exit 0 }
}
New-Item -ItemType Directory -Path $lock -Force | Out-Null
try { $owner = [IO.File]::Open($ownerFile, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None) } catch [IO.IOException] { exit 0 }

function ConvertTo-AideUpdateDiagnostic([string]$Text, [string]$KnownToken='', [int]$Limit=2048) {
  if ($KnownToken) { $Text=$Text.Replace($KnownToken,'[REDACTED]') }
  $Text=[regex]::Replace($Text,'(?i)([#?&](?:access[-_]?token|token|password|secret|key)=)[^\s"''<>]+','$1[REDACTED]')
  $Text=[regex]::Replace($Text,'(?i)(\bBearer\s+)\S+','$1[REDACTED]')
  $Text=[regex]::Replace($Text,'(?i)((?:access[-_ ]?token|api[-_]?key|password|secret)\s*[:=]\s*)(?:"[^"]*"|''[^'']*''|\S+)','$1[REDACTED]')
  $Text=[regex]::Replace($Text,'[\r\n]+',' | ')
  if ($Text.Length -gt $Limit) { $Text=$Text.Substring(0,$Limit)+'...' }
  return $Text
}
function Write-AideUpdateDiagnostic([string]$Path, [string]$Event, [string]$Message, [string]$KnownToken='') {
  # Never write raw launcher output. Reject links for both rotation targets.
  foreach ($file in @($Path,($Path+'.1'))) {
    $item=Get-Item -Force -LiteralPath $file -ErrorAction Ignore
    if ($null -ne $item -and ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Update diagnostic path cannot be a link.' }
  }
  if ((Test-Path -LiteralPath $Path) -and (Get-Item -LiteralPath $Path).Length -ge 1MB) { Move-Item -LiteralPath $Path -Destination ($Path+'.1') -Force }
  $safe=ConvertTo-AideUpdateDiagnostic $Message $KnownToken
  [IO.File]::AppendAllText($Path,([DateTime]::UtcNow.ToString('o')+' '+$Event+' '+$safe+"`n"),(New-Object Text.UTF8Encoding $false))
}
function Invoke-AideSlotLauncher([string]$Phase='activation', [int]$TimeoutMilliseconds=300000) {
  if ($Phase -notin @('activation','recovery')) { throw 'Invalid launcher phase.' }
  $launcher = Join-Path $PWD 'start.ps1'
  $logPath = Join-Path $updateLogDir ($Phase+'.log')
  # A child PowerShell pipeline redacts before disk writes, including host/error
  # streams. Wait only for this launcher, not its permanent monitor descendants.
  $bootstrap='function ConvertTo-AideUpdateDiagnostic {'+${function:ConvertTo-AideUpdateDiagnostic}.ToString()+"}`n"+
    'function Write-AideUpdateDiagnostic {'+${function:Write-AideUpdateDiagnostic}.ToString()+"}`n"+
    ('$launcher=' + "'"+$launcher.Replace("'","''")+"'`n")+
    ('$logPath=' + "'"+$logPath.Replace("'","''")+"'`n")+@'
$ErrorActionPreference='Stop'
$diagnosticToken=$env:AIDE_UPDATE_DIAGNOSTIC_TOKEN
Remove-Item Env:AIDE_UPDATE_DIAGNOSTIC_TOKEN -ErrorAction SilentlyContinue
try {
  $script:launcherOK=$false
  $script:launcherExit=1
  & {
    & $launcher
    $script:launcherOK=$?
    $script:launcherExit=$LASTEXITCODE
  } *>&1 | ForEach-Object { Write-AideUpdateDiagnostic $logPath 'output' ([string]$_) $diagnosticToken }
  if (-not $script:launcherOK) { if ($script:launcherExit) { exit $script:launcherExit }; exit 1 }
} catch {
  Write-AideUpdateDiagnostic $logPath 'error' ($_.Exception.Message+' | '+$_.FullyQualifiedErrorId+' | '+$_.ScriptStackTrace) $diagnosticToken
  exit 1
}
exit 0
'@
  $encoded=[Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($bootstrap))
  $previousToken=$env:AIDE_UPDATE_DIAGNOSTIC_TOKEN
  $env:AIDE_OPEN_BROWSER='0'
  $proc=$null
  try {
    $env:AIDE_UPDATE_DIAGNOSTIC_TOKEN=$token
    $proc=Start-Process -FilePath $currentPowerShell -ArgumentList @('-NoProfile','-ExecutionPolicy','Bypass','-EncodedCommand',$encoded) -PassThru -WindowStyle Hidden
    if (-not $proc.WaitForExit($TimeoutMilliseconds)) {
      $script:launcherUnsafe=$true
      try {
        # Terminate only this owned launcher's tree on timeout, so a surviving
        # Docker child cannot race recovery. Normal completion never waits for
        # or terminates the permanent monitor descendants.
        if ($PSVersionTable.PSEdition -eq 'Core') { $proc.Kill($true) }
        else { & taskkill.exe /PID $proc.Id /T /F *> $null; if ($LASTEXITCODE -ne 0) { throw 'Launcher termination failed.' } }
        if (-not $proc.WaitForExit(5000)) { throw 'Launcher termination was not confirmed.' }
        $script:launcherUnsafe=$false
      } catch {
        try { $proc.Refresh(); if ($proc.HasExited) { $script:launcherUnsafe=$false } } catch {}
        if ($script:launcherUnsafe) {
          [IO.File]::WriteAllText((Join-Path $updateLogDir 'launcher-stop-required'),([string]$proc.Id))
          throw 'Launcher termination was not confirmed; automatic recovery is paused.'
        }
      }
      throw 'Launcher process timed out.'
    }
    $proc.Refresh()
    if ($proc.ExitCode -ne 0) { throw ('Target launcher failed (exit '+$proc.ExitCode+'); see '+$Phase+'.log.') }
  } finally {
    $env:AIDE_UPDATE_DIAGNOSTIC_TOKEN=$previousToken
    if ($null -ne $proc) { $proc.Dispose() }
  }
}
function Save-AideUpdateResult([string]$Operation, [bool]$Success, [string]$Message, $Canonical=$null) {
  $item=Get-Item -Force -LiteralPath $pendingResult -ErrorAction Ignore
  if ($null -ne $item -and ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Pending update result cannot be a link.' }
  $record=@{result=@{operationId=$Operation;success=$Success;message=(ConvertTo-AideUpdateDiagnostic $Message $token 220)};canonical=$Canonical}
  $tempResult=Join-Path $updateLogDir ('result-'+[guid]::NewGuid().ToString('N')+'.tmp')
  try {
    [IO.File]::WriteAllText($tempResult,($record|ConvertTo-Json -Depth 7 -Compress),(New-Object Text.UTF8Encoding $false))
    Move-Item -LiteralPath $tempResult -Destination $pendingResult -Force
  } finally { if (Test-Path -LiteralPath $tempResult) { Remove-Item -LiteralPath $tempResult -Force -ErrorAction SilentlyContinue } }
}
function Submit-AideUpdateResult {
  $item=Get-Item -Force -LiteralPath $pendingResult -ErrorAction Ignore
  if ($null -ne $item -and ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Pending update result cannot be a link.' }
  if (-not $token -or -not (Test-Path -LiteralPath $pendingResult)) { return $false }
  if ((Get-Item -LiteralPath $pendingResult).Length -gt 16384) { throw 'Invalid pending update result.' }
  $record=Get-Content -LiteralPath $pendingResult -Raw | ConvertFrom-Json
  if ($record.result.operationId -notmatch '^[a-f0-9]{32}$' -or $record.result.success -isnot [bool] -or $record.result.message -isnot [string]) { throw 'Invalid pending update result.' }
  $request=Join-Path $tmp 'result.json'
  [IO.File]::WriteAllText($request,($record.result|ConvertTo-Json -Compress),(New-Object Text.UTF8Encoding $false))
  $previous=$ErrorActionPreference
  try {
    $ErrorActionPreference='Continue'
    $http=((& curl.exe -k -f -sS --connect-timeout 3 --max-time 10 -H ('Authorization: Bearer '+$token) -H 'Content-Type: application/json' -X POST --data-binary "@$request" -o (Join-Path $tmp 'response.json') -w '%{http_code}' "$base/agent/result" 2>$null)|Out-String).Trim()
    $code=$LASTEXITCODE
    if ($code -ne 0) {
      if ($http -eq '409') {
        # A lost acknowledgement or an operator's newer command must not cause
        # the old operation to run again. Compatible with existing RC servers.
        $check=Join-Path $tmp 'pending-check.tsv'
        & curl.exe -k -f -sS --connect-timeout 3 --max-time 10 -H ('Authorization: Bearer '+$token) "$base/agent" -o $check 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0 -and (Test-Path -LiteralPath $check)) {
          $line=([IO.File]::ReadAllText($check)).Trim()
          $current=($line -split "`t")[0]
          if (-not $line -or ($current -match '^[a-f0-9]{32}$' -and $current -cne $record.result.operationId)) {
            Remove-Item -LiteralPath $pendingResult -Force
            Write-AideUpdateDiagnostic $agentLog 'result-obsolete' $record.result.operationId
            return $true
          }
        }
      }
      Write-AideUpdateDiagnostic $agentLog 'result-retry' $record.result.operationId
      return $false
    }
    Remove-Item -LiteralPath $pendingResult -Force
    Write-AideUpdateDiagnostic $agentLog 'result-acknowledged' $record.result.operationId
    if ($null -ne $record.canonical) {
      [IO.File]::WriteAllText($request,($record.canonical|ConvertTo-Json -Depth 5 -Compress),(New-Object Text.UTF8Encoding $false))
      & curl.exe -k -f -sS --connect-timeout 3 --max-time 10 -H ('Authorization: Bearer '+$token) -H 'Content-Type: application/json' -X POST --data-binary "@$request" "$base/agent/sync" 2>$null | Out-Null
    }
    return $true
  } catch {
    Write-AideUpdateDiagnostic $agentLog 'result-retry' $_.Exception.Message $token
    return $false
  } finally { $ErrorActionPreference=$previous }
}
function Resolve-AideUpdateImage([string]$Expected, [string]$Platform, [string]$LoadOutput) {
  # Resolve only IDs/references emitted by this verified archive import.
  $meta = (& $docker image inspect $Expected --format '{{.Id}}|{{.Os}}/{{.Architecture}}' 2>$null | Out-String).Trim()
  if ($LASTEXITCODE -ne 0) {
    $refs = @($LoadOutput -split '\r?\n' | ForEach-Object {
      if ($_ -cmatch '^Loaded image(?: ID)?: ([A-Za-z0-9][A-Za-z0-9._:/@-]*)\s*$') { $Matches[1] }
    } | Sort-Object -Unique)
    if ($refs.Count -ne 1) { throw 'Cannot identify the verified imported image.' }
    $meta = (& $docker image inspect $refs[0] --format '{{.Id}}|{{.Os}}/{{.Architecture}}' 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Imported image is unavailable.' }
  }
  $parts = $meta -split '\|'
  if ($parts.Count -ne 2 -or $parts[0] -cnotmatch '^sha256:[a-f0-9]{64}$' -or $parts[1] -cne $Platform) { throw 'Imported image identity/platform mismatch.' }
  return $parts[0]
}
function Send-AideUpdateProgress([string]$Status, [int]$Progress, [string]$Message) {
  $file = Join-Path $tmp 'progress.json'
  [IO.File]::WriteAllText($file, (@{operationId=$op;status=$Status;progress=$Progress;message=$Message} | ConvertTo-Json -Compress), (New-Object System.Text.UTF8Encoding $false))
  $previous=$ErrorActionPreference
  try {
    $ErrorActionPreference='Continue'
    & curl.exe -k -f -sS --connect-timeout 3 --max-time 10 -H $headers[0] -H 'Content-Type: application/json' -X POST --data-binary "@$file" "$base/agent/progress" 2>$null | Out-Null
  } catch { Write-AideUpdateDiagnostic $agentLog 'progress-unavailable' $_.Exception.Message $token }
  finally { $ErrorActionPreference=$previous }
}
try {
  $updateLogDir=Join-Path $PWD '.aide-data/update-agent'
  $pendingResult=Join-Path $updateLogDir 'pending-result.json'
  $agentLog=Join-Path $updateLogDir 'agent.log'
  $launcherBlocker=Join-Path $updateLogDir 'launcher-stop-required'
  foreach ($path in @((Join-Path $PWD '.aide-data'),$updateLogDir,$pendingResult,$launcherBlocker)) {
    $item=Get-Item -Force -LiteralPath $path -ErrorAction Ignore
    if ($null -ne $item -and ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Update state cannot be a link.' }
  }
  New-Item -ItemType Directory -Path $updateLogDir -Force | Out-Null
  $script:launcherUnsafe=Test-Path -LiteralPath $launcherBlocker
  Write-AideUpdateDiagnostic $agentLog 'agent-start' 'Update agent acquired ownership'
  $docker = (Get-Command docker -ErrorAction Stop).Source
  $marker = (Get-Content -Raw '.aide-image').Trim() -split '\s+'
  $portLine = & $docker compose port aide 8080 2>$null | Select-Object -Last 1
  if ($portLine -notmatch ':(\d+)\s*$') { exit 0 }
  $port = $Matches[1]
  $token = (& $docker compose exec -T aide sh -c 'cat /data/auth/access-token 2>/dev/null || cat /data/access-token 2>/dev/null' 2>$null | Out-String).Trim()
  if (-not $token) { exit 0 }
  $base = "https://127.0.0.1:$port/api/updates"
  $headers = @('Authorization: Bearer ' + $token)
  $oldRef, $oldId, $platform, $tag = $marker
  if ($oldRef -notlike 'aide:slot-*') { $oldRef = 'aide:slot-a' }
  & $docker image tag $oldId $oldRef 2>$null | Out-Null
  $activeSlot = if ($oldRef -eq 'aide:slot-b') { 'B' } else { 'A' }
  $activeVersion = if ($tag) { $tag.TrimStart('v').Replace('-RC',' RC') } else { '' }
  $sync = @{ version=1; activeSlot=$activeSlot; slots=@{} }
  $sync.slots[$activeSlot] = @{ version=$activeVersion; tag=$tag; imageRef=$oldRef; imageId=$oldId; platform=$platform }
  $syncFile = Join-Path $env:TEMP ('aide-update-sync-' + [guid]::NewGuid().ToString('N') + '.json')
  try {
    [System.IO.File]::WriteAllText($syncFile, ($sync | ConvertTo-Json -Compress -Depth 5), (New-Object System.Text.UTF8Encoding $false))
    & curl.exe -k -sS --connect-timeout 3 --max-time 10 -H $headers[0] -H 'Content-Type: application/json' -X POST --data-binary "@$syncFile" "$base/agent/sync" 2>$null | Out-Null
  } finally { Remove-Item -LiteralPath $syncFile -Force -ErrorAction SilentlyContinue }
  while ($true) {
    $op = $null
    $oldMarker = $null
    $portLine = & $docker compose port aide 8080 2>$null | Select-Object -Last 1
    if ($portLine -notmatch ':(\d+)\s*$') { Start-Sleep -Seconds 3; continue }
    $port = $Matches[1]
    $token = (& $docker compose exec -T aide sh -c 'cat /data/auth/access-token 2>/dev/null || cat /data/access-token 2>/dev/null' 2>$null | Out-String).Trim()
    if (-not $token) { Start-Sleep -Seconds 3; continue }
    $headers = @('Authorization: Bearer ' + $token)
    $base = "https://127.0.0.1:$port/api/updates"
    $tmp = Join-Path $env:TEMP ('aide-update-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
      if (Test-Path -LiteralPath $pendingResult) {
        [void](Submit-AideUpdateResult)
        Start-Sleep -Seconds 3
        continue
      }
      if ($script:launcherUnsafe -or (Test-Path -LiteralPath $launcherBlocker)) {
        Write-AideUpdateDiagnostic $agentLog 'launcher-blocked' 'Confirm the owned launcher has stopped before removing launcher-stop-required.'
        Start-Sleep -Seconds 3
        continue
      }
      $commandFile = Join-Path $tmp 'command.tsv'
      & curl.exe -k -sS --connect-timeout 3 --max-time 10 -H $headers[0] "$base/agent" -o $commandFile 2>$null | Out-Null
      $line = if (Test-Path $commandFile) { (Get-Content -Raw $commandFile).Trim() } else { '' }
      if ($line) {
        $f = $line -split "`t"
        if ($f.Count -eq 9) {
          if ($f[2] -eq '-') { $f[2] = '' }
          if ($f[6] -eq '-') { $f[6] = '' }
        }
        if ($f.Count -eq 9 -and $f[0] -match '^[a-f0-9]{32}$' -and $f[1] -in @('A','B') -and ($f[2] -eq '' -or $f[2] -match '^[a-f0-9]{32}$') -and $f[4] -match '^sha256:[a-f0-9]{64}$' -and $f[5] -match '^linux/(arm64|amd64)$' -and $f[3] -match '^v[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+-RC[0-9]+$' -and $f[7] -ceq ('aide:slot-' + $f[1].ToLowerInvariant()) -and ($f[2] -eq '' -or $f[6] -match '^[a-f0-9]{64}$')) {
          $op,$target,$packageId,$releaseTag,$imageId,$imagePlatform,$imageHash,$imageRef,$version = $f[0..8]
          Send-AideUpdateProgress 'preparing' 5 'Preparing slot activation'
          $oldMarker = Get-Content -Raw '.aide-image'
          $ready = $false
          if ($packageId) {
            Send-AideUpdateProgress 'downloading' 15 'Downloading the staged update package'
            $zip = Join-Path $tmp 'package.zip'
            & curl.exe -k -f -sS --retry 2 -H $headers[0] "$base/agent/packages/$packageId" -o $zip
            if ($LASTEXITCODE -ne 0) { throw 'Update package download failed.' }
            Expand-Archive -LiteralPath $zip -DestinationPath (Join-Path $tmp 'pkg') -Force
            $archive = "aide-$releaseTag-linux-$($imagePlatform.Split('/')[-1])-image.tar.gz"
            Send-AideUpdateProgress 'verifying' 38 'Verifying image SHA256'
            $sumLine = Get-Content -LiteralPath (Join-Path $tmp 'pkg/SHA256SUMS') | Where-Object { $_ -match [regex]::Escape($archive) } | Select-Object -First 1
            if (-not $sumLine -or (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $tmp "pkg/$archive")).Hash -ne $imageHash) { throw '升级镜像 SHA256 校验失败。' }
            Send-AideUpdateProgress 'importing' 52 'Importing the verified image'
            $loaded = (& $docker image load -i (Join-Path $tmp "pkg/$archive") | Out-String).Trim()
            $ready = $LASTEXITCODE -eq 0
            if ($ready) { $imageId = Resolve-AideUpdateImage $imageId $imagePlatform $loaded }
          } else {
            $localId = (& $docker image inspect $imageRef --format '{{.Id}}' 2>$null | Out-String).Trim()
            $ready = $localId -eq $imageId
          }
          if (-not $ready) { throw '升级镜像不可用或导入失败。' }
          if ($ready) {
            & $docker image tag $imageId $imageRef | Out-Null
            if ($LASTEXITCODE -ne 0) { throw 'Cannot tag the verified target image.' }
            Send-AideUpdateProgress 'activating' 70 'Activating target slot'
            "$imageRef $imageId $imagePlatform $releaseTag" | Set-Content -NoNewline -Encoding ASCII '.aide-image'
            Invoke-AideSlotLauncher 'activation'
            $running = (& $docker compose ps --status running --quiet aide 2>$null | Out-String).Trim()
            if ($LASTEXITCODE -ne 0 -or $running -notmatch '^[a-f0-9]{12,64}$') { throw 'Target service is not running.' }
            $runningImage = (& $docker inspect $running --format '{{.Image}}' 2>$null | Out-String).Trim()
            if ($LASTEXITCODE -ne 0 -or $runningImage -cne $imageId) { throw 'Running service does not use the target image.' }
            Send-AideUpdateProgress 'health-check' 88 'Checking the target service'
            $portLine = & $docker compose port aide 8080 2>$null | Select-Object -Last 1
            if ($portLine -match ':(\d+)\s*$') { $port = $Matches[1] }
            $healthy = $false
            for ($i=0; $i -lt 45; $i++) { if ((& curl.exe -k -f -sS --max-time 3 "https://127.0.0.1:$port/healthz" 2>$null)) { $healthy = $true; break }; Start-Sleep -Seconds 1 }
            if (-not $healthy) { throw 'Target health check timed out.' }
            $token = (& $docker compose exec -T aide sh -c 'cat /data/auth/access-token 2>/dev/null || cat /data/access-token 2>/dev/null' 2>$null | Out-String).Trim()
            $base = "https://127.0.0.1:$port/api/updates"
            $canonical=$null
            if ($imageId -cne $f[4]) {
              $canonical=@{version=1;activeSlot=$target;slots=@{}}
              $canonical.slots[$target]=@{version=$version;tag=$releaseTag;imageRef=$imageRef;imageId=$imageId;platform=$imagePlatform}
            }
            Save-AideUpdateResult $op $true 'healthz passed' $canonical
            [void](Submit-AideUpdateResult)
          }
        }
      }
    } catch {
      $originalFailure=ConvertTo-AideUpdateDiagnostic $_.Exception.Message $token 90
      Write-AideUpdateDiagnostic $agentLog 'operation-failed' ($op+' '+$_.Exception.Message+' | '+$_.ScriptStackTrace) $token
      $recovery=if ($script:launcherUnsafe) { 'paused: launcher termination unconfirmed' } else { 'not attempted' }
      if ($op -and $oldMarker -and -not $script:launcherUnsafe) {
        try {
          Send-AideUpdateProgress 'rollback' 95 'Restoring the previous slot'
          Set-Content -NoNewline -Encoding ASCII '.aide-image' $oldMarker
          Invoke-AideSlotLauncher 'recovery'
          $recovery='previous launcher completed'
        } catch {
          $recovery='failed: '+(ConvertTo-AideUpdateDiagnostic $_.Exception.Message $token 90)
          Write-AideUpdateDiagnostic $agentLog 'recovery-failed' ($op+' '+$_.Exception.Message+' | '+$_.ScriptStackTrace) $token
        }
        Write-AideUpdateDiagnostic $agentLog 'recovery' ($op+' '+$recovery) $token
      }
      if ($op) {
        Save-AideUpdateResult $op $false ('Activation failed: '+$originalFailure+'; Recovery: '+$recovery)
        try {
          $portLine = & $docker compose port aide 8080 2>$null | Select-Object -Last 1
          if ($portLine -match ':(\d+)\s*$') { $port = $Matches[1] }
          $token = (& $docker compose exec -T aide sh -c 'cat /data/auth/access-token 2>/dev/null || cat /data/access-token 2>/dev/null' 2>$null | Out-String).Trim()
          $base = "https://127.0.0.1:$port/api/updates"
          [void](Submit-AideUpdateResult)
        } catch { Write-AideUpdateDiagnostic $agentLog 'result-retry' $_.Exception.Message $token }
      }
      Start-Sleep -Seconds 2
    }
    finally { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue }
    Start-Sleep -Seconds 3
  }
} finally { $owner.Dispose() }
