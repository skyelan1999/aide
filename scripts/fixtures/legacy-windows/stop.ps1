param([switch]$DryRun)
& (Join-Path $PSScriptRoot 'scripts/aide-maintenance.ps1') -Action 'stop' -DryRun:$DryRun
exit $LASTEXITCODE
