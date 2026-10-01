# Windows host-side A/B installer. Runs outside the aide container.
$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)
$currentPowerShell = (Get-Process -Id $PID).Path
Remove-Item Env:AIDE_PORT -ErrorAction SilentlyContinue
$env:COMPOSE_FILE = 'compose.yaml'
if (-not (Test-Path -LiteralPath '.aide-image')) { exit 0 }
$lock = Join-Path $PWD '.aide-update-agent.lock'
try { New-Item -ItemType Directory -Path $lock -ErrorAction Stop | Out-Null } catch { exit 0 }
try {
  $docker = (Get-Command docker -ErrorAction Stop).Source
  $marker = (Get-Content -Raw '.aide-image').Trim() -split '\s+'
  $portLine = & $docker compose -f compose.yaml port aide 8080 2>$null | Select-Object -Last 1
  if ($portLine -notmatch ':(\d+)\s*$') { exit 0 }
  $port = $Matches[1]
  $token = (& $docker compose -f compose.yaml exec -T aide sh -c 'cat /data/auth/access-token 2>/dev/null || cat /data/access-token 2>/dev/null' 2>$null | Out-String).Trim()
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
        if ($f.Count -ge 9 -and $f[0] -match '^[a-f0-9]{32}$' -and $f[1] -in @('A','B') -and ($f[2] -eq '' -or $f[2] -match '^[a-f0-9]{32}$') -and $f[4] -match '^sha256:[a-f0-9]{64}$' -and $f[5] -match '^linux/(arm64|amd64)$') {
          $op,$target,$packageId,$releaseTag,$imageId,$imagePlatform,$imageHash,$imageRef,$version = $f[0..8]
          $oldMarker = Get-Content -Raw '.aide-image'
          $ready = $false
          if ($packageId) {
            $zip = Join-Path $tmp 'package.zip'
            & curl.exe -k -f -sS --retry 2 -H $headers[0] "$base/agent/packages/$packageId" -o $zip
            Expand-Archive -LiteralPath $zip -DestinationPath (Join-Path $tmp 'pkg') -Force
            $archive = "aide-$releaseTag-linux-$($imagePlatform.Split('/')[-1])-image.tar.gz"
            $sumLine = Get-Content -LiteralPath (Join-Path $tmp 'pkg/SHA256SUMS') | Where-Object { $_ -match [regex]::Escape($archive) } | Select-Object -First 1
            if (-not $sumLine -or (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $tmp "pkg/$archive")).Hash -ne $imageHash) { throw '升级镜像 SHA256 校验失败。' }
            & $docker image load -i (Join-Path $tmp "pkg/$archive") | Out-Null
            $ready = $LASTEXITCODE -eq 0
          } else {
            $localId = (& $docker image inspect $imageRef --format '{{.Id}}' 2>$null | Out-String).Trim()
            $ready = $localId -eq $imageId
          }
          if (-not $ready) { throw '升级镜像不可用或导入失败。' }
          if ($ready) {
            & $docker image tag $imageId $imageRef | Out-Null
            "$imageRef $imageId $imagePlatform $releaseTag" | Set-Content -NoNewline -Encoding ASCII '.aide-image'
            $env:AIDE_OPEN_BROWSER = '0'
            $proc = Start-Process -FilePath $currentPowerShell -ArgumentList @('-NoProfile','-ExecutionPolicy','Bypass','-File',(Join-Path $PWD 'start.ps1')) -PassThru -Wait -WindowStyle Hidden
            $portLine = & $docker compose port aide 8080 2>$null | Select-Object -Last 1
            if ($portLine -match ':(\d+)\s*$') { $port = $Matches[1] }
            $healthy = $false
            for ($i=0; $i -lt 45; $i++) { if ((& curl.exe -k -f -sS --max-time 3 "https://127.0.0.1:$port/healthz" 2>$null) -and $proc.ExitCode -eq 0) { $healthy = $true; break }; Start-Sleep -Seconds 1 }
            $result = @{ operationId=$op; success=$healthy; message=$(if ($healthy) {'healthz passed'} else {'health check timed out; restored previous slot'}) } | ConvertTo-Json -Compress
            if (-not $healthy) {
              Set-Content -NoNewline -Encoding ASCII '.aide-image' $oldMarker
              Start-Process -FilePath $currentPowerShell -ArgumentList @('-NoProfile','-ExecutionPolicy','Bypass','-File',(Join-Path $PWD 'start.ps1')) -Wait -WindowStyle Hidden
              $portLine = & $docker compose port aide 8080 2>$null | Select-Object -Last 1
              if ($portLine -match ':(\d+)\s*$') { $port = $Matches[1] }
            }
            $token = (& $docker compose exec -T aide sh -c 'cat /data/auth/access-token 2>/dev/null || cat /data/access-token 2>/dev/null' 2>$null | Out-String).Trim()
            $headers = @('Authorization: Bearer ' + $token)
            $base = "https://127.0.0.1:$port/api/updates"
            $resultFile = Join-Path $tmp 'result.json'; [System.IO.File]::WriteAllText($resultFile, $result, (New-Object System.Text.UTF8Encoding $false))
            & curl.exe -k -sS --max-time 10 -H $headers[0] -H 'Content-Type: application/json' -X POST --data-binary "@$resultFile" "$base/agent/result" | Out-Null
          }
        }
      }
    } catch {
      if ($op -and $oldMarker) {
        Set-Content -NoNewline -Encoding ASCII '.aide-image' $oldMarker
        Start-Process -FilePath $currentPowerShell -ArgumentList @('-NoProfile','-ExecutionPolicy','Bypass','-File',(Join-Path $PWD 'start.ps1')) -Wait -WindowStyle Hidden
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
} finally { Remove-Item -LiteralPath $lock -Force -ErrorAction SilentlyContinue }
