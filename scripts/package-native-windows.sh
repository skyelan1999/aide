#!/usr/bin/env bash
# Build an experimental per-user Windows installer with bundled Node, Python and Aide.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
OUT="${AIDE_NATIVE_PACKAGE_OUT:-$ROOT/dist/native-windows}"
[[ ! -e "$OUT/Aide-Native-Windows-x64-Setup.exe" ]] || { echo 'Installer output already exists; choose another AIDE_NATIVE_PACKAGE_OUT.' >&2; exit 1; }
mkdir -p "$OUT"
SERVER="${AIDE_NATIVE_SERVER_BINARY:-$ROOT/build/native/windows/aide-server.exe}"
if [[ ! -f "$SERVER" ]]; then
  command -v docker >/dev/null || { echo 'Set AIDE_NATIVE_SERVER_BINARY or install Docker to cross-build the Windows service.' >&2; exit 1; }
  mkdir -p "$ROOT/build/native/windows"
  docker run --rm -v "$ROOT:/work" -w /work "${AIDE_GO_BUILDER_IMAGE:-golang:1.26-bookworm}" \
    bash -c 'GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags="-s -w -H=windowsgui" -o build/native/windows/aide-server.exe ./cmd/aide'
  SERVER="$ROOT/build/native/windows/aide-server.exe"
fi
[[ "$(file -b "$SERVER")" == *PE32+* ]] || { echo 'Windows service must be an x64 PE executable.' >&2; exit 1; }
PYTHON_RUNTIME="${AIDE_WINDOWS_PYTHON_RUNTIME:-$ROOT/build/native/windows/python}"
if [[ ! -f "$PYTHON_RUNTIME/python.exe" ]]; then
  AIDE_NATIVE_PYTHON_OUT="$PYTHON_RUNTIME" scripts/build-native-python-windows.sh
fi
"${PYTHON_RUNTIME}/python.exe" --version >/dev/null 2>&1 && { echo 'Cross-host Windows Python execution unexpectedly succeeded; use a Windows host to validate it.' >&2; }
NODE_RUNTIME="${AIDE_WINDOWS_NODE_RUNTIME:-$ROOT/build/native/windows/node}"
if [[ ! -f "$NODE_RUNTIME/node.exe" ]]; then
  VERSION=24.19.0
  SHA256=57f71ab3652e797d84acddc79c81cc9ff1c6ddb2a1974cdb83f00fee9bff4c73
  WORK="$(mktemp -d "${TMPDIR:-/tmp}/aide-native-node-windows.XXXXXX")"
  trap 'python3 -c "import shutil,sys; shutil.rmtree(sys.argv[1],ignore_errors=True)" "$WORK"' EXIT
  curl -fL --retry 2 --connect-timeout 15 --max-time 240 "https://nodejs.org/dist/v${VERSION}/node-v${VERSION}-win-x64.zip" -o "$WORK/node.zip"
  printf '%s  %s\n' "$SHA256" "$WORK/node.zip" | shasum -a 256 -c -
  mkdir "$WORK/unpack"
  unzip -q "$WORK/node.zip" -d "$WORK/unpack"
  mkdir -p "$(dirname "$NODE_RUNTIME")"
  cp -a "$WORK/unpack/node-v${VERSION}-win-x64/." "$WORK/runtime/"
  rm -rf "$NODE_RUNTIME"
  mv "$WORK/runtime" "$NODE_RUNTIME"
  rm -rf "$WORK"
  trap - EXIT
fi
[[ -f "$NODE_RUNTIME/node.exe" && -f "$NODE_RUNTIME/node_modules/npm/bin/npm-cli.js" ]] || { echo 'Bundled Node runtime is incomplete.' >&2; exit 1; }
"$NODE_RUNTIME/node.exe" --version >/dev/null 2>&1 && { echo 'Cross-host Windows Node execution unexpectedly succeeded; use a Windows host to validate it.' >&2; }
if ! command -v makensis >/dev/null; then
  command -v docker >/dev/null || { echo 'NSIS is required to create the Windows installer.' >&2; exit 1; }
