[CmdletBinding()]
param([Parameter(Mandatory)][ValidateSet('stop','clean-cache')][string]$Action, [switch]$DryRun)
$ErrorActionPreference = 'Stop'
$maintenanceRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $maintenanceRoot
$docker = (Get-Command docker -ErrorAction SilentlyContinue).Source
if (-not $docker) { throw 'Docker CLI not found.' }
& $docker info *> $null
if ($LASTEXITCODE -ne 0) { throw 'Docker engine unavailable; maintenance did not start it.' }
if ($Action -eq 'clean-cache') {
    Write-Host "Clearing unused build cache in the current Docker engine's default builder (shared by projects)."
    Write-Host 'Containers, images, volumes, settings, workspace files and offline archives are preserved.'
    if ($DryRun) { Write-Host '[dry-run] docker builder prune --all --force'; exit 0 }
    & $docker builder prune --all --force
    exit $LASTEXITCODE
}
$ids = @(foreach ($service in @('aide','cad')) {
    & $docker ps -q --filter "label=com.docker.compose.project.working_dir=$maintenanceRoot" --filter "label=com.docker.compose.service=$service"
    if ($LASTEXITCODE -ne 0) { throw 'Could not list AIDE containers.' }
}) | Sort-Object -Unique
if ($LASTEXITCODE -ne 0) { throw 'Could not list AIDE containers.' }
if ($DryRun) { Write-Host "[dry-run] stop AIDE in $maintenanceRoot; containers: $ids"; exit 0 }
New-Item -ItemType Directory -Path '.cache' -Force | Out-Null
New-Item -ItemType File -Path '.cache/aide-manual-stop' -Force | Out-Null
foreach ($id in $ids) {
    if ($id) { & $docker stop --time 30 $id; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE } }
}
Write-Host 'AIDE stopped. Data and images retained; use the normal start launcher to resume.'
exit 0
