param([Parameter(Mandatory=$true)][string]$Installer)
$ErrorActionPreference = 'Stop'
$installerPath = (Resolve-Path $Installer).Path
$install = Join-Path $env:LOCALAPPDATA 'Programs\Aide Native'
if (Test-Path $install) { throw "Unexpected pre-existing test installation: $install" }
$testRoot = Join-Path $env:TEMP ('aide-native-smoke-' + [guid]::NewGuid().ToString('N'))
$testLocal = Join-Path $testRoot 'localappdata'
$testProfile = Join-Path $testRoot 'profile'
$testBin = Join-Path $testRoot 'bin'
$dockerMarker = Join-Path $testRoot 'docker-called.txt'
New-Item -ItemType Directory -Force -Path $testLocal,$testProfile,$testBin | Out-Null
@('@echo off', 'echo unexpected Docker invocation>>"%AIDE_NATIVE_DOCKER_MARKER%"', 'exit /b 97') |
  Set-Content -Encoding Ascii (Join-Path $testBin 'docker.cmd')
$oldLocal = $env:LOCALAPPDATA
$oldProfile = $env:USERPROFILE
$oldPath = $env:PATH
try {
  $installResult = Start-Process -FilePath $installerPath -ArgumentList '/S' -Wait -PassThru
  if ($installResult.ExitCode -ne 0) { throw "NSIS installer exited with $($installResult.ExitCode)" }
  foreach ($relative in @('aide-server.exe','AideNative.ps1','AideNativeStop.ps1','Uninstall.exe',
                          'AideComputerBridge.ps1',
                          'runtime\node\node.exe','runtime\python\python.exe',
                          'runtime\git\cmd\git.exe','runtime\openssh\ssh.exe')) {
    if (-not (Test-Path (Join-Path $install $relative))) { throw "Missing installed payload: $relative" }
  }
  $env:LOCALAPPDATA = $testLocal
  $env:USERPROFILE = $testProfile
  $env:AIDE_NATIVE_DOCKER_MARKER = $dockerMarker
  $env:PATH = "$testBin;$oldPath"
  $workspace = Join-Path $testProfile 'Documents\Aide\Workspace'
  $pluginDir = Join-Path $workspace 'plugins\native-runtime-smoke-daemon'
  New-Item -ItemType Directory -Force -Path $pluginDir | Out-Null
  $fixture = Join-Path $PSScriptRoot '..\..\plugins\testdata\mock-daemon'
  Copy-Item (Join-Path $fixture 'index.js') (Join-Path $pluginDir 'index.js')
  $registry = @{ version = 1; plugins = @(@{ id='native-runtime-smoke-daemon'; name='Native Runtime Smoke Daemon'; version='0.0.1'; main='index.js'; daemon=$true; enabled=$false }) }
  $registryJson = ConvertTo-Json -InputObject $registry -Depth 8 -Compress
  [IO.File]::WriteAllText((Join-Path $workspace 'plugins\registry.json'),$registryJson,[Text.UTF8Encoding]::new($false))
  $node = Join-Path $install 'runtime\node\node.exe'
  if (-not (& $node --version).StartsWith('v24.')) { throw 'Bundled Node runtime version mismatch.' }
  $python = Join-Path $install 'runtime\python\python.exe'
  & $python -I -c 'import docx,openpyxl,pptx,ezdxf,pypdf'
  & powershell.exe -NoProfile -ExecutionPolicy Bypass -File (Join-Path $install 'AideNative.ps1')
  $bridgeScript = Join-Path $install 'AideComputerBridge.ps1'
  $parseTokens = $null; $parseErrors = $null
  [System.Management.Automation.Language.Parser]::ParseFile($bridgeScript,[ref]$parseTokens,[ref]$parseErrors) | Out-Null
  if ($parseErrors.Count -gt 0) { throw "Computer bridge PowerShell parse failed: $($parseErrors -join '; ')" }
  $data = Join-Path $testLocal 'AideNative'
  $pidFile = Join-Path $data 'service.pid'
  $portFile = Join-Path $data 'service.port'
  $tokenFile = Join-Path $data 'auth\access-token'
  $bridgeTokenFile = Join-Path $data 'auth\computer-bridge-token'
  $bridgePidFile = Join-Path $data 'computer-bridge.pid'
  if (-not (Test-Path $pidFile) -or -not (Test-Path $portFile) -or -not (Test-Path $tokenFile)) {
    throw 'Native launcher did not persist service readiness metadata.'
  }
  if (-not (Test-Path $bridgeTokenFile) -or -not (Test-Path $bridgePidFile)) { throw 'Native launcher did not start the computer bridge.' }
  $bridgePid = [int]((Get-Content -Raw $bridgePidFile).Trim())
  $servicePid = [int]((Get-Content -Raw $pidFile).Trim())
  $port = [int]((Get-Content -Raw $portFile).Trim())
  $base = "http://127.0.0.1:$port"
  $health = $null
  for ($i=0; $i -lt 30; $i++) {
    try { $health = Invoke-RestMethod -TimeoutSec 2 "$base/healthz"; break } catch { Start-Sleep -Milliseconds 500 }
  }
  if ($health.status -ne 'ok' -or $health.integrity -ne 'ok') { throw "Service health failed: $health" }
  $token = (Get-Content -Raw $tokenFile).Trim()
  $headers = @{ Authorization = "Bearer $token" }
  $config = Invoke-RestMethod -TimeoutSec 3 -Headers $headers "$base/api/config"
  if ($config.runtimeMode -ne 'native') { throw "Runtime mode was not reported as native: $($config.runtimeMode)" }
  if ((Invoke-WebRequest -UseBasicParsing -TimeoutSec 3 "$base/").StatusCode -ne 200) { throw 'Workbench home did not return HTTP 200.' }
  $python = Join-Path $install 'runtime\python\python.exe'
  $fixture = Join-Path $PSScriptRoot '..\..\plugins\testdata\mock-daemon'
  & $python -I (Join-Path $PSScriptRoot 'native-plugin-daemon-smoke.py') --base $base --token $token --workspace $workspace --fixture (Resolve-Path $fixture).Path
  if ($LASTEXITCODE -ne 0) { throw "Bundled Node plugin daemon smoke failed with exit code $LASTEXITCODE." }
  $bridgeToken = (Get-Content -Raw $bridgeTokenFile).Trim()
  $bridgeHeaders = @{ Authorization = "Bearer $bridgeToken" }
  $bridgeHealth = Invoke-RestMethod -TimeoutSec 3 -Headers $bridgeHeaders 'http://127.0.0.1:17778/healthz'
  if ($bridgeHealth.service -ne 'aide-windows-computer-bridge') { throw "Computer bridge health failed: $bridgeHealth" }
  $unauthorized = $false
  try { Invoke-RestMethod -TimeoutSec 3 'http://127.0.0.1:17778/healthz' | Out-Null } catch { $unauthorized = $_.Exception.Response.StatusCode.value__ -eq 401 }
  if (-not $unauthorized) { throw 'Computer bridge accepted a request without its bearer token.' }
  & powershell.exe -NoProfile -ExecutionPolicy Bypass -File (Join-Path $install 'AideNativeStop.ps1')
  for ($i=0; $i -lt 30; $i++) {
    if (-not (Get-Process -Id $servicePid -ErrorAction SilentlyContinue)) { break }
    Start-Sleep -Milliseconds 300
  }
  if (Get-Process -Id $servicePid -ErrorAction SilentlyContinue) { throw 'Native service did not stop.' }
  if (Get-Process -Id $bridgePid -ErrorAction SilentlyContinue) { throw 'Native computer bridge did not stop.' }
  if (Test-Path $dockerMarker) { throw 'Native package attempted to invoke Docker.' }
  $uninstall = Start-Process -FilePath (Join-Path $install 'Uninstall.exe') -ArgumentList '/S' -Wait -PassThru
  if ($uninstall.ExitCode -ne 0 -or (Test-Path $install)) { throw 'Native package uninstall failed.' }
  if (-not (Test-Path $data)) { throw 'Uninstall unexpectedly removed per-user data.' }
  Write-Output 'Native Windows package install/start/auth/browser/stop/uninstall/no-Docker smoke: PASS'
}
finally {
  $env:LOCALAPPDATA = $oldLocal
  $env:USERPROFILE = $oldProfile
  $env:PATH = $oldPath
  if (Test-Path $install) {
    $stop = Join-Path $install 'AideNativeStop.ps1'
    if (Test-Path $stop) { & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $stop }
    $uninstaller = Join-Path $install 'Uninstall.exe'
    if (Test-Path $uninstaller) { Start-Process -FilePath $uninstaller -ArgumentList '/S' -Wait }
  }
  Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue
}
