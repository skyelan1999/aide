# Read-only checks. Docker Desktop / WSL global settings remain user-controlled.
function Test-AideDiskBudget {
    param([string]$Root, [switch]$Build)
    $minimum = if ($Build) { 20GB } else { 10GB }
    $paths = @($Root, $env:SystemRoot, $env:LOCALAPPDATA) | Where-Object { $_ } | Select-Object -Unique
    foreach ($path in $paths) {
        $driveRoot = [IO.Path]::GetPathRoot([IO.Path]::GetFullPath($path))
        if ($driveRoot.StartsWith('\\')) { throw 'AIDE: network share disk budget cannot be verified; use a local project path.' }
        $drive = New-Object System.IO.DriveInfo($driveRoot)
        if (-not $drive.IsReady -or $drive.AvailableFreeSpace -lt $minimum) {
            throw "AIDE: insufficient disk space at $driveRoot; need $($minimum / 1GB) GiB. Free rebuildable cache and retry. User data was not deleted."
        }
    }
}
function Test-AideEngineBudget {
    param([string]$Docker)
    $report = Invoke-Native $Docker @('info', '--format', '{{.MemTotal}} {{.NCPU}}') -Capture
    if ($report.Code -ne 0 -or $report.Text -notmatch '^(\d+) (\d+)$') { throw 'AIDE: cannot read Docker resource budget.' }
    $memory = [long]$Matches[1]; $cpuCount = [int]$Matches[2]
    if ($memory -lt 4GB) { throw 'AIDE: Docker requires at least 4 GiB; 6 GiB is recommended with native CAD.' }
    Write-Host "Docker budget: $([math]::Round($memory / 1MB)) MiB / $cpuCount CPUs; AIDE caps: app 2 GiB + CAD 4 GiB, 2 CPUs each; logs 3 x 10 MiB per service."
    if ($memory -gt 8GB -or $cpuCount -gt 4) { Write-Warning 'Docker global allocation is high; review Docker Desktop / WSL resources and disk capacity. Global settings were not changed.' }
}