fi
STAGE="$(mktemp -d "${TMPDIR:-/tmp}/aide-native-windows.XXXXXX")"
trap 'python3 -c "import shutil,sys; shutil.rmtree(sys.argv[1],ignore_errors=True)" "$STAGE"' EXIT
DEPS="$STAGE/dependencies"
mkdir -p "$DEPS"
MINGIT_VERSION=2.56.0.4
MINGIT_SHA256=e3bf78351fe777fba47ddee5950082f8a98bb0f02604fdc7ba3ee2cb68ffa877
MINGIT_ZIP="${AIDE_WINDOWS_MINGIT_ZIP:-$DEPS/MinGit.zip}"
if [[ ! -f "$MINGIT_ZIP" ]]; then
  curl -fL --retry 2 --connect-timeout 15 --max-time 240 "https://github.com/git-for-windows/git/releases/download/v2.56.0.windows.4/MinGit-${MINGIT_VERSION}-64-bit.zip" -o "$MINGIT_ZIP"
fi
printf '%s  %s\n' "$MINGIT_SHA256" "$MINGIT_ZIP" | shasum -a 256 -c -
OPENSSH_VERSION=10.4.0.0p1-Preview
OPENSSH_SHA256=b67ff8becd203502190ec42ac7787c24c2cd3325ed7700485fe7a1c83d2a7957
OPENSSH_ZIP="${AIDE_WINDOWS_OPENSSH_ZIP:-$DEPS/OpenSSH-Win64.zip}"
if [[ ! -f "$OPENSSH_ZIP" ]]; then
  curl -fL --retry 2 --connect-timeout 15 --max-time 240 "https://github.com/PowerShell/Win32-OpenSSH/releases/download/${OPENSSH_VERSION}/OpenSSH-Win64.zip" -o "$OPENSSH_ZIP"
