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

function Invoke-AideSlotLauncher {
  # Start-Process -Wait waits for the process tree on Windows; start.ps1 spawns
  # permanent monitors. Wait only for the launcher process, with a quoted path.
  $launcher = Join-Path $PWD 'start.ps1'
  $env:AIDE_OPEN_BROWSER = '0'
  $proc = Start-Process -FilePath $currentPowerShell -ArgumentList @('-NoProfile','-ExecutionPolicy','Bypass','-File',('"{0}"' -f $launcher)) -PassThru -WindowStyle Hidden
  if (-not $proc.WaitForExit(300000)) {
    $proc.Kill(); $proc.WaitForExit()
    throw 'Launcher process timed out.'
  }
  $proc.Refresh()
  if ($proc.ExitCode -ne 0) { throw 'Target launcher failed.' }
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
  & curl.exe -k -f -sS --max-time 10 -H $headers[0] -H 'Content-Type: application/json' -X POST --data-binary "@$file" "$base/agent/progress" 2>$null | Out-Null
}
try {
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
    $base = "https://127.0.0.1:$port/api/updates"
    $tmp = Join-Path $env:TEMP ('aide-update-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
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
            Invoke-AideSlotLauncher
            $running = (& $docker compose ps --status running --quiet aide 2>$null | Out-String).Trim()
            if ($LASTEXITCODE -ne 0 -or $running -notmatch '^[a-f0-9]{12,64}$') { throw 'Target service is not running.' }
            $runningImage = (& $docker inspect $running --format '{{.Image}}' 2>$null | Out-String).Trim()
            if ($LASTEXITCODE -ne 0 -or $runningImage -cne $imageId) { throw 'Running service does not use the target image.' }
            Send-AideUpdateProgress 'health-check' 88 'Checking the target service'
            $portLine = & $docker compose port aide 8080 2>$null | Select-Object -Last 1
            if ($portLine -match ':(\d+)\s*$') { $port = $Matches[1] }
            $healthy = $false
            for ($i=0; $i -lt 45; $i++) { if ((& curl.exe -k -f -sS --max-time 3 "https://127.0.0.1:$port/healthz" 2>$null)) { $healthy = $true; break }; Start-Sleep -Seconds 1 }
            $result = @{ operationId=$op; success=$healthy; message=$(if ($healthy) {'healthz passed'} else {'health check timed out; restored previous slot'}) } | ConvertTo-Json -Compress
            if (-not $healthy) {
              Send-AideUpdateProgress 'rollback' 95 'Restoring the previous slot'
              Set-Content -NoNewline -Encoding ASCII '.aide-image' $oldMarker
              Invoke-AideSlotLauncher
              $portLine = & $docker compose port aide 8080 2>$null | Select-Object -Last 1
              if ($portLine -match ':(\d+)\s*$') { $port = $Matches[1] }
            }
            $token = (& $docker compose exec -T aide sh -c 'cat /data/auth/access-token 2>/dev/null || cat /data/access-token 2>/dev/null' 2>$null | Out-String).Trim()
            $headers = @('Authorization: Bearer ' + $token)
            $base = "https://127.0.0.1:$port/api/updates"
            $resultFile = Join-Path $tmp 'result.json'; [System.IO.File]::WriteAllText($resultFile, $result, (New-Object System.Text.UTF8Encoding $false))
            & curl.exe -k -f -sS --max-time 10 -H $headers[0] -H 'Content-Type: application/json' -X POST --data-binary "@$resultFile" "$base/agent/result" | Out-Null
            if ($LASTEXITCODE -eq 0 -and $healthy -and $imageId -cne $f[4]) {
              # Persist Docker's canonical local ID after an OCI index import.
              $canonical = @{version=1;activeSlot=$target;slots=@{}}
              $canonical.slots[$target] = @{version=$version;tag=$releaseTag;imageRef=$imageRef;imageId=$imageId;platform=$imagePlatform}
              [IO.File]::WriteAllText($resultFile, ($canonical | ConvertTo-Json -Depth 5 -Compress), (New-Object System.Text.UTF8Encoding $false))
              & curl.exe -k -f -sS --max-time 10 -H $headers[0] -H 'Content-Type: application/json' -X POST --data-binary "@$resultFile" "$base/agent/sync" | Out-Null
            }
          }
        }
      }
    } catch {
      if ($op -and $oldMarker) {
        Send-AideUpdateProgress 'rollback' 95 'Restoring the previous slot'
        Set-Content -NoNewline -Encoding ASCII '.aide-image' $oldMarker
        Invoke-AideSlotLauncher
        $portLine = & $docker compose port aide 8080 2>$null | Select-Object -Last 1
        if ($portLine -match ':(\d+)\s*$') { $port = $Matches[1] }
        $token = (& $docker compose exec -T aide sh -c 'cat /data/auth/access-token 2>/dev/null || cat /data/access-token 2>/dev/null' 2>$null | Out-String).Trim()
        if ($token) {
          $resultFile = Join-Path $tmp 'failure.json'
          $failure = @{ operationId=$op; success=$false; message='package installation failed; restored previous slot' } | ConvertTo-Json -Compress
          [System.IO.File]::WriteAllText($resultFile, $failure, (New-Object System.Text.UTF8Encoding $false))
          & curl.exe -k -sS --max-time 10 -H ('Authorization: Bearer ' + $token) -H 'Content-Type: application/json' -X POST --data-binary "@$resultFile" "https://127.0.0.1:$port/api/updates/agent/result" | Out-Null
        }
      }
      Start-Sleep -Seconds 2
    }
    finally { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue }
    Start-Sleep -Seconds 3
  }
} finally { $owner.Dispose() }
