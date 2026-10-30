param([switch]$DryRun)
& (Join-Path $PSScriptRoot 'scripts/aide-maintenance.ps1') -Action 'clean-cache' -DryRun:$DryRun
exit $LASTEXITCODE