fi
printf '%s  %s\n' "$OPENSSH_SHA256" "$OPENSSH_ZIP" | shasum -a 256 -c -
mkdir "$DEPS/mingit" "$DEPS/openssh"
unzip -q "$MINGIT_ZIP" -d "$DEPS/mingit"
unzip -q "$OPENSSH_ZIP" -d "$DEPS/openssh"
[[ -f "$DEPS/mingit/cmd/git.exe" && -f "$DEPS/mingit/usr/bin/ssh.exe" ]] || { echo 'MinGit archive is missing git.exe or ssh.exe.' >&2; exit 1; }
[[ -f "$DEPS/openssh/OpenSSH-Win64/ssh-keygen.exe" && -f "$DEPS/openssh/OpenSSH-Win64/libcrypto.dll" ]] || { echo 'OpenSSH archive is missing required SSH client files.' >&2; exit 1; }
PAYLOAD="$STAGE/payload"
mkdir -p "$PAYLOAD/runtime/python" "$PAYLOAD/runtime/node" "$PAYLOAD/runtime/git" "$PAYLOAD/runtime/openssh" "$PAYLOAD/office-scripts"
install -m 0755 "$SERVER" "$PAYLOAD/aide-server.exe"
cp -a "$PYTHON_RUNTIME/." "$PAYLOAD/runtime/python/"
cp "$PAYLOAD/runtime/python/python.exe" "$PAYLOAD/runtime/python/python3.exe"
cp -a "$NODE_RUNTIME/." "$PAYLOAD/runtime/node/"
cp -a "$DEPS/mingit/." "$PAYLOAD/runtime/git/"
cp -a "$DEPS/openssh/OpenSSH-Win64/." "$PAYLOAD/runtime/openssh/"
find "$PAYLOAD/runtime/python" -type f -name '*.pdb' -delete
cp -a scripts/office/. "$PAYLOAD/office-scripts/"
cat > "$PAYLOAD/AideNative.ps1" <<'PS1'
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$install = Split-Path -Parent $MyInvocation.MyCommand.Path
$data = Join-Path $env:LOCALAPPDATA 'AideNative'
$workspace = Join-Path $env:USERPROFILE 'Documents\Aide\Workspace'
$context = Join-Path $env:USERPROFILE 'Documents\Aide\References'
$null = New-Item -ItemType Directory -Force -Path $data,$workspace,$context,(Join-Path $data 'logs')
$env:AIDE_DATA = $data
$env:AIDE_WORKSPACE = $workspace
$env:AIDE_CONTEXT = $context
$env:AIDE_HOST_LOCAL = $env:USERPROFILE
$env:AIDE_RUNTIME_MODE = 'native'
$env:AIDE_NATIVE_RUNTIME_ROOT = Join-Path $install 'runtime'
$env:AIDE_NATIVE_LOCAL_HTTP = '1'
$env:AIDE_LAN_ADDR = '127.0.0.1:0'
$env:AIDE_OFFICE_SCRIPTS = Join-Path $install 'office-scripts'
$env:PATH = (Join-Path $install 'runtime\node') + ';' + (Join-Path $install 'runtime\python') + ';' + (Join-Path $install 'runtime\openssh') + ';' + (Join-Path $install 'runtime\git\cmd') + ';' + (Join-Path $install 'runtime\git\usr\bin') + ';' + $env:PATH
$env:GIT_SSH = Join-Path $install 'runtime\openssh\ssh.exe'
$env:PYTHONPATH = Join-Path $install 'runtime\python\Lib\site-packages'
$tokenFile = Join-Path $data 'auth\access-token'
$token = if (Test-Path $tokenFile) { (Get-Content -Raw $tokenFile).Trim() } else { '' }
$healthy = $false
$pidFile = Join-Path $data 'service.pid'
$portFile = Join-Path $data 'service.port'
$existing = $null
if ((Test-Path $pidFile) -and (Test-Path $portFile)) {
  $rawPid = (Get-Content -Raw $pidFile).Trim()
  $rawPort = (Get-Content -Raw $portFile).Trim()
  if (($rawPid -match '^\d+$') -and ($rawPort -match '^\d{1,5}$')) {
    $candidate = Get-Process -Id ([int]$rawPid) -ErrorAction SilentlyContinue
    if ($candidate -and $candidate.Path -and ($candidate.Path -ieq (Join-Path $install 'aide-server.exe'))) {
      $existing = [int]$rawPort
    }
  }
}
if ($existing -and ($token -match '^[A-Za-z0-9_.-]{16,512}$')) {
  try {
    $base = "http://127.0.0.1:$existing"
    $null = Invoke-RestMethod -TimeoutSec 2 "$base/healthz"
    $headers = @{ Authorization = "Bearer $token" }
    $null = Invoke-RestMethod -TimeoutSec 2 -Headers $headers "$base/api/config"
    $healthy = $true
  } catch { }
}
if (-not $healthy) {
  $preferred = 18097
  $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback,$preferred)
  try { $listener.Start(); $listener.Stop(); $port = $preferred }
  catch {
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback,0)
    $listener.Start(); $port = ([System.Net.IPEndPoint]$listener.LocalEndpoint).Port; $listener.Stop()
  }
  $base = "http://127.0.0.1:$port"
  $env:AIDE_ADDR = "127.0.0.1:$port"
  $log = Join-Path $data 'logs\service.log'
  $stdout = Join-Path $data 'logs\service.stdout.log'
  $proc = Start-Process -FilePath (Join-Path $install 'aide-server.exe') -WorkingDirectory $workspace -WindowStyle Hidden -PassThru -RedirectStandardOutput $stdout -RedirectStandardError $log
  Set-Content -Path (Join-Path $data 'service.pid') -Value $proc.Id -NoNewline
  Set-Content -Path $portFile -Value $port -NoNewline
  for ($i=0; $i -lt 60; $i++) {
    Start-Sleep -Milliseconds 500
    if ($proc.HasExited) { throw "Aide service exited ($($proc.ExitCode)). Check $log" }
    try {
      $health = Invoke-RestMethod -TimeoutSec 2 "$base/healthz"
      if ($health.status -eq 'ok') { $healthy = $true; break }
    } catch { }
  }
  if (-not $healthy) { throw "Aide service health check timed out. Check $log" }
  if (Test-Path $tokenFile) { $token = (Get-Content -Raw $tokenFile).Trim() }
}
if ($token -notmatch '^[A-Za-z0-9_.-]{16,512}$') { throw 'Aide local access token is missing or invalid.' }
Start-Process "$base/#token=$token"
PS1
cat > "$PAYLOAD/AideNativeStop.ps1" <<'PS1'
$ErrorActionPreference = 'Stop'
$install = Split-Path -Parent $MyInvocation.MyCommand.Path
$data = Join-Path $env:LOCALAPPDATA 'AideNative'
$pidFile = Join-Path $data 'service.pid'
if (Test-Path $pidFile) {
  $raw = (Get-Content -Raw $pidFile).Trim()
  if ($raw -match '^\d+$') {
    $p = Get-Process -Id ([int]$raw) -ErrorAction SilentlyContinue
    if ($p -and $p.Path -and ($p.Path -ieq (Join-Path $install 'aide-server.exe'))) {
      Stop-Process -Id $p.Id
      Wait-Process -Id $p.Id -Timeout 10 -ErrorAction SilentlyContinue
    }
  }
  Remove-Item $pidFile -Force
}
Remove-Item (Join-Path $data 'service.port') -Force -ErrorAction SilentlyContinue
PS1
cat > "$STAGE/installer.nsi" <<'NSI'
Unicode true
!include "MUI2.nsh"
!ifndef PAYLOAD_DIR
  !error "PAYLOAD_DIR is required"
