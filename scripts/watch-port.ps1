# Windows host-side monitor for the offline launcher. Applies settings-page port
# changes by updating the host mapping and recreating only the container; /data
# and workspace mounts remain managed by the existing Compose project.
$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)
Remove-Item Env:AIDE_PORT -ErrorAction SilentlyContinue
$env:COMPOSE_FILE = 'compose.yaml'
$dockerCmd = Get-Command docker -ErrorAction SilentlyContinue
if (-not $dockerCmd) { exit 0 }
$docker = $dockerCmd.Source
$lock = Join-Path $PWD '.aide-port-watcher.lock'
try { New-Item -ItemType Directory -Path $lock -ErrorAction Stop | Out-Null } catch { exit 0 }

function Get-ComposeText {
    param([string[]]$Arguments)
    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $output = & $docker @Arguments 2>$null
        $code = $LASTEXITCODE
        return @{ Code = $code; Text = (($output | Out-String).Trim()) }
    } finally {
        $ErrorActionPreference = $previousPreference
    }
}
function Test-LocalHostPortAvailable([int]$Candidate) {
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, $Candidate)
    try { $listener.Start(); return $true }
    catch { return $false }
    finally { if ($listener.Server.IsBound) { $listener.Stop() } }
}
function Set-EnvHostPort([string]$Value) {
    $envPath = Join-Path $PWD '.env'
    $text = if (Test-Path -LiteralPath $envPath) { [System.IO.File]::ReadAllText($envPath) } else { '' }
    if ($text -match '(?m)^\s*AIDE_PORT\s*=') { $text = [regex]::Replace($text, '(?m)^\s*AIDE_PORT\s*=.*$', "AIDE_PORT=$Value") }
    else { $text = $text.TrimEnd() + "`r`nAIDE_PORT=$Value`r`n" }
    [System.IO.File]::WriteAllText($envPath, $text, (New-Object System.Text.UTF8Encoding $false))
}

try {
    while ($true) {
        $services = Get-ComposeText @('compose', '-f', 'compose.yaml', 'ps', '--status', 'running', '--services')
        if ($services.Code -ne 0 -or ($services.Text -split '\r?\n') -notcontains 'aide') { break }

        $desired = (Get-ComposeText @('compose', '-f', 'compose.yaml', 'exec', '-T', 'aide', 'sh', '-c', 'cat /data/config/host-port 2>/dev/null || true')).Text
        if ($desired -match '^\d{1,5}$' -and [int]$desired -ge 1 -and [int]$desired -le 65535) {
            $mapped = (Get-ComposeText @('compose', '-f', 'compose.yaml', 'port', 'aide', '8080')).Text
            $current = if ($mapped -match ':(\d+)\s*$') { $Matches[1] } else { '' }
            if ($current -and $desired -ne $current -and (Test-LocalHostPortAvailable ([int]$desired))) {
                $envPath = Join-Path $PWD '.env'
                $oldText = if (Test-Path -LiteralPath $envPath) { [System.IO.File]::ReadAllText($envPath) } else { '' }
                $oldProcessPort = $env:AIDE_PORT
                try {
                    Set-EnvHostPort $desired
                    $env:AIDE_PORT = $desired
                    $restart = Get-ComposeText @('compose', '-f', 'compose.yaml', 'up', '-d', '--no-build', '--pull', 'never')
                    if ($restart.Code -ne 0) { throw 'Compose did not accept the requested port mapping.' }
                } catch {
                    [System.IO.File]::WriteAllText($envPath, $oldText, (New-Object System.Text.UTF8Encoding $false))
                    if ($null -eq $oldProcessPort) { Remove-Item Env:AIDE_PORT -ErrorAction SilentlyContinue } else { $env:AIDE_PORT = $oldProcessPort }
                    Get-ComposeText @('compose', '-f', 'compose.yaml', 'up', '-d', '--no-build', '--pull', 'never') | Out-Null
                }
            }
        }
        Start-Sleep -Seconds 2
    }
} finally {
    Remove-Item -LiteralPath $lock -Force -ErrorAction SilentlyContinue
}