!endif
Name "Aide Native (Experimental)"
OutFile "${INSTALLER_OUT}"
InstallDir "$LOCALAPPDATA\Programs\Aide Native"
InstallDirRegKey HKCU "Software\Aide\Native" "InstallDir"
RequestExecutionLevel user
SetCompressor /SOLID lzma
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"
Function .onInit
  ReadRegStr $0 HKCU "Software\Aide\Native" "InstallDir"
  IfFileExists "$0\AideNativeStop.ps1" 0 done
  ExecWait '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "$0\AideNativeStop.ps1"'
done:
FunctionEnd
Section "Aide Native"
  SetShellVarContext current
  SetOutPath "$INSTDIR"
  File /r "${PAYLOAD_DIR}\*.*"
  WriteRegStr HKCU "Software\Aide\Native" "InstallDir" "$INSTDIR"
  CreateDirectory "$SMPROGRAMS\Aide Native"
  CreateShortcut "$SMPROGRAMS\Aide Native\Aide.lnk" "$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" '-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "$INSTDIR\AideNative.ps1"'
  CreateShortcut "$SMPROGRAMS\Aide Native\停止本机服务.lnk" "$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" '-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "$INSTDIR\AideNativeStop.ps1"'
  CreateShortcut "$DESKTOP\Aide Native.lnk" "$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" '-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "$INSTDIR\AideNative.ps1"'
  WriteUninstaller "$INSTDIR\Uninstall.exe"
SectionEnd
Section "Uninstall"
  SetShellVarContext current
  ExecWait '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "$INSTDIR\AideNativeStop.ps1"'
  Delete "$DESKTOP\Aide Native.lnk"
  Delete "$SMPROGRAMS\Aide Native\Aide.lnk"
  Delete "$SMPROGRAMS\Aide Native\停止本机服务.lnk"
  RMDir "$SMPROGRAMS\Aide Native"
  DeleteRegKey HKCU "Software\Aide\Native"
  RMDir /r "$INSTDIR"
SectionEnd
NSI
if command -v makensis >/dev/null; then
  makensis -V2 -DPAYLOAD_DIR="$PAYLOAD" -DINSTALLER_OUT="$OUT/Aide-Native-Windows-x64-Setup.exe" "$STAGE/installer.nsi"
else
  docker run --rm -v "$STAGE:/work" -v "$OUT:/out" "${AIDE_NSIS_IMAGE:-debian:bookworm-slim}" bash -c 'apt-get update -qq && apt-get install -y -qq nsis >/dev/null && makensis -V2 -DPAYLOAD_DIR=/work/payload -DINSTALLER_OUT=/out/Aide-Native-Windows-x64-Setup.exe /work/installer.nsi'
fi
printf 'Installer: %s/Aide-Native-Windows-x64-Setup.exe\n' "$OUT"
